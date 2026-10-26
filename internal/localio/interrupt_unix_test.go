//go:build darwin || linux

package localio

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestInterruptReadWakesWithoutConsumingInput(t *testing.T) {
	r, w := pipe(t)
	session, err := Open(context.Background(), Files{Stdin: r})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	result := make(chan error, 1)
	go func() {
		_, err := session.Stdin.Read(make([]byte, 8))
		result <- err
	}()
	time.Sleep(20 * time.Millisecond) // let the Read block in poll
	if !session.InterruptRead() || !session.InterruptRead() {
		t.Fatal("Unix backend refused an interrupt")
	}
	select {
	case err := <-result:
		if !errors.Is(err, ErrInterrupted) {
			t.Fatal("pending read", err)
		}
	case <-time.After(time.Second):
		t.Fatal("interrupt did not wake the pending read")
	}
	// Coalesced requests produce one interruption; data is then intact.
	if _, err := w.Write([]byte("kept")); err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 8)
	n, err := session.Stdin.Read(b)
	if err != nil || string(b[:n]) != "kept" {
		t.Fatalf("read after interrupt: %q %v", b[:n], err)
	}
	// A request made with no Read pending applies to the next Read.
	session.InterruptRead()
	if _, err := session.Stdin.Read(b); !errors.Is(err, ErrInterrupted) {
		t.Fatal("queued interrupt", err)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	// A closed session accepts and ignores a late request.
	session.InterruptRead()
}
