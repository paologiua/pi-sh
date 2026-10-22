package connectsession

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
	"pi-sh/internal/browserauth"
	"pi-sh/internal/shellbroker"
	"pi-sh/internal/transport"
)

type localSource struct{ peer *transport.Peer }

func (s localSource) Configuration(context.Context) (browserauth.Configuration, error) {
	return browserauth.Configuration{}, nil
}
func (s localSource) Exchange(ctx context.Context, offer webrtc.SessionDescription) (webrtc.SessionDescription, error) {
	return s.peer.Answer(ctx, offer)
}
func (s localSource) Failures() <-chan string { return nil }
func (s localSource) Close() error            { return s.peer.Close() }

func TestDialSingleUseBrokerAndCancelledStart(t *testing.T) {
	lifetime, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	peer, err := transport.New(lifetime, transport.Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	session, err := Dial(lifetime, localSource{peer})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	remote, err := peer.Accept(lifetime)
	if err != nil {
		t.Fatal(err)
	}
	ready, err := remote.Receive(lifetime)
	if err != nil || remote.Label() != "shell" || len(ready.Data) != 0 || !ready.Text {
		t.Fatal("unexpected channel/readiness message", err)
	}
	ctx, cancel := context.WithCancel(lifetime)
	cancel()
	if _, err := session.Run(ctx, "true", shellbroker.IO{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled launch: %v", err)
	}
	if _, err := session.Run(lifetime, "true", shellbroker.IO{}); err == nil {
		t.Fatal("reused a single-use broker carrier")
	}
	if message, err := remote.Receive(lifetime); err == nil && len(message.Data) > 0 {
		t.Fatal("cancelled start sent a remote command")
	}
}
