package connectsession

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
)

type fakeSignaling struct {
	failures chan string
	mu       sync.Mutex
	closed   bool
}

func newFakeSignaling() *fakeSignaling { return &fakeSignaling{failures: make(chan string, 1)} }

func (f *fakeSignaling) Exchange(context.Context, webrtc.SessionDescription) (webrtc.SessionDescription, error) {
	return webrtc.SessionDescription{}, nil
}
func (f *fakeSignaling) Failures() <-chan string { return f.failures }
func (f *fakeSignaling) Close() error {
	f.mu.Lock()
	f.closed = true
	f.mu.Unlock()
	return nil
}
func (f *fakeSignaling) isClosed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}

// A connection that carries a session (the forwarding carrier) keeps its
// authorization watcher when the next session is prepared; its failure still
// reaches Failures. Before, preparing the second session closed the first
// connection's watcher, and Failures followed only the newest connection.
func TestDirectSourceKeepsWatchersOfUsedConnections(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := newDirectSource(nil, ctx, "pi", nil)
	carrier, abandoned, session := newFakeSignaling(), newFakeSignaling(), newFakeSignaling()
	failures := s.Failures()
	s.adopt(carrier)
	if _, err := s.Exchange(ctx, webrtc.SessionDescription{}); err != nil {
		t.Fatal(err)
	}
	s.adopt(abandoned) // a retry whose exchange never ran
	s.adopt(session)
	if carrier.isClosed() || !abandoned.isClosed() || session.isClosed() {
		t.Fatalf("closed: carrier %v abandoned %v session %v", carrier.isClosed(), abandoned.isClosed(), session.isClosed())
	}
	if s.Failures() != failures {
		t.Fatal("Failures changed between attempts")
	}
	carrier.failures <- "direct Connect authorization check failed"
	select {
	case msg := <-failures:
		if msg != "direct Connect authorization check failed" {
			t.Fatal(msg)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the carrier's failure was not relayed")
	}
	s.Close()
	if !carrier.isClosed() || !session.isClosed() {
		t.Fatal("Close left a watcher running")
	}
}
