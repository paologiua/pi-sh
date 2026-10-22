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
	"pi-sh/internal/loginprompt"
	"pi-sh/internal/shellbroker"
	"pi-sh/internal/transport"
	"pi-sh/internal/webauth"
)

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
	if direct {
		client := webauth.New()
		loginCtx, stop := context.WithTimeout(ctx, 10*time.Minute)
		err := loginprompt.Authenticate(loginCtx, client)
		stop()
		if err != nil {
			return nil, err
		}
		preparing, cancel := context.WithTimeout(ctx, 45*time.Second)
		defer cancel()
		connection, err := client.PrepareWithContext(ctx, preparing, destination)
		if err != nil {
			return nil, err
		}
		fmt.Fprintln(notices, program+": signed in directly; session is held in memory for this process")
		return directSource{connection}, nil
	}
	bridge, err := browserauth.New(ctx, destination)
	if err != nil {
		return nil, err
	}
	url, _ := json.Marshal(bridge.ScriptURL())
	fmt.Fprintln(notices, program+": experimental browser authentication; no account credentials leave Chrome")
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
	cfg, err := source.Configuration(connecting)
	if err != nil {
		return nil, err
	}
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
	offer, err := peer.Offer(connecting)
	if err != nil {
		return nil, err
	}
	answer, err := source.Exchange(connecting, offer)
	if err != nil {
		return nil, err
	}
	if err := peer.ApplyAnswer(answer); err != nil {
		return nil, err
	}
	if err := ch.Send(connecting, transport.Message{Text: true}); err != nil {
		return nil, err
	}
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
	return commandrun.Run(ctx, s.failures, func(ctx context.Context) (int, error) { return plan.Run(ctx, byteStream{s.shell}, stdio) }, 3*time.Second)
}
