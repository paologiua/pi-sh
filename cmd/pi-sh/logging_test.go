package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func logFields(text string) map[string]string {
	fields := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		key, value, _ := strings.Cut(line, " ")
		if key == "loglevel" || key == "syslogfacility" {
			fields[key] = value
		}
	}
	return fields
}

// Offline differential against the installed OpenSSH `ssh -G`: -v/-q order,
// LogLevel/SyslogFacility values, first-obtained -o/file precedence and Host
// scoping. No connection or authentication is attempted by either client.
func TestLogLevelOpenSSHDifferential(t *testing.T) {
	ssh, err := exec.LookPath("ssh")
	if err != nil {
		t.Skip("local OpenSSH unavailable")
	}
	dir := t.TempDir()
	logPath := filepath.Join(dir, "reference.log")
	scoped := "Host host\nLogLevel ERROR\nSyslogFacility LOCAL3\nHost *\nLogLevel DEBUG3\nSyslogFacility DAEMON\n"
	for _, tc := range []struct {
		config string
		flags  []string
	}{
		{"", nil}, {"", []string{"-v"}}, {"", []string{"-vv"}}, {"", []string{"-vvv"}}, {"", []string{"-vvvvv"}},
		{"", []string{"-q"}}, {"", []string{"-qq"}}, {"", []string{"-q", "-v"}}, {"", []string{"-v", "-q"}},
		{"", []string{"-vqv"}}, {"", []string{"-v", "-q", "-v", "-v"}}, {"", []string{"-nvT"}},
		{"", []string{"-oLogLevel=ERROR"}}, {"", []string{"-v", "-oLogLevel=ERROR"}},
		{"", []string{"-oLogLevel=ERROR", "-v", "-v"}}, {"", []string{"-v", "-oLogLevel=QUIET", "-v"}},
		{"", []string{"-q", "-oLogLevel=DEBUG"}}, {"", []string{"-oLogLevel=DEBUG3", "-q", "-v"}},
		{"", []string{"-oLogLevel=debug1"}}, {"", []string{"-oLogLevel=DEBUG", "-oLogLevel=ERROR"}},
		{"", []string{"-oLogLevel=VERBOSE"}}, {"", []string{"-oLogLevel=FATAL"}}, {"", []string{"-oLogLevel=quiet"}},
		{"", []string{"-oLogLevel=DEBUG2"}}, {"", []string{"-o", "LogLevel = \"Info\""}},
		{"", []string{"-oSyslogFacility=local7"}}, {"", []string{"-oSyslogFacility=AUTHPRIV", "-oSyslogFacility=USER"}},
		{"", []string{"-y"}}, {"", []string{"-y", "-y", "-v"}}, {"", []string{"-E", logPath}}, {"", []string{"-E" + logPath, "-q"}},
		{scoped, nil}, {scoped, []string{"-v"}}, {scoped, []string{"-oLogLevel=VERBOSE"}}, {scoped, []string{"-q"}},
		{scoped, []string{"-oSyslogFacility=local0"}},
		{"LogLevel verbose\nSyslogFacility auth\n", []string{"-oLogLevel=info"}},
		{"Host other\nLogLevel DEBUG\nHost *\nLogLevel FATAL\n", nil},
	} {
		config := "none"
		if tc.config != "" {
			config = filepath.Join(dir, "config")
			if err := os.WriteFile(config, []byte(tc.config), 0600); err != nil {
				t.Fatal(err)
			}
		}
		args := append(append([]string{"-G", "-F", config}, tc.flags...), "host")
		out, err := exec.Command(ssh, args...).Output()
		if err != nil {
			t.Fatalf("%q: reference failed: %v", args, err)
		}
		want := logFields(string(out))
		if want["loglevel"] == "" || want["syslogfacility"] == "" {
			t.Fatalf("reference omitted log fields: %s", out)
		}
		o, err := parseOptions(args)
		if err != nil {
			t.Fatalf("%q: %v", args, err)
		}
		var got bytes.Buffer
		if err := o.writeConfig(&got); err != nil {
			t.Fatal(err)
		}
		if g := logFields(got.String()); g["loglevel"] != want["loglevel"] || g["syslogfacility"] != want["syslogfacility"] {
			t.Errorf("%q / %q: pi-sh %v, OpenSSH %v", tc.config, tc.flags, g, want)
		}
	}
}

func TestLogOptionsRejectedLikeOpenSSH(t *testing.T) {
	ssh, err := exec.LookPath("ssh")
	if err != nil {
		t.Skip("local OpenSSH unavailable")
	}
	dir := t.TempDir()
	for _, flags := range [][]string{
		{"-oLogLevel=bogus"}, {"-oLogLevel=SILENT"}, {"-oLogLevel=DEBUG4"}, {"-oLogLevel="},
		{"-oSyslogFacility=bogus"}, {"-oSyslogFacility=LOCAL8"}, {"-oSyslogFacility=KERN"},
		{"-y", "-E", filepath.Join(dir, "both.log")}, {"-E" + filepath.Join(dir, "both.log"), "-y"},
		{"-oLogLevel=INFO", "-oLogLevel=bogus"},
	} {
		args := append(append([]string{"-G", "-F", "none"}, flags...), "host")
		if exec.Command(ssh, args...).Run() == nil {
			t.Fatalf("reference accepted %q", flags)
		}
		if _, err := parseOptions(args); err == nil {
			t.Errorf("pi-sh accepted %q", flags)
		}
	}
	if _, err := parseOptions([]string{"-G", "-E"}); err == nil {
		t.Fatal("-E without an argument accepted")
	}
	for _, text := range []string{"LogLevel bogus\n", "LogLevel\n", "LogLevel INFO extra\n", "Host other\nLogLevel bogus\n", "SyslogFacility kern\n"} {
		path := filepath.Join(dir, "config")
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
		args := []string{"-G", "-F", path, "host"}
		if exec.Command(ssh, args...).Run() == nil {
			t.Fatalf("reference accepted %q", text)
		}
		if _, err := parseOptions(args); err == nil {
			t.Errorf("pi-sh accepted %q", text)
		}
	}
}

// Real subprocesses (offline: every case stops before authentication).
func TestLogDestinationsAndQuietSubprocess(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad-config-secret-path")
	if err := os.WriteFile(bad, []byte("LogLevel bogus\n"), 0600); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "client.log")
	if err := os.WriteFile(logPath, []byte("previous\n"), 0600); err != nil {
		t.Fatal(err)
	}

	// Configuration-file errors use the selected destination, as in ssh(1).
	code, out, stderr := runInspection(t, "-E", logPath, "-G", "-F", bad, "host")
	logged, _ := os.ReadFile(logPath)
	if code != 255 || out != "" || stderr != "" || !strings.HasPrefix(string(logged), "previous\npi-sh: ") ||
		!strings.HasSuffix(string(logged), "\r\n") || strings.Contains(string(logged), "secret-path") {
		t.Fatalf("-E config error: %d %q %q log %q", code, out, stderr, logged)
	}
	if code, out, stderr = runInspection(t, "-q", "-G", "-F", bad, "host"); code != 255 || out != "" || stderr != "" {
		t.Fatalf("-q config error: %d %q %q", code, out, stderr)
	}
	if code, _, stderr = runInspection(t, "-G", "-F", bad, "host"); code != 255 || !strings.HasPrefix(stderr, "pi-sh: ") || strings.Contains(stderr, "usage") {
		t.Fatalf("default config error: %d %q", code, stderr)
	}
	// Option errors are not routed through -q/-E, as with ssh -o errors.
	if code, _, stderr = runInspection(t, "-q", "-E", logPath, "-G", "-oLogLevel=bogus", "host"); code != 255 || !strings.Contains(stderr, "LogLevel") {
		t.Fatalf("option error: %d %q", code, stderr)
	}

	// Unopenable log file: exit 1, path and OS error text are not echoed.
	missing := filepath.Join(dir, "missing-dir-secret", "x.log")
	if code, _, stderr = runInspection(t, "-E", missing, "-G", "-Fnone", "host"); code != 1 || stderr != "pi-sh: cannot open log file: no such file or directory\n" {
		t.Fatalf("missing log dir: %d %q", code, stderr)
	}

	// -V exits before opening; -G opens (and creates privately) as ssh does.
	versionLog := filepath.Join(dir, "version.log")
	if code, _, _ = runInspection(t, "-E", versionLog, "-V"); code != 0 {
		t.Fatal("-V failed")
	}
	if _, err := os.Stat(versionLog); !os.IsNotExist(err) {
		t.Fatal("-V opened the log file")
	}
	fresh := filepath.Join(dir, "fresh.log")
	code, out, stderr = runInspection(t, "-vvv", "-E", fresh, "-G", "-Fnone", "host")
	logged, _ = os.ReadFile(fresh)
	if code != 0 || stderr != "" || !strings.Contains(out, "loglevel DEBUG3\n") ||
		!strings.Contains(string(logged), "debug1: unofficial client version=pi-sh_") ||
		!strings.Contains(string(logged), "debug2: configuration: effective diagnostics loglevel=DEBUG3 syslogfacility=USER logfile=true syslog=false\r\n") {
		t.Fatalf("-vvv -E: %d %q %q log %q", code, out, stderr, logged)
	}
	if info, err := os.Stat(fresh); err != nil || runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("log file not private: %v %v", info, err)
	}

	// -v writes debug to stderr and never the configuration path.
	good := filepath.Join(dir, "good-config-private-path")
	if err := os.WriteFile(good, []byte("Host host\nLogLevel DEBUG\n"), 0600); err != nil {
		t.Fatal(err)
	}
	code, out, stderr = runInspection(t, "-G", "-F", good, "host")
	if code != 0 || !strings.Contains(out, "loglevel DEBUG\n") || !strings.Contains(stderr, "debug1: configuration: reading explicit configuration file\r\n") || strings.Contains(stderr, "private-path") {
		t.Fatalf("config LogLevel: %d %q %q", code, out, stderr)
	}
	if code, _, stderr = runInspection(t, "-G", "-Fnone", "host"); code != 0 || stderr != "" {
		t.Fatalf("default -G emitted diagnostics: %q", stderr)
	}

	// Pre-authentication fatal error: reported by default, captured by -E,
	// suppressed by -q (exit status still 255), as ssh's fatal messages are.
	cmd := []string{"--browser", "--experimental-command", "-T", "device", "\xff"}
	if code, _, stderr = runInspection(t, cmd...); code != 255 || !strings.HasPrefix(stderr, "pi-sh: command must be UTF-8") || !strings.HasSuffix(stderr, "\r\n") {
		t.Fatalf("fatal default: %d %q", code, stderr)
	}
	if code, _, stderr = runInspection(t, append([]string{"-q"}, cmd...)...); code != 255 || stderr != "" {
		t.Fatalf("fatal -q: %d %q", code, stderr)
	}
	if code, _, stderr = runInspection(t, append([]string{"-oLogLevel=FATAL"}, cmd...)...); code != 255 || !strings.Contains(stderr, "UTF-8") {
		t.Fatalf("fatal LogLevel=FATAL: %d %q", code, stderr)
	}
	fatalLog := filepath.Join(dir, "fatal.log")
	code, _, stderr = runInspection(t, append([]string{"-E", fatalLog}, cmd...)...)
	logged, _ = os.ReadFile(fatalLog)
	if code != 255 || stderr != "" || !strings.Contains(string(logged), "command must be UTF-8") {
		t.Fatalf("fatal -E: %d %q %q", code, stderr, logged)
	}

	// -y with -E is refused before any file is opened.
	both := filepath.Join(dir, "both.log")
	if code, _, stderr = runInspection(t, "-y", "-E", both, "-G", "-Fnone", "host"); code != 255 || !strings.Contains(stderr, "both -y and -E") {
		t.Fatalf("-y -E: %d %q", code, stderr)
	}
	if _, err := os.Stat(both); !os.IsNotExist(err) {
		t.Fatal("-y -E created the log file")
	}
}
