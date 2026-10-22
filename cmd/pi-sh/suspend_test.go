package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestSuspendEscapeIsLocalAndPreservesBufferedRemainder(t *testing.T) {
	for _, tc := range []struct {
		name, data, want string
		escape           rune
		calls            int
	}{
		{"default", "~\x1aafter\r~.", "after\r", '~', 1},
		{"alternate", "!\x1aafter\r!.", "after\r", '!', 1},
		{"quoted", "~~\x1aafter\r~.", "~\x1aafter\r", '~', 0},
		{"midline", "x~\x1a\r~.", "x~\x1a\r", '~', 0},
		{"disabled", "~\x1aafter", "~\x1aafter", -1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sender := &recordingSender{}
			calls := 0
			var diagnostic bytes.Buffer
			err := inputWithActions(context.Background(), sender, strings.NewReader(tc.data), tc.escape, inputActions{diagnostic: &diagnostic, suspend: func(context.Context) error { calls++; return nil }})
			wantErr := errEscape
			if tc.escape < 0 {
				wantErr = io.EOF
			}
			if !errors.Is(err, wantErr) || calls != tc.calls || sender.data.String() != tc.want {
				t.Fatalf("calls%d, remote%q, err%v", calls, sender.data.String(), err)
			}
		})
	}
}

func TestUnavailableSuspendIsDiagnosedWithoutRemoteControlBytes(t *testing.T) {
	sender := &recordingSender{}
	var diagnostic bytes.Buffer
	err := input(context.Background(), sender, strings.NewReader("~\x1a~."), '~', &diagnostic)
	if !errors.Is(err, errEscape) || sender.data.Len() != 0 || !strings.Contains(diagnostic.String(), "unavailable") {
		t.Fatalf("%v, remote%q, diagnostic%q", err, sender.data.String(), diagnostic.String())
	}
}

func TestSuspendWriteOrActionFailureStopsInput(t *testing.T) {
	failure := errors.New("fixture suspend refused")
	for _, writeFails := range []bool{false, true} {
		sender := &recordingSender{}
		calls := 0
		var writer io.Writer = io.Discard
		if writeFails {
			writer = refusedDiagnostic{}
		}
		err := inputWithActions(context.Background(), sender, strings.NewReader("~\x1adangerous"), '~', inputActions{diagnostic: writer, suspend: func(context.Context) error { calls++; return failure }})
		if err == nil || sender.data.Len() != 0 {
			t.Fatal("suspend failure leaked input", err, sender.data.String())
		}
		if writeFails && calls != 0 {
			t.Fatal("suspended after failed local notice")
		}
		if !writeFails && (!errors.Is(err, failure) || calls != 1) {
			t.Fatal("suspend failure hidden", err, calls)
		}
	}
}

func TestSuspendHelpOnlyAdvertisesAvailableAction(t *testing.T) {
	for _, available := range []bool{false, true} {
		sender := &recordingSender{}
		var diagnostic bytes.Buffer
		actions := inputActions{diagnostic: &diagnostic}
		if available {
			actions.suspend = func(context.Context) error { return nil }
		}
		err := inputWithActions(context.Background(), sender, strings.NewReader("~?~."), '~', actions)
		if !errors.Is(err, errEscape) || strings.Contains(diagnostic.String(), "~^Z - suspend pi-sh") != available {
			t.Fatal("help capability mismatch", available, err, diagnostic.String())
		}
	}
}
