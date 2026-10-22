//go:build darwin || linux

package shellbroker

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

type ttyProcess struct {
	parent, group int
	state         string
}

func ttyProcessSnapshot(t *testing.T) map[int]ttyProcess {
	t.Helper()
	output, err := exec.Command("ps", "-axo", "pid=,ppid=,pgid=,stat=").Output()
	if err != nil {
		t.Fatal("local process inspection unavailable", err)
	}
	result := make(map[int]ttyProcess)
	for _, line := range strings.Split(string(output), "\n") {
		f := strings.Fields(line)
		if len(f) != 4 {
			continue
		}
		pid, e1 := strconv.Atoi(f[0])
		parent, e2 := strconv.Atoi(f[1])
		group, e3 := strconv.Atoi(f[2])
		if e1 == nil && e2 == nil && e3 == nil {
			result[pid] = ttyProcess{parent, group, f[3]}
		}
	}
	return result
}

func ttyOwnedProcesses(root int, snapshot map[int]ttyProcess) map[int]ttyProcess {
	owned := map[int]ttyProcess{root: snapshot[root]}
	for changed := true; changed; {
		changed = false
		for pid, process := range snapshot {
			if _, already := owned[pid]; already {
				continue
			}
			if _, child := owned[process.parent]; child {
				owned[pid] = process
				changed = true
			}
		}
	}
	return owned
}

func TestTTYJobTableCandidateHUPCleanupBeforeCarrierClose(t *testing.T) {
	for _, signal := range []syscall.Signal{syscall.SIGHUP, syscall.SIGTERM} {
		t.Run(signal.String(), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			text := "trap '' HUP TERM; (trap '' HUP TERM; while :; do sleep 1; done) & printf 'fixture-ready\\n'; wait"
			cmd := exec.CommandContext(ctx, modernTTYBash(t), "--noprofile", "--norc", "-m", "-c", ttyJobsScript(text))
			var owned map[int]ttyProcess
			t.Cleanup(func() {
				// Also clean up a failing candidate. Inspection before carrier close
				// remains the pass/fail oracle; this final safeguard cannot make it pass.
				snapshot := ttyProcessSnapshot(t)
				groups := make(map[int]bool)
				for pid, previous := range owned {
					if current, alive := snapshot[pid]; alive && current.group == previous.group && !strings.HasPrefix(current.state, "Z") {
						groups[previous.group] = true
					}
				}
				for group := range groups {
					if group > 1 {
						syscall.Kill(-group, syscall.SIGKILL)
					}
				}
			})
			code, _ := runPTYFixture(t, cmd, "", nil, false, ptyFixtureHooks{
				ready: func(cmd *exec.Cmd, carrier *os.File) {
					foreground, err := unix.IoctlGetInt(int(carrier.Fd()), unix.TIOCGPGRP)
					if err != nil {
						t.Fatal("foreground TTY group unavailable", err)
					}
					owned = ttyOwnedProcesses(cmd.Process.Pid, ttyProcessSnapshot(t))
					if len(owned) < 5 || foreground == owned[cmd.Process.Pid].group {
						t.Fatal("worker did not own a separate foreground group")
					}
					foregroundMembers := 0
					for _, process := range owned {
						if process.group == foreground {
							foregroundMembers++
						}
					}
					if foregroundMembers < 3 {
						t.Fatal("command/helpers not in foreground group")
					}
					if err := cmd.Process.Signal(signal); err != nil {
						t.Fatal(err)
					}
				},
				afterExit: func() {
					status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus)
					if !ok || !status.Signaled() || status.Signal() != signal {
						t.Fatal("supervisor did not terminate with the requested signal")
					}
					deadline := time.Now().Add(2 * time.Second)
					for {
						live := 0
						for pid, process := range ttyProcessSnapshot(t) {
							if _, wasOwned := owned[pid]; wasOwned && !strings.HasPrefix(process.state, "Z") {
								live++
							}
						}
						if live == 0 {
							return
						}
						if time.Now().After(deadline) {
							t.Fatalf("%d owned processes remain before carrier close", live)
						}
						time.Sleep(10 * time.Millisecond)
					}
				},
			})
			if code != -1 {
				t.Fatalf("HUP cleanup returned %d", code)
			}
		})
	}
}
