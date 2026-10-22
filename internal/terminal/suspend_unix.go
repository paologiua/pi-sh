//go:build darwin || linux

package terminal

import "syscall"

const canStopProcess = true

func stopProcess() error { return syscall.Kill(syscall.Getpid(), syscall.SIGTSTP) }
