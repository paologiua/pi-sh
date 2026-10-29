//go:build darwin || linux

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
	"pi-sh/internal/shellbroker"
	"pi-sh/internal/terminal"
	"pi-sh/internal/transport"
)

// A local shell PTY stands in for the agent. Signaling, authentication and
// actual Pi teardown are not simulated by this fixture.
func ttyPeers(t *testing.T, ctx context.Context) (*transport.Peer, *transport.Channel, *transport.Peer, <-chan terminal.Size) {
	t.Helper()
	bash := os.Getenv("PI_SH_TEST_BASH")
	if bash == "" {
		bash, _ = exec.LookPath("bash")
	}
	if bash == "" || exec.Command(bash, "-c", "((BASH_VERSINFO[0]>4||(BASH_VERSINFO[0]==4&&BASH_VERSINFO[1]>=3)))").Run() != nil {
		t.Skip("local Bash >=4.3 required")
	}
	tools := t.TempDir()
	if err := os.Symlink(bash, filepath.Join(tools, "bash")); err != nil {
		t.Fatal(err)
	}
	client, err := transport.New(ctx, transport.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	server, err := transport.New(ctx, transport.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { server.Close() })
	ch, err := client.CreateChannel("shell")
	if err != nil {
		t.Fatal(err)
	}
	offer, err := client.Offer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	answer, err := server.Answer(ctx, offer)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.ApplyAnswer(answer); err != nil {
		t.Fatal(err)
	}
	remote, err := server.Accept(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh", "-i")
	cmd.Env = append(os.Environ(), "PATH="+tools+":"+os.Getenv("PATH"), "HISTFILE=/dev/null", "ENV=/dev/null", "BASH_ENV=/dev/null", "SHELL=/bin/sh")
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 24, Cols: 80})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close(); cmd.Process.Signal(syscall.SIGHUP); _ = cmd.Wait() })
	go func() {
		for {
			m, err := remote.Receive(ctx)
			if err != nil {
				return
			}
			if _, err := f.Write(m.Data); err != nil {
				return
			}
		}
	}()
	go func() {
		defer remote.Close()
		buffer := make([]byte, 4096)
		for {
			n, err := f.Read(buffer)
			if n > 0 {
				if err := remote.Send(ctx, transport.Message{Data: buffer[:n]}); err != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	resize, err := server.CreateChannel("resize")
	if err != nil {
		t.Fatal(err)
	}
	applied := make(chan terminal.Size, 8)
	control, err := f.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		// Open the channel even if no resize is needed for a short command.
		if err := resize.Send(ctx, transport.Message{Text: true}); err != nil {
			return
		}
		for {
			m, err := resize.Receive(ctx)
			if err != nil {
				return
			}
			var size struct {
				Columns int `json:"cols"`
				Rows    int `json:"rows"`
			}
			if json.Unmarshal(m.Data, &size) != nil {
				return
			}
			var resizeError error
			if err := control.Control(func(fd uintptr) {
				resizeError = unix.IoctlSetWinsize(int(fd), unix.TIOCSWINSZ, &unix.Winsize{Row: uint16(size.Rows), Col: uint16(size.Columns)})
			}); err != nil || resizeError != nil {
				return
			}
			select {
			case applied <- terminal.Size{Columns: size.Columns, Rows: size.Rows}:
			case <-ctx.Done():
				return
			}
		}
	}()
	return client, ch, server, applied
}

type ttyOutput struct {
	mu    sync.Mutex
	data  bytes.Buffer
	ready chan struct{}
	once  sync.Once
}

func (w *ttyOutput) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.data.Write(b)
	if strings.Contains(w.data.String(), "fixture-ready") {
		w.once.Do(func() { close(w.ready) })
	}
	return len(b), nil
}

func (w *ttyOutput) text() string { w.mu.Lock(); defer w.mu.Unlock(); return w.data.String() }

func TestTTYCommandSessionOverWebRTC(t *testing.T) {
	for _, name := range []string{"input-resize", "forced-pipe", "null-input", "escape", "authorization", "transport", "ordinary130", "local-modes", "output-backpressure", "idle-input-reuse", "escape-help", "ctrl-z-default", "ctrl-z-handler", "partial-eof", "eof-record", "early-command-close", "early-close-no-status"} {
		t.Run(name, func(t *testing.T) {
			lifetime, stop := context.WithTimeout(context.Background(), 15*time.Second)
			defer stop()
			ctx, cancel := context.WithCancel(lifetime)
			defer cancel()
			client, channel, server, applied := ttyPeers(t, lifetime)
			master, local, err := pty.Open()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { master.Close(); local.Close() }()
			localFD := int(local.Fd())
			if err := pty.Setsize(master, &pty.Winsize{Rows: 29, Cols: 91}); err != nil {
				t.Fatal(err)
			}
			if name == "local-modes" {
				cmd := exec.Command("stty", "-echo", "-icanon", "-isig", "-opost", "-icrnl", "intr", "^]", "eof", "^G", "19200")
				cmd.Stdin = local
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("local mode fixture: %v %q", err, out)
				}
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
			outputFD := int(output.Fd())
			observed := &ttyOutput{ready: make(chan struct{})}
			drained := make(chan struct{})
			releaseOutput := make(chan struct{})
			var releaseOnce sync.Once
			defer releaseOnce.Do(func() { close(releaseOutput) })
			go func() {
				defer close(drained)
				if name == "output-backpressure" {
					b := make([]byte, 4096)
					for {
						n, err := readOutput.Read(b)
						if n > 0 {
							observed.Write(b[:n])
						}
						if err != nil {
							return
						}
						select {
						case <-observed.ready:
							<-releaseOutput
							io.Copy(observed, readOutput)
							return
						default:
						}
					}
				}
				io.Copy(observed, readOutput)
			}()
			diagnosticReader, diagnosticFile, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer diagnosticReader.Close()
			defer diagnosticFile.Close()
			var diagnostics bytes.Buffer
			diagnosticsDone := make(chan struct{})
			go func() { io.Copy(&diagnostics, diagnosticReader); close(diagnosticsDone) }()
			request := ttyCommandRequest{term: "fixture-'é", text: "printf fixture-ready; IFS= read -r x"}
			var stdin io.Reader = local
			inputFile := local
			wantCode := -1
			switch name {
			case "partial-eof":
				request.text = `printf 'fixture-ready\n'; IFS= read -r x; rc=$?; printf 'read:%s:%s\n' "$rc" "$x"; exit 23`
				wantCode = 23
			case "eof-record":
				request.text = `printf 'fixture-ready\n'; first=$(dd bs=16 count=1 2>/dev/null | od -An -tx1 | tr -d '[:space:]'); printf 'first:%s\n' "$first"; IFS= read -r x; printf 'next:%s\n' "$x"; exit 23`
				wantCode = 23
			case "ctrl-z-default", "ctrl-z-handler":
				request.text = "printf 'fixture-ready\\n'; IFS= read -r x; printf 'seen:%s\\n' \"$x\"; exit 23"
				if name == "ctrl-z-handler" {
					// As the caught-suspend fixtures (TESTING.md 2026-10-26): the
					// test Bash, not /bin/sh (Bash 3.2 can lose the line when the
					// trap interrupts read), retrying the interrupted read.
					request.text = `exec bash -c 'trap "printf caught-suspend" TSTP; printf "fixture-ready\n"; until IFS= read -r x; do :; done; printf "seen:%s\n" "$x"; exit 23'`
				}
				wantCode = 23
			case "escape-help":
				request.text = "stty -echo; printf fixture-ready; IFS= read -r x; printf 'seen:%s\\n' \"$x\"; exit 23"
				wantCode = 23
			case "input-resize":
				request.text = "printf 'first:%s\\n' \"$TERM\"; stty size; printf 'fixture-ready\\n'; IFS= read -r x; stty size; printf 'seen:%s\\n' \"$x\"; exit 23"
				wantCode = 23
			case "early-command-close", "early-close-no-status":
				inputFile = nil
				stdin = ttyReadyInput{ctx: ctx, ready: observed.ready, reader: strings.NewReader(strings.Repeat("x", 64*1024))}
				request.text = `stty raw -echo; printf fixture-ready; c=$(dd bs=1 count=1 2>/dev/null); printf 'seen:%s\n' "$c"; exit 23`
				wantCode = 23
				if name == "early-close-no-status" {
					request.text = strings.TrimSuffix(request.text, "exit 23") + "sleep 60"
					wantCode = -1
				}

			case "forced-pipe":
				inputFile = nil
				stdin = strings.NewReader("pipe-é\r")
				request.text = "stty size; IFS= read -r x; printf 'seen:%s\\n' \"$x\"; exit 17"
				wantCode = 17
			case "null-input":
				request.stdinNull = true
				stdin = unreadableInput{t}
			case "escape":
				request.text = "printf fixture-ready; sleep 60"
			case "ordinary130":
				request.text = "printf first; exit 130"
				wantCode = 130
			case "local-modes":
				request.text = "printf 'fixture-ready\\n'; c=$(dd bs=1 count=1 2>/dev/null | od -An -tu1 | tr -d '[:space:]'); printf 'byte:%s\\n' \"$c\"; stty speed; exit 21"
				wantCode = 21
			case "output-backpressure":
				request.text = "printf fixture-ready; i=0; while [ \"$i\" -lt 131072 ]; do printf 0123456789abcdef; i=$((i+1)); done"
			case "idle-input-reuse":
				request.text = "trap 'exit 23' WINCH; printf fixture-ready; while :; do IFS= read -r x; done"
				wantCode = 23
			}
			failures := make(chan string, 1)
			done := make(chan error, 1)
			go func() {
				done <- runTTYSession(ctx, failures, client, channel, request, stdin, inputFile, output, diagnosticFile, '~')
			}()
			if wantCode < 0 || name == "input-resize" || name == "local-modes" || name == "idle-input-reuse" || name == "escape-help" || name == "ctrl-z-default" || name == "ctrl-z-handler" || name == "partial-eof" || name == "eof-record" {
				select {
				case <-observed.ready:
				case err := <-done:
					t.Fatalf("ended before readiness: %v %q", err, observed.text())
				case <-lifetime.Done():
					t.Fatal("command readiness timeout", observed.text())
				}
				switch name {
				case "early-close-no-status":
					for !strings.Contains(observed.text(), "seen:x") {
						select {
						case <-lifetime.Done():
							t.Fatal("command did not consume input", observed.text())
						case <-time.After(time.Millisecond):
						}
					}
					server.Close()
				case "partial-eof", "eof-record":
					data := []byte("abc\x04\x04")
					if name == "eof-record" {
						data = []byte("abc\x04next\r")
					}
					if _, err := master.Write(data); err != nil {
						t.Fatal(err)
					}
				case "ctrl-z-default":
					if _, err := master.Write([]byte{26, 'x', '\r'}); err != nil {
						t.Fatal(err)
					}
				case "ctrl-z-handler":
					// The line follows the handler's output: a trapped TSTP
					// interrupts read, so one write with ^Z was racy.
					if _, err := master.Write([]byte{26}); err != nil {
						t.Fatal(err)
					}
					for !strings.Contains(observed.text(), "caught-suspend") {
						select {
						case <-lifetime.Done():
							t.Fatal("TSTP handler did not run", observed.text())
						case <-time.After(10 * time.Millisecond):
						}
					}
					if _, err := master.Write([]byte("x\r")); err != nil {
						t.Fatal(err)
					}
				case "escape-help":
					if _, err := master.WriteString("~?value\r"); err != nil {
						t.Fatal(err)
					}
				case "output-backpressure":
					waitIOReady(t, outputFD, unix.POLLOUT, false)
					failures <- "synthetic authorization expired"
				case "idle-input-reuse":
					if _, err := master.Write([]byte{0xc3}); err != nil {
						t.Fatal(err)
					}
					waitIOReady(t, localFD, unix.POLLIN, false)
					if err := pty.Setsize(master, &pty.Winsize{Rows: 31, Cols: 97}); err != nil {
						t.Fatal(err)
					}
					if err := syscall.Kill(os.Getpid(), syscall.SIGWINCH); err != nil {
						t.Fatal(err)
					}
				case "local-modes":
					if _, err := master.Write([]byte{4}); err != nil {
						t.Fatal(err)
					}
				case "input-resize":
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
					if _, err := master.WriteString("l'amour é 世界\r"); err != nil {
						t.Fatal(err)
					}
				case "escape":
					if _, err := master.WriteString("~."); err != nil {
						t.Fatal(err)
					}
				case "authorization":
					failures <- "synthetic authorization expired"
				case "transport":
					server.Close()
				case "null-input":
					// Local EOF/-n must not finish the remote canonical read.
					select {
					case err := <-done:
						t.Fatal("stdin-null synthesized EOF", err)
					case <-time.After(150 * time.Millisecond):
					}
					cancel()
				}
			}
			select {
			case err = <-done:
			case <-lifetime.Done():
				t.Fatal("session did not finish", observed.text())
			}
			output.Close()
			releaseOnce.Do(func() { close(releaseOutput) })
			<-drained
			diagnosticFile.Close()
			<-diagnosticsDone
			text := observed.text()
			if (name == "early-command-close" || name == "early-close-no-status") && !strings.Contains(text, "seen:x") {
				t.Fatal("early command lost input/output", text)
			}
			if name == "partial-eof" && !strings.Contains(text, "read:1:abc\r\n") {
				t.Fatal("partial EOF lost bytes/status", text)
			}
			if name == "eof-record" && !strings.Contains(text, "first:616263\r\nnext:next\r\n") {
				t.Fatal("EOF record lost stream continuation", text)
			}
			if (name == "ctrl-z-default" || name == "ctrl-z-handler") && !strings.Contains(text, "seen:x") {
				t.Fatal("Ctrl-Z lost command completion", text)
			}
			if name == "ctrl-z-handler" && !strings.Contains(text, "caught-suspend") {
				t.Fatal("TSTP handler did not run", text)
			}
			if name == "escape-help" && (!strings.Contains(diagnostics.String(), "Supported escape sequences:") || strings.Contains(text, "Supported escape sequences:") || !strings.Contains(text, "seen:value")) {
				t.Fatalf("help crossed streams: out=%q diag=%q", text, diagnostics.String())
			}
			if wantCode >= 0 {
				var status *commandStatus
				if !errors.As(err, &status) || status.code != wantCode {
					t.Fatalf("status %v want %d: %q", err, wantCode, text)
				}
			} else if err == nil || !errors.Is(err, shellbroker.ErrCleanupUnconfirmed) {
				t.Fatalf("abnormal close claimed cleanup: %v", err)
			}
			if name == "input-resize" && (!strings.HasPrefix(text, "first:fixture-'é\r\n29 91\r\n") || !strings.Contains(text, "35 111\r\nseen:l'amour é 世界\r\n")) {
				t.Fatal("TERM/size/input/first bytes", text)
			}
			if name == "forced-pipe" && (!strings.Contains(text, "0 0\r\n") || !strings.Contains(text, "seen:pipe-é\r\n")) {
				t.Fatal("forced pipe input/size", text)
			}
			if name == "local-modes" && text != "fixture-ready\nbyte:4\n19200\n" {
				t.Fatal("local terminal modes not forwarded", text)
			}
			if name == "escape" && !errors.Is(err, errEscape) {
				t.Fatal("escape cause lost", err)
			}
			if name == "null-input" && !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation cause lost", err)
			}
			if (name == "authorization" || name == "output-backpressure") && !strings.Contains(err.Error(), "synthetic authorization expired") {
				t.Fatal("authorization cause lost", err)
			}
			after, e := term.GetState(int(local.Fd()))
			if e != nil || !reflect.DeepEqual(before, after) {
				t.Fatal("local terminal not restored", e)
			}
			if name == "idle-input-reuse" {
				if _, err := master.WriteString("next\r"); err != nil {
					t.Fatal(err)
				}
				reused := make(chan string, 1)
				go func() {
					b := make([]byte, 5)
					_, err := io.ReadFull(local, b)
					if err != nil {
						reused <- err.Error()
					} else {
						reused <- string(b)
					}
				}()
				select {
				case got := <-reused:
					if got != "next\n" {
						t.Fatal("TTY command consumed next local line", got)
					}
				case <-time.After(time.Second):
					t.Fatal("input worker survived TTY command completion")
				}
			}
		})
	}
}

// Wait for the command's own raw-mode/readiness marker, after protocol readiness.
// The fixture reader is bounded and wakes when the owning session ends.
type ttyReadyInput struct {
	ctx    context.Context
	ready  <-chan struct{}
	reader io.Reader
}

func (r ttyReadyInput) Read(b []byte) (int, error) {
	select {
	case <-r.ctx.Done():
		return 0, r.ctx.Err()
	case <-r.ready:
		return r.reader.Read(b)
	}
}
