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
		os.Args = append([]string{"pi-cp", "--direct-login", "-prrp"}, options...)
		os.Args = append(os.Args, "source", "alias:target")
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
