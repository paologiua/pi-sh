//go:build darwin || linux

package shellbroker

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
)

// This file measures a test-only candidate, not an enabled CLI TTY adapter.
// Literal control frames cannot be satisfied by the echoed bootstrap text.
const ttyCandidateReady = "\x1ePISH_TTY_CANDIDATE_READY\x1f"
const ttyCandidateExit = "\x1ePISH_TTY_CANDIDATE_EXIT:"

type ttyWatch struct {
	mu        sync.Mutex
	data      bytes.Buffer
	ready     chan struct{}
	once      sync.Once
	candidate bool
}

func (w *ttyWatch) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.data.Write(p)
	b := w.data.Bytes()
	if w.candidate {
		i := bytes.Index(b, []byte(ttyCandidateReady))
		if i < 0 {
			return len(p), nil
		}
		b = b[i+len(ttyCandidateReady):]
	}
	if bytes.Contains(b, []byte("fixture-ready")) {
		w.once.Do(func() { close(w.ready) })
	}
	return len(p), nil
}

func (w *ttyWatch) text() string { w.mu.Lock(); defer w.mu.Unlock(); return w.data.String() }

// sendTTYInput writes fixture input. A trapped TSTP interrupts Bash's read
// (status >128) and discards what it had already consumed, so a ^Z sent
// together with a line could lose it. The line is sent after the handler ran,
// and fixtures retry the interrupted read. They run it under modern Bash:
// macOS /bin/sh (Bash 3.2) loses the line about 3 times in 100 on a bare PTY
// when the trap runs during read, even when the line is sent afterwards.
func sendTTYInput(ctx context.Context, w *ttyWatch, input []byte, send func([]byte) error) error {
	if len(input) > 1 && input[0] == 26 {
		if err := send(input[:1]); err != nil {
			return err
		}
		// An untrapped ^Z stops the job instead and never prints the marker.
		deadline := time.After(time.Second)
	wait:
		for !strings.Contains(w.text(), "caught-suspend") {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-deadline:
				break wait
			case <-time.After(10 * time.Millisecond):
			}
		}
		input = input[1:]
	}
	return send(input)
}

type ptyFixtureHooks struct {
	ready     func(*exec.Cmd, *os.File)
	afterExit func()
}

func runPTYFixture(t *testing.T, cmd *exec.Cmd, bootstrap string, input []byte, resize bool, hooks ...ptyFixtureHooks) (int, string) {
	t.Helper()
	cmd.Env = append(os.Environ(), "HISTFILE=/dev/null", "ENV=/dev/null", "BASH_ENV=/dev/null")
	// pty.Start only assigns nil streams. The reference factory defaults stdin
	// to a byte reader for no-TTY tests; all three fds must instead use this PTY.
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 24, Cols: 80})
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w := &ttyWatch{ready: make(chan struct{}), candidate: bootstrap != ""}
	readDone := make(chan struct{})
	go func() { io.Copy(w, f); close(readDone) }()
	wait := make(chan error, 1)
	go func() { wait <- cmd.Wait() }()
	completed := false
	defer func() {
		if !completed {
			// Give a candidate's owned-group cleanup trap a chance to run before
			// the test falls back to killing its local supervisor group.
			cmd.Process.Signal(syscall.SIGHUP)
			select {
			case <-wait:
			case <-time.After(time.Second):
				syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
				<-wait
			}
		}
		f.Close()
		<-readDone
	}()
	if bootstrap != "" {
		if _, err := f.WriteString(bootstrap); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case <-w.ready:
	case err := <-wait:
		completed = true
		select {
		case <-readDone:
		case <-time.After(time.Second):
			t.Fatal("PTY output did not close")
		}
		// A short command can finish before this goroutine selects readiness.
		// It is still valid if the drained output proves the marker was seen.
		select {
		case <-w.ready:
			if len(input) == 0 && !resize {
				if err == nil {
					return 0, w.text()
				}
				if exit, ok := err.(*exec.ExitError); ok {
					return exit.ExitCode(), w.text()
				}
			}
		default:
		}
		t.Fatalf("PTY ended before command readiness: %v %q", err, w.text())
	case <-time.After(5 * time.Second):
		t.Fatalf("PTY readiness timeout: %q", w.text())
	}
	if resize {
		if err := pty.Setsize(f, &pty.Winsize{Rows: 35, Cols: 111}); err != nil {
			t.Fatal(err)
		}
		// SIGWINCH is the ordinary local client's resize notification. Explicit
		// delivery avoids relying on which test PTY the host considers foreground.
		if bootstrap == "" {
			if err := cmd.Process.Signal(syscall.SIGWINCH); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, hook := range hooks {
		if hook.ready != nil {
			hook.ready(cmd, f)
		}
	}
	if len(input) > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := sendTTYInput(ctx, w, input, func(b []byte) error { _, err := f.Write(b); return err })
		cancel()
		if err != nil {
			t.Fatal(err)
		}
	}
	select {
	case err := <-wait:
		completed = true
		for _, hook := range hooks {
			if hook.afterExit != nil {
				hook.afterExit()
			}
		}
		// Drain all PTY output after exit before inspecting frames/output.
		select {
		case <-readDone:
		case <-time.After(time.Second):
			t.Fatal("PTY output did not close")
		}
		if err == nil {
			return 0, w.text()
		}
		if exit, ok := err.(*exec.ExitError); ok {
			return exit.ExitCode(), w.text()
		}
		t.Fatal(err)
	case <-time.After(5 * time.Second):
		t.Fatalf("PTY command timeout: %q", w.text())
	}
	return 0, ""
}

func runTTYCandidate(t *testing.T, text string, input []byte, resize bool) (int, string) {
	t.Helper()
	script := "trap ':' INT QUIT; printf '\\036PISH_TTY_CANDIDATE_READY\\037'; /bin/sh -c " + quote(text) +
		"; s=$?; stty -echo -onlcr || exit 125; printf '\\036PISH_TTY_CANDIDATE_EXIT:%d\\037' \"$s\"; exit \"$s\""
	bootstrap := " unset HISTFILE; exec /bin/sh -c " + quote(script) + "\r"
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	processCode, raw := runPTYFixture(t, exec.CommandContext(ctx, "/bin/sh", "-i"), bootstrap, input, resize)
	start := strings.Index(raw, ttyCandidateReady)
	end := strings.Index(raw, ttyCandidateExit)
	if start < 0 || end < start+len(ttyCandidateReady) {
		t.Fatalf("missing candidate status: code%d %q", processCode, raw)
	}
	statusText := raw[end+len(ttyCandidateExit):]
	stop := strings.IndexByte(statusText, '\x1f')
	if stop < 0 {
		t.Fatal("truncated candidate status")
	}
	code, err := strconv.Atoi(statusText[:stop])
	if err != nil || code != processCode {
		t.Fatalf("candidate status mismatch: %d %q", processCode, statusText)
	}
	return code, raw[start+len(ttyCandidateReady) : end]
}

func ttySSHCommand(t *testing.T) func(string) *exec.Cmd {
	base := localSSHCommand(t)
	return func(text string) *exec.Cmd {
		cmd := base(text, nil)
		for i, a := range cmd.Args {
			if a == "-T" {
				cmd.Args[i] = "-tt"
			}
		}
		return cmd
	}
}

type ttyFunctionalCase struct {
	name, text, want string
	input            []byte
	code             int
	resize           bool
}

func ttyFunctionalCases() []ttyFunctionalCase {
	return []ttyFunctionalCase{
		{"tty-and-status", "printf 'fixture-ready\\n'; test -t 0 && test -t 1 && test -t 2 && test -r /dev/tty && printf 'tty-ok\\n'; exit 23", "tty-ok\r\n", nil, 23, false},
		{"merged-stderr", "printf 'fixture-ready\\n'; printf out; printf err >&2; exit 7", "outerr", nil, 7, false},
		{"unicode-input", "printf 'fixture-ready\\n'; IFS= read -r x; printf 'seen:%s\\n' \"$x\"; exit 9", "seen:l'amour é 世界\r\n", []byte("l'amour é 世界\r"), 9, false},
		{"canonical-eof", "printf 'fixture-ready\\n'; IFS= read -r x; printf 'read-status:%s\\n' \"$?\"", "read-status:1\r\n", []byte{4}, 0, false},
		{"raw-eof-byte", "stty raw -echo; printf fixture-ready; c=$(dd bs=1 count=1 2>/dev/null | od -An -tu1 | tr -d '[:space:]'); printf 'byte:%s' \"$c\"", "byte:4", []byte{4}, 0, false},
		{"resize", "stty size; printf 'fixture-ready\\n'; IFS= read -r x; stty size", "35 111\r\n", []byte{'\r'}, 0, true},
		{"ordinary-130", "printf 'fixture-ready\\n'; exit 130", "fixture-ready\r\n", nil, 130, false},
	}
}

func TestTTYCommandCandidateAndOpenSSH(t *testing.T) {
	reference := ttySSHCommand(t)
	for _, tc := range ttyFunctionalCases() {
		t.Run(tc.name, func(t *testing.T) {
			code, output := runTTYCandidate(t, tc.text, tc.input, tc.resize)
			if code != tc.code || !strings.Contains(output, tc.want) {
				t.Fatalf("candidate code%d %q", code, output)
			}
			refCode, refOutput := runPTYFixture(t, reference(tc.text), "", tc.input, tc.resize)
			if refCode != tc.code || !strings.Contains(refOutput, tc.want) {
				t.Fatalf("reference code%d %q", refCode, refOutput)
			}
			if tc.resize && (!strings.Contains(output, "24 80\r\n") || !strings.Contains(refOutput, "24 80\r\n")) {
				t.Fatal("initial size differed")
			}
		})
	}
}

func TestTTYCommandNumericSupervisorLosesSignalIdentity(t *testing.T) {
	reference := ttySSHCommand(t)
	for _, tc := range []struct {
		name, text string
		input      []byte
	}{
		{"self-int", "printf 'fixture-ready\\n'; kill -INT $$", nil},
		{"terminal-int", "printf 'fixture-ready\\n'; sleep 60", []byte{3}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, _ := runTTYCandidate(t, tc.text, tc.input, false)
			refCode, _ := runPTYFixture(t, reference(tc.text), "", tc.input, false)
			if code != 130 || refCode != 255 {
				t.Fatal(fmt.Sprintf("candidate=%d reference=%d; signal behavior changed", code, refCode))
			}
		})
	}
}

func TestOpenSSHForcedTTYStdinEOFDoesNotSynthesizeVEOF(t *testing.T) {
	reference := ttySSHCommand(t)
	cmd := reference("printf fixture-ready; IFS= read -r x; printf 'read-finished:%s' \"$?\"")
	// No local PTY: -tt explicitly forces the remote PTY while the local input
	// reader reaches EOF. This is distinct from sending byte 4 to the terminal.
	w := &ttyWatch{ready: make(chan struct{})}
	var stderr guardedBuffer
	cmd.Stdout, cmd.Stderr = w, &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	stopped := make(chan error, 1)
	go func() { stopped <- cmd.Wait() }()
	done := false
	defer func() {
		if !done {
			cmd.Process.Kill()
			<-stopped
		}
	}()
	select {
	case <-w.ready:
	case err := <-stopped:
		done = true
		t.Fatalf("forced TTY ended before readiness: %v %q", err, w.text())
	case <-time.After(5 * time.Second):
		t.Fatal("forced TTY did not start")
	}
	select {
	case err := <-stopped:
		done = true
		t.Fatalf("local EOF ended remote canonical read: %v %q", err, w.text())
	case <-time.After(200 * time.Millisecond):
	}
	if strings.Contains(w.text(), "read-finished:") {
		t.Fatal("local EOF synthesized terminal EOF")
	}
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-stopped:
		done = true
		if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 255 {
			t.Fatalf("client interrupt status: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("reference client did not terminate")
	}
}
