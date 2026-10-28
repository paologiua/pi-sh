package main

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestOptionsBoundariesAndValidation(t *testing.T) {
	o, err := parseOptions([]string{"--direct-login", "--experimental-command", "-nTe!", "--", "-device", "printf", "%s", "-n", "two words"})
	if err != nil {
		t.Fatal(err)
	}
	if !o.stdinNull || o.tty != "no" || o.escape != "!" || !reflect.DeepEqual(o.args, []string{"-device", "printf", "%s", "-n", "two words"}) {
		t.Fatalf("argument boundaries lost: %+v", o)
	}
	for _, args := range [][]string{
		{"--browser", "-e"}, {"--browser", "-o"}, {"--browser", "-F"},
		{"--browser", "-F", "config"}, {"--browser", "-R/tmp/r.sock:localhost:80", "pi"}, {"--browser", "-\x1b"},
		{"--browser", "-oProxyCommand=echo secret"}, {"--browser", "-oStdinNull=maybe"},
		{"--browser", "-oRequestTTY=maybe"}, {"--browser", "-oEscapeChar=ab"},
		{"--browser", "-oStdinNull=yes extra"}, {"--browser", "-oStdinNull=yes\nLocalCommand=x"},
		{"--browser", "--direct-login"}, {"--browser", "--experimental-command", "--legacy-command"},
		{"--browser", "--legacy-shell", "-T", "device"}, {"--browser", "--legacy-shell", "-n", "device"},
		{"--browser", "--legacy-shell", "device", "true"}, {"--browser", "--legacy-shell", "--experimental-command", "device", "true"},
		{"--browser", "--legacy-command", "-tt", "device", "true"},
		{"--browser", "--legacy-command", "-T", "device", "true"},
		{"--browser", "device", "echo"}, {"--browser", "--experimental-command", "device"},
		{"-G", "-F", ""}, {"-G", "-F", "none", "device\nInjected"}, {"-G", "-oHostName=%h"},
	} {
		if _, err := parseOptions(args); err == nil {
			t.Errorf("unsupported/malformed invocation accepted: %q", args)
		}
	}
}

func TestConfigFileOpenSSHDifferential(t *testing.T) {
	ssh, err := exec.LookPath("ssh")
	if err != nil {
		t.Skip("local OpenSSH unavailable")
	}
	for _, tc := range []struct {
		name, text, host string
		flags            []string
	}{
		{"matching", "Host pi-* ?ox !pi-excluded\nHostName device-%h\nEscapeChar !\nStdinNull yes\nHost *\nEscapeChar none\nRequestTTY no\n", "pi-one", nil},
		{"negation", "Host pi-* !pi-excluded\nEscapeChar !\nHost *\nEscapeChar none\n", "pi-excluded", nil},
		{"only-negative", "Host !blocked\nEscapeChar !\nHost *\nEscapeChar none\n", "other", nil},
		{"global-first", "EscapeChar !\nHost *\nEscapeChar none\nHostName device\n", "alias", nil},
		{"cli-wins", "Host alias\nHostName device\nEscapeChar !\nStdinNull no\nRequestTTY force\n", "alias", []string{"-enone", "-nT", "-oHostName=override"}},
		{"tokens", "HostName device-%h-%%\n", "alias", nil},
		{"quotes-comments", "  eScApEcHaR = \"#\" # comment\r\nHost = alias other\r\nHostName \"device\"\r\nStdinNull = yes # comment\r\n", "alias", nil},
		{"pattern-case", "Host PI-*\nEscapeChar !\nHost *\nEscapeChar none\n", "pi-one", nil},
		{"no-cascade", "Host alias\nHostName device\nHost device\nEscapeChar !\nHost *\nEscapeChar none\n", "alias", nil},
		{"literal-brackets", "Host [ab]\nEscapeChar !\nHost *\nEscapeChar none\n", "a", nil},
		{"equals-and-comment", "Host alias\nStdinNull yes #comment\nHostName=device\nEscapeChar==\n", "alias", nil},
		{"quote-adjacent", "Host alias\nHostName \"device-\"%h\n", "alias", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "configuration with spaces")
			if err := os.WriteFile(path, []byte(tc.text), 0600); err != nil {
				t.Fatal(err)
			}
			args := append([]string{"-G", "-F", path}, tc.flags...)
			args = append(args, tc.host)
			out, err := exec.Command(ssh, args...).Output()
			if err != nil {
				if failure, ok := err.(*exec.ExitError); ok {
					t.Fatalf("reference: %v: %s", err, failure.Stderr)
				}
				t.Fatal(err)
			}
			o, err := parseOptions(args)
			if err != nil {
				t.Fatal(err)
			}
			var got bytes.Buffer
			if err := o.writeConfig(&got); err != nil {
				t.Fatal(err)
			}
			want := map[string]string{}
			for _, line := range strings.Split(string(out), "\n") {
				key, value, _ := strings.Cut(line, " ")
				want[key] = value
			}
			for _, line := range strings.Split(strings.TrimSpace(got.String()), "\n") {
				key, value, _ := strings.Cut(line, " ")
				if value != want[key] {
					t.Errorf("%s: pi-sh %q, OpenSSH %q", key, value, want[key])
				}
			}
		})
	}
}

func TestConfigRejectsLikeOpenSSH(t *testing.T) {
	ssh, err := exec.LookPath("ssh")
	if err != nil {
		t.Skip("local OpenSSH unavailable")
	}
	for _, text := range []string{"StdinNull yes#comment\n", "StdinNull yes extra\n", "HostName \"unterminated\n", "RequestTTY invalid\n", "EscapeChar invalid\n", "UnknownDirective value\n"} {
		path := filepath.Join(t.TempDir(), "config")
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
		args := []string{"-G", "-F", path, "alias"}
		if _, err := exec.Command(ssh, args...).Output(); err == nil {
			t.Fatalf("reference accepted malformed config %q", text)
		}
		if _, err := parseOptions(args); err == nil {
			t.Fatalf("pi-sh accepted malformed config %q", text)
		}
	}
}

func TestAliasResolutionAndConfigRefusal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte("Host desktop\nHostName \"Pi #1\"\nEscapeChar none\n"), 0600); err != nil {
		t.Fatal(err)
	}
	o, err := parseOptions([]string{"--direct-login", "--experimental-command", "-F", path, "desktop", "printf", "'%s'", "hello"})
	if err != nil || o.destination() != "Pi #1" || o.args[0] != "desktop" || o.escape != "none" {
		t.Fatalf("alias target/command changed: %+v %v", o, err)
	}
	o, err = parseOptions([]string{"-G", "-F", path, "-Fnone", "desktop"})
	if err != nil || o.destination() != "desktop" || o.escape != "~" {
		t.Fatal("last -F none must suppress file selection")
	}
	for _, text := range []string{"Host *\nProxyCommand sensitive-command", "Host other\nInclude secret-path%r"} {
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
		_, err := parseOptions([]string{"--direct-login", "--experimental-command", "-F", path, "desktop", "true"})
		if err == nil || strings.Contains(err.Error(), "sensitive-command") || strings.Contains(err.Error(), "secret-path") {
			t.Fatalf("config did not fail safely: %v", err)
		}
	}
}

type unreadableInput struct{ t *testing.T }

func (r unreadableInput) Read([]byte) (int, error) {
	r.t.Fatal("-n read local stdin")
	return 0, io.EOF
}

func TestStdinNullNeverReadsInput(t *testing.T) {
	o, err := parseOptions([]string{"--direct-login", "--experimental-command", "-n", "device", "cat"})
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(o.stdin(unreadableInput{t}))
	if err != nil || len(data) != 0 {
		t.Fatalf("null stdin: %q, %v", data, err)
	}
	o.stdinNull = false
	data, err = io.ReadAll(o.stdin(strings.NewReader("\x00\xff\r\n")))
	if err != nil || string(data) != "\x00\xff\r\n" {
		t.Fatal("ordinary stdin changed")
	}
}

func TestRequestTTYPolicy(t *testing.T) {
	for _, policy := range []string{"auto", "no", "yes", "force"} {
		for _, terminal := range []bool{false, true} {
			for _, null := range []bool{false, true} {
				for _, command := range []bool{false, true} {
					o := options{tty: policy, stdinNull: null, broker: command}
					got, warning := o.requestTTY(terminal)
					want := policy == "force" || ((policy == "yes" || (policy == "auto" && !command)) && terminal && !null)
					// OpenSSH logs the notice whenever a TTY was wanted but suppressed.
					wantWarning := (policy == "yes" || (policy == "auto" && !command)) && (!terminal || null)
					if got != want || warning != wantWarning {
						t.Fatalf("%s terminal=%v null=%v command=%v: %v/%v want %v/%v", policy, terminal, null, command, got, warning, want, wantWarning)
					}
				}
			}
		}
	}
	// A login shell, like ssh without a command, accepts every TTY policy and -n.
	for _, flags := range [][]string{{}, {"-T"}, {"-n"}, {"-t"}, {"-tt"}, {"-nT"}, {"-oRequestTTY=no"}} {
		args := append([]string{"--direct-login"}, flags...)
		if _, err := parseOptions(append(args, "device")); err != nil {
			t.Fatal("login shell policy rejected", flags, err)
		}
	}
	for _, flags := range [][]string{{"-t"}, {"-tt"}, {"-n", "-oRequestTTY=force"}} {
		args := append([]string{"--direct-login", "--experimental-command"}, flags...)
		args = append(args, "device", "true")
		if _, err := parseOptions(args); err != nil {
			t.Fatal("TTY command rejected", err)
		}
	}
}

// ssh -G does not authenticate or open a network connection. -F none excludes
// account/system configuration; compare only our explicitly supported fields.
func TestOptionsOpenSSHDifferential(t *testing.T) {
	ssh, err := exec.LookPath("ssh")
	if err != nil {
		t.Skip("local OpenSSH unavailable")
	}
	for _, args := range [][]string{
		{}, {"-nTe!"}, {"-tt"}, {"-tT"}, {"-Tt"}, {"-e", "^]"},
		{"-o", "eScApEcHaR = \"none\"", "-oStdinNull=yes", "-oRequestTTY=force"},
		{"-oEscapeChar=!", "-oEscapeChar=none", "-oStdinNull=no", "-oStdinNull=yes"},
		{"-e!", "-oEscapeChar=none", "-n", "-oStdinNull=no", "-T", "-oRequestTTY=force"},
		{"-oEscapeChar=none", "-e!", "-oStdinNull=no", "-n", "-oRequestTTY=no", "-t"},
	} {
		command := append([]string{"-G", "-F", "none"}, args...)
		command = append(command, "example.invalid")
		out, err := exec.Command(ssh, command...).Output()
		if err != nil {
			t.Fatalf("ssh -G %q: %v", args, err)
		}
		o, err := parseOptions(command)
		if err != nil {
			t.Fatal(err)
		}
		var got bytes.Buffer
		if err := o.writeConfig(&got); err != nil {
			t.Fatal(err)
		}
		want := map[string]string{}
		for _, line := range strings.Split(string(out), "\n") {
			key, value, _ := strings.Cut(line, " ")
			want[key] = value
		}
		for _, line := range strings.Split(strings.TrimSpace(got.String()), "\n") {
			key, value, _ := strings.Cut(line, " ")
			if value != want[key] {
				t.Errorf("%q: %s = %q, OpenSSH %q", args, key, value, want[key])
			}
		}
	}
}

func TestEscapeDumpOpenSSHDifferential(t *testing.T) {
	ssh, err := exec.LookPath("ssh")
	if err != nil {
		t.Skip("local OpenSSH unavailable")
	}
	for c := 1; c < 128; c++ {
		args := []string{"-G", "-Fnone", "-e", string(rune(c)), "example.invalid"}
		out, err := exec.Command(ssh, args...).Output()
		if err != nil {
			t.Fatalf("reference escape %d: %v", c, err)
		}
		o, err := parseOptions(args)
		if err != nil {
			t.Fatal(err)
		}
		var got bytes.Buffer
		if err := o.writeConfig(&got); err != nil {
			t.Fatal(err)
		}
		var want string
		for _, line := range strings.Split(string(out), "\n") {
			if strings.HasPrefix(line, "escapechar ") {
				want = line + "\n"
			}
		}
		if !strings.Contains(got.String(), want) || want == "" {
			t.Errorf("escape %d: got %q; want %q", c, got.String(), want)
		}
	}
}
