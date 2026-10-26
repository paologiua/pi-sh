package main

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// openSSHDump runs the local reference. ssh -G evaluates Match (including
// exec and localnetwork) locally; it neither authenticates nor connects.
func openSSHDump(t *testing.T, ssh string, args []string) (map[string]string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, ssh, args...).Output()
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
