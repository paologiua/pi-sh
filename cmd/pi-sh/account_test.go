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

	"pi-sh/internal/account"
	"pi-sh/internal/connpolicy"
	"pi-sh/internal/fwdbroker/fwdtest"
)

// TestMain isolates every test from the developer's own default account
// (PI_SH_ACCOUNT or ~/.config/pi-sh/account); an empty value disables both.
func TestMain(m *testing.M) {
	// Device-side tests run this binary as the connector program and setsid.
	switch filepath.Base(os.Args[0]) {
	case "nc":
		os.Exit(fwdtest.Netcat(os.Args[1:]))
	case "setsid":
		os.Exit(fwdtest.Setsid(os.Args[1:]))
	}
	os.Setenv(account.EnvName, "")
	os.Exit(m.Run())
}

// -l, User and URI users are first obtained in command-line order, as in
// ssh.c. With email values they are accepted, and -G prints the same user
// line as local OpenSSH.
func TestAccountPrecedenceOpenSSHDifferential(t *testing.T) {
	ssh, err := exec.LookPath("ssh")
	if err != nil {
		t.Skip("local OpenSSH unavailable")
	}
	path := filepath.Join(t.TempDir(), "config")
	text := "Host other\nUser pi\nHost alias\nHostName device\nUser file@example.com\n" +
		"Match user *@example.org\nStdinNull yes\n"
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"-G", "-F", "none", "-l", "a@example.com", "pi"},
		{"-G", "-F", "none", "pi", "-l", "a@example.com"},
		{"-G", "-F", "none", "-l", "a@example.com", "-l", "b@example.com", "pi"},
		{"-G", "-F", "none", "-oUser=c@example.com", "-l", "a@example.com", "pi"},
		{"-G", "-F", "none", "-l", "a@example.com", "-oUser=c@example.com", "pi"},
		{"-G", "-F", "none", "ssh://u%40example.com@pi", "-l", "a@example.com"},
		{"-G", "-F", "none", "-l", "a@example.com", "ssh://u%40example.com@pi"},
		{"-G", "-F", path, "alias"},
		{"-G", "-F", path, "-l", "a@example.org", "alias"},
		{"-G", "-F", path, "-oUser=x@example.org", "other"},
	} {
		want, err := openSSHDump(t, ssh, args)
		if err != nil {
			t.Fatal(args, err)
		}
		o, err := parseOptions(args)
		if err != nil {
			t.Fatalf("%q: %v", args, err)
		}
		var got strings.Builder
		if err := o.writeConfig(&got); err != nil {
			t.Fatal(err)
		}
		fields := map[string]string{}
		for _, line := range strings.Split(strings.TrimSpace(got.String()), "\n") {
			k, v, _ := strings.Cut(line, " ")
			fields[k] = v
		}
		for _, k := range []string{"user", "stdinnull"} {
			if fields[k] != want[k] {
				t.Errorf("%q %s: pi-sh %q, OpenSSH %q", args, k, fields[k], want[k])
			}
		}
	}
}

// The pi-sh account destination email::device and its precedence against -l
// (first obtained in command-line order), plus refusals of Unix-style users.
func TestAccountDestinationPolicy(t *testing.T) {
	for _, tc := range []struct {
		args         []string
		user, target string
	}{
		{[]string{"-G", "me@example.com::raspberrypi"}, "me@example.com", "raspberrypi"},
		{[]string{"-G", "me@example.com::Pi #1"}, "me@example.com", "Pi #1"},
		{[]string{"-G", "me@example.com::pi", "-l", "other@example.com"}, "me@example.com", "pi"},
		{[]string{"-G", "-l", "other@example.com", "me@example.com::pi"}, "other@example.com", "pi"},
		{[]string{"-G", "--", "me@example.com::pi"}, "me@example.com", "pi"},
		{[]string{"-G", "-l", "other@example.com", "--", "me@example.com::pi"}, "other@example.com", "pi"},
	} {
		o, err := parseOptions(tc.args)
		if err != nil || o.policy.Account != tc.user || o.destination() != tc.target {
			t.Errorf("%q: %+v %q %v", tc.args, o.policy, o.destination(), err)
		}
	}
	for _, tc := range []struct {
		args []string
		want error
	}{
		{[]string{"-G", "pi@raspberrypi"}, connpolicy.ErrUser},
		{[]string{"-G", "-l", "me@example.com", "pi@raspberrypi"}, connpolicy.ErrUser},
		{[]string{"-G", "me@example.com@raspberrypi"}, connpolicy.ErrUser},
		{[]string{"-G", "-l", "pi", "raspberrypi"}, connpolicy.ErrUser},
		{[]string{"-G", "ssh://pi@raspberrypi"}, connpolicy.ErrUser},
		{[]string{"-G", "bad@local::raspberrypi"}, account.ErrInvalid},
		{[]string{"-G", "me@example.com::"}, nil},
	} {
		_, err := parseOptions(tc.args)
		if err == nil || (tc.want != nil && !errors.Is(err, tc.want)) {
			t.Errorf("%q: %v", tc.args, err)
		}
	}
	// The Unix-style refusal names the replacement forms.
	if _, err := parseOptions([]string{"-G", "pi@raspberrypi"}); err == nil || !strings.Contains(err.Error(), "email::device") || !strings.Contains(err.Error(), "-l email") {
		t.Errorf("refusal must point to email::device and -l email: %v", err)
	}
}

// The local default (PI_SH_ACCOUNT here) is the lowest-precedence user: it
// is printed by -G, seen by Match user, and overridden by any explicit one.
func TestDefaultAccount(t *testing.T) {
	t.Setenv(account.EnvName, "default@example.com")
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte("Match user default@example.com\nStdinNull yes\n"), 0600); err != nil {
		t.Fatal(err)
	}
	o, err := parseOptions([]string{"-G", "-F", path, "pi"})
	if err != nil || o.policy.Account != "" || o.policy.LoginAccount() != "default@example.com" || !o.stdinNull {
		t.Fatalf("%+v %v", o.policy, err)
	}
	var out strings.Builder
	if err := o.writeConfig(&out); err != nil || !strings.HasPrefix(out.String(), "user default@example.com\nhostname pi\n") {
		t.Fatalf("%q %v", out.String(), err)
	}
	o, err = parseOptions([]string{"-G", "-F", path, "me@example.com::pi"})
	if err != nil || o.policy.LoginAccount() != "me@example.com" || o.stdinNull {
		t.Fatalf("explicit account must win: %+v %v", o.policy, err)
	}
	t.Setenv(account.EnvName, "not-an-email")
	if _, err := parseOptions([]string{"-G", "pi"}); err == nil || !strings.Contains(err.Error(), account.EnvName) {
		t.Fatal("an invalid default must fail, not be ignored", err)
	}
}

// --browser refuses an explicit account before any listener or prompt; a
// local default is not an explicit selection and does not block it.
func TestBrowserRefusesExplicitAccount(t *testing.T) {
	for _, args := range [][]string{
		{"--browser", "me@example.com::device"},
		{"--browser", "-l", "me@example.com", "device"},
		{"--browser", "-oUser=me@example.com", "device"},
	} {
		oldArgs, oldErrors := os.Args, os.Stderr
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		os.Args = append([]string{"pi-sh"}, args...)
		os.Stderr = w
		started := time.Now()
		code := app()
		os.Args, os.Stderr = oldArgs, oldErrors
		w.Close()
		output, err := io.ReadAll(r)
		r.Close()
		if err != nil || code != 255 || !strings.Contains(string(output), "cannot select or verify the Raspberry Pi ID account") ||
			strings.Contains(string(output), "fetch(") || time.Since(started) > 5*time.Second {
			t.Errorf("%q: %d %q", args, code, output)
		}
	}
	t.Setenv(account.EnvName, "default@example.com")
	o, err := parseOptions([]string{"--browser", "device"})
	if err != nil || o.policy.Admit(false) != nil {
		t.Fatal("the local default must not block the browser companion", err)
	}
}
