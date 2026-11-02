package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// openSSHDump runs the local reference. ssh -G evaluates Match (including
// exec and localnetwork) locally; it neither authenticates nor connects.
func openSSHDump(t *testing.T, ssh string, args []string) (map[string]string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := referenceDumpContext(ctx, ssh, args)
	if err != nil {
		return nil, err
	}
	want := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		key, value, _ := strings.Cut(line, " ")
		want[key] = value
	}
	return want, nil
}

// referenceDump runs the local `ssh -G` reference and normalizes
// distribution-patched defaults to upstream OpenSSH, which pi-sh follows.
func referenceDump(ssh string, args []string) ([]byte, error) {
	return referenceDumpContext(context.Background(), ssh, args)
}

func referenceDumpContext(ctx context.Context, ssh string, args []string) ([]byte, error) {
	out, err := exec.CommandContext(ctx, ssh, args...).Output()
	if err == nil && debianTrustedDefault(ssh) && !mentionsOption(args, "forwardx11trusted") {
		out = bytes.Replace(out, []byte("\nforwardx11trusted yes\n"), []byte("\nforwardx11trusted no\n"), 1)
	}
	return out, err
}

var (
	debianTrustedOnce sync.Once
	debianTrusted     bool
)

// debianTrustedDefault reports whether the local OpenSSH was built with
// Debian's patch that makes ForwardX11Trusted default to yes (Debian, Ubuntu
// and GitHub's Ubuntu runners). Upstream OpenSSH, which pi-sh follows,
// defaults to no.
func debianTrustedDefault(ssh string) bool {
	debianTrustedOnce.Do(func() {
		out, err := exec.Command(ssh, "-G", "-F", "none", "pi").Output()
		debianTrusted = err == nil && strings.Contains(string(out), "\nforwardx11trusted yes\n")
	})
	return debianTrusted
}

// mentionsOption reports whether a case sets the option on the command line
// or in a -F file, in which case the reference value is kept as printed.
func mentionsOption(args []string, option string) bool {
	for i, arg := range args {
		text := arg
		if i > 0 && args[i-1] == "-F" {
			if data, err := os.ReadFile(arg); err == nil {
				text = string(data)
			}
		}
		if strings.Contains(strings.ToLower(text), option) {
			return true
		}
	}
	return false
}

func compareDump(t *testing.T, args []string, want map[string]string) {
	t.Helper()
	o, err := parseOptions(args)
	if err != nil {
		t.Fatalf("pi-sh rejected configuration OpenSSH accepted: %v", err)
	}
	var got bytes.Buffer
	if err := o.writeConfig(&got); err != nil {
		t.Fatal(err)
	}
	tag := false
	for _, line := range strings.Split(strings.TrimSpace(got.String()), "\n") {
		key, value, _ := strings.Cut(line, " ")
		if key == "hostname" {
			// OpenSSH lowercases the target; pi-sh keeps exact Connect names.
			value = strings.ToLower(value)
		}
		tag = tag || key == "tag"
		if value != want[key] {
			t.Errorf("%s: pi-sh %q, OpenSSH %q", key, value, want[key])
		}
	}
	if _, ok := want["tag"]; ok != tag {
		t.Errorf("tag presence: pi-sh %v, OpenSSH %v", tag, ok)
	}
}
