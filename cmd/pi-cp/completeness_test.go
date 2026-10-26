package main

import (
	"io"
	"os"
	"strings"
	"testing"
)

func runCopy(t *testing.T, args ...string) (int, string) {
	t.Helper()
	oldArgs, oldErrors := os.Args, os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Args = append([]string{"pi-cp"}, args...)
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

// Every known scp option has an explicit decision, applied before login.
func TestSCPOptionDecisions(t *testing.T) {
	for _, tc := range []struct {
		args       []string
		code       int
		diagnostic string
	}{
		{[]string{"--direct-login", "-B", "source", "pi:x"}, 255, "BatchMode"},
		{[]string{"--direct-login", "-sT", "-oBatchMode=yes", "source", "pi:x"}, 255, "BatchMode"},
		{[]string{"--direct-login", "-3", "pi:a", "pi:b"}, 1, "remote-to-remote"},
		{[]string{"--direct-login", "-4", "source", "pi:x"}, 1, "address family"},
		{[]string{"--direct-login", "-6", "source", "pi:x"}, 1, "address family"},
		{[]string{"--direct-login", "-C", "source", "pi:x"}, 1, "compression"},
		{[]string{"--direct-login", "-O", "source", "pi:x"}, 1, "legacy SCP"},
		{[]string{"--direct-login", "-R", "pi:a", "pi:b"}, 1, "remote-to-remote"},
		{[]string{"--direct-login", "-c", "aes128-ctr", "source", "pi:x"}, 1, "algorithms"},
		{[]string{"--direct-login", "-i", "key", "source", "pi:x"}, 1, "authentication"},
		{[]string{"--direct-login", "-l", "100", "source", "pi:x"}, 1, "bandwidth"},
		{[]string{"--direct-login", "-S", "ssh", "source", "pi:x"}, 1, "transport program"},
		{[]string{"--direct-login", "-D", "/usr/libexec/sftp-server", "source", "pi:x"}, 1, "SFTP server program"},
		{[]string{"--direct-login", "-X", "nrequests=1", "source", "pi:x"}, 1, "tuning"},
		{[]string{"--direct-login", "-oRemoteCommand=echo", "source", "pi:x"}, 1, "remote command"},
		{[]string{"--direct-login", "-oRefuseConnection=no", "source", "pi:x"}, 1, "RefuseConnection"},
		{[]string{"--direct-login", "-oIdentityFile=key", "source", "pi:x"}, 1, "authentication"},
	} {
		code, output := runCopy(t, tc.args...)
		if code != tc.code || !strings.Contains(output, tc.diagnostic) || strings.Contains(output, "Password (hidden") {
			t.Errorf("%q: %d %q", tc.args, code, output)
		}
	}
}
