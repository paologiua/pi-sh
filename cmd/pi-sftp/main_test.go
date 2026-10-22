package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pi-sh/internal/diag"
	"pi-sh/internal/sftpcli"
)

func TestAttachedOptionsAndIncludesReachConfigurationBeforeLogin(t *testing.T) {
	config := filepath.Join(t.TempDir(), "config")
	leaf := filepath.Join(filepath.Dir(config), "included")
	if err := os.WriteFile(leaf, []byte("Host alias\nHostName actual-device\nRequestTTY force\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, []byte("Include \""+filepath.ToSlash(leaf)+"\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, options := range [][]string{{"-F" + config}, {"-Fnone", "-oRequestTTY=force"}} {
		oldArgs, oldErrors := os.Args, os.Stderr
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		os.Args = append([]string{"pi-sftp", "--direct-login", "-afpb-"}, options...)
		os.Args = append(os.Args, "alias")
		os.Stderr = w
		code := app()
		os.Args, os.Stderr = oldArgs, oldErrors
		w.Close()
		output, err := io.ReadAll(r)
		r.Close()
		if err != nil || code != 1 || !strings.Contains(string(output), "pipe/no-TTY mode") {
			t.Fatalf("attached config not applied before login: %d %s %v", code, output, err)
		}
	}
}

func TestDestinationAliasesAndIncompatibleModes(t *testing.T) {
	file := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(file, []byte("Host desk\nHostName Pi-name\nEscapeChar none\nRequestTTY no\n"), 0600); err != nil {
		t.Fatal(err)
	}
	target, err := destination("desk", file, nil)
	if err != nil || target != "Pi-name" {
		t.Fatal(target, err)
	}
	target, err = destination("desk", file, []string{"HostName=override", "HostName=ignored"})
	if err != nil || target != "override" {
		t.Fatal("CLI precedence lost", target, err)
	}
	for _, options := range [][]string{{"RequestTTY=force"}, {"StdinNull=yes"}, {"ProxyCommand=secret"}, {"HostName=%n"}, {"User=other"}} {
		if _, err := destination("desk", "none", options); err == nil {
			t.Fatal("unsupported/incompatible option accepted", options)
		}
	}
	if _, err := destination("desk", filepath.Join(t.TempDir(), "missing"), nil); err == nil {
		t.Fatal("missing config accepted")
	}
}

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
	output, _ := io.ReadAll(r)
	r.Close()
	return code, string(output)
}

// -v/-q mirror scp/sftp: repeatable/clusterable switches that only steer the
// connection layer; LogLevel is validated pre-authentication like ssh -o.
func TestVerboseQuietFlagsAndLogLevel(t *testing.T) {
	for _, flags := range [][]string{{"-vvv", "-q"}, {"-qv"}, {"-v", "-oLogLevel=QUIET"}, {"-oSyslogFacility=local0"}} {
		code, out := runApp(t, append(append([]string{"--direct-login"}, flags...), "-oRequestTTY=force", "alias")...)
		if code != 1 || !strings.Contains(out, "pipe/no-TTY mode") {
			t.Fatalf("%q: %d %s", flags, code, out)
		}
	}
	for _, flags := range [][]string{{"-oLogLevel=bogus"}, {"-q", "-oLogLevel=SILENT"}, {"-oSyslogFacility=bogus"}, {"-v=false"}, {"-E", "x"}, {"-y"}} {
		if code, out := runApp(t, append(append([]string{"--direct-login"}, flags...), "alias")...); code != 1 || strings.Contains(out, "pipe/no-TTY") {
			t.Fatalf("%q accepted: %d %s", flags, code, out)
		}
	}
	_, settings, err := sftpcli.Settings("alias", "none", []string{"LogLevel=debug2", "LogLevel=ERROR"})
	if err != nil || settings["loglevel"] != "debug2" {
		t.Fatalf("first obtained LogLevel lost: %v %v", settings, err)
	}
}

func TestSessionFailureReporting(t *testing.T) {
	for _, tc := range []struct {
		level diag.Level
		want  string
	}{
		{diag.Info, "pi-sftp: login failed password=[redacted]\r\n"},
		{diag.Fatal, "pi-sftp: login failed password=[redacted]\r\n"},
		{diag.Quiet, "pi-sftp: connection closed\n"},
	} {
		var out bytes.Buffer
		log, _ := diag.Open(diag.Config{Program: "pi-sftp", Level: tc.level, Stderr: &out})
		oldErrors := os.Stderr
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		os.Stderr = w
		sessionFailure(log, errors.New("login failed password=hunter2"))
		os.Stderr = oldErrors
		w.Close()
		direct, _ := io.ReadAll(r)
		r.Close()
		if got := out.String() + string(direct); got != tc.want {
			t.Errorf("%v: got %q want %q", tc.level, got, tc.want)
		}
	}
}
