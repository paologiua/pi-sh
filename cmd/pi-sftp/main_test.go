package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
