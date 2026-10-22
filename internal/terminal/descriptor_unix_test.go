//go:build darwin || linux

package terminal

import (
	"context"
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

func descriptorFlags(t *testing.T, file *os.File) int {
	t.Helper()
	raw, err := file.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var value int
	var getErr error
	if err := raw.Control(func(fd uintptr) { value, getErr = unix.FcntlInt(fd, unix.F_GETFL, 0) }); err != nil {
		t.Fatal(err)
	}
	if getErr != nil {
		t.Fatal(getErr)
	}
	return value
}

func TestTerminalQueriesPreserveDescriptorFlags(t *testing.T) {
	for _, name := range []string{"detect", "capture", "session"} {
		t.Run(name, func(t *testing.T) {
			_, file := openPTY(t)
			before := descriptorFlags(t, file)
			if before&unix.O_NONBLOCK == 0 {
				t.Fatal("fixture must exercise a Go-pollable nonblocking terminal")
			}
			switch name {
			case "detect":
				if !IsTerminal(file) {
					t.Fatal("terminal not detected")
				}
			case "capture":
				if _, err := CaptureModes(file); err != nil {
					t.Fatal(err)
				}
			case "session":
				s, err := Open(context.Background(), file, file)
				if err != nil {
					t.Fatal(err)
				}
				if err := s.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if got := descriptorFlags(t, file); got != before {
				t.Fatalf("query changed flags %x to %x", before, got)
			}
		})
	}
}
