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

type directSource struct{ *webauth.Connection }

func (s directSource) Configuration(context.Context) (browserauth.Configuration, error) {
	var c browserauth.Configuration
	c.Device.ID, c.Device.Name, c.ICE = s.Device.ID, s.Device.Name, s.ICE
	return c, nil
}

// Authenticate does not persist credentials. The caller owns the returned source.
// notices is a local operator stream; browser mode prints a temporary capability.
func Authenticate(ctx context.Context, destination string, direct bool, notices io.Writer, program string) (Source, error) {
	log := diag.From(ctx)
	if log == nil {
		log = diag.Default(notices, program)
	}
	if direct {
		client := webauth.New()
		loginCtx, stop := context.WithTimeout(ctx, 10*time.Minute)
		log.Debug(MsgLoginStart)
		err := loginprompt.Authenticate(loginCtx, loggedLogin{client, log})
		stop()
		if err != nil {
			return nil, err
		}
		log.Debug(MsgLoginDone)
		preparing, cancel := context.WithTimeout(ctx, 45*time.Second)
		defer cancel()
		log.Debug(MsgDiscovery)
		started := time.Now()
		connection, err := client.PrepareWithContext(ctx, preparing, destination)
		if err != nil {
			return nil, err
		}
		log.Debug(MsgDiscoveryDone, diag.Elapsed("elapsed", time.Since(started)), diag.Count("ice_servers", len(connection.ICE.ICEServers)))
		log.Verbose(MsgAuthenticated, diag.State("method", "direct-login"))
		log.Info(MsgSignedIn)
		return directSource{connection}, nil
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
	return bridge, nil
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
	connecting, stop := context.WithTimeout(ctx, 2*time.Minute)
	defer stop()
	log := diag.From(ctx)
	started := time.Now()
	log.Debug(MsgConfiguration)
	cfg, err := source.Configuration(connecting)
	if err != nil {
		return nil, err
	}
	log.Debug2(MsgICEServers, diag.Count("ice_servers", len(cfg.ICE.ICEServers)))
	log.Debug(MsgPeer)
	peer, err := transport.New(context.WithoutCancel(ctx), transport.Config{ICEServers: cfg.ICE.ICEServers})
	if err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		if !ok {
			peer.Close()
		}
	}()
	ch, err := peer.CreateChannel("shell")
	if err != nil {
		return nil, err
	}
	log.Debug(MsgOffer)
	offer, err := peer.Offer(connecting)
	if err != nil {
		return nil, err
	}
	answer, err := source.Exchange(connecting, offer)
	if err != nil {
		return nil, err
	}
	log.Debug(MsgAnswer)
	if err := peer.ApplyAnswer(answer); err != nil {
		return nil, err
	}
	if err := ch.Send(connecting, transport.Message{Text: true}); err != nil {
		return nil, err
	}
	log.Debug(MsgChannelOpen, diag.Elapsed("elapsed", time.Since(started)))
	ok = true
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
