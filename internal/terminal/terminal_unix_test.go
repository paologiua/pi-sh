//go:build darwin || linux

package terminal

import (
	"context"
	"io"
	"os"
	"reflect"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
	"golang.org/x/term"
)

func openPTY(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	m, s, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close(); _ = m.Close() })
	if err := pty.Setsize(m, &pty.Winsize{Rows: 24, Cols: 80}); err != nil {
		t.Fatal(err)
	}
	return m, s
}

func nextSize(t *testing.T, ctx context.Context, s *Session, want Size) {
	t.Helper()
	for {
		select {
		case got, ok := <-s.Sizes():
			if !ok {
				t.Fatal("size watcher closed")
			}
			if got == want {
				return
			}
		case <-ctx.Done():
			t.Fatal("terminal size update timed out")
		}
	}
}

func TestRawBytesResizeAndRestoration(t *testing.T) {
	m, tty := openPTY(t)
	before, err := term.GetState(int(tty.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, err := Open(ctx, tty, tty)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	nextSize(t, ctx, s, Size{Columns: 80, Rows: 24})
	raw, err := term.GetState(int(tty.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(raw, before) {
		t.Fatal("raw mode did not change terminal state")
	}
	// ISIG and canonical input must be disabled: these control bytes must arrive
	// immediately as input data, not trigger signals or wait for a newline.
	payload := []byte{3, 4, 26, 13, 0, 255}
	read := make(chan []byte, 1)
	go func() {
		b := make([]byte, len(payload))
		_, err := io.ReadFull(tty, b)
		if err != nil {
			read <- nil
			return
		}
		read <- b
	}()
	if _, err := m.Write(payload); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-read:
		if !reflect.DeepEqual(got, payload) {
			t.Fatal("terminal transformed control/binary input")
		}
	case <-ctx.Done():
		t.Fatal("raw read blocked")
	}
	if err := pty.Setsize(m, &pty.Winsize{Rows: 40, Cols: 120}); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(os.Getpid(), syscall.SIGWINCH); err != nil {
		t.Fatal(err)
	}
	nextSize(t, ctx, s, Size{Columns: 120, Rows: 40})
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	after, err := term.GetState(int(tty.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatal("original terminal state not restored")
	}
}

func TestCancelRestoresBeforeDone(t *testing.T) {
	_, tty := openPTY(t)
	before, err := term.GetState(int(tty.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s, err := Open(ctx, tty, tty)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	cancel()
	select {
	case <-s.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("cancellation did not stop terminal session")
	}
	if err := s.Wait(); err != nil {
		t.Fatal(err)
	}
	after, err := term.GetState(int(tty.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatal("Done signaled before restoration")
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.Close(); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
}

func TestUnknownDimensionsArePreserved(t *testing.T) {
	m, tty := openPTY(t)
	if err := pty.Setsize(m, &pty.Winsize{}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	s, err := Open(ctx, tty, tty)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// A local kernel may report an unknown size as zero. The terminal layer
	// must not invent remote defaults or impose unverified protocol bounds.
	nextSize(t, ctx, s, Size{})
}
