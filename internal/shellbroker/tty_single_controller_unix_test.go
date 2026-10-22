//go:build darwin || linux

package shellbroker

import (
	"context"
	"errors"
	"golang.org/x/sys/unix"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

func mustTTYPlan(t *testing.T, text string) *TTYPlan {
	t.Helper()
	plan, err := NewTTY(text)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func TestStagedTTYControllerAcrossCarriers(t *testing.T) {
	cases := ttyFunctionalCases()
	cases = append(cases, []ttyFunctionalCase{
		{"history-private", `if test "${HISTFILE+x}" = x; then printf history-leaked; exit 24; fi; printf history-private; exit 23`, "history-private", nil, 23, false},
		{"self-stop-resume", `parent=$$; (sleep 0.2; kill -CONT "$parent") & kill -STOP $$; wait; printf resumed; exit 23`, "resumed", nil, 23, false},
		{"terminal-suspend", `printf 'fixture-ready\n'; IFS= read -r x; printf 'seen:%s\n' "$x"; exit 23`, "seen:x", []byte{26, 'x', '\r'}, 23, false},
		{"caught-self-tstp", `trap 'printf caught-suspend' TSTP; kill -TSTP $$; printf resumed; exit 23`, "caught-suspendresumed", nil, 23, false},
		{"caught-suspend", `trap 'printf caught-suspend' TSTP; printf 'fixture-ready\n'; IFS= read -r x; printf 'seen:%s\n' "$x"; exit 23`, "caught-suspendseen:x", []byte{26, 'x', '\r'}, 23, false},
		{"ordinary143", `exit 143`, "", nil, 143, false},
		{"self-int", `printf first; kill -INT $$`, "first", nil, 255, false},
		{"self-term", `printf first; kill -TERM $$`, "first", nil, 255, false},
		{"self-kill", `printf first; kill -KILL $$`, "first", nil, 255, false},
		{"terminal-int", `printf fixture-ready; sleep 60`, "fixture-ready", []byte{3}, 255, false},
		{"caught-int", `trap 'exit 42' INT; printf fixture-ready; IFS= read -r x`, "fixture-ready", []byte{3}, 42, false},
	}...)
	for _, carrier := range []string{"/bin/sh", modernTTYBash(t)} {
		for _, tc := range cases {
			t.Run(carrier+"/"+tc.name, func(t *testing.T) {
				code, out, err := runStagedTTYPlan(t, carrier, mustTTYPlan(t, tc.text), tc.input, tc.resize)
				if err != nil || code != tc.code || !strings.Contains(out, tc.want) {
					t.Fatalf("code%d want%d err=%v out=%q", code, tc.code, err, out)
				}
			})
		}
	}
}

func TestStagedTTYControllerCleanupBeforeCarrierClose(t *testing.T) {
	for _, carrier := range []string{"/bin/sh", modernTTYBash(t)} {
		for _, sig := range []syscall.Signal{syscall.SIGHUP, syscall.SIGTERM} {
			t.Run(carrier+"/"+sig.String(), func(t *testing.T) {
				sentinel := exec.Command("sleep", "30")
				if err := sentinel.Start(); err != nil {
					t.Fatal(err)
				}
				defer func() { sentinel.Process.Kill(); sentinel.Wait() }()
				var owned map[int]ttyProcess
				verifiedReady := make(chan struct{})
				plan := mustTTYPlan(t, `trap '' HUP TERM; (trap '' HUP TERM; while :; do sleep 1; done) & printf 'fixture-ready\n'; wait`)
				_, _, err := runStagedTTYPlan(t, carrier, plan, nil, false, stagedTTYHooks{
					ready: func(ctx context.Context, cmd *exec.Cmd, f *os.File, _ *ttyWatch) error {
						raw, err := f.SyscallConn()
						if err != nil {
							return err
						}
						foreground := 0
						var ioctlErr error
						if err := raw.Control(func(fd uintptr) { foreground, ioctlErr = unix.IoctlGetInt(int(fd), unix.TIOCGPGRP) }); err != nil {
							return err
						}
						if ioctlErr != nil {
							return ioctlErr
						}
						snapshot := ttyProcessSnapshot(t)
						owned = ttyOwnedProcesses(cmd.Process.Pid, snapshot)
						if len(owned) < 4 || foreground != cmd.Process.Pid {
							return errors.New("controller has not acquired its private foreground group")
						}
						for _, process := range owned {
							if process.group != foreground {
								return errors.New("owned process escaped controller group")
							}
						}
						if sentinelGroup, e := syscall.Getpgid(sentinel.Process.Pid); e != nil || sentinelGroup == foreground {
							return errors.New("sentinel overlaps owned group")
						}
						close(verifiedReady)
						return cmd.Process.Signal(sig)
					},
					beforeClose: func() {
						select {
						case <-verifiedReady:
						default:
							t.Fatal("controller never established validated group ownership")
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
								break
							}
							if time.Now().After(deadline) {
								t.Fatalf("%d owned processes survive before carrier close", live)
							}
							time.Sleep(10 * time.Millisecond)
						}
						if err := sentinel.Process.Signal(syscall.Signal(0)); err != nil {
							t.Fatal("controller killed unrelated sentinel", err)
						}
					},
				})
				if !errors.Is(err, ErrCleanupUnconfirmed) {
					t.Fatalf("failed controller fabricated cleanup: %v", err)
				}
			})
		}
	}
}

func TestStagedTTYControllerWholeGroupStopResume(t *testing.T) {
	for _, carrier := range []string{"/bin/sh", modernTTYBash(t)} {
		t.Run(carrier, func(t *testing.T) {
			plan := mustTTYPlan(t, `printf fixture-ready; IFS= read -r x; printf 'seen:%s' "$x"; exit 23`)
			code, out, err := runStagedTTYPlan(t, carrier, plan, nil, false, stagedTTYHooks{
				ready: func(ctx context.Context, cmd *exec.Cmd, f *os.File, _ *ttyWatch) error {
					group := cmd.Process.Pid
					owned := ttyOwnedProcesses(group, ttyProcessSnapshot(t))
					if len(owned) < 3 {
						return errors.New("stop fixture has not started command and metadata helper")
					}
					for _, process := range owned {
						if process.group != group {
							return errors.New("stop fixture lost private group ownership")
						}
					}
					defer syscall.Kill(-group, syscall.SIGCONT)
					if err := syscall.Kill(-group, syscall.SIGSTOP); err != nil {
						return err
					}
					deadline := time.Now().Add(time.Second)
					for {
						snapshot := ttyProcessSnapshot(t)
						stopped := 0
						for pid := range owned {
							if p, ok := snapshot[pid]; ok && strings.HasPrefix(p.state, "T") {
								stopped++
							}
						}
						if stopped == len(owned) {
							break
						}
						if time.Now().After(deadline) {
							return errors.New("owned group did not reach stopped state")
						}
						select {
						case <-ctx.Done():
							return ctx.Err()
						case <-time.After(time.Millisecond):
						}
					}
					if err := syscall.Kill(-group, syscall.SIGCONT); err != nil {
						return err
					}
					_, err := f.WriteString("x\r")
					return err
				},
			})
			if err != nil || code != 23 || !strings.Contains(out, "seen:x") {
				t.Fatalf("group resume code%d err=%v out=%q", code, err, out)
			}
		})
	}
}
