//go:build darwin || linux

package main

import (
	"context"
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

func waitIOReady(t *testing.T, fd int, events int16, want bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		fds := []unix.PollFd{{Fd: int32(fd), Events: events}}
		_, err := unix.Poll(fds, 1)
		if err == unix.EINTR {
			continue
		}
		if err != nil || fds[0].Revents&unix.POLLNVAL != 0 {
			t.Fatal("invalid local I/O fixture", err)
		}
		if (fds[0].Revents&events != 0) == want {
			return
		}
	}
	t.Fatal("local descriptor did not reach expected readiness")
}

func TestInteractiveSessionQuiescesBorrowedIO(t *testing.T) {
	for _, name := range []string{"escape-idle", "remote-exit", "cancel-idle", "authorization-backpressure", "cancel-backpressure", "escape-backpressure", "escape-help"} {
		t.Run(name, func(t *testing.T) {
			lifetime, stop := context.WithTimeout(context.Background(), 15*time.Second)
			defer stop()
			ctx, cancel := context.WithCancel(lifetime)
			defer cancel()
			client, channel, _, _ := ttyPeers(t, lifetime)
			master, file, err := pty.Open()
			if err != nil {
				t.Fatal(err)
			}
			fd := int(file.Fd())
			masterFD := int(master.Fd())
			before, err := term.GetState(fd)
			if err != nil {
				t.Fatal(err)
			}
			failures := make(chan string, 1)
			done := make(chan error, 1)
			finished := false
			defer func() {
				cancel()
				if !finished {
					select {
					case <-done:
					case <-time.After(time.Second):
						master.Close()
						<-done
					}
				}
				master.Close()
				file.Close()
			}()
			var diagnosticFile *os.File
			if name == "escape-help" {
				diagnosticFile = file
			}
			go func() { done <- runInteractiveSession(ctx, failures, client, channel, file, file, diagnosticFile, '~') }()
			// Observe actual raw-mode acquisition, without touching File.Fd after
			// the I/O owner temporarily sets O_NONBLOCK on that file description.
			deadline := time.Now().Add(time.Second)
			for {
				now, err := term.GetState(fd)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(now, before) {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("interactive raw mode was not acquired")
				}
				time.Sleep(time.Millisecond)
			}
			switch name {
			case "escape-help":
				if _, err := master.WriteString("~?~."); err != nil {
					t.Fatal(err)
				}
			case "escape-idle":
				if _, err := master.WriteString("~."); err != nil {
					t.Fatal(err)
				}
			case "remote-exit":
				if _, err := master.WriteString("exit\r"); err != nil {
					t.Fatal(err)
				}
			case "cancel-idle":
				cancel()
			default:
				if _, err := master.WriteString("i=0; while [ \"$i\" -lt 131072 ]; do printf 0123456789abcdef; i=$((i+1)); done\r"); err != nil {
					t.Fatal(err)
				}
				waitIOReady(t, fd, unix.POLLOUT, false)
				if name == "escape-backpressure" {
					if _, err := master.WriteString("~."); err != nil {
						t.Fatal(err)
					}
				} else if name == "authorization-backpressure" {
					failures <- "synthetic authorization expired"
				} else {
					cancel()
				}
			}
			select {
			case err = <-done:
				finished = true
			case <-lifetime.Done():
				t.Fatal("interactive I/O did not stop")
			}
			switch name {
			case "escape-idle", "escape-backpressure", "escape-help":
				if err != nil {
					t.Fatal("intentional disconnect failed", err)
				}
			case "remote-exit":
				if err == nil || !strings.Contains(err.Error(), "has not supplied a remote exit status") {
					t.Fatal("remote close invented success", err)
				}
			case "authorization-backpressure":
				if err == nil || !strings.Contains(err.Error(), "synthetic authorization expired") {
					t.Fatal("authorization cause lost", err)
				}
			default:
				if !errors.Is(err, context.Canceled) {
					t.Fatal("cancellation cause lost", err)
				}
			}
			after, e := term.GetState(fd)
			if e != nil || !reflect.DeepEqual(before, after) {
				t.Fatal("interactive terminal not restored", e)
			}
			if name == "escape-help" {
				var output strings.Builder
				b := make([]byte, 4096)
				for !strings.Contains(output.String(), "escapes are only recognized immediately after newline.") {
					waitIOReady(t, masterFD, unix.POLLIN, true)
					n, err := unix.Read(masterFD, b)
					if err != nil || n == 0 {
						t.Fatal("help diagnostic not readable", err)
					}
					output.Write(b[:n])
				}
				if !strings.Contains(output.String(), "Supported escape sequences:") {
					t.Fatal("help missing", output.String())
				}
			}
			if name == "escape-idle" {
				// The next local command's line must not be stolen by an old read.
				if _, err := master.WriteString("next\r"); err != nil {
					t.Fatal(err)
				}
				reused := make(chan string, 1)
				go func() {
					b := make([]byte, 5)
					_, err := io.ReadFull(file, b)
					if err != nil {
						reused <- err.Error()
					} else {
						reused <- string(b)
					}
				}()
				select {
				case got := <-reused:
					if got != "next\n" {
						t.Fatal("old session consumed future input", got)
					}
				case <-time.After(time.Second):
					t.Fatal("borrowed input was not reusable")
				}
			}
		})
	}
}
