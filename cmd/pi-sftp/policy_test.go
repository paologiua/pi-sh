package main

import (
	"io"
	"os"
	"strings"
	"testing"
)

func runApp(t *testing.T, args ...string) (int, string) {
	t.Helper()
	oldArgs, oldErrors := os.Args, os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Args = append([]string{"pi-sftp"}, args...)
	os.Stderr = w
	code := app()
	os.Args, os.Stderr = oldArgs, oldErrors
	w.Close()
	output, err := io.ReadAll(r)
	r.Close()
	if err != nil {
		t.Fatal(err)
	}
	return code, string(output)
}

// All refusals happen before authentication: no prompt, listener or request.
func TestConnectionPolicyRefusedBeforeLogin(t *testing.T) {
	for _, tc := range []struct {
		args       []string
		code       int
		diagnostic string
	}{
		{[]string{"--direct-login", "alice@pi"}, 1, "remote user selection"},
		{[]string{"--direct-login", "sftp://alice@pi"}, 1, "remote user selection"},
		{[]string{"--direct-login", "-P", "22", "pi"}, 1, "port selection"},
		{[]string{"--direct-login", "sftp://pi:2222"}, 1, "port selection"},
		{[]string{"--direct-login", "-oPort=22", "pi"}, 1, "port selection"},
		{[]string{"--direct-login", "[::1]"}, 1, "network addresses"},
		{[]string{"--direct-login", "pi:dir"}, 1, "initial remote path"},
		{[]string{"--direct-login", "-oBatchMode=yes", "pi"}, 255, "BatchMode"},
		{[]string{"--browser", "-oBatchMode=yes", "pi"}, 255, "BatchMode"},
	} {
		code, output := runApp(t, tc.args...)
		if code != tc.code || !strings.Contains(output, tc.diagnostic) || strings.Contains(output, "Password (hidden") {
			t.Errorf("%q: %d %q", tc.args, code, output)
		}
	}
}
