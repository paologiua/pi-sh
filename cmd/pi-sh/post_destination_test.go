package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPostDestinationOptionsAgainstOpenSSH(t *testing.T) {
	ssh, err := exec.LookPath("ssh")
	if err != nil {
		t.Skip("local OpenSSH unavailable")
	}
	for i, tail := range [][]string{
		{"host", "-T"}, {"host", "-tt"}, {"host", "-nT"}, {"host", "-enone"},
		{"-t", "host", "-t"}, {"host", "-oRequestTTY=force", "-T"},
		{"host", "-T", "-oRequestTTY=force"}, {"host", "-o", "StdinNull=yes", "command", "-T"},
		{"host", "command", "-tt"}, {"--", "host", "-tt"}, {"host", "--", "-tt"},
		{"host", "-T", "--", "-tt"}, {"host", "-Fnone", "-n"},
	} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			args := append([]string{"-G", "-Fnone"}, tail...)
			reference, err := exec.Command(ssh, args...).Output()
			if err != nil {
				t.Fatal("offline reference failed", err)
			}
			opts, err := parseOptions(args)
			if err != nil {
				t.Fatal(err)
			}
			var ours bytes.Buffer
			if err := opts.writeConfig(&ours); err != nil {
				t.Fatal(err)
			}
			values := map[string]string{}
			for _, line := range strings.Split(string(reference), "\n") {
				key, value, _ := strings.Cut(line, " ")
				values[key] = value
			}
			for _, line := range strings.Split(strings.TrimSpace(ours.String()), "\n") {
				key, value, _ := strings.Cut(line, " ")
				if value != values[key] {
					t.Fatalf("%s differs: pi-sh %q OpenSSH %q", key, value, values[key])
				}
			}
		})
	}
}

func TestPostDestinationCommandBoundariesAndRefusal(t *testing.T) {
	for _, tc := range []struct{ args, want []string }{
		{[]string{"device", "-T", "printf", "-tt", "--browser"}, []string{"device", "printf", "-tt", "--browser"}},
		{[]string{"device", "--", "-T", "-V"}, []string{"device", "-T", "-V"}},
		{[]string{"--", "device", "-T", "-V"}, []string{"device", "-T", "-V"}},
		{[]string{"device", "printf", "--", "-T"}, []string{"device", "printf", "--", "-T"}},
	} {
		opts, err := parseOptions(append([]string{"--browser", "--experimental-command"}, tc.args...))
		if err != nil || !reflect.DeepEqual(opts.args, tc.want) || opts.inspection != "" {
			t.Fatalf("command boundary changed: %+v %v", opts, err)
		}
	}
	for _, tail := range [][]string{{"device", "-z"}, {"device", "--unknown"}, {"device", "-oLocalCommand=PRIVATE"}, {"device", "-F"}, {"device", "-o"}} {
		if _, err := parseOptions(append([]string{"-G"}, tail...)); err == nil {
			t.Fatal("unsupported post-host option treated as a command", tail)
		}
	}
	// Custom modes follow the same option/command boundary, and do not need to
	// precede the exact destination when no command has started yet.
	opts, err := parseOptions([]string{"device", "--browser", "--experimental-command", "-T", "true"})
	if err != nil || !opts.browser || !opts.broker || opts.tty != "no" || !reflect.DeepEqual(opts.args, []string{"device", "true"}) {
		t.Fatal("custom mode placement lost", opts, err)
	}
}

func TestPostDestinationConfigAndImmediateInspection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte("Host alias\nHostName actual-device\nRequestTTY force\n"), 0600); err != nil {
		t.Fatal(err)
	}
	opts, err := parseOptions([]string{"-G", "alias", "-F", path, "-T"})
	if err != nil || opts.destination() != "actual-device" || opts.tty != "no" {
		t.Fatal("post-host config/precedence failed", opts, err)
	}
	missing := filepath.Join(t.TempDir(), "missing-config")
	for _, flag := range []string{"-V", "-Qhelp"} {
		code, out, diagnostic := runInspection(t, "-F", missing, "device", flag, "-z")
		if code != 0 || flag == "-V" && (out != "" || !strings.HasPrefix(diagnostic, "pi-sh_")) || flag == "-Qhelp" && (out == "" || diagnostic != "") {
			t.Fatal("post-host inspection loaded config or lost early exit", code, out, diagnostic)
		}
	}
}
