//go:build darwin || linux

package fwdtest

import (
	"os"
	"os/exec"
	"syscall"
)

// Setsid stands in for util-linux setsid, which macOS lacks: it starts a new
// session then execs, keeping the PID. Tests run their own binary under the
// name "setsid"; it is never sent to a device.
func Setsid(args []string) int {
	if len(args) > 0 && args[0] == "--wait" {
		args = args[1:]
	}
	if len(args) == 0 {
		return 125
	}
	if _, err := syscall.Setsid(); err != nil {
		return 125
	}
	path, err := exec.LookPath(args[0])
	if err != nil {
		return 127
	}
	if err := syscall.Exec(path, args, os.Environ()); err != nil {
		return 126
	}
	return 0
}
