package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"testing/iotest"
)

func TestEscapeHelpIsLocalAndKeepsLineBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, data, want string
		escape           rune
		help             bool
	}{
		{"default", "~?~.ignored", "", '~', true},
		{"alternate", "!?!.ignored", "", '!', true},
		{"control", "\x1d?\x1d.ignored", "", 29, true},
		{"question-prefix", "??? .\r?.", "? .\r", '?', true},
		{"middle", "x~?\r~.", "x~?\r", '~', false},
		{"quoted", "~~?\r~.", "~?\r", '~', false},
		{"disabled", "~?~.", "~?~.", -1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &recordingSender{}
			var diagnostic bytes.Buffer
			err := input(context.Background(), s, iotest.OneByteReader(strings.NewReader(tc.data)), tc.escape, &diagnostic)
			wantErr := errEscape
			if tc.escape < 0 {
				wantErr = io.EOF
			}
			if !errors.Is(err, wantErr) || s.data.String() != tc.want {
				t.Fatalf("remote %q, err %v", s.data.String(), err)
			}
			got := diagnostic.String()
			if tc.help {
				if !strings.Contains(got, "Supported escape sequences:") || !strings.Contains(got, "this message") {
					t.Fatalf("missing local help: %q", got)
				}
				if strings.Contains(got, "forward") || strings.Contains(got, "rekey") || strings.Contains(got, "suspend") {
					t.Fatal("help advertised unimplemented actions")
				}
			} else if got != "" {
				t.Fatalf("unexpected help %q", got)
			}
		})
	}
}

type refusedDiagnostic struct{}

func (refusedDiagnostic) Write([]byte) (int, error) {
	return 0, errors.New("fixture diagnostic refused")
}

type shortDiagnostic struct{}

func (shortDiagnostic) Write(p []byte) (int, error) { return len(p) - 1, nil }

func TestEscapeHelpWriteFailureStopsWithoutSendingCommand(t *testing.T) {
	for _, writer := range []io.Writer{refusedDiagnostic{}, shortDiagnostic{}} {
		sender := &recordingSender{}
		err := input(context.Background(), sender, strings.NewReader("~?dangerous"), '~', writer)
		if err == nil || !strings.Contains(err.Error(), "diagnostic") || sender.data.Len() != 0 {
			t.Fatalf("%v, remote %q", err, sender.data.String())
		}
	}
}
