//go:build darwin || linux

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
	"golang.org/x/term"
	"pi-sh/internal/shellbroker"
)

// The interactive login shell runs through the real staged TTY adapter, local
// Pion peers and PTYs: TERM, initial size, resize, typed input, escapes and the
// shell's own exit status. The fixture shell is /bin/sh with a private HOME.
func TestLoginShellSessionOverWebRTC(t *testing.T) {
	for _, name := range []string{"exit-status", "ctrl-d", "escape-disconnect", "nested-job"} {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			if err := os.WriteFile(filepath.Join(home, ".profile"), []byte("unset HISTFILE\nprintf 'fixture-ready\\n'\n"), 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("HOME", home)
			t.Setenv("ENV", "")
			lifetime, stop := context.WithTimeout(context.Background(), 15*time.Second)
			defer stop()
			client, channel, _, applied := ttyPeers(t, lifetime)
			master, local, err := pty.Open()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { master.Close(); local.Close() }()
			if err := pty.Setsize(master, &pty.Winsize{Rows: 29, Cols: 91}); err != nil {
				t.Fatal(err)
			}
			before, err := term.GetState(int(local.Fd()))
			if err != nil {
				t.Fatal(err)
			}
			readOutput, output, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer readOutput.Close()
			defer output.Close()
			observed := &ttyOutput{ready: make(chan struct{})}
			drained := make(chan struct{})
			go func() {
				defer close(drained)
				buffer := make([]byte, 4096)
				for {
					n, err := readOutput.Read(buffer)
					observed.Write(buffer[:n])
					if err != nil {
						return
					}
				}
			}()
			diagnostics, err := os.CreateTemp(t.TempDir(), "stderr")
			if err != nil {
				t.Fatal(err)
			}
			defer diagnostics.Close()
			request := ttyCommandRequest{text: shellbroker.LoginShell, term: "xterm-login", shell: true}
			done := make(chan error, 1)
			go func() {
				done <- runTTYSession(lifetime, make(chan string), client, channel, request, local, local, output, diagnostics, '~')
			}()
			waitFor := func(text string) {
				t.Helper()
				for !strings.Contains(observed.text(), text) {
					select {
					case err := <-done:
						// The session can end while its last output is still on the
						// way to the observer; keep the result for the final check.
						done <- err
						deadline := time.Now().Add(time.Second)
						for !strings.Contains(observed.text(), text) {
							if time.Now().After(deadline) {
								t.Fatalf("ended before %q: %v %q", text, err, observed.text())
							}
							time.Sleep(5 * time.Millisecond)
						}
						return
					case <-lifetime.Done():
						t.Fatalf("timeout waiting for %q: %q", text, observed.text())
					case <-time.After(5 * time.Millisecond):
					}
				}
			}
			waitFor("fixture-ready")
			if _, err := master.WriteString("printf 'term:%s argv0:%s\\n' \"$TERM\" \"$0\"; stty size\r"); err != nil {
				t.Fatal(err)
			}
			waitFor("29 91\r\n")
			if err := pty.Setsize(master, &pty.Winsize{Rows: 35, Cols: 111}); err != nil {
				t.Fatal(err)
			}
			if err := syscall.Kill(os.Getpid(), syscall.SIGWINCH); err != nil {
				t.Fatal(err)
			}
			select {
			case size := <-applied:
				if size.Columns != 111 || size.Rows != 35 {
					t.Fatal("unexpected resize", size)
				}
			case <-lifetime.Done():
				t.Fatal("resize not delivered")
			}
			switch name {
			case "nested-job":
				// The login shell's own job control: Ctrl-Z stops its foreground
				// job, the shell regains the terminal and reports the stop.
				if _, err := master.WriteString("stty size; sleep 30\r"); err != nil {
					t.Fatal(err)
				}
				waitFor("35 111\r\n")
				time.Sleep(300 * time.Millisecond)
				if _, err := master.Write([]byte{26}); err != nil {
					t.Fatal(err)
				}
				waitFor("Stopped")
				if _, err := master.WriteString("kill -9 %1\r"); err != nil {
					t.Fatal(err)
				}
				// Bash reports the change at a later prompt or jobs listing.
				time.Sleep(200 * time.Millisecond)
				if _, err := master.WriteString("jobs\r"); err != nil {
					t.Fatal(err)
				}
				waitFor("Killed")
				_, err = master.WriteString("exit 23\r")
			case "exit-status":
				_, err = master.WriteString("stty size; exit 23\r")
			case "ctrl-d":
				_, err = master.WriteString("stty size\r\x04")
			case "escape-disconnect":
				_, err = master.WriteString("stty size\r~.")
			}
			if err != nil {
				t.Fatal(err)
			}
			if name != "escape-disconnect" {
				waitFor("35 111\r\n")
			}
			select {
			case err = <-done:
			case <-lifetime.Done():
				t.Fatal("login shell session did not finish", observed.text())
			}
			output.Close()
			<-drained
			text := observed.text()
			if !strings.Contains(text, "term:xterm-login argv0:-sh\r\n") {
				t.Fatal("TERM or login argv0 not observed", text)
			}
			var status *commandStatus
			switch name {
			case "exit-status", "nested-job":
				if !errors.As(err, &status) || status.code != 23 {
					t.Fatalf("status %v: %q", err, text)
				}
			case "ctrl-d":
				if !errors.As(err, &status) || status.code != 0 {
					t.Fatalf("Ctrl-D logout status %v: %q", err, text)
				}
			case "escape-disconnect":
				if !errors.Is(err, errEscape) || !errors.Is(err, shellbroker.ErrCleanupUnconfirmed) {
					t.Fatalf("escape result %v", err)
				}
			}
			after, err := term.GetState(int(local.Fd()))
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatal("local terminal not restored", err)
			}
		})
	}
}
