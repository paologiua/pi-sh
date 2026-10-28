package main

import (
	"strings"
	"testing"
)

// Every known sftp option has an explicit decision, applied before login.
func TestSFTPOptionDecisions(t *testing.T) {
	for _, tc := range []struct {
		args       []string
		diagnostic string
	}{
		{[]string{"--direct-login", "-4", "pi"}, "address family"},
		// -A is agent forwarding and -J ProxyJump (not X11 or tunnels).
		{[]string{"--direct-login", "-A", "pi"}, "agent forwarding"},
		{[]string{"--direct-login", "-J", "jump", "pi"}, "ProxyJump"},
		{[]string{"--direct-login", "-6", "pi"}, "address family"},
		{[]string{"--direct-login", "-C", "pi"}, "compression"},
		{[]string{"--direct-login", "-N", "pi"}, "not quiet"},
		{[]string{"--direct-login", "-r", "pi"}, "recursive"},
		{[]string{"--direct-login", "-B", "32768", "pi"}, "buffer size"},
		{[]string{"--direct-login", "-R", "64", "pi"}, "request count"},
		{[]string{"--direct-login", "-c", "aes128-ctr", "pi"}, "algorithms"},
		{[]string{"--direct-login", "-i", "key", "pi"}, "authentication"},
		{[]string{"--direct-login", "-l", "100", "pi"}, "bandwidth"},
		{[]string{"--direct-login", "-S", "ssh", "pi"}, "transport program"},
		{[]string{"--direct-login", "-D", "/usr/libexec/sftp-server", "pi"}, "SFTP server program"},
		{[]string{"--direct-login", "-o", "RemoteCommand=echo hi", "pi"}, "remote command"},
		{[]string{"--direct-login", "-oRefuseConnection=no", "pi"}, "RefuseConnection"},
		{[]string{"--direct-login", "-oCiphers=aes128-ctr", "pi"}, "algorithms"},
		{[]string{"--direct-login", "-oCompression=no", "-oAddressFamily=any", "-oRequestTTY=force", "pi"}, "pipe/no-TTY"},
	} {
		code, output := runApp(t, tc.args...)
		if code != 1 || !strings.Contains(output, tc.diagnostic) || strings.Contains(output, "Password (hidden") {
			t.Errorf("%q: %d %q", tc.args, code, output)
		}
	}
}
