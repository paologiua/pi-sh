// Package connectsession shares native login, signaling and a single-use broker
// connection. It uses the existing official Connect shell channel exclusively.
package connectsession

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"
	"pi-sh/internal/browserauth"
	"pi-sh/internal/commandrun"
	"pi-sh/internal/connpolicy"
	"pi-sh/internal/diag"
	"pi-sh/internal/loginprompt"
	"pi-sh/internal/shellbroker"
	"pi-sh/internal/transport"
	"pi-sh/internal/webauth"
)

// Phase messages are fixed, secret-free text. Device names/IDs, URLs, ICE
// servers, SDP and credentials are never logged; only counts and enum states.
const (
	MsgLoginStart     diag.Message = "authentication: interactive direct login started"
	MsgLoginStage     diag.Message = "authentication: login stage"
	MsgLoginDone      diag.Message = "authentication: direct login completed"
	MsgDiscovery      diag.Message = "discovery: resolving destination device"
	MsgDiscoveryDone  diag.Message = "discovery: destination device prepared"
	MsgAuthenticated  diag.Message = "Authenticated to Raspberry Pi Connect"
	MsgSignedIn       diag.Message = "signed in directly; session is held in memory for this process"
	MsgBridgeReady    diag.Message = "authentication: loopback companion bridge listening"
	MsgBrowserNotice  diag.Message = "experimental browser authentication; no account credentials leave Chrome"
	MsgConfiguration  diag.Message = "signaling: requesting session configuration"
	MsgICEServers     diag.Message = "signaling: session configuration received"
	MsgPeer           diag.Message = "transport: creating WebRTC peer and shell data channel"
	MsgOffer          diag.Message = "signaling: sending local offer"
	MsgAnswer         diag.Message = "signaling: remote answer received"
	MsgChannelOpen    diag.Message = "transport: shell data channel open"
	MsgAttemptFailed  diag.Message = "connection: establishment attempt failed before any command; retrying"
	MsgBrokerStart    diag.Message = "broker: starting shell-only broker"
	MsgBrokerFinished diag.Message = "broker: finished"
)

type loggedLogin struct {
	loginprompt.Authenticator
	log *diag.Logger
}

func (l loggedLogin) Begin(ctx context.Context) (webauth.Stage, error) {
	stage, err := l.Authenticator.Begin(ctx)
	l.stage(stage, err)
	return stage, err
}

func (l loggedLogin) Password(ctx context.Context, email string, password []byte) (webauth.Stage, error) {
	stage, err := l.Authenticator.Password(ctx, email, password)
	l.stage(stage, err)
	return stage, err
}

func (l loggedLogin) Code(ctx context.Context, code []byte) (webauth.Stage, error) {
	stage, err := l.Authenticator.Code(ctx, code)
	l.stage(stage, err)
	return stage, err
}

// Stage values are fixed webauth constants; State still redacts any other text.
func (l loggedLogin) stage(stage webauth.Stage, err error) {
	if err == nil {
		l.log.Debug(MsgLoginStage, diag.State("stage", string(stage)))
	}
}

type Source interface {
	Configuration(context.Context) (browserauth.Configuration, error)
	Exchange(context.Context, webrtc.SessionDescription) (webrtc.SessionDescription, error)
	Failures() <-chan string
	Close() error
}

// operatorPaced sources wait for a person before their configuration exists
// (the browser companion). That wait is sign-in, not establishment, so it
// happens before the ConnectTimeout-bounded attempts start.
type operatorPaced interface {
	awaitOperator(context.Context) error
}

// Built-in phase limits. ConnectTimeout can only shorten an attempt.
const (
	loginLimit       = 10 * time.Minute
	prepareLimit     = 45 * time.Second
	establishLimit   = 2 * time.Minute
	browserWaitLimit = 2 * time.Minute
)

// retryPause separates connection attempts, as OpenSSH sleeps one second.
var retryPause = time.Second

// ErrConnectTimeout reports that one establishment attempt (device
// preparation, signaling, ICE/DTLS and data-channel open) exceeded
// ConnectTimeout or a built-in limit before any remote command was sent.
var ErrConnectTimeout = errors.New("Connect connection establishment timed out before any remote command was sent")

// directSource holds one memory-only web session. Every establishment attempt
// prepares a fresh Connect connection, because signaling is single-use.
type directSource struct {
	client      *webauth.Client
	lifetime    context.Context
	destination string
	log         *diag.Logger
	mu          sync.Mutex
	current     *webauth.Connection
}

func (s *directSource) Configuration(ctx context.Context) (browserauth.Configuration, error) {
	s.mu.Lock()
	old := s.current
	s.current = nil
	s.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
	preparing, cancel := context.WithTimeout(ctx, prepareLimit)
	defer cancel()
	s.log.Debug(MsgDiscovery)
	started := time.Now()
	connection, err := s.client.PrepareWithContext(s.lifetime, preparing, s.destination)
	if err != nil {
		return browserauth.Configuration{}, err
	}
	s.log.Debug(MsgDiscoveryDone, diag.Elapsed("elapsed", time.Since(started)), diag.Count("ice_servers", len(connection.ICE.ICEServers)))
	s.mu.Lock()
	s.current = connection
	s.mu.Unlock()
	var c browserauth.Configuration
	c.Device.ID, c.Device.Name, c.ICE = connection.Device.ID, connection.Device.Name, connection.ICE
	return c, nil
}

func (s *directSource) connection() *webauth.Connection {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.current
}

func (s *directSource) Exchange(ctx context.Context, offer webrtc.SessionDescription) (webrtc.SessionDescription, error) {
	c := s.connection()
	if c == nil {
		return webrtc.SessionDescription{}, errors.New("Connect device preparation must precede signaling")
	}
	return c.Exchange(ctx, offer)
}

func (s *directSource) Failures() <-chan string {
	if c := s.connection(); c != nil {
		return c.Failures()
	}
	return nil
}

func (s *directSource) Close() error {
	if c := s.connection(); c != nil {
		return c.Close()
	}
	return nil
}

// browserSource waits once, outside ConnectTimeout, for the configuration the
// companion posts after the person runs it, then reuses it.
type browserSource struct {
	*browserauth.Bridge
	mu            sync.Mutex
	done          bool
	configuration browserauth.Configuration
	err           error
}

func (s *browserSource) awaitOperator(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.done {
		waiting, cancel := context.WithTimeout(ctx, browserWaitLimit)
		defer cancel()
		s.configuration, s.err = s.Bridge.Configuration(waiting)
		s.done = s.err == nil
	}
	return s.err
}

func (s *browserSource) Configuration(ctx context.Context) (browserauth.Configuration, error) {
	if err := s.awaitOperator(ctx); err != nil {
		return browserauth.Configuration{}, err
	}
	return s.configuration, nil
}

// boundedLogin limits each sign-in HTTPS step by ConnectTimeout. Time a
// person spends typing between steps is not part of any step.
type boundedLogin struct {
	client loginprompt.Authenticator
	limit  time.Duration
}

func (b boundedLogin) bound(ctx context.Context, step func(context.Context) (webauth.Stage, error)) (webauth.Stage, error) {
	if b.limit <= 0 {
		return step(ctx)
	}
	bounded, cancel := context.WithTimeout(ctx, b.limit)
	defer cancel()
	stage, err := step(bounded)
	if err != nil && ctx.Err() == nil && bounded.Err() != nil {
		return stage, errors.New("Connect sign-in request exceeded ConnectTimeout; no automatic retry")
	}
	return stage, err
}

func (b boundedLogin) Begin(ctx context.Context) (webauth.Stage, error) {
	return b.bound(ctx, b.client.Begin)
}

func (b boundedLogin) Password(ctx context.Context, email string, password []byte) (webauth.Stage, error) {
	return b.bound(ctx, func(ctx context.Context) (webauth.Stage, error) { return b.client.Password(ctx, email, password) })
}

func (b boundedLogin) Code(ctx context.Context, code []byte) (webauth.Stage, error) {
	return b.bound(ctx, func(ctx context.Context) (webauth.Stage, error) { return b.client.Code(ctx, code) })
}

// Authenticate does not persist credentials. The caller owns the returned source.
// notices is a local operator stream; browser mode prints a temporary capability.
func Authenticate(ctx context.Context, destination string, direct bool, notices io.Writer, program string) (Source, error) {
	return AuthenticateWithPolicy(ctx, destination, direct, connpolicy.Default(), notices, program)
}

// AuthenticateWithPolicy applies BatchMode/NumberOfPasswordPrompts before any
// prompt, loopback listener or network request, and bounds sign-in HTTPS
// steps by ConnectTimeout. Device discovery happens in each establishment
// attempt so that ConnectionAttempts can repeat it.
func AuthenticateWithPolicy(ctx context.Context, destination string, direct bool, policy connpolicy.Policy, notices io.Writer, program string) (Source, error) {
	if err := policy.Admit(direct); err != nil {
		return nil, err
	}
	log := diag.From(ctx)
	if log == nil {
		log = diag.Default(notices, program)
	}
	if direct {
		client := webauth.New()
		loginCtx, stop := context.WithTimeout(ctx, loginLimit)
		log.Debug(MsgLoginStart)
		err := loginprompt.Authenticate(loginCtx, loggedLogin{boundedLogin{client, policy.ConnectTimeout()}, log})
		stop()
		if err != nil {
			return nil, err
		}
		log.Debug(MsgLoginDone)
		log.Verbose(MsgAuthenticated, diag.State("method", "direct-login"))
		log.Info(MsgSignedIn)
		return &directSource{client: client, lifetime: ctx, destination: destination, log: log}, nil
	}
	bridge, err := browserauth.New(ctx, destination)
	if err != nil {
		return nil, err
	}
	log.Debug(MsgBridgeReady)
	url, _ := json.Marshal(bridge.ScriptURL())
	log.Info(MsgBrowserNotice)
	// The companion step is an instruction the user must act on, carrying a
	// temporary loopback capability. It always goes to the notices stream
	// (never -q-suppressed, never to an -E log file or syslog).
	fmt.Fprintln(notices, "On your authenticated Connect Devices page, run in the browser console:")
	fmt.Fprintf(notices, "fetch(%s).then(r=>r.text()).then(s=>(0,eval)(s))\n", url)
	return &browserSource{Bridge: bridge}, nil
}

// Establish opens the Connect shell data channel. Each attempt prepares the
// device, signals, completes ICE/DTLS and waits for the channel to open; only
// the empty readiness message is sent. ConnectionAttempts repeats an attempt
// that failed transiently (timeout, network request, missing answer or
// transport failure). Nothing is retried once the channel is open, so a remote
// command is never replayed. peerLifetime owns the returned peer.
func Establish(ctx, peerLifetime context.Context, source Source, policy connpolicy.Policy) (*transport.Peer, *transport.Channel, error) {
	if paced, ok := source.(operatorPaced); ok {
		if err := paced.awaitOperator(ctx); err != nil {
			return nil, nil, err
		}
	}
	log := diag.From(ctx)
	attempts := max(policy.ConnectionAttempts, 1)
	for attempt := 1; ; attempt++ {
		peer, ch, err := establishOnce(ctx, peerLifetime, source, policy)
		if err == nil {
			return peer, ch, nil
		}
		if attempt >= attempts || ctx.Err() != nil || !retryable(err) {
			return nil, nil, err
		}
		log.Debug(MsgAttemptFailed, diag.Count("attempt", attempt), diag.Count("attempts", attempts))
		timer := time.NewTimer(retryPause)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func retryable(err error) bool {
	return errors.Is(err, ErrConnectTimeout) || errors.Is(err, transport.ErrClosed) ||
		errors.Is(err, webauth.ErrRequest) || errors.Is(err, webauth.ErrAnswerTimeout)
}

func establishOnce(ctx, peerLifetime context.Context, source Source, policy connpolicy.Policy) (_ *transport.Peer, _ *transport.Channel, err error) {
	limit := establishLimit
	if t := policy.ConnectTimeout(); t > 0 && t < limit {
		limit = t
	}
	connecting, stop := context.WithTimeout(ctx, limit)
	defer stop()
	defer func() {
		if err != nil && ctx.Err() == nil && connecting.Err() != nil {
			err = ErrConnectTimeout
		}
	}()
	log := diag.From(ctx)
	started := time.Now()
	log.Debug(MsgConfiguration)
	cfg, err := source.Configuration(connecting)
	if err != nil {
		return nil, nil, err
	}
	log.Debug2(MsgICEServers, diag.Count("ice_servers", len(cfg.ICE.ICEServers)))
	log.Debug(MsgPeer)
	peer, err := transport.New(peerLifetime, transport.Config{ICEServers: cfg.ICE.ICEServers, DeadPeerTimeout: policy.DeadPeerTimeout()})
	if err != nil {
		return nil, nil, err
	}
	ok := false
	defer func() {
		if !ok {
			peer.Close()
		}
	}()
	ch, err := peer.CreateChannel("shell")
	if err != nil {
		return nil, nil, err
	}
	log.Debug(MsgOffer)
	offer, err := peer.Offer(connecting)
	if err != nil {
		return nil, nil, err
	}
	answer, err := source.Exchange(connecting, offer)
	if err != nil {
		return nil, nil, err
	}
	log.Debug(MsgAnswer)
	if err := peer.ApplyAnswer(answer); err != nil {
		return nil, nil, err
	}
	// Wait for data-channel readiness without sending a remote command.
	if err := ch.Send(connecting, transport.Message{Text: true}); err != nil {
		return nil, nil, err
	}
	log.Debug(MsgChannelOpen, diag.Elapsed("elapsed", time.Since(started)))
	ok = true
	return peer, ch, nil
}

type Session struct {
	peer     *transport.Peer
	shell    *transport.Channel
	failures <-chan string
	mu       sync.Mutex
	used     bool
}

// Dial owns only its peer. Source remains caller-owned, including on failure.
func Dial(ctx context.Context, source Source) (*Session, error) {
	return DialWithPolicy(ctx, source, connpolicy.Default())
}

// DialWithPolicy applies ConnectTimeout, ConnectionAttempts and the
// ServerAlive dead-peer bound to the single-use broker carrier.
func DialWithPolicy(ctx context.Context, source Source, policy connpolicy.Policy) (*Session, error) {
	peer, ch, err := Establish(ctx, context.WithoutCancel(ctx), source, policy)
	if err != nil {
		return nil, err
	}
	return &Session{peer: peer, shell: ch, failures: source.Failures()}, nil
}

type byteStream struct{ channel *transport.Channel }

func (s byteStream) Send(ctx context.Context, b []byte) error {
	return s.channel.Send(ctx, transport.Message{Text: true, Data: b})
}
func (s byteStream) Receive(ctx context.Context) ([]byte, error) {
	m, err := s.channel.Receive(ctx)
	return m.Data, err
}

func (s *Session) Close() error { return s.peer.Close() }

// Run may be called only once: the broker replaces and ultimately closes the
// shell carrier. Multiple file operations use one server process inside Run.
func (s *Session) Run(ctx context.Context, text string, stdio shellbroker.IO) (int, error) {
	s.mu.Lock()
	if s.used {
		s.mu.Unlock()
		return 0, errors.New("Connect broker session is single-use")
	}
	s.used = true
	s.mu.Unlock()
	defer s.Close()
	plan, err := shellbroker.New(text)
	if err != nil {
		return 0, err
	}
	log := diag.From(ctx)
	log.Debug(MsgBrokerStart)
	code, err := commandrun.Run(ctx, s.failures, func(ctx context.Context) (int, error) { return plan.Run(ctx, byteStream{s.shell}, stdio) }, 3*time.Second)
	if err == nil {
		log.Debug2(MsgBrokerFinished, diag.Count("exit_status", code))
	}
	return code, err
}
