//go:build darwin || linux

package shellbroker

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
)

// Exercise the production loader, source upload, readiness and result parser,
// rather than the independent job-control prototype in tty_jobs_unix_test.go.
func runStagedTTY(t *testing.T, text string, input []byte, resize bool, settings ...TTYSettings) (int, string, error) {
	t.Helper()
	return runStagedTTYCarrier(t, "/bin/sh", text, input, resize, settings...)
}

func runStagedTTYCarrier(t *testing.T, carrier, text string, input []byte, resize bool, settings ...TTYSettings) (int, string, error) {
	t.Helper()
	var configuration TTYSettings
	if len(settings) > 0 {
		configuration = settings[0]
	}
	plan, err := NewTTYConfigured(text, configuration)
	if err != nil {
		t.Fatal(err)
	}
	return runStagedTTYPlan(t, carrier, plan, input, resize)
}

type stagedTTYHooks struct {
	ready       func(context.Context, *exec.Cmd, *os.File, *ttyWatch) error
	beforeClose func()
}

func runStagedTTYPlan(t *testing.T, carrier string, plan *TTYPlan, input []byte, resize bool, hooks ...stagedTTYHooks) (int, string, error) {
	t.Helper()
	text := plan.plan.text
	tools := t.TempDir()
	if err := os.Symlink(modernTTYBash(t), filepath.Join(tools, "bash")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.Command(carrier, "-i")
	if carrier != "/bin/sh" {
		cmd = exec.Command(carrier, "--noprofile", "--norc", "-i")
	}
	cmd.Env = append(os.Environ(), "PATH="+tools+":"+os.Getenv("PATH"), "HISTFILE=/dev/null", "ENV=/dev/null", "BASH_ENV=/dev/null", "SHELL=/bin/sh")
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 24, Cols: 80})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { f.Close(); cmd.Process.Signal(syscall.SIGHUP); _ = cmd.Wait() }()
	w := &ttyWatch{ready: make(chan struct{})}
	code, err := plan.Run(ctx, newTestStream(ctx, f), TTYIO{Stdout: w, Input: func(ctx context.Context, stream Stream) error {
		if len(hooks) > 0 && hooks[0].ready != nil {
			select {
			case <-w.ready:
			case <-ctx.Done():
				return ctx.Err()
			}
			if err := hooks[0].ready(ctx, cmd, f, w); err != nil {
				return err
			}
		}
		if len(input) == 0 && !resize {
			return io.EOF
		}
		if strings.Contains(text, "fixture-ready") {
			select {
			case <-w.ready:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if resize {
			if err := pty.Setsize(f, &pty.Winsize{Rows: 35, Cols: 111}); err != nil {
				return err
			}
		}
		return sendTTYInput(ctx, w, input, func(b []byte) error { return stream.Send(ctx, b) })
	}})
	if len(hooks) > 0 && hooks[0].beforeClose != nil {
		hooks[0].beforeClose()
	}
	return code, w.text(), err
}

func TestStagedTTYInitialSettings(t *testing.T) {
	value := "xterm-é'; printf injected; #"
	code, out, err := runStagedTTY(t, "printf 'term:%s\\n' \"$TERM\"; stty size", nil, false,
		TTYSettings{TERM: &value, Size: &TTYSize{Columns: 79, Rows: 17}})
	if err != nil || code != 0 || out != "term:"+value+"\r\n17 79\r\n" {
		t.Fatalf("initial TERM/size: %d %q %v", code, out, err)
	}
	code, out, err = runStagedTTY(t, "stty size", nil, false, TTYSettings{Size: &TTYSize{}})
	if err != nil || code != 0 || out != "0 0\r\n" {
		t.Fatalf("zero dimensions changed: %d %q %v", code, out, err)
	}
}

func TestStagedTTYTerminalBehavior(t *testing.T) {
	for _, tc := range ttyFunctionalCases() {
		t.Run(tc.name, func(t *testing.T) {
			code, out, err := runStagedTTY(t, tc.text, tc.input, tc.resize)
			if err != nil || code != tc.code || !strings.Contains(out, tc.want) {
				t.Fatalf("staged TTY: code %d want %d, error %v, output %q", code, tc.code, err, out)
			}
		})
	}
}

func TestStagedTTYExitSignalAndFirstBytes(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		input      []byte
		code       int
	}{
		{"zero", "printf first; exit 0", nil, 0},
		{"exit130", "printf first; exit 130", nil, 130},
		{"exit143", "printf first; exit 143", nil, 143},
		{"int", "printf first; kill -INT $$", nil, 255},
		{"term", "printf first; kill -TERM $$", nil, 255},
		{"kill", "printf first; kill -KILL $$", nil, 255},
		{"ctrl-c", "printf 'first fixture-ready\\n'; sleep 60", []byte{3}, 255},
		{"caught-int", "trap 'exit 42' INT; printf 'first fixture-ready\\n'; IFS= read -r x", []byte{3}, 42},
		{"background-child", "sleep 60 & printf first; exit 0", nil, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, out, err := runStagedTTY(t, tc.text, tc.input, false)
			if err != nil || code != tc.code || !strings.HasPrefix(out, "first") {
				t.Fatalf("staged TTY: code %d want %d, error %v, output %q", code, tc.code, err, out)
			}
		})
	}
}

func TestStagedTTYCancellationDoesNotClaimCleanup(t *testing.T) {
	_, _, err := runStagedTTY(t, "printf fixture-ready; IFS= read -r x", nil, false)
	if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, ErrCleanupUnconfirmed) {
		t.Fatalf("TTY cancellation must report unconfirmed cleanup: %v", err)
	}
}

func TestStagedTTYEarlyInputAndLongCommand(t *testing.T) {
	// There is no command-output prompt here: input is sent as soon as the
	// adapter's actual TR frame is parsed. Staging/stty must not eat these bytes.
	code, out, err := runStagedTTY(t, "IFS= read -r x; printf 'seen:%s' \"$x\"; exit 17", []byte("l'amour é 世界\r"), false)
	if err != nil || code != 17 || !strings.Contains(out, "seen:l'amour é 世界") {
		t.Fatalf("early input: %d %q %v", code, out, err)
	}
	// Commands travel over the staged pipe, not a canonical terminal line or
	// an exec argument. Include literal newlines and multibyte UTF-8 at the bound.
	text := "#" + strings.Repeat("x", 128*1024-len("#\nprintf 'long-é'; exit 19")) + "\nprintf 'long-é'; exit 19"
	code, out, err = runStagedTTY(t, text, nil, false)
	if err != nil || code != 19 || out != "long-é" {
		t.Fatalf("long command: %d %q %v", code, out, err)
	}
}

func TestStagedTTYOpenSSHDifferential(t *testing.T) {
	reference := ttySSHCommand(t)
	for _, tc := range ttyFunctionalCases() {
		t.Run(tc.name, func(t *testing.T) {
			code, out, err := runStagedTTY(t, tc.text, tc.input, tc.resize)
			refCode, refOut := runPTYFixture(t, reference(tc.text), "", tc.input, tc.resize)
			if err != nil || code != refCode || !strings.Contains(out, tc.want) || !strings.Contains(refOut, tc.want) {
				t.Fatalf("production/reference: code %d/%d, error %v, output %q/%q", code, refCode, err, out, refOut)
			}
			if tc.resize && (!strings.Contains(out, "24 80\r\n") || !strings.Contains(refOut, "24 80\r\n")) {
				t.Fatal("initial size was not retained")
			}
		})
	}
}
