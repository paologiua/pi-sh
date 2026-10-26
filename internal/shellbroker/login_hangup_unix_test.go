//go:build darwin || linux

package shellbroker

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// When the carrier terminal hangs up (the expected effect of a lost Connect
// session on the agent's PTY, not observed natively), the interactive login
// shell and its jobs receive SIGHUP exactly as under sshd: the kernel signals
// the foreground group and Bash resends HUP to its jobs. The client reports
// unconfirmed cleanup; this local fixture checks what the remote side does.
func TestStagedTTYLoginShellCarrierHangup(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, ".profile"), []byte("unset HISTFILE; set +H\nprintf 'fixture-ready\\n'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("ENV", "")
	plan, err := NewTTY(LoginShell)
	if err != nil {
		t.Fatal(err)
	}
	pattern := regexp.MustCompile(`pids=([0-9]+)=([0-9]+)=([0-9]+)=`)
	var pids []string
	hangup := errors.New("fixture carrier hangup")
	_, out, err := runStagedTTYPlan(t, "/bin/sh", plan, nil, false, stagedTTYHooks{
		ready: func(ctx context.Context, _ *exec.Cmd, carrier *os.File, w *ttyWatch) error {
			// A background job and a stopped-free foreground job (sleep).
			if _, err := carrier.WriteString("sleep 300 & printf 'pids=%s=%s=' $$ $!; sh -c 'printf \"%s=\\n\" $$; exec sleep 300'\r"); err != nil {
				return err
			}
			deadline := time.Now().Add(5 * time.Second)
			for {
				if m := pattern.FindStringSubmatch(w.text()); m != nil {
					pids = m[1:]
					break
				}
				if time.Now().After(deadline) {
					return errors.New("fixture pids not printed")
				}
				time.Sleep(5 * time.Millisecond)
			}
			// Closing the only master descriptor hangs up the carrier terminal.
			if err := carrier.Close(); err != nil {
				return err
			}
			return hangup
		},
	})
	if err == nil || !errors.Is(err, ErrCleanupUnconfirmed) {
		t.Fatalf("carrier hangup claimed cleanup or status: %v %q", err, out)
	}
	if len(pids) != 3 {
		t.Fatalf("fixture pids missing: %q", out)
	}
	deadline := time.Now().Add(5 * time.Second)
	for _, pid := range pids {
		for {
			state, e := exec.Command("ps", "-p", pid, "-o", "stat=").Output()
			s := strings.TrimSpace(string(state))
			if e != nil || s == "" || strings.HasPrefix(s, "Z") {
				break
			}
			if time.Now().After(deadline) {
				exec.Command("kill", "-KILL", pid).Run()
				t.Fatalf("process %s survived carrier hangup (%s) of %v: %q", pid, s, pids, out)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}
