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
	"os/signal"
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
	// The hangup cases need SIGHUP's default action in child processes. Under
	// nohup it is ignored and exec keeps that (the reference ssh then ignored
	// HUP, 2026-10-28); a handler installed here is reset to default at exec.
	if signal.Ignored(syscall.SIGHUP) {
		signal.Notify(make(chan os.Signal, 1), syscall.SIGHUP)
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
	if testTools != nil {
		testTools(tools)
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
	eachImpl(t, func(t *testing.T) {
		for i := 0; i < 5; i++ {
			_, err := runLocalControlled(t, "sleep 5", func(cancel context.CancelFunc) IO {
				return IO{Stdin: cancellationReader{cancel}}
			}, nil)
			if !errors.Is(err, context.Canceled) || errors.Is(err, ErrCleanupUnconfirmed) {
				t.Fatalf("startup cancellation: %v", err)
			}
		}
	})
}

func (w *cancellationWriter) Write(b []byte) (int, error) {
	n, err := w.Buffer.Write(b)
	if bytes.Contains(w.Bytes(), []byte("ready\n")) {
		w.cancel()
	}
	return n, err
}

func TestShellBrokerCancellationReapsBeforeCarrierClose(t *testing.T) {
	eachImpl(t, func(t *testing.T) {
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
	})
}

func TestShellBrokerStreamsAndStatus(t *testing.T) {
	eachImpl(t, func(t *testing.T) {
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
	})
}

func TestShellBrokerEveryByte(t *testing.T) {
	eachImpl(t, func(t *testing.T) {
		input := make([]byte, 256)
		for i := range input {
			input[i] = byte(i)
		}
		var out, stderr bytes.Buffer
		code, err := runLocal(t, "cat; exit 7", bytes.NewReader(input), &out, &stderr)
		if err != nil || code != 7 || !bytes.Equal(out.Bytes(), input) || stderr.Len() != 0 {
			t.Fatalf("binary failed: code=%d err=%v stdout length=%d stderr=%q", code, err, out.Len(), stderr.String())
		}
	})
}

func TestShellBrokerDuplex(t *testing.T) {
	eachImpl(t, func(t *testing.T) {
		data := bytes.Repeat([]byte("abcdef\x00\xff\r\n"), 4096)
		var out, stderr bytes.Buffer
		// The stderr producer runs while cat consumes input; both exceed pipe sizes.
		text := "(i=0; while [ $i -lt 8192 ]; do printf 0123456789abcdef >&2; i=$((i+1)); done) & cat; wait"
		code, err := runLocal(t, text, bytes.NewReader(data), &out, &stderr)
		if err != nil || code != 0 || !bytes.Equal(out.Bytes(), data) || stderr.Len() != 8192*16 {
			t.Fatalf("duplex: code=%d err=%v stdout=%d/%d stderr=%s first=%q last=%q", code, err, out.Len(), len(data), strconv.Itoa(stderr.Len()), stderr.Bytes()[:min(stderr.Len(), 150)], stderr.Bytes()[max(0, stderr.Len()-150):])
		}
	})
}

func TestShellBrokerBlockedInputDoesNotBlockOutput(t *testing.T) {
	eachImpl(t, func(t *testing.T) {
		data := bytes.Repeat([]byte{0xff}, 1024*1024)
		var out, stderr bytes.Buffer
		text := "i=0; while [ $i -lt 8192 ]; do printf 0123456789abcdef; i=$((i+1)); done; exit 7"
		code, err := runLocal(t, text, bytes.NewReader(data), &out, &stderr)
		if err != nil || code != 7 || out.Len() != 8192*16 || stderr.Len() != 0 {
			t.Fatalf("blocked stdin: code=%d err=%v out=%d stderr=%q", code, err, out.Len(), stderr.String())
		}
	})
}

// Mirrors the native read-only stall fixture. With Bash 5.2, a timed read could
// report a timeout after consuming a record's newline; under stop-and-wait input
// the broker then waited for the rest of an already complete I record.
func TestShellBrokerMegabyteStdinCount(t *testing.T) {
	eachImpl(t, func(t *testing.T) {
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
	})
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
	eachImpl(t, func(t *testing.T) {
		input := bytes.Repeat([]byte("abc\x00\xff\n"), 4096)
		var out slowWriter
		var stderr bytes.Buffer
		code, err := runLocal(t, "cat", bytes.NewReader(input), &out, &stderr)
		if err != nil || code != 0 || !bytes.Equal(out.Bytes(), input) || stderr.Len() != 0 {
			t.Fatalf("slow output: code=%d err=%v out=%d stderr=%q", code, err, out.Len(), stderr.String())
		}
	})
}

// Pipelined input: several I records are in flight when the command closes
// stdin. Records arriving after K must be dropped without a protocol failure.
func TestShellBrokerEarlyCloseWithPipelinedInput(t *testing.T) {
	eachImpl(t, func(t *testing.T) {
		input := bytes.Repeat([]byte{0, 0xff, 'x', '\n'}, 256*1024)
		var out, stderr bytes.Buffer
		code, err := runLocal(t, "head -c 5000 >/dev/null; exec 0<&-; printf closed; exit 4", bytes.NewReader(input), &out, &stderr)
		if err != nil || code != 4 || out.String() != "closed" || stderr.Len() != 0 {
			t.Fatalf("code=%d err=%v stdout=%q stderr=%q", code, err, out.String(), stderr.String())
		}
	})
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
	eachImpl(t, func(t *testing.T) {
		stdin, answer := io.Pipe()
		out := &promptAnswerer{answer: answer}
		var stderr bytes.Buffer
		code, err := runLocal(t, `printf 'prompt>'; printf 'e>' >&2; read x; printf 'got:%s' "$x"`, stdin, out, &stderr)
		if err != nil || code != 0 || out.String() != "prompt>got:answer" || stderr.String() != "e>" {
			t.Fatalf("code=%d err=%v stdout=%q stderr=%q", code, err, out.String(), stderr.String())
		}
	})
}

// eachImpl runs a broker test with the Perl broker and with the Bash one
// (PI_SH_REMOTE_IMPL=bash), as eachConnector does for the forwarding adapter.
func eachImpl(t *testing.T, test func(*testing.T)) {
	for _, impl := range []string{"perl", "bash"} {
		t.Run(impl, func(t *testing.T) {
			if impl == "perl" {
				if _, err := exec.LookPath("perl"); err != nil {
					t.Skip("perl unavailable")
				}
				t.Setenv("PI_SH_REMOTE_IMPL", "")
				t.Setenv("PI_SH_BROKER_WORKERS", "")
			} else {
				t.Setenv("PI_SH_REMOTE_IMPL", "bash")
			}
			test(t)
		})
	}
}

// implProbe prints the program of the broker running this command and how
// many perl workers (two output readers, one input writer) it has. The broker
// is the command's parent or, where setsid forks (Linux), its grandparent;
// the Perl broker has no workers and never forks setsid.
const implProbe = `a=$PPID; b=$(ps -o ppid= -p $PPID | tr -d " "); p=$(ps -o args= -p $PPID); p=${p%% *}; p=${p##*/}; printf "%s " "${p%%[0-9.]*}"; ps -A -o pid=,ppid=,args= | awk -v a="$a" -v b="$b" '($2==a || $2==b) && $1!=a && $3 ~ /(^|\/)perl[0-9.]*$/ && $4=="-e"' | wc -l | tr -d " "`

// With perl on PATH the whole broker is Perl; PI_SH_REMOTE_IMPL=bash, or the
// older PI_SH_BROKER_WORKERS=bash, keeps the Bash broker and Bash workers.
func TestShellBrokerUsesPerlBrokerWhenPresent(t *testing.T) {
	if _, err := exec.LookPath("perl"); err != nil {
		t.Skip("perl unavailable")
	}
	for _, c := range []struct{ name, value, want string }{
		{"PI_SH_REMOTE_IMPL", "", "perl 0"},
		{"PI_SH_REMOTE_IMPL", "bash", "bash 0"},
		{"PI_SH_BROKER_WORKERS", "bash", "bash 0"},
	} {
		t.Setenv("PI_SH_REMOTE_IMPL", "")
		t.Setenv("PI_SH_BROKER_WORKERS", "")
		t.Setenv(c.name, c.value)
		// Unread input would keep a Bash writer blocked, so it is still there.
		var out bytes.Buffer
		if code, err := runLocal(t, implProbe, bytes.NewReader(make([]byte, 1<<20)), &out, io.Discard); err != nil || code != 0 || strings.TrimSpace(out.String()) != c.want {
			t.Fatalf("%s=%q: %q, code %d, %v", c.name, c.value, out.String(), code, err)
		}
	}
}

// testTools, when set, adds programs to the PATH directory of runLocal.
var testTools func(dir string)

// A perl that cannot compile the Perl broker leaves the Bash broker in place,
// with the same data: with Perl workers when perl compiles those, else with
// Bash workers. TestBrokerPerlHasNoApostrophe names a comment of broker.pl
// only, so the stub recognizes that program by it.
func TestShellBrokerFallsBackToBash(t *testing.T) {
	path, err := exec.LookPath("perl")
	if err != nil {
		t.Skip("perl unavailable")
	}
	t.Setenv("PI_SH_REMOTE_IMPL", "")
	t.Setenv("PI_SH_BROKER_WORKERS", "")
	for _, c := range []struct{ name, refuse, want string }{
		{"no-perl-program", "*", "bash 0"},
		{"no-perl-broker", "*TestBrokerPerlHasNoApostrophe*", "bash 3"},
	} {
		t.Run(c.name, func(t *testing.T) {
			testTools = func(dir string) {
				stub := "#!/bin/sh\nc=0 r=0\nfor a; do case $a in -c) c=1;; " + c.refuse + ") r=1;; esac; done\n[ $c$r = 11 ] && exit 1\nexec " + path + " \"$@\"\n"
				if err := os.WriteFile(filepath.Join(dir, "perl"), []byte(stub), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			t.Cleanup(func() { testTools = nil })
			var out bytes.Buffer
			if code, err := runLocal(t, implProbe, bytes.NewReader(make([]byte, 1<<20)), &out, io.Discard); err != nil || code != 0 || strings.TrimSpace(out.String()) != c.want {
				t.Fatalf("broker %q, code %d, %v", out.String(), code, err)
			}
			input := make([]byte, 256)
			for i := range input {
				input[i] = byte(i)
			}
			out.Reset()
			if code, err := runLocal(t, "cat", bytes.NewReader(input), &out, io.Discard); err != nil || code != 0 || !bytes.Equal(out.Bytes(), input) {
				t.Fatalf("bash broker: %d bytes, code %d, %v", out.Len(), code, err)
			}
		})
	}
}

// The command starts with the same environment, ignored signals, process
// group and session in both brokers: the Perl broker restores the
// dispositions it started with, as the Bash coprocess did with trap -.
func TestShellBrokerImplsGiveCommandSameContext(t *testing.T) {
	if _, err := exec.LookPath("perl"); err != nil {
		t.Skip("perl unavailable")
	}
	// PATH and PI_SH_* are set per subtest by the harness.
	text := `env | LC_ALL=C sort | grep -Ev '^(_|PATH|PI_SH_[A-Z_]*)=';perl -e 'print join(",", grep { ($SIG{$_} // "") eq "IGNORE" } sort keys %SIG), "\n"'; ` +
		`[ "$(ps -o pgid= -p $$ | tr -d " ")" = $$ ] && echo group-leader; ` +
		`[ "$(ps -o sess= -p $$ | tr -d " ")" = "$(ps -o sess= -p $PPID | tr -d " ")" ] || echo own-session; ulimit -a`
	got := map[string]string{}
	eachImpl(t, func(t *testing.T) {
		var out, stderr bytes.Buffer
		if code, err := runLocal(t, text, nil, &out, &stderr); err != nil || code != 0 {
			t.Fatalf("code %d, %v, stderr %q", code, err, stderr.String())
		}
		got[filepath.Base(t.Name())] = out.String() + stderr.String()
	})
	if got["perl"] != got["bash"] {
		t.Fatalf("perl broker:\n%s\nbash broker:\n%s", got["perl"], got["bash"])
	}
	if !strings.Contains(got["perl"], "group-leader") {
		t.Fatalf("command does not lead its own group:\n%s", got["perl"])
	}
}

// hangupWriter closes the carrier PTY once the command printed its group.
type hangupWriter struct {
	bytes.Buffer
	file *os.File
}

func (w *hangupWriter) Write(b []byte) (int, error) {
	n, err := w.Buffer.Write(b)
	if bytes.Contains(w.Bytes(), []byte("\n")) && w.file != nil {
		w.file.Close()
		w.file = nil
	}
	return n, err
}

// Losing the carrier hangs up the broker, which must kill the command group
// (here a shell and a sleep that would outlive it) before exiting.
func TestShellBrokerCarrierLossKillsCommandGroup(t *testing.T) {
	eachImpl(t, func(t *testing.T) {
		out := &hangupWriter{}
		start := time.Now()
		_, err := runLocalWithStream(t, `sleep 300 & printf '%s\n' "$$"; wait`, func(context.CancelFunc) IO {
			return IO{Stdout: out}
		}, nil, 20*time.Second, func(ctx context.Context, file *os.File) Stream {
			out.file = file
			return newTestStream(ctx, file)
		})
		if err == nil {
			t.Fatal("carrier loss reported success")
		}
		pid, e := strconv.Atoi(strings.TrimSpace(out.String()))
		if e != nil || pid <= 1 {
			t.Fatalf("no command group: %q", out.String())
		}
		// The harness waits for the carrier process, so this also bounds how
		// long the broker took to clean up and exit. A leak keeps the group
		// for 300 s; 20 s leaves room for a loaded machine (8 s was exceeded
		// once with the Bash broker while three packages ran, 2026-10-29).
		deadline := start.Add(20 * time.Second)
		for syscall.Kill(-pid, 0) == nil {
			if time.Now().After(deadline) {
				_ = syscall.Kill(-pid, syscall.SIGKILL)
				t.Fatal("command group survived the carrier")
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Logf("command group gone %v after start", time.Since(start).Round(time.Millisecond))
	})
}

// Bash before 5.2 has no patsub_replacement; its loader decodes each chunk
// with the per-byte loop, which must give the same source and command.
func TestLoaderPerByteDecodePath(t *testing.T) {
	testLoader = func(loader string) string {
		patched := strings.Replace(loader, "&&f=1;", ";", 1)
		if patched == loader {
			t.Fatal("loader fast-path switch not found")
		}
		return patched
	}
	t.Cleanup(func() { testLoader = nil })
	eachImpl(t, func(t *testing.T) {
		var out bytes.Buffer
		if code, err := runLocal(t, "printf 'caf\\303\\251 %s' \"$(printf 'x%.0s' $(seq 1 3000) | wc -c | tr -d ' ')\"; exit 3", nil, &out, io.Discard); err != nil || code != 3 || out.String() != "café 3000" {
			t.Fatalf("code %d, %v, %q", code, err, out.String())
		}
	})
}

// A slow local stdout holds the broker's output while input keeps coming:
// the broker must keep taking input (SHELL_BROKER.md "Perl broker"). A Perl
// broker that blocked on carrier output, with no relay pipe to absorb the
// in-flight input, hung here with Run blocked sending input.
func TestShellBrokerSlowOutputWithFastInput(t *testing.T) {
	eachImpl(t, func(t *testing.T) {
		input := bytes.Repeat([]byte("0123456789abcdef"), 64*1024)
		var out slowWriter
		code, err := runLocalControlledWithin(t, "cat", func(context.CancelFunc) IO {
			return IO{Stdin: bytes.NewReader(input), Stdout: &out}
		}, nil, 60*time.Second)
		if err != nil || code != 0 || !bytes.Equal(out.Bytes(), input) {
			t.Fatalf("code %d, %v, %d of %d bytes", code, err, out.Len(), len(input))
		}
	})
}
