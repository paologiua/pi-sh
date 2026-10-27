//go:build darwin || linux

package shellbroker

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The fixtures run SHELL=/bin/sh. A private HOME supplies a profile, so these
// observe login-shell startup without reading the developer's own files.
func loginFixtureHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, ".profile"), []byte("unset HISTFILE\nprintf 'profile-ran\\n'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("ENV", "")
	return home
}

func TestStagedTTYLoginShell(t *testing.T) {
	home := loginFixtureHome(t)
	input := "printf 'argv0:%s pwd:%s\\n' \"$0\" \"$PWD\"; test -t 0 && test -t 1 && test -t 2 && printf 'tty:yes\\n'; stty size; exit 23\r"
	code, out, err := runStagedTTY(t, LoginShell, []byte(input), false, TTYSettings{Size: &TTYSize{Columns: 91, Rows: 29}})
	if err != nil || code != 23 {
		t.Fatalf("login shell status: %d %v %q", code, err, out)
	}
	for _, want := range []string{"profile-ran\r\n", "argv0:-sh pwd:" + home + "\r\n", "tty:yes\r\n", "29 91\r\n"} {
		if !strings.Contains(out, want) {
			t.Fatalf("login shell output lacks %q: %q", want, out)
		}
	}
}

func TestStagedTTYLoginShellEOFLogout(t *testing.T) {
	loginFixtureHome(t)
	// Ctrl-D on an empty interactive line ends the login shell with status 0.
	code, out, err := runStagedTTY(t, LoginShell, []byte("exit_value=0\r\x04"), false)
	if err != nil || code != 0 || !strings.Contains(out, "profile-ran") {
		t.Fatalf("login shell EOF: %d %v %q", code, err, out)
	}
}

func TestBrokerLoginShellWithoutTTY(t *testing.T) {
	home := loginFixtureHome(t)
	var out, stderr bytes.Buffer
	input := "printf 'argv0:%s pwd:%s\\n' \"$0\" \"$PWD\"; test -t 0 || printf 'tty:no\\n'; exit 7\n"
	code, err := runLocal(t, LoginShell, strings.NewReader(input), &out, &stderr)
	if err != nil || code != 7 {
		t.Fatalf("no-TTY login shell: %d %v %q %q", code, err, out.String(), stderr.String())
	}
	// Bash reads no profile for a noninteractive argv0 login shell, as with sshd.
	if out.String() != "argv0:-sh pwd:"+home+"\ntty:no\n" {
		t.Fatalf("no-TTY login shell output: %q stderr %q", out.String(), stderr.String())
	}
	// Exhausted stdin (-n) ends a noninteractive login shell successfully.
	out.Reset()
	code, err = runLocal(t, LoginShell, nil, &out, &stderr)
	if err != nil || code != 0 || out.String() != "" {
		t.Fatalf("null-stdin login shell: %d %v %q", code, err, out.String())
	}
}

func TestLoginShellMissingHome(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent")
	t.Setenv("HOME", missing)
	t.Setenv("ENV", "")
	var out, stderr bytes.Buffer
	code, err := runLocal(t, LoginShell, strings.NewReader("exit 5\n"), &out, &stderr)
	if err != nil || code != 5 || !strings.Contains(stderr.String(), "Could not chdir to home directory "+missing) {
		t.Fatalf("missing home: %d %v %q", code, err, stderr.String())
	}
}

// ssh without a command asks sshd to exec the account's login shell. The local
// reference account's shell and home differ from the fixture's /bin/sh and
// private HOME, so compare the conventions: argv0 is "-" plus the basename of
// $SHELL, the working directory is $HOME, TTY presence and the exit status.
func TestLoginShellOpenSSHDifferential(t *testing.T) {
	command := localSSHCommand(t)
	probe := `printf 'login=%s=%s=%s=%s=\n' "$0" "${SHELL##*/}" "$PWD" "$HOME"; if test -t 0; then printf 'tty=yes=\n'; else printf 'tty=no=\n'; fi; exit 7`
	pattern := regexp.MustCompile(`login=(-[^=]*)=([^=]*)=([^=]*)=([^=]*)=`)
	conventions := func(t *testing.T, side, out string) string {
		t.Helper()
		m := pattern.FindStringSubmatch(out)
		if m == nil {
			t.Fatalf("%s printed no login line: %q", side, out)
		}
		tty := "tty=no="
		if strings.Contains(out, "tty=yes=") {
			tty = "tty=yes="
		}
		return fmt.Sprintf("argv0-matches-shell=%v pwd-is-home=%v %s", m[1] == "-"+m[2], m[3] == m[4], tty)
	}
	for _, mode := range []string{"-T", "-tt"} {
		t.Run(mode, func(t *testing.T) {
			cmd := command("", nil)
			args := append([]string(nil), cmd.Args[:len(cmd.Args)-1]...) // no command
			for i, arg := range args {
				if arg == "-T" {
					args[i] = mode
				}
			}
			cmd.Args = args
			line := probe + "\n"
			if mode == "-tt" {
				line = probe + "\r"
			}
			cmd.Stdin = strings.NewReader(line)
			wantCode, wantOut, _ := runSSHReference(t, cmd)
			want := conventions(t, "OpenSSH", string(wantOut))
			home := loginFixtureHome(t)
			var code int
			var out string
			var err error
			if mode == "-T" {
				var buffer, stderr bytes.Buffer
				code, err = runLocal(t, LoginShell, strings.NewReader(line), &buffer, &stderr)
				out = buffer.String()
			} else {
				code, out, err = runStagedTTY(t, LoginShell, []byte(line), false)
			}
			if err != nil {
				t.Fatal(err)
			}
			got := conventions(t, "pi-sh", out)
			if code != wantCode || got != want || !strings.Contains(out, "="+home+"="+home+"=") {
				t.Fatalf("pi-sh %d %q, OpenSSH %d %q; output %q", code, got, wantCode, want, out)
			}
		})
	}
}
