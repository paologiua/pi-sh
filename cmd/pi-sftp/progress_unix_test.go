//go:build darwin || linux

package main

import (
	"github.com/creack/pty"
	"os"
	"testing"
)

func TestSFTPProgressRequiresTerminalDiagnostics(t *testing.T) {
	master, terminal, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer terminal.Close()
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer read.Close()
	defer write.Close()
	if sftpProgress(terminal, write) != nil {
		t.Fatal("redirected stderr enabled progress")
	}
	if sftpProgress(terminal, terminal) == nil {
		t.Fatal("terminal stdout/stderr disabled progress")
	}
}
