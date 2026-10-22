//go:build darwin || linux

package command

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
)

type ptyStream struct{ file *os.File }

func (s ptyStream) Send(_ context.Context, b []byte) error { _, err := s.file.Write(b); return err }
func (s ptyStream) Receive(context.Context) ([]byte, error) {
	b := make([]byte, 1024)
	n, err := s.file.Read(b)
	if n > 0 {
		return b[:n], nil
	}
	if errors.Is(err, syscall.EIO) {
		err = io.EOF
	}
	return nil, err
}

func TestRealPTYCommandStatusAndOutput(t *testing.T) {
	for _, tc := range []struct {
		name, text, want string
		code             int
	}{
		{"success", "printf 'hello\\n'", "hello\n", 0},
		{"false", "false", "", 1},
		{"exit", "exit 23", "", 23},
		{"quotes-unicode", "printf '%s' \"l'amour é 世界\"", "l'amour é 世界", 0},
		{"merged-stderr", "printf out; printf err >&2; exit 7", "outerr", 7},
		{"null-stdin", "read x; exit $?", "", 1},
		{"binary-output", "printf '\\000\\377\\015\\012'", "\x00\xff\r\n", 0},
		{"exec", "exec /bin/sh -c 'exit 42'", "", 42},
		{"status255", "exit 255", "", 255},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "/bin/sh", "-i")
			cmd.Env = append(os.Environ(), "HISTFILE=/dev/null", "ENV=/dev/null", "BASH_ENV=/dev/null")
			file, err := pty.Start(cmd)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			defer func() { cancel(); _ = cmd.Wait() }()
			plan, err := New(tc.text)
			if err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			code, err := plan.Run(ctx, ptyStream{file}, &out)
			if err != nil || code != tc.code || out.String() != tc.want {
				t.Fatalf("code=%d err=%v output=%q; want %d %q", code, err, out.String(), tc.code, tc.want)
			}
		})
	}
}

func TestSupervisorLossDoesNotInventSuccess(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/sh", "-i")
	cmd.Env = append(os.Environ(), "HISTFILE=/dev/null", "ENV=/dev/null", "BASH_ENV=/dev/null")
	file, err := pty.Start(cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	defer func() { cancel(); _ = cmd.Wait() }()
	// This kills only the supervisor started for this local test command.
	plan, err := New("kill -KILL \"$PPID\"")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := plan.Run(ctx, ptyStream{file}, io.Discard); !errors.Is(err, ErrStatus) {
		t.Fatalf("missing supervisor status was not rejected: %v", err)
	}
}
