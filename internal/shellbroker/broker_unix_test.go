//go:build darwin || linux

package shellbroker

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
)

// macOS does not ship util-linux setsid. This test-only executable path invokes
// the native setsid syscall then exec, preserving the PID as the Linux tool does
// in the non-process-group-leader case. It is never sent to the Raspberry Pi.
func TestMain(m *testing.M) {
	if filepath.Base(os.Args[0]) == "setsid" {
		args := os.Args[1:]
		if len(args) > 0 && args[0] == "--wait" {
			args = args[1:]
		}
		if len(args) == 0 {
			os.Exit(125)
		}
		if _, err := syscall.Setsid(); err != nil {
			os.Exit(125)
		}
		path, err := exec.LookPath(args[0])
		if err != nil {
			os.Exit(127)
		}
		if err = syscall.Exec(path, args, os.Environ()); err != nil {
			os.Exit(126)
		}
	}
	os.Exit(m.Run())
}

type testStream struct {
	file   *os.File
	frames chan readResult
}

func newTestStream(ctx context.Context, file *os.File) testStream {
	s := testStream{file: file, frames: make(chan readResult, 32)}
	go func() {
		for {
			b := make([]byte, 4096)
			n, err := file.Read(b)
			if errors.Is(err, syscall.EIO) {
				err = io.EOF
			}

			select {
			case s.frames <- readResult{data: b[:n], err: err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
	return s
}

func (s testStream) Send(ctx context.Context, b []byte) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	_, err := s.file.Write(b)
	return err
}
func (s testStream) Receive(ctx context.Context) ([]byte, error) {
	select {
	case frame := <-s.frames:
		return frame.data, frame.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func runLocal(t *testing.T, text string, in io.Reader, out, stderr io.Writer) (int, error) {
	return runLocalControlled(t, text, func(context.CancelFunc) IO { return IO{Stdin: in, Stdout: out, Stderr: stderr} }, nil)
}

// The carrier remains open until Run returns, including during cancellation.
// beforeClose checks cleanup while a carrier hangup cannot mask a leak.
func runLocalControlled(t *testing.T, text string, configure func(context.CancelFunc) IO, beforeClose func(int, error)) (int, error) {
	return runLocalControlledWithin(t, text, configure, beforeClose, 20*time.Second)
}

func runLocalControlledWithin(t *testing.T, text string, configure func(context.CancelFunc) IO, beforeClose func(int, error), limit time.Duration) (int, error) {
	return runLocalWithStream(t, text, configure, beforeClose, limit, func(ctx context.Context, file *os.File) Stream {
		return newTestStream(ctx, file)
	})
}

func runLocalWithStream(t *testing.T, text string, configure func(context.CancelFunc) IO, beforeClose func(int, error), limit time.Duration, makeStream func(context.Context, *os.File) Stream) (int, error) {
	t.Helper()
	bash := os.Getenv("PI_SH_TEST_BASH")
	if bash == "" {
		var err error
		bash, err = exec.LookPath("bash")
		if err != nil {
			t.Skip("Bash unavailable")
		}
	}
	err := exec.Command(bash, "-c", "((BASH_VERSINFO[0]>4||(BASH_VERSINFO[0]==4&&BASH_VERSINFO[1]>=3)))").Run()
	if err != nil {
		t.Skip("Bash >=4.3 required; set PI_SH_TEST_BASH to a modern local Bash")
	}
	tools := t.TempDir()
	if err = os.Symlink(bash, filepath.Join(tools, "bash")); err != nil {
		t.Fatal(err)
	}
	if _, err = exec.LookPath("setsid"); err != nil {
		executable, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		if err = os.Symlink(executable, filepath.Join(tools, "setsid")); err != nil {
			t.Fatal(err)
		}
	}
	lifetime, stopLifetime := context.WithTimeout(context.Background(), limit)
	defer stopLifetime()
	ctx, cancel := context.WithCancel(lifetime)
	defer cancel()
	cmd := exec.Command("/bin/sh", "-i")
	cmd.Env = append(os.Environ(), "PATH="+tools+":"+os.Getenv("PATH"), "HISTFILE=/dev/null", "BASH_ENV=/dev/null", "ENV=/dev/null", "SHELL=/bin/sh")
	file, err := pty.Start(cmd)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		select {
		case <-lifetime.Done():
			cmd.Process.Signal(syscall.SIGHUP)
		case <-done:
		}
	}()
	defer func() { close(done); file.Close(); cmd.Process.Signal(syscall.SIGHUP); _ = cmd.Wait() }()
	plan, err := New(text)
	if err != nil {
		t.Fatal(err)
	}
	code, err := plan.Run(ctx, makeStream(lifetime, file), configure(cancel))
	if lifetime.Err() != nil {
		t.Fatalf("broker timeout for %q: %v", text, err)
	}
	if beforeClose != nil {
		beforeClose(code, err)
	}
	return code, err
}

type cancellationWriter struct {
	bytes.Buffer
	cancel context.CancelFunc
}

type cancellationReader struct{ cancel context.CancelFunc }

func (r cancellationReader) Read([]byte) (int, error) { r.cancel(); return 0, io.EOF }

func TestShellBrokerCancellationDuringWorkerStartup(t *testing.T) {
	for i := 0; i < 5; i++ {
		_, err := runLocalControlled(t, "sleep 5", func(cancel context.CancelFunc) IO {
			return IO{Stdin: cancellationReader{cancel}}
		}, nil)
		if !errors.Is(err, context.Canceled) || errors.Is(err, ErrCleanupUnconfirmed) {
			t.Fatalf("startup cancellation: %v", err)
		}
	}
}

func (w *cancellationWriter) Write(b []byte) (int, error) {
	n, err := w.Buffer.Write(b)
	if bytes.Contains(w.Bytes(), []byte("ready\n")) {
		w.cancel()
	}
	return n, err
}

func TestShellBrokerCancellationReapsBeforeCarrierClose(t *testing.T) {
	// Both command shell and its child ignore ordinary termination. Only the
	// broker-owned group cleanup can finish this command while PTY stays open.
	text := "trap '' HUP INT TERM; (trap '' HUP INT TERM; while :; do sleep 1; done) & printf '%s %s ' \"$$\" \"$!\"; i=0; while [ $i -lt 8192 ]; do printf '                '; i=$((i+1)); done; printf 'ready\\n'; wait"
	var out cancellationWriter
	confirmed := false
	defer func() {
		if confirmed {
			return
		}
		fields := strings.Fields(out.String())
		if len(fields) > 0 {
			if pid, err := strconv.Atoi(fields[0]); err == nil && pid > 1 {
				_ = syscall.Kill(-pid, syscall.SIGKILL)
			}
		}
	}()
	code, err := runLocalControlled(t, text, func(cancel context.CancelFunc) IO {
		out.cancel = cancel
		// The command never reads stdin. Sending more than pipe capacity while
		// it emits 128 KiB exercises cancellation of a blocked input worker.
		return IO{Stdin: bytes.NewReader(bytes.Repeat([]byte{0xff}, 1<<20)), Stdout: &out}
	}, func(_ int, err error) {
		if !errors.Is(err, context.Canceled) || errors.Is(err, ErrCleanupUnconfirmed) {
			t.Fatalf("cancel/cleanup: %v", err)
		}
		fields := strings.Fields(out.String())
		if len(fields) != 3 || fields[2] != "ready" {
			t.Fatalf("readiness not observed (%d output bytes)", out.Len())
		}
		for _, value := range fields[:2] {
			pid, e := strconv.Atoi(value)
			if e != nil || pid <= 1 {
				t.Fatal("invalid local test process ID")
			}
			if syscall.Kill(pid, 0) == nil {
				if value == fields[0] {
					t.Fatal("owned command leader still exists before carrier close")
				}
				// Orphaned descendants may remain zombies until the OS reaps
				// them. They must not be running after cleanup acknowledgement.
				state, e := exec.Command("ps", "-p", value, "-o", "stat=").Output()
				if e != nil || !strings.HasPrefix(strings.TrimSpace(string(state)), "Z") {
					t.Fatal("owned descendant still running, or its state could not be verified")
				}
			}
		}
		confirmed = true
	})
	if code != 0 || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation fabricated a status: %d %v", code, err)
	}
}

func TestShellBrokerStreamsAndStatus(t *testing.T) {
	for _, tc := range []struct {
		name, text, in, out, stderr string
		code                        int
	}{
		{"separate", "printf out; printf err >&2; exit 23", "", "out", "err", 23},
		{"stdin-eof", "cat; printf after >&2", "input\x00\xff\r\n", "input\x00\xff\r\n", "after", 0},
		{"binary", "printf '\\000\\377\\015\\012'", "", "\x00\xff\r\n", "", 0},
		{"tty-isolation", "test ! -t 0 && test ! -t 1 && test ! -t 2 && ! ( : </dev/tty ) 2>/dev/null", "", "", "", 0},
		{"early-close", "exec 0<&-; printf closed", "unused", "closed", "", 0},
		{"newline-command", "printf one\nprintf two", "", "onetwo", "", 0},
		{"exit143", "exit 143", "", "", "", 143},
		{"signal-term", "kill -TERM $$", "", "", "", 255},
		{"signal-int", "kill -INT $$", "", "", "", 255},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, stderr bytes.Buffer
			code, err := runLocal(t, tc.text, strings.NewReader(tc.in), &out, &stderr)
			if err != nil || code != tc.code || out.String() != tc.out || stderr.String() != tc.stderr {
				t.Fatalf("code=%d err=%v stdout=%q stderr=%q", code, err, out.String(), stderr.String())
			}
		})
	}
}

func TestShellBrokerEveryByte(t *testing.T) {
	input := make([]byte, 256)
	for i := range input {
		input[i] = byte(i)
	}
	var out, stderr bytes.Buffer
	code, err := runLocal(t, "cat; exit 7", bytes.NewReader(input), &out, &stderr)
	if err != nil || code != 7 || !bytes.Equal(out.Bytes(), input) || stderr.Len() != 0 {
		t.Fatalf("binary failed: code=%d err=%v stdout length=%d stderr=%q", code, err, out.Len(), stderr.String())
	}
}

func TestShellBrokerDuplex(t *testing.T) {
	data := bytes.Repeat([]byte("abcdef\x00\xff\r\n"), 4096)
	var out, stderr bytes.Buffer
	// The stderr producer runs while cat consumes input; both exceed pipe sizes.
	text := "(i=0; while [ $i -lt 8192 ]; do printf 0123456789abcdef >&2; i=$((i+1)); done) & cat; wait"
	code, err := runLocal(t, text, bytes.NewReader(data), &out, &stderr)
	if err != nil || code != 0 || !bytes.Equal(out.Bytes(), data) || stderr.Len() != 8192*16 {
		t.Fatalf("duplex: code=%d err=%v stdout=%d/%d stderr=%s first=%q last=%q", code, err, out.Len(), len(data), strconv.Itoa(stderr.Len()), stderr.Bytes()[:min(stderr.Len(), 150)], stderr.Bytes()[max(0, stderr.Len()-150):])
	}
}

func TestShellBrokerBlockedInputDoesNotBlockOutput(t *testing.T) {
	data := bytes.Repeat([]byte{0xff}, 1024*1024)
	var out, stderr bytes.Buffer
	text := "i=0; while [ $i -lt 8192 ]; do printf 0123456789abcdef; i=$((i+1)); done; exit 7"
	code, err := runLocal(t, text, bytes.NewReader(data), &out, &stderr)
	if err != nil || code != 7 || out.Len() != 8192*16 || stderr.Len() != 0 {
		t.Fatalf("blocked stdin: code=%d err=%v out=%d stderr=%q", code, err, out.Len(), stderr.String())
	}
}

// Mirrors the native read-only stall fixture. With Bash 5.2, a timed read could
// report a timeout after consuming a record's newline; under stop-and-wait input
// the broker then waited for the rest of an already complete I record.
func TestShellBrokerMegabyteStdinCount(t *testing.T) {
	for _, tc := range []struct {
		name  string
		split bool
	}{{"whole-records", false}, {"late-newlines", true}} {
		t.Run(tc.name, func(t *testing.T) {
			input := make([]byte, 1024*1024)
			for i := range input {
				input[i] = byte(i)
			}
			var out, stderr bytes.Buffer
			text := `count=$(LC_ALL=C wc -c) || exit 124; printf "read-only-mib:%s\n" "$count"; exit 23`
			code, err := runLocalWithStream(t, text, func(context.CancelFunc) IO {
				return IO{Stdin: bytes.NewReader(input), Stdout: &out, Stderr: &stderr}
			}, nil, 120*time.Second, func(ctx context.Context, file *os.File) Stream {
				s := newTestStream(ctx, file)
				if tc.split {
					return lateNewlineStream{s}
				}
				return s
			})
			if err != nil || code != 23 || strings.Join(strings.Fields(out.String()), "") != "read-only-mib:1048576" || stderr.Len() != 0 {
				t.Fatalf("megabyte stdin: code=%d err=%v stdout=%q stderr=%q", code, err, out.String(), stderr.String())
			}
		})
	}
}

// lateNewlineStream delivers each record's terminator separately, around the
// broker's former 1ms read deadline. A fast local PTY otherwise hands Bash whole
// records, while the Pi's slower unbuffered TTY reads span that deadline.
type lateNewlineStream struct{ testStream }

func (s lateNewlineStream) Send(ctx context.Context, b []byte) error {
	body, ok := bytes.CutSuffix(b, []byte("\n"))
	if !ok || len(body) == 0 {
		return s.testStream.Send(ctx, b)
	}
	if err := s.testStream.Send(ctx, body); err != nil {
		return err
	}
	time.Sleep(time.Duration(500+rand.IntN(1000)) * time.Microsecond)
	return s.testStream.Send(ctx, []byte("\n"))
}

type slowWriter struct{ bytes.Buffer }

func (w *slowWriter) Write(b []byte) (int, error) {
	time.Sleep(time.Millisecond)
	return w.Buffer.Write(b)
}
func TestShellBrokerSlowConsumer(t *testing.T) {
	input := bytes.Repeat([]byte("abc\x00\xff\n"), 4096)
	var out slowWriter
	var stderr bytes.Buffer
	code, err := runLocal(t, "cat", bytes.NewReader(input), &out, &stderr)
	if err != nil || code != 0 || !bytes.Equal(out.Bytes(), input) || stderr.Len() != 0 {
		t.Fatalf("slow output: code=%d err=%v out=%d stderr=%q", code, err, out.Len(), stderr.String())
	}
}

// Pipelined input: several I records are in flight when the command closes
// stdin. Records arriving after K must be dropped without a protocol failure.
func TestShellBrokerEarlyCloseWithPipelinedInput(t *testing.T) {
	input := bytes.Repeat([]byte{0, 0xff, 'x', '\n'}, 256*1024)
	var out, stderr bytes.Buffer
	code, err := runLocal(t, "head -c 5000 >/dev/null; exec 0<&-; printf closed; exit 4", bytes.NewReader(input), &out, &stderr)
	if err != nil || code != 4 || out.String() != "closed" || stderr.Len() != 0 {
		t.Fatalf("code=%d err=%v stdout=%q stderr=%q", code, err, out.String(), stderr.String())
	}
}

type promptAnswerer struct {
	bytes.Buffer
	answer *io.PipeWriter
	sent   bool
}

func (w *promptAnswerer) Write(b []byte) (int, error) {
	n, err := w.Buffer.Write(b)
	if !w.sent && bytes.Contains(w.Bytes(), []byte("prompt>")) {
		w.sent = true
		go func() { _, _ = w.answer.Write([]byte("answer\n")); _ = w.answer.Close() }()
	}
	return n, err
}

// A short partial record must be delivered while the command waits for input;
// output readers may not hold bytes back to fill a larger record.
func TestShellBrokerPartialOutputIsNotHeldBack(t *testing.T) {
	stdin, answer := io.Pipe()
	out := &promptAnswerer{answer: answer}
	var stderr bytes.Buffer
	code, err := runLocal(t, `printf 'prompt>'; printf 'e>' >&2; read x; printf 'got:%s' "$x"`, stdin, out, &stderr)
	if err != nil || code != 0 || out.String() != "prompt>got:answer" || stderr.String() != "e>" {
		t.Fatalf("code=%d err=%v stdout=%q stderr=%q", code, err, out.String(), stderr.String())
	}
}
