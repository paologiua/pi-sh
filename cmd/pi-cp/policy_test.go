package main

import (
	"io"
	"os"
	"strings"
	"testing"
)

// All refusals happen before authentication: no prompt, listener or request.
func TestConnectionPolicyRefusedBeforeLogin(t *testing.T) {
	for _, tc := range []struct {
		args       []string
		code       int
		diagnostic string
	}{
		{[]string{"--direct-login", "source", "alice@pi:x"}, 1, "remote user selection"},
		{[]string{"--direct-login", "scp://alice@pi/x", "target"}, 1, "remote user selection"},
		{[]string{"--direct-login", "-P", "22", "source", "pi:x"}, 1, "port selection"},
		{[]string{"--direct-login", "source", "scp://pi:2222/x"}, 1, "port selection"},
		{[]string{"--direct-login", "-oUser=pi", "source", "pi:x"}, 1, "remote user selection"},
		{[]string{"--direct-login", "source", "[::1]:x"}, 1, "network addresses"},
		{[]string{"--direct-login", "-oBatchMode=yes", "source", "scp://pi/x"}, 255, "BatchMode"},
		{[]string{"--direct-login", "-oNumberOfPasswordPrompts=0", "source", "pi:x"}, 255, "NumberOfPasswordPrompts"},
	} {
		oldArgs, oldErrors := os.Args, os.Stderr
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		os.Args = append([]string{"pi-cp"}, tc.args...)
		os.Stderr = w
		code := app()
		os.Args, os.Stderr = oldArgs, oldErrors
		w.Close()
		output, err := io.ReadAll(r)
		r.Close()
		if err != nil || code != tc.code || !strings.Contains(string(output), tc.diagnostic) || strings.Contains(string(output), "Password (hidden") {
			t.Errorf("%q: %d %q", tc.args, code, output)
		}
	}
}
