package main

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
	"pi-sh/internal/browserauth"
	"pi-sh/internal/shellbroker"
	"pi-sh/internal/transport"
)

type cleanupSource struct {
	peer     *transport.Peer
	lifetime context.Context
	cancel   context.CancelFunc
	finished chan error
}

func (*cleanupSource) Configuration(context.Context) (browserauth.Configuration, error) {
	return browserauth.Configuration{}, nil
}
func (*cleanupSource) Failures() <-chan string { return nil }
func (s *cleanupSource) Close() error          { return s.peer.Close() }
func (s *cleanupSource) Exchange(ctx context.Context, offer webrtc.SessionDescription) (webrtc.SessionDescription, error) {
	answer, err := s.peer.Answer(ctx, offer)
	if err == nil {
		go func() { s.finished <- s.serve() }()
	}
	return answer, err
}

// This peer speaks just the independently authored broker framing, not Connect
// HTTP/auth or a remote agent. Cancellation happens only after broker readiness.
func (s *cleanupSource) serve() error {
	ch, err := s.peer.Accept(s.lifetime)
	if err != nil {
		return err
	}
	prefix := ""
	phase := 0
	for {
		m, err := ch.Receive(s.lifetime)
		if err != nil {
			return err
		}
		if len(m.Data) == 0 {
			continue
		}
		text, reply := string(m.Data), ""
		switch phase {
		case 0:
			match := regexp.MustCompile(`PISH3:([a-f0-9]{32}):`).FindStringSubmatch(text)
			if len(match) != 2 {
				return errors.New("missing synthetic broker marker")
			}
			prefix = "\x1ePISH3:" + match[1] + ":"
			reply, phase = "P", 1
		case 1:
			reply, phase = "B", 2
		default:
			text = strings.TrimSuffix(strings.TrimPrefix(text, prefix), "\n")
			f := strings.Fields(text)
			if len(f) == 0 {
				return errors.New("empty synthetic broker record")
			}
			switch f[0] {
			case "L":
				reply = "A L " + f[1]
			case "C":
				if len(f) == 3 {
					reply = "A C " + f[1]
				} else {
					return ch.Send(s.lifetime, transport.Message{Text: true, Data: []byte(prefix + "D\n")})
				}
			case "G":
				reply = "R"
			case "F":
				s.cancel()
			default:
				return fmt.Errorf("unexpected synthetic broker record kind %s", f[0])
			}
		}
		if reply != "" {
			if err := ch.Send(s.lifetime, transport.Message{Text: true, Data: []byte(prefix + reply + "\n")}); err != nil {
				return err
			}
		}
	}
}

func TestConnectRetainsPeerUntilBrokerAbortAcknowledged(t *testing.T) {
	lifetime, stop := context.WithTimeout(context.Background(), 15*time.Second)
	defer stop()
	ctx, cancel := context.WithCancel(lifetime)
	defer cancel()
	peer, err := transport.New(lifetime, transport.Config{})
	if err != nil {
		t.Fatal(err)
	}
	source := &cleanupSource{peer: peer, lifetime: lifetime, cancel: cancel, finished: make(chan error, 1)}
	defer source.Close()
	plan, err := shellbroker.New("true")
	if err != nil {
		t.Fatal(err)
	}
	err = connect(ctx, source, -1, nil, plan, nil, nil)
	if !errors.Is(err, context.Canceled) || errors.Is(err, shellbroker.ErrCleanupUnconfirmed) {
		t.Fatalf("peer closed before confirmed abort: %v", err)
	}
	select {
	case err := <-source.finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-lifetime.Done():
		t.Fatal("synthetic peer never acknowledged abort")
	}
}
