package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"testing/iotest"

	"pi-sh/internal/diag"
)

// OpenSSH consumes ~B ~C ~R ~V ~v ~# ~& locally. pi-sh performs ~V/~v/~#,
// reproduces the disabled-commandline notice, and refuses the others; none of
// them may reach the remote side, and the line-start state is kept.
func TestLocalEscapeActionsAreConsumed(t *testing.T) {
	var logged bytes.Buffer
	log := diag.Default(&logged, "pi-sh")
	ctx := diag.With(context.Background(), log)
	for _, tc := range []struct{ data, notice string }{
		{"~B", "~B [BREAK is not supported"},
		{"~C", "commandline disabled\r\n"},
		{"~R", "~R [rekeying is not supported"},
		{"~&", "~& [backgrounding is not supported"},
		{"~#", "~#\r\nThe following connections are open:\r\n  #0 client-session"},
		{"~v", "~v [LogLevel VERBOSE]\r\n"},
		{"~v~v", "~v [LogLevel DEBUG]\r\n"},
		{"~V~V~V", "~V [LogLevel ERROR]\r\n"},
	} {
		t.Run(tc.data, func(t *testing.T) {
			log.SetLevel(diag.Info)
			sender := &recordingSender{}
			var diagnostic bytes.Buffer
			err := inputWithActions(ctx, sender, iotest.OneByteReader(strings.NewReader(tc.data+"~.")), '~', inputActions{diagnostic: &diagnostic})
			if !errors.Is(err, errEscape) || sender.data.Len() != 0 || !strings.Contains(diagnostic.String(), tc.notice) {
				t.Fatalf("err %v remote %q notice %q", err, sender.data.String(), diagnostic.String())
			}
		})
	}
	log.SetLevel(diag.Info)
	sender := &recordingSender{}
	if err := inputWithActions(ctx, sender, strings.NewReader("x~B\r~."), '~', inputActions{}); !errors.Is(err, errEscape) || sender.data.String() != "x~B\r" {
		t.Fatalf("midline escape changed: %v %q", err, sender.data.String())
	}
}

func TestVerbosityEscapeRespectsLogDestination(t *testing.T) {
	log, err := diag.Open(diag.Config{Program: "pi-sh", Level: diag.Info, UseFile: true, File: t.TempDir() + "/log"})
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	var diagnostic bytes.Buffer
	err = inputWithActions(diag.With(context.Background(), log), &recordingSender{}, strings.NewReader("~v~."), '~', inputActions{diagnostic: &diagnostic})
	if !errors.Is(err, errEscape) || !strings.Contains(diagnostic.String(), "~v [Logging to syslog or file]") || log.Level() != diag.Info {
		t.Fatalf("file logging changed: %v %q %v", err, diagnostic.String(), log.Level())
	}
}
