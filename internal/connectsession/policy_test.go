package connectsession

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
	"pi-sh/internal/browserauth"
	"pi-sh/internal/connpolicy"
	"pi-sh/internal/transport"
	"pi-sh/internal/webauth"
)

// scriptedSource answers each establishment attempt with a fresh local Pion
// peer; per-attempt behaviour simulates hung signaling, failed ICE or errors.
type scriptedSource struct {
	t         *testing.T
	lifetime  context.Context
	behaviour []string // "hang", "dead-answer", "request-error", "fatal", "ok"
	mu        sync.Mutex
	configs   int
	exchanges int
	remotes   []*transport.Peer
}

func (s *scriptedSource) Configuration(context.Context) (browserauth.Configuration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.configs++
	return browserauth.Configuration{}, nil
}

func (s *scriptedSource) Exchange(ctx context.Context, offer webrtc.SessionDescription) (webrtc.SessionDescription, error) {
	s.mu.Lock()
	step := s.behaviour[min(s.exchanges, len(s.behaviour)-1)]
	s.exchanges++
	s.mu.Unlock()
	switch step {
	case "hang":
		<-ctx.Done()
		return webrtc.SessionDescription{}, ctx.Err()
	case "request-error":
		return webrtc.SessionDescription{}, webauth.ErrRequest
	case "fatal":
		return webrtc.SessionDescription{}, errors.New("no unique authorized device matches the destination")
	}
	remote, err := transport.New(s.lifetime, transport.Config{})
	if err != nil {
		s.t.Fatal(err)
	}
	s.mu.Lock()
	s.remotes = append(s.remotes, remote)
	s.mu.Unlock()
	answer, err := remote.Answer(ctx, offer)
	if step == "dead-answer" {
		// A valid answer whose peer disappears: ICE/DTLS never completes.
		remote.Close()
	}
	return answer, err
}
func (s *scriptedSource) Failures() <-chan string { return nil }
func (s *scriptedSource) Close() error            { return nil }

func withPause(t *testing.T) {
	old := retryPause
	retryPause = 10 * time.Millisecond
	t.Cleanup(func() { retryPause = old })
}

func TestConnectTimeoutBoundsSignalingAndICE(t *testing.T) {
	lifetime, stop := context.WithTimeout(context.Background(), 20*time.Second)
	defer stop()
	policy := connpolicy.Default()
	policy.ConnectTimeoutSeconds = 1
	for _, step := range []string{"hang", "dead-answer"} {
		source := &scriptedSource{t: t, lifetime: lifetime, behaviour: []string{step}}
		started := time.Now()
		_, _, err := Establish(lifetime, lifetime, source, policy)
		elapsed := time.Since(started)
		if !errors.Is(err, ErrConnectTimeout) || elapsed < 900*time.Millisecond || elapsed > 5*time.Second {
			t.Fatalf("%s: %v after %v", step, err, elapsed)
		}
		if source.configs != 1 {
			t.Fatalf("%s: default ConnectionAttempts retried", step)
		}
	}
}

func TestConnectionAttemptsRetryOnlyBeforeChannelOpens(t *testing.T) {
	withPause(t)
	lifetime, stop := context.WithTimeout(context.Background(), 30*time.Second)
	defer stop()
	policy := connpolicy.Default()
	policy.ConnectTimeoutSeconds = 1
	policy.ConnectionAttempts = 3
	source := &scriptedSource{t: t, lifetime: lifetime, behaviour: []string{"request-error", "dead-answer", "ok", "ok"}}
	session, err := DialWithPolicy(lifetime, source, policy)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if source.configs != 3 || source.exchanges != 3 || len(source.remotes) != 2 {
		t.Fatalf("attempts: configurations=%d exchanges=%d remotes=%d", source.configs, source.exchanges, len(source.remotes))
	}
	// The failed attempt's peer never opened a channel; the successful one
	// received exactly the empty readiness message and no command.
	if _, err := source.remotes[0].Accept(lifetime); !errors.Is(err, transport.ErrClosed) {
		t.Fatal("failed attempt delivered a channel", err)
	}
	remote, err := source.remotes[1].Accept(lifetime)
	if err != nil {
		t.Fatal(err)
	}
	ready, err := remote.Receive(lifetime)
	if err != nil || len(ready.Data) != 0 || !ready.Text {
		t.Fatal("unexpected readiness", err)
	}
	waiting, cancel := context.WithTimeout(lifetime, 300*time.Millisecond)
	defer cancel()
	if m, err := remote.Receive(waiting); err == nil {
		t.Fatalf("establishment sent remote data: %q", m.Data)
	}
	// Once established, failures are never re-dialled: closing the carrier
	// leaves the attempt count unchanged and the broker carrier is single-use.
	session.Close()
	if source.configs != 3 {
		t.Fatal("re-established after channel open")
	}
}

func TestConnectionAttemptsDoNotRetryPermanentOrCancelled(t *testing.T) {
	withPause(t)
	lifetime, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	policy := connpolicy.Default()
	policy.ConnectionAttempts = 5
	source := &scriptedSource{t: t, lifetime: lifetime, behaviour: []string{"fatal"}}
	if _, _, err := Establish(lifetime, lifetime, source, policy); err == nil || source.configs != 1 {
		t.Fatal("permanent failure retried", source.configs, err)
	}
	source = &scriptedSource{t: t, lifetime: lifetime, behaviour: []string{"request-error"}}
	if _, _, err := Establish(lifetime, lifetime, source, policy); !errors.Is(err, webauth.ErrRequest) || source.configs != 5 {
		t.Fatal("attempt budget not honoured", source.configs, err)
	}
	cancelled, cancel := context.WithCancel(lifetime)
	cancel()
	source = &scriptedSource{t: t, lifetime: lifetime, behaviour: []string{"hang"}}
	if _, _, err := Establish(cancelled, lifetime, source, policy); !errors.Is(err, context.Canceled) || source.configs > 1 {
		t.Fatal("cancelled establishment retried", source.configs, err)
	}
}

func TestBatchModeFailsBeforeAnyPromptOrListener(t *testing.T) {
	policy := connpolicy.Default()
	policy.BatchMode = true
	for _, direct := range []bool{true, false} {
		var notices strings.Builder
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		source, err := AuthenticateWithPolicy(ctx, "device", direct, policy, &notices, "test")
		cancel()
		if err == nil || source != nil || !strings.Contains(err.Error(), "BatchMode") || notices.Len() != 0 {
			t.Fatalf("direct=%v: %v %q", direct, err, notices.String())
		}
	}
	policy = connpolicy.Default()
	policy.ConnectionAttempts = 2
	if _, err := AuthenticateWithPolicy(context.Background(), "device", false, policy, io.Discard, "test"); err == nil {
		t.Fatal("browser companion accepted repeated signaling")
	}
}
