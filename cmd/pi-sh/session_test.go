package main

import (
	"context"
	"errors"
	"io"
	"pi-sh/internal/shellbroker"
	"strings"
	"testing"
)

type stoppedTerminal struct {
	done chan struct{}
	err  error
}

func TestLocalCleanupFailureCannotBecomeSuccessOrEOF(t *testing.T) {
	failure := errors.New("synthetic local flag restoration failed")
	for _, result := range []error{nil, &commandStatus{0}, &commandStatus{23}, errEscape, io.EOF, errors.Join(errEscape, shellbroker.ErrCleanupUnconfirmed), context.Canceled} {
		got := localCleanupResult(result, failure)
		var status *commandStatus
		if !errors.Is(got, failure) || errors.As(got, &status) || errors.Is(got, errEscape) || errors.Is(got, io.EOF) {
			t.Fatalf("cleanup failure retained a successful completion classifier: %v", got)
		}
		if errors.Is(result, shellbroker.ErrCleanupUnconfirmed) && !errors.Is(got, shellbroker.ErrCleanupUnconfirmed) {
			t.Fatal("remote cleanup uncertainty lost")
		}
		if errors.Is(result, context.Canceled) && !errors.Is(got, context.Canceled) {
			t.Fatal("cancellation cause lost")
		}
	}
}

func TestSessionEndReasons(t *testing.T) {
	sentinel := errors.New("test I/O failure")
	for _, tc := range []struct {
		name     string
		input    error
		output   error
		resize   error
		auth     string
		terminal error
		stopped  bool
		want     error
		wantText string
	}{
		{name: "local-disconnect", input: errEscape, want: errEscape},
		{name: "remote-close", output: io.EOF, wantText: "has not supplied a remote exit status"},
		{name: "input-failure", input: sentinel, want: sentinel},
		{name: "output-failure", output: sentinel, want: sentinel},
		{name: "resize-failure", resize: sentinel, want: sentinel},
		{name: "authorization", auth: "browser unavailable", wantText: "authorization check failed"},
		{name: "terminal-failure", stopped: true, terminal: sentinel, want: sentinel},
		{name: "terminal-without-request", stopped: true, wantText: "without a disconnect request"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			in, out, resize := make(chan error, 1), make(chan error, 1), make(chan error, 1)
			auth := make(chan string, 1)
			if tc.input != nil {
				in <- tc.input
			}
			if tc.output != nil {
				out <- tc.output
			}
			if tc.resize != nil {
				resize <- tc.resize
			}
			if tc.auth != "" {
				auth <- tc.auth
			}
			done := make(chan struct{})
			if tc.stopped {
				close(done)
			}
			err := waitSession(ctx, cancel, stoppedTerminal{done: done, err: tc.terminal}, in, out, resize, auth)
			if tc.wantText != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantText) {
					t.Fatalf("got %v", err)
				}
			} else if !errors.Is(err, tc.want) {
				t.Fatalf("got %v; want %v", err, tc.want)
			}
		})
	}
}

func (t stoppedTerminal) Done() <-chan struct{} { return t.done }
func (t stoppedTerminal) Wait() error           { return t.err }

func TestCanceledSessionNeverReportsSuccess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	close(done) // Terminal has already restored modes in response to cancellation.
	for i := 0; i < 100; i++ {
		err := waitSession(ctx, cancel, stoppedTerminal{done: done}, nil, nil, nil, nil)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled session reported %v instead of cancellation", err)
		}
	}
}

func TestCanceledSessionNeverAcceptsQueuedEscape(t *testing.T) {
	for i := 0; i < 100; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		input := make(chan error, 1)
		input <- errEscape
		err := waitSession(ctx, cancel, stoppedTerminal{done: make(chan struct{})}, input, nil, nil, nil)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("known cancellation became escape success: %v", err)
		}
	}
}
