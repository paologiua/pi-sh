//go:build darwin || linux

package terminal

import (
	"os"
	"os/signal"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"
)

const canStopProcess = true

// Once os/signal has caught SIGTSTP, the Go runtime keeps its handler for the
// rest of the process: signal.Stop/Reset then discard SIGTSTP instead of
// restoring the default stop. The first watcher therefore installs a permanent
// dispatcher that routes SIGTSTP to the newest watcher, or emulates the default
// action when none is registered. Self-stops then use the uncatchable SIGSTOP.
var suspendWatch struct {
	sync.Mutex
	installed bool
	watchers  []chan struct{}
}

func stopProcess() error {
	suspendWatch.Lock()
	installed := suspendWatch.installed
	suspendWatch.Unlock()
	if !installed {
		// The inherited default action applies, including the kernel's
		// orphaned-process-group rule.
		return syscall.Kill(syscall.Getpid(), syscall.SIGTSTP)
	}
	return defaultStop()
}

// defaultStop emulates SIGTSTP's default action. Like the kernel, it does not
// stop a process whose group may be orphaned: no job-control shell could then
// continue it. The check is conservative: the parent must be in the same
// session but another process group.
func defaultStop() error {
	if orphaned() {
		return nil
	}
	return syscall.Kill(syscall.Getpid(), syscall.SIGSTOP)
}

func orphaned() bool {
	parent := unix.Getppid()
	parentGroup, err := unix.Getpgid(parent)
	if err != nil {
		return true
	}
	parentSession, err := unix.Getsid(parent)
	if err != nil {
		return true
	}
	session, err := unix.Getsid(0)
	if err != nil {
		return true
	}
	return parentGroup == unix.Getpgrp() || parentSession != session
}

// WatchSuspendSignals reports SIGTSTP sent to this process (by kill -TSTP or a
// job-control shell) so that the caller can restore local state and then call
// Session.Suspend or SuspendProcess. Register only while such a handoff can
// run, and call stop afterwards; SIGTSTP then has its default effect again.
// SIGSTOP cannot be caught and keeps its own semantics.
func WatchSuspendSignals() (<-chan struct{}, func()) {
	c := make(chan struct{}, 1)
	suspendWatch.Lock()
	if !suspendWatch.installed {
		suspendWatch.installed = true
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, syscall.SIGTSTP)
		go dispatchSuspendSignals(signals)
	}
	suspendWatch.watchers = append(suspendWatch.watchers, c)
	suspendWatch.Unlock()
	var once sync.Once
	return c, func() {
		once.Do(func() {
			suspendWatch.Lock()
			defer suspendWatch.Unlock()
			for i, w := range suspendWatch.watchers {
				if w == c {
					suspendWatch.watchers = append(suspendWatch.watchers[:i], suspendWatch.watchers[i+1:]...)
					break
				}
			}
		})
	}
}

func dispatchSuspendSignals(signals <-chan os.Signal) {
	for range signals {
		suspendWatch.Lock()
		var target chan struct{}
		if n := len(suspendWatch.watchers); n > 0 {
			target = suspendWatch.watchers[n-1]
		}
		suspendWatch.Unlock()
		if target == nil {
			_ = defaultStop()
			continue
		}
		select {
		case target <- struct{}{}:
		default: // A request is already pending.
		}
	}
}
