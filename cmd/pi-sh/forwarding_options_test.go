package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"pi-sh/internal/forwardspec"
)

// Forwarding flags, -o list directives and file directives against local ssh -G.
// Each case has at most one forwarding of each kind, since compareDump keys
// lines by keyword.
func TestForwardingOptionsOpenSSHDifferential(t *testing.T) {
	ssh, err := exec.LookPath("ssh")
	if err != nil {
		t.Skip("local OpenSSH unavailable")
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "config")
	text := "Host pi\nLocalForward 9000 h:1\nDynamicForward 1080\nGatewayPorts yes\nMatch final\nLocalForward 9000 h:1\n"
	if err := os.WriteFile(file, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"-G", "-F", "none", "-L", "8080:localhost:80", "-D", "1080", "pi"},
		{"-G", "-F", "none", "-g", "-oGatewayPorts=no", "-L", "*:8080:h:80", "pi"},
		{"-G", "-F", "none", "-oGatewayPorts=no", "-g", "pi"},
		{"-G", "-F", "none", "-W", "h:22", "-L", "1:h:2", "pi"},
		{"-G", "-F", "none", "-t", "-W", "h:22", "pi"},
		{"-G", "-F", "none", "-W", "h:22", "-t", "pi"},
		{"-G", "-F", "none", "-N", "-oSessionType=default", "pi"},
		{"-G", "-F", "none", "-oSessionType=none", "-N", "pi"},
		{"-G", "-F", "none", "-oSessionType=none", "pi"},
		{"-G", "-F", "none", "-N", "-t", "pi"},
		{"-G", "-F", "none", "-t", "-N", "pi"},
		{"-G", "-F", "none", "-o", "LocalForward 6 h:7", "-o", "ExitOnForwardFailure yes", "pi"},
		{"-G", "-F", "none", "-oClearAllForwardings=yes", "-L", "1:h:2", "-D", "3", "pi"},
		{"-G", "-F", "none", "-oStreamLocalBindMask=022", "-oStreamLocalBindUnlink=yes", "-L", "/tmp/a.sock:h:80", "pi"},
		{"-G", "-F", "none", "-a", "-x", "-oForwardAgent=no", "-oControlMaster=no", "-oControlPath=none", "-oProxyCommand=none", "pi"},
		{"-G", "-F", file, "pi"},
		{"-G", "-F", file, "-L", "9000:h:1", "pi"},
		{"-G", "-F", "none", "-R", "8080:localhost:3000", "pi"},
		{"-G", "-F", "none", "-R", "*:0:h:22", "pi"},
		{"-G", "-F", "none", "-R", "1080", "pi"},
		{"-G", "-F", "none", "-R", "127.0.0.1:8080:/tmp/l.sock", "-oPermitRemoteOpen=h:22 *:80", "pi"},
		{"-G", "-F", "none", "-o", "RemoteForward [::1]:8080 h:22", "-oClearAllForwardings=yes", "-R", "1:h:2", "pi"},
	} {
		want, err := openSSHDump(t, ssh, args)
		if err != nil {
			t.Fatal(args, err)
		}
		compareDump(t, args, want)
	}
}

func TestForwardingOptionsSemantics(t *testing.T) {
	o, err := parseOptions([]string{"--browser", "-W", "h:22", "-L", "1:h:2", "-tt", "pi", "ignored", "words"})
	if err != nil {
		t.Fatal(err)
	}
	if !o.noSession() || !o.fwd.StdioSet || len(o.fwd.Local) != 0 || !o.fwd.ExitOnForwardFailure || len(o.args) != 1 {
		t.Fatalf("-W: %+v args %q", o.fwd, o.args)
	}
	o, err = parseOptions([]string{"--browser", "-N", "-L", "8080:localhost:80", "pi", "echo", "hi"})
	if err != nil || !o.noSession() || !o.forwarding() || len(o.args) != 1 {
		t.Fatalf("-N: %v %q", err, o.args)
	}
	// Forwarding alongside a session keeps the normal session rules.
	o, err = parseOptions([]string{"--browser", "-D", "1080", "pi"})
	if err != nil || o.noSession() || !o.forwarding() {
		t.Fatalf("-D with shell: %v", err)
	}
	if _, err := parseOptions([]string{"--browser", "-D", "1080", "pi", "echo"}); err == nil {
		t.Fatal("command without adapter accepted")
	}
	if _, err := parseOptions([]string{"-G", "-oSessionType=default", "-N", "pi"}); err == nil || err.Error() != "Cannot specify -N with -s/SessionType" {
		t.Fatalf("-N after SessionType: %v", err)
	}
	o, err = parseOptions([]string{"--browser", "-N", "-R", "8080:localhost:3000", "pi"})
	if err != nil || !o.forwarding() || len(o.fwd.Remote) != 1 {
		t.Fatalf("-N -R: %v", err)
	}
	if _, err := parseOptions([]string{"--browser", "-W", "h:1", "-W", "h:2", "pi"}); err == nil || err.Error() != "stdio forward already specified" {
		t.Fatalf("second -W: %v", err)
	}
	for _, args := range [][]string{
		{"--browser", "-N", "--legacy-shell", "pi"},
		{"--browser", "-L", "1:h:2", "--legacy-command", "pi", "true"},
	} {
		if _, err := parseOptions(args); err == nil {
			t.Errorf("%q accepted", args)
		}
	}
}

// Every forwarding-family flag and keyword has an explicit decision.
func TestForwardingRefusals(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want error
		text string
	}{
		{[]string{"-G", "-R", "/tmp/r.sock:h:22", "pi"}, errRemoteUnixListen, ""},
		{[]string{"-G", "-o", "RemoteForward /tmp/r.sock h:22", "pi"}, errRemoteUnixListen, ""},
		{[]string{"-G", "-A", "pi"}, forwardspec.ErrAgentForwarding, ""},
		{[]string{"-G", "-oForwardAgent=yes", "pi"}, forwardspec.ErrAgentForwarding, ""},
		{[]string{"-G", "-X", "pi"}, forwardspec.ErrX11Forwarding, ""},
		{[]string{"-G", "-Y", "pi"}, forwardspec.ErrX11Forwarding, ""},
		{[]string{"-G", "-M", "pi"}, forwardspec.ErrMultiplexing, ""},
		{[]string{"-G", "-S", "/tmp/ctl", "pi"}, forwardspec.ErrMultiplexing, ""},
		{[]string{"-G", "-O", "check", "pi"}, forwardspec.ErrMultiplexing, ""},
		{[]string{"-G", "-oControlMaster=auto", "pi"}, forwardspec.ErrMultiplexing, ""},
		{[]string{"-G", "-w", "0", "pi"}, forwardspec.ErrTunnel, ""},
		{[]string{"-G", "-J", "jump", "pi"}, forwardspec.ErrProxy, ""},
		{[]string{"-G", "-oProxyJump=jump", "pi"}, forwardspec.ErrProxy, ""},
		{[]string{"-G", "-oSessionType=subsystem", "pi"}, forwardspec.ErrSubsystem, ""},
		{[]string{"-G", "-L", "bad", "pi"}, nil, "Bad local forwarding specification 'bad'"},
		{[]string{"-G", "-oExitOnForwardFailure=maybe", "pi"}, nil, "unsupported option"},
		{[]string{"-G", "-oForwardX11Timeout=1", "pi"}, nil, "X11"},
	} {
		_, err := parseOptions(tc.args)
		if tc.want != nil && !errors.Is(err, tc.want) || tc.text != "" && (err == nil || !strings.Contains(err.Error(), tc.text)) {
			t.Errorf("%q: %v", tc.args, err)
		}
	}
	dir := t.TempDir()
	for _, text := range []string{"Host pi\nRemoteForward /tmp/r.sock h:22\n", "Host pi\nProxyCommand nc %h %p\n", "Host other\nLocalForward bad\n"} {
		file := filepath.Join(dir, "c")
		if err := os.WriteFile(file, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := parseOptions([]string{"-G", "-F", file, "pi"}); err == nil {
			t.Errorf("%q accepted", text)
		}
	}
}
