package main

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"testing/iotest"

	"pi-sh/internal/transport"
)

type recordingSender struct {
	data strings.Builder
	err  error
}

func (s *recordingSender) Send(_ context.Context, m transport.Message) error {
	if !m.Text {
		return errors.New("input must use verified text framing")
	}
	if s.err != nil {
		return s.err
	}
	_, _ = s.data.Write(m.Data)
	return nil
}

func TestInteractiveInput(t *testing.T) {
	for _, tc := range []struct {
		name, input, want string
		escape            bool
	}{
		{"unicode-and-controls", "héllo 世界\r\x03\x04", "héllo 世界\r\x03\x04", false},
		{"local-disconnect", "~.ignored", "", true},
		{"line-aware", "echo ~.\r~.", "echo ~.\r", true},
		{"literal-tilde", "~~.\r", "~.\r", false},
		{"unknown-escape", "~x", "~x", false},
		{"eof-after-tilde", "~", "~", false},
		{"ctrl-c-does-not-reset-local-line", "x\x03~.\r~.", "x\x03~.\r", true},
		{"bracketed-paste-tilde", "\x1b[200~~.\x1b[201~\r~.", "\x1b[200~~.\x1b[201~\r", true},
		{"bracketed-multiline", "\x1b[200~one\ntwo\x1b[201~\r~.", "\x1b[200~one\ntwo\x1b[201~\r", true},
		// Like OpenSSH's line-based filter, paste framing does not disable
		// escape recognition after an embedded newline. Use -e none for that.
		{"escape-inside-multiline-paste", "\x1b[200~one\n~.\x1b[201~", "\x1b[200~one\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &recordingSender{}
			err := input(context.Background(), s, iotest.OneByteReader(strings.NewReader(tc.input)), '~', io.Discard)
			wantErr := io.EOF
			if tc.escape {
				wantErr = errEscape
			}
			if !errors.Is(err, wantErr) {
				t.Fatal(err)
			}
			if s.data.String() != tc.want {
				t.Fatalf("input bytes changed: %q", s.data.String())
			}
		})
	}
}

func TestConfiguredEscapes(t *testing.T) {
	for _, tc := range []struct {
		name, data, want string
		escape           rune
		disconnect       bool
	}{
		{"disabled-paste", "\x1b[200~a\n~.\n~~\x1b[201~", "\x1b[200~a\n~.\n~~\x1b[201~", -1, false},
		{"alternate", "~.\r!!\r!.", "~.\r!\r", '!', true},
		{"alternate-eof", "!", "!", '!', false},
		{"control-escape", "\x1d.", "", 29, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &recordingSender{}
			err := input(context.Background(), s, iotest.OneByteReader(strings.NewReader(tc.data)), tc.escape, io.Discard)
			wantErr := io.EOF
			if tc.disconnect {
				wantErr = errEscape
			}
			if !errors.Is(err, wantErr) || s.data.String() != tc.want {
				t.Fatalf("got %q, %v; want %q, %v", s.data.String(), err, tc.want, wantErr)
			}
		})
	}
}

func TestParseEscape(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  rune
	}{
		{"none", -1}, {"~", '~'}, {"!", '!'}, {"^]", 29}, {"^@", 0}, {"^a", 1},
	} {
		got, err := parseEscape(tc.value)
		if err != nil || got != tc.want {
			t.Fatalf("parseEscape(%q) = %d, %v", tc.value, got, err)
		}
	}
	for _, value := range []string{"", "ab", "é", "NONE", "^?", "^é"} {
		if _, err := parseEscape(value); err == nil {
			t.Fatalf("invalid escape %q accepted", value)
		}
	}
}

func TestInvalidUTF8IsRejected(t *testing.T) {
	s := &recordingSender{}
	err := input(context.Background(), s, strings.NewReader(string([]byte{255})), '~', io.Discard)
	if err == nil || s.data.Len() != 0 {
		t.Fatal("invalid UTF-8 was silently forwarded or replaced")
	}
}

func TestSendFailureStopsInput(t *testing.T) {
	sentinel := errors.New("test send failure")
	s := &recordingSender{err: sentinel}
	if err := input(context.Background(), s, strings.NewReader("abc"), '~', io.Discard); !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
}
