package shellbroker

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type abortStream struct {
	plan           *Plan
	replies        chan []byte
	ack            bool
	aborted        bool
	closed         bool
	cancelOnCommit context.CancelFunc
	commitError    error
	// completedBeforeAbort models a broker that emitted X and exited before
	// C arrived: C is never answered, but X is still in flight.
	completedBeforeAbort bool
	// inputFailure, when set, fails every I send after queueing these records.
	inputFailure []string
}

func (s *abortStream) Send(ctx context.Context, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	text := string(data)
	reply := ""
	var result error
	switch text {
	case s.plan.bootstrap:
		reply = "P"
	case s.plan.loader + "\n":
		reply = "B"
	default:
		text = strings.TrimSuffix(strings.TrimPrefix(text, s.plan.prefix), "\n")
		f := strings.Fields(text)
		if len(f) == 0 {
			return ErrProtocol
		}
		switch f[0] {
		case "L":
			reply = "A L " + f[1]
		case "C":
			if len(f) == 3 {
				reply = "A C " + f[1]
			} else {
				s.aborted = true
				if s.completedBeforeAbort {
					reply = "F O\n" + s.plan.prefix + "F E\n" + s.plan.prefix + "X 0"
				}
				if s.ack {
					reply = "D"
				}
				if s.closed {
					close(s.replies)
				}
			}
		case "G":
			result = s.commitError
			reply = "R\n" + s.plan.prefix + "O 1 41"
			if s.completedBeforeAbort {
				reply = "R"
			}
			if s.cancelOnCommit != nil {
				s.cancelOnCommit()
			}
		case "I":
			if s.inputFailure == nil {
				return ErrProtocol
			}
			for _, r := range s.inputFailure {
				s.replies <- []byte(s.plan.prefix + r + "\n")
			}
			result = errors.New("synthetic carrier closed")
		case "F", "A":
		default:
			return ErrProtocol
		}
	}
	if reply != "" {
		s.replies <- []byte(s.plan.prefix + reply + "\n")
	}
	return result
}

func TestBrokerCommitSendFailureStillAttemptsCleanup(t *testing.T) {
	for _, ack := range []bool{false, true} {
		p, err := New("true")
		if err != nil {
			t.Fatal(err)
		}
		fault := errors.New("synthetic launch send failed after acceptance")
		s := &abortStream{plan: p, replies: make(chan []byte, 32), ack: ack, closed: !ack, commitError: fault}
		code, err := p.Run(context.Background(), s, IO{})
		if code != 0 || !errors.Is(err, fault) || !s.aborted || errors.Is(err, ErrCleanupUnconfirmed) == ack {
			t.Fatalf("uncertain commit lost cleanup evidence: %d %v ack=%v aborted=%v", code, err, ack, s.aborted)
		}
	}
}

type stoppedInput struct {
	started, finished chan struct{}
	stop              <-chan struct{}
}

func (r stoppedInput) Read([]byte) (int, error) {
	close(r.started)
	<-r.stop
	close(r.finished)
	return 0, context.Canceled
}

type stoppedOutput struct {
	started chan struct{}
	stop    <-chan struct{}
}

func (w stoppedOutput) Write([]byte) (int, error) {
	close(w.started)
	<-w.stop
	return 0, context.Canceled
}

func TestBrokerInterruptsBorrowedIOBeforeAbortAndJoinsInput(t *testing.T) {
	p, err := New("true")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	s := &abortStream{plan: p, replies: make(chan []byte, 32), ack: true}
	stop := make(chan struct{})
	in := stoppedInput{started: make(chan struct{}), finished: make(chan struct{}), stop: stop}
	out := stoppedOutput{started: make(chan struct{}), stop: stop}
	var stopCalls atomic.Int32
	done := make(chan error, 1)
	go func() {
		_, err := p.Run(ctx, s, IO{Stdin: in, Stdout: out, StopIO: func() { stopCalls.Add(1); close(stop) }})
		done <- err
	}()
	for _, started := range []<-chan struct{}{in.started, out.started} {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("fixture did not block both streams")
		}
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) || errors.Is(err, ErrCleanupUnconfirmed) || !s.aborted || stopCalls.Load() != 1 {
			t.Fatal("cancellation/acknowledgment lost", err, s.aborted, stopCalls.Load())
		}
	case <-time.After(time.Second):
		t.Fatal("blocked I/O prevented abort")
	}
	select {
	case <-in.finished:
	default:
		t.Fatal("input callback survived Run")
	}
}

func TestBrokerAbortWhenCancelledBeforeReady(t *testing.T) {
	p, err := New("true")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := &abortStream{plan: p, replies: make(chan []byte, 32), ack: true, cancelOnCommit: cancel}
	code, err := p.Run(ctx, s, IO{})
	if code != 0 || !errors.Is(err, context.Canceled) || errors.Is(err, ErrCleanupUnconfirmed) || !s.aborted {
		t.Fatalf("launch-commit cancellation lost cleanup: %d %v %v", code, err, s.aborted)
	}
}

func (s *abortStream) Receive(ctx context.Context) ([]byte, error) {
	select {
	case data, ok := <-s.replies:
		if !ok {
			return nil, io.EOF
		}
		return data, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type failedWriter struct{ err error }

func (w failedWriter) Write([]byte) (int, error) { return 0, w.err }

func TestBrokerAbortAfterLocalFailure(t *testing.T) {
	for _, tc := range []struct {
		name        string
		ack, closed bool
	}{
		{"confirmed", true, false}, {"closed-before-ack", false, true}, {"silent", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := New("true")
			if err != nil {
				t.Fatal(err)
			}
			s := &abortStream{plan: p, replies: make(chan []byte, 32), ack: tc.ack, closed: tc.closed}
			failure := errors.New("synthetic local output failure")
			start := time.Now()
			code, err := p.Run(context.Background(), s, IO{Stdout: failedWriter{failure}})
			if code != 0 || !errors.Is(err, failure) || !s.aborted {
				t.Fatalf("failure lost or no abort: %d %v %v", code, err, s.aborted)
			}
			if errors.Is(err, ErrCleanupUnconfirmed) == tc.ack {
				t.Fatalf("incorrect cleanup evidence: %v", err)
			}
			if time.Since(start) > 3*time.Second {
				t.Fatal("abort handshake was not bounded")
			}
		})
	}
}

type failingInput struct{}

func (failingInput) Read([]byte) (int, error) { return 0, errors.New("synthetic stdin failure") }

// Native 2026-10-22: a local stdin read failure with a trivial `exit 0`
// reported unconfirmed cleanup. The broker had already sent X and exited, so
// C was never answered; X proves the command and helpers were reaped.
func TestBrokerAbortAcceptsCompletionAlreadyInFlight(t *testing.T) {
	p, err := New("exit 0")
	if err != nil {
		t.Fatal(err)
	}
	s := &abortStream{plan: p, replies: make(chan []byte, 32), completedBeforeAbort: true}
	start := time.Now()
	code, err := p.Run(context.Background(), s, IO{Stdin: failingInput{}})
	if code != 0 || err == nil || !strings.Contains(err.Error(), "local stdin read failed") || errors.Is(err, ErrCleanupUnconfirmed) || !s.aborted {
		t.Fatalf("completed broker misreported cleanup: %d %v aborted=%v", code, err, s.aborted)
	}
	if time.Since(start) > time.Second {
		t.Fatal("abort waited for an acknowledgement that cannot arrive")
	}
}

// A broker that completed exits and closes the carrier while input is still
// in flight. Its X, behind only K, A and EOF records, wins over the failed
// send, as OpenSSH reports the exit status; without it the send error stands.
func TestBrokerCompletionWinsOverFailedInputSend(t *testing.T) {
	for _, tc := range []struct {
		name    string
		replies []string
		code    int
		failed  bool
	}{
		{"exit", []string{"K", "F E", "X 4"}, 4, false},
		{"signal", []string{"F E", "Y 15"}, 255, false},
		{"no-status", []string{"K"}, 0, true},
		{"output-pending", []string{"O 2 42", "F E", "X 4"}, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := New("exit 4")
			if err != nil {
				t.Fatal(err)
			}
			// G replies R and O 1, acknowledged before the input is read;
			// F O then ends that stream.
			s := &abortStream{plan: p, replies: make(chan []byte, 32), ack: true, inputFailure: append([]string{"F O"}, tc.replies...)}
			code, err := p.Run(context.Background(), s, IO{Stdin: strings.NewReader("x")})
			if code != tc.code || (err != nil) != tc.failed || s.aborted != tc.failed {
				t.Fatalf("code %d, err %v, aborted %v", code, err, s.aborted)
			}
		})
	}
}
