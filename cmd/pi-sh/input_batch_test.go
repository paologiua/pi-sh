package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"pi-sh/internal/transport"
)

type frameSender struct {
	messages  []transport.Message
	failAfter int
}

func (s *frameSender) Send(_ context.Context, m transport.Message) error {
	if s.failAfter > 0 && len(s.messages) >= s.failAfter {
		return transport.ErrReceiveOverflow
	}
	s.messages = append(s.messages, m)
	return nil
}
func (s *frameSender) bytes() []byte {
	var b []byte
	for _, m := range s.messages {
		b = append(b, m.Data...)
	}
	return b
}

func TestBufferedPasteDoesNotExhaustReceiverFrameBudget(t *testing.T) {
	for _, text := range []string{strings.Repeat("x", 64*1024), strings.Repeat("aé世界\r\x03", 8192)} {
		sender := &frameSender{failAfter: 4096}
		err := input(context.Background(), sender, strings.NewReader(text), -1, io.Discard)
		if !errors.Is(err, io.EOF) || !bytes.Equal(sender.bytes(), []byte(text)) {
			t.Fatal("valid paste exhausted frame budget or changed bytes", len(sender.messages), err)
		}
		if len(sender.messages) > len(text)/1024+2 {
			t.Fatal("already-buffered input still fragmented per character", len(sender.messages))
		}
		for _, message := range sender.messages {
			if !message.Text || !utf8.Valid(message.Data) || len(message.Data) > transport.MaxSendSize {
				t.Fatal("invalid/big input frame")
			}
		}
	}
}

type notifyingSender chan []byte

func (s notifyingSender) Send(_ context.Context, m transport.Message) error {
	s <- bytes.Clone(m.Data)
	return nil
}

func TestBufferedInputFlushesBeforeWaitingForPartialRune(t *testing.T) {
	r, w := io.Pipe()
	defer r.Close()
	defer w.Close()
	sent := make(notifyingSender, 8)
	done := make(chan error, 1)
	go func() { done <- input(context.Background(), sent, r, '~', io.Discard) }()
	defer func() {
		r.Close()
		w.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("input worker did not stop")
		}
	}()
	if _, err := w.Write([]byte{'a', 0xe2}); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-sent:
		if string(got) != "a" {
			t.Fatal("valid prefix not flushed", got)
		}
	case <-time.After(time.Second):
		t.Fatal("valid input waits for incomplete next rune")
	}
	if _, err := w.Write([]byte{0x82, 0xac}); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-sent:
		if string(got) != "€" {
			t.Fatal("partial rune split", got)
		}
	case <-time.After(time.Second):
		t.Fatal("complete character delayed until next read")
	}
	w.Close()
	select {
	case err := <-done:
		done <- err
		if !errors.Is(err, io.EOF) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("EOF not returned")
	}
}

func TestBufferedInputFlushesAtEscapeErrorAndEOFBorders(t *testing.T) {
	for _, tc := range []struct {
		input, want string
		wantErr     error
	}{
		{"valid\r~.unread", "valid\r", errEscape},
		{"valid\r~?~.unread", "valid\r", errEscape},
		{"valid\r~~end", "valid\r~end", io.EOF},
		{"valid\r~", "valid\r~", io.EOF},
		{"valid\xff", "valid", nil},
	} {
		sender := &frameSender{}
		err := input(context.Background(), sender, strings.NewReader(tc.input), '~', io.Discard)
		if string(sender.bytes()) != tc.want {
			t.Fatalf("buffered border lost bytes: got %q want %q", sender.bytes(), tc.want)
		}
		if tc.wantErr != nil && !errors.Is(err, tc.wantErr) || tc.wantErr == nil && err == nil {
			t.Fatal("input result changed", err)
		}
	}
}

type inputOrderWriter struct{ before func() }

func (w inputOrderWriter) Write(b []byte) (int, error) { w.before(); return len(b), nil }

func TestBufferedInputFlushesBeforeHelpAndSuspension(t *testing.T) {
	for _, suspend := range []bool{false, true} {
		sender := &frameSender{}
		calls := 0
		check := func() {
			calls++
			if string(sender.bytes()) != "before\r" {
				t.Fatal("local action preceded pending remote input", string(sender.bytes()))
			}
		}
		actions := inputActions{diagnostic: io.Discard}
		control := "?"
		if suspend {
			control = "\x1a"
			actions.suspend = func(context.Context) error { check(); return nil }
		} else {
			actions.diagnostic = inputOrderWriter{check}
		}
		err := inputWithActions(context.Background(), sender, strings.NewReader("before\r~"+control+"after\r~."), '~', actions)
		if !errors.Is(err, errEscape) || calls != 1 || string(sender.bytes()) != "before\rafter\r" {
			t.Fatal("buffered local action changed ordering", calls, string(sender.bytes()), err)
		}
	}
}
