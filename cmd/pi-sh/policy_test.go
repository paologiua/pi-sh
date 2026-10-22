package main

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pi-sh/internal/connpolicy"
)

func TestDestinationUserPortAndAddressPolicy(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"-G", "ssh://pi"}, "pi"}, {[]string{"-G", "ssh://pi/"}, "pi"}, {[]string{"-G", "ssh://[pi]"}, "pi"},
		{[]string{"-G", "Pi #1"}, "Pi #1"}, {[]string{"--direct-login", "ssh://pi:"}, "pi"},
	} {
		o, err := parseOptions(tc.args)
		if err != nil || o.destination() != tc.want {
			t.Errorf("%q: %q %v", tc.args, o.destination(), err)
		}
	}
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte("Host other\nUser pi\nPort 2222\nHost pi\nHostName device\nHost addr\nHostName ::1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if o, err := parseOptions([]string{"-G", "-F", path, "ssh://pi"}); err != nil || o.destination() != "device" {
		t.Fatal("URI host must select Host blocks; inactive User/Port must not apply", err)
	}
	for _, tc := range []struct {
		args []string
		want error
	}{
		{[]string{"--direct-login", "alice@pi"}, connpolicy.ErrUser},
		{[]string{"--direct-login", "-l", "pi", "pi"}, connpolicy.ErrUser},
		{[]string{"--direct-login", "pi", "-lpi"}, connpolicy.ErrUser},
		{[]string{"-G", "-oUser=pi", "pi"}, connpolicy.ErrUser},
		{[]string{"-G", "ssh://alice@pi"}, connpolicy.ErrUser},
		{[]string{"--direct-login", "-p", "22", "pi"}, connpolicy.ErrPort},
		{[]string{"-G", "-oPort=22", "pi"}, connpolicy.ErrPort},
		{[]string{"-G", "ssh://pi:22"}, connpolicy.ErrPort},
		{[]string{"-G", "-F", path, "other"}, connpolicy.ErrUser},
		{[]string{"-G", "-F", path, "addr"}, connpolicy.ErrAddress},
		{[]string{"-G", "ssh://[::1]"}, nil},
		{[]string{"-G", "-p0", "pi"}, nil},
		{[]string{"-G", "@pi"}, nil},
		{[]string{"-G", "ssh://pi/path"}, nil},
		{[]string{"-G", "-oTCPKeepAlive=no", "pi"}, nil},
		{[]string{"-G", "-oChannelTimeout=session=1m", "pi"}, nil},
		{[]string{"-G", "-oUnusedConnectionTimeout=1m", "pi"}, nil},
		{[]string{"-G", "-oNumberOfPasswordPrompts=3", "pi"}, nil},
		{[]string{"-G", "-oConnectionAttempts=11", "pi"}, nil},
	} {
		_, err := parseOptions(tc.args)
		if err == nil || (tc.want != nil && !errors.Is(err, tc.want)) {
			t.Errorf("%q: %v", tc.args, err)
		}
	}
}

// The -G output for accepted connection-policy settings matches local ssh -G.
func TestPolicyDumpOpenSSHDifferential(t *testing.T) {
	ssh, err := exec.LookPath("ssh")
	if err != nil {
		t.Skip("local OpenSSH unavailable")
	}
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte("Host alias\nHostName device\nConnectTimeout 1m\nServerAliveInterval 15\nServerAliveCountMax 0\nBatchMode yes\nHost *\nConnectTimeout 9\nConnectionAttempts 4\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"-G", "-F", path, "alias"},
		{"-G", "-F", path, "-oConnectTimeout=3", "-oBatchMode=no", "alias"},
		{"-G", "-F", "none", "-oTCPKeepAlive=yes", "-oChannelTimeout=none", "ssh://pi"},
	} {
		out, err := exec.Command(ssh, args...).Output()
		if err != nil {
			t.Fatal(args, err)
		}
		want := map[string]string{}
		for _, line := range strings.Split(string(out), "\n") {
			k, v, _ := strings.Cut(line, " ")
			want[k] = v
		}
		o, err := parseOptions(args)
		if err != nil {
			t.Fatal(args, err)
		}
		var got strings.Builder
		if err := o.writeConfig(&got); err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(strings.TrimSpace(got.String()), "\n") {
			k, v, _ := strings.Cut(line, " ")
			if want[k] != v && !(k == "hostname" && strings.EqualFold(want[k], v)) {
				t.Errorf("%q %s: pi-sh %q, OpenSSH %q", args, k, v, want[k])
			}
		}
	}
}

// BatchMode fails with status 255 before any prompt, listener or request.
func TestBatchModeExitsWithoutPrompting(t *testing.T) {
	for _, mode := range []string{"--direct-login", "--browser"} {
		oldArgs, oldErrors := os.Args, os.Stderr
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		os.Args = []string{"pi-sh", mode, "-oBatchMode=yes", "device"}
		os.Stderr = w
		started := time.Now()
		code := app()
		os.Args, os.Stderr = oldArgs, oldErrors
		w.Close()
		output, err := io.ReadAll(r)
		r.Close()
		if err != nil || code != 255 || !strings.Contains(string(output), "BatchMode") ||
			strings.Contains(string(output), "Password (hidden") || strings.Contains(string(output), "fetch(") || time.Since(started) > 5*time.Second {
			t.Fatalf("%s: %d %q", mode, code, output)
		}
	}
}
