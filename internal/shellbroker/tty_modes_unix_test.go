//go:build darwin || linux

package shellbroker

import (
	"context"
	"os"
	"os/exec"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	"pi-sh/internal/terminal"
	"pi-sh/internal/ttymode"
)

func TestStagedTTYModesOpenSSHDifferential(t *testing.T) {
	reference := ttySSHCommand(t)
	for _, tc := range []struct {
		name, text, want string
		input            []byte
		code             int
		modes            *ttymode.Settings
	}{
		{"echo-off", "printf fixture-ready; IFS= read -r x; printf 'seen:%s' \"$x\"", "fixture-readyseen:secret-input", []byte("secret-input\r"), 0, &ttymode.Settings{Flags: map[string]bool{"echo": false}}},
		{"canonical-eof-seven", "printf fixture-ready; IFS= read -r x; printf 'read:%s' \"$?\"", "fixture-readyread:1", []byte{7}, 0, &ttymode.Settings{Flags: map[string]bool{"echo": false, "icanon": true}, Characters: map[string]uint32{"eof": 7}}},
		{"intr-29", "trap 'printf caught; exit 42' INT; printf fixture-ready; IFS= read -r x", "fixture-readycaught", []byte{29}, 42, &ttymode.Settings{Flags: map[string]bool{"echo": false, "isig": true}, Characters: map[string]uint32{"intr": 29}}},
		{"raw-eof-data", "printf fixture-ready; c=$(dd bs=1 count=1 2>/dev/null | od -An -tu1 | tr -d '[:space:]'); printf 'byte:%s' \"$c\"", "fixture-readybyte:4", []byte{4}, 0, &ttymode.Settings{Flags: map[string]bool{"echo": false, "icanon": false, "isig": false}}},
		{"no-output-mapping", "printf 'fixture-ready\\nline\\n'", "fixture-ready\nline\n", nil, 0, &ttymode.Settings{Flags: map[string]bool{"opost": false}}},
		{"disabled-intr", "printf fixture-ready; IFS= read -r x; printf 'read:%s' \"$?\"", "fixture-readyread:0", []byte{3, '\r'}, 0, &ttymode.Settings{Flags: map[string]bool{"echo": false}, Characters: map[string]uint32{"intr": 255}}},
		{"baud19200", "printf 'fixture-ready\\n'; stty speed", "19200", nil, 0, &ttymode.Settings{Baud: &ttymode.Baud{Input: 19200, Output: 19200}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, out, err := runStagedTTY(t, tc.text, tc.input, false, TTYSettings{Modes: tc.modes})
			if err != nil || code != tc.code || !strings.Contains(out, tc.want) {
				t.Fatalf("staged mode: %d %v %q", code, err, out)
			}
			// Set only this local reference client's ephemeral PTY before exec.
			// OpenSSH captures those modes and negotiates them with local sshd.
			args, err := tc.modes.SttyArgs()
			if err != nil {
				t.Fatal(err)
			}
			script := "stty"
			for _, arg := range args {
				script += " " + quote(arg)
			}
			script += " || exit 125; exec \"$@\""
			cmd := reference(tc.text)
			referenceArgs := append([]string{cmd.Path, "-q"}, cmd.Args[1:]...)
			wrappedArgs := append([]string{"-c", script, "tty-mode-reference"}, referenceArgs...)
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			refCode, refOut := runPTYFixture(t, exec.CommandContext(ctx, "/bin/sh", wrappedArgs...), "", tc.input, false)
			if refCode != tc.code || !strings.Contains(refOut, tc.want) {
				t.Fatalf("reference mode: %d %q", refCode, refOut)
			}
			if tc.name == "echo-off" && (out != tc.want || refOut != tc.want) {
				t.Fatal("unexpected echo or output", out, refOut)
			}
		})
	}
}

func TestStagedTTYControlArgumentsDoNotBreakSource(t *testing.T) {
	// These include shell metacharacters and invalid-UTF-8 bytes in mode values.
	// The carrier still receives only ASCII hex source records, and the child
	// command has no injected shell fragment. VQUIT is not exercised as input.
	for _, value := range []uint32{0, 32, 39, 36, 59, 92, 94, 96, 127, 128, 200, 254, 255} {
		code, out, err := runStagedTTY(t, "printf first; exit 19", nil, false, TTYSettings{Modes: &ttymode.Settings{Characters: map[string]uint32{"quit": value}}})
		if err != nil || code != 19 || out != "first" {
			t.Fatalf("control%d: %d %q %v", value, code, out, err)
		}
	}
}

// This executes the production dialect helper, not a test-only approximation.
// Every byte is checked in kernel state; other captured modes must stay intact.
func TestControlByteSttyEncoding(t *testing.T) {
	bash := modernTTYBash(t)
	m, file, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	defer file.Close()
	before, err := terminal.CaptureModes(file)
	if err != nil {
		t.Fatal(err)
	}
	script := strings.TrimSuffix(ttySource, "_pt_main\n") + "\n_pt_stty_modes \"$@\""
	for value := 0; value <= 255; value++ {
		settings := &ttymode.Settings{Characters: map[string]uint32{"intr": uint32(value)}}
		args, err := settings.SttyArgs()
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(bash, append([]string{"--noprofile", "--norc", "-c", script, "mode-test"}, args...)...)
		cmd.Stdin = file
		cmd.Env = append(os.Environ(), "LC_ALL=C")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("byte%d: %v %q", value, err, out)
		}
		after, err := terminal.CaptureModes(file)
		if err != nil {
			t.Fatal(err)
		}
		want := uint32(value)
		if runtime.GOOS == "linux" && value == 0 {
			want = 255
		}
		if after.Characters["intr"] != want {
			t.Fatalf("byte%d became%d want%d", value, after.Characters["intr"], want)
		}
		before.Characters["intr"] = want
		if !reflect.DeepEqual(before, after) {
			t.Fatalf("byte%d changed unrelated modes", value)
		}
	}
}
