//go:build darwin || linux

package sshconfig

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestIncludeRetainsSymlinkParentTraversalAndDetectsAliasCycles(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "real", "sub"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real/sub", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	configFile(t, dir, "target", "EscapeChar none\n")
	configFile(t, filepath.Join(dir, "real"), "target", "EscapeChar !\n")
	root := configFile(t, dir, "root", "Include "+includeName(dir+"/link/../target")+"\n")
	got, err := Load(root, "alias")
	if err != nil || got["escapechar"] != "!" {
		t.Fatal("Include normalized away symlink/.. traversal", got, err)
	}
	alias := filepath.Join(dir, "hard-link")
	if err := os.Link(root, alias); err != nil {
		t.Fatal(err)
	}
	configFile(t, dir, "root", "Include "+includeName(alias)+"\n")
	if _, err := Load(root, "alias"); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatal("hard-link cycle not detected", err)
	}
}

func TestIncludeFIFOIsNotOpenedAsDirectoryOrConfig(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	for _, pattern := range []string{fifo + "/*", fifo} {
		root := configFile(t, dir, "root", "Include "+includeName(pattern)+"\n")
		done := make(chan error, 1)
		go func() { _, err := Load(root, "alias"); done <- err }()
		select {
		case err := <-done:
			if pattern == fifo && err == nil {
				t.Fatal("FIFO accepted as regular config")
			}
			if pattern != fifo && err != nil {
				t.Fatal("non-directory glob parent should not match", err)
			}
		case <-time.After(time.Second):
			// Unblock a regression without leaving a stuck test goroutine.
			f, _ := os.OpenFile(fifo, os.O_RDWR|syscall.O_NONBLOCK, 0)
			if f != nil {
				f.Close()
			}
			t.Fatal("configuration opening blocked on FIFO")
		}
	}
}
