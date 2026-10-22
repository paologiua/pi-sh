//go:build darwin || linux

package main

import (
	"context"
	"errors"
	"io"
	"os"
	"os/signal"
	"reflect"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
	"golang.org/x/term"

	"pi-sh/internal/terminal"
)

func TestIdleTTYInputCancellationDoesNotNeedAnotherByte(t *testing.T) {
	m, file, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	defer file.Close()
	tty, err := terminal.Open(context.Background(), file, file)
	if err != nil {
		t.Fatal(err)
	}
	defer tty.Close()
	fd := int32(file.Fd())
	waitReadable := func(want bool) {
		t.Helper()
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) {
			fds := []unix.PollFd{{Fd: fd, Events: unix.POLLIN}}
			n, err := unix.Poll(fds, 0)
			if err != nil {
				t.Fatal(err)
			}
			if (n > 0) == want {
				return
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatal("input queue did not reach expected readiness")
	}
	// An incomplete UTF-8 rune forces the actual file reader to await a second
	// byte. Kernel readiness proves it consumed byte1, avoiding a scheduling guess.
	if _, err := m.Write([]byte{0xc3}); err != nil {
		t.Fatal(err)
	}
	waitReadable(true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- input(ctx, &recordingSender{}, file, '~', io.Discard) }()
	defer func() {
		select {
		case <-done:
		default:
			m.Close()
			file.Close()
			<-done
		}
	}()
	waitReadable(false)
	cancel()
	select {
	case err := <-done:
		// Let cleanup join the same completion without depending on another read.
		done <- err
		if !errors.Is(err, context.Canceled) {
			t.Fatal("cancellation cause lost", err)
		}
	case <-time.After(300 * time.Millisecond):
		t.Fatal("cancelled terminal reader still needs input or descriptor close")
	}
}

func TestPTYControlPasteAndDisconnect(t *testing.T) {
	m, tty, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	defer tty.Close()
	before, err := term.GetState(int(tty.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := terminal.Open(ctx, tty, tty)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	sender := &recordingSender{}
	done := make(chan error, 1)
	go func() { done <- input(ctx, sender, tty, '~', io.Discard) }()
	// Ctrl-D must be a transmitted byte, not a local EOF. Paste wrappers and
	// multibyte text must survive the actual OS raw-terminal/input-reader path.
	payload := "\x04\x03\r\x1b[200~~. é 世界\nsecond line\x1b[201~\r"
	if _, err := m.Write([]byte(payload + "~.")); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, errEscape) || sender.data.String() != payload {
			t.Fatalf("raw input changed: got %q, %v", sender.data.String(), err)
		}
	case <-ctx.Done():
		t.Fatal("control byte or paste handling blocked")
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	after, err := term.GetState(int(tty.Fd()))
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("terminal not restored")
	}
}

func TestTerminationSignalsRestoreTerminal(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT} {
		t.Run(sig.String(), func(t *testing.T) {
			m, tty, err := pty.Open()
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			defer tty.Close()
			before, err := term.GetState(int(tty.Fd()))
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := signal.NotifyContext(context.Background(), terminationSignals()...)
			defer cancel()
			session, err := terminal.Open(ctx, tty, tty)
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			if err := syscall.Kill(os.Getpid(), sig); err != nil {
				t.Fatal(err)
			}
			select {
			case <-session.Done():
			case <-time.After(5 * time.Second):
				t.Fatal("termination did not restore terminal")
			}
			if err := waitSession(ctx, cancel, session, nil, nil, nil, nil); !errors.Is(err, context.Canceled) {
				t.Fatalf("termination reported %v", err)
			}
			after, err := term.GetState(int(tty.Fd()))
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatal("terminal not restored after termination")
			}
		})
	}
}
