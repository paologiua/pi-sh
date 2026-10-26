//go:build darwin || linux

package progress

import (
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/sys/unix"
)

func foreground(fd int) bool {
	group, err := unix.IoctlGetInt(fd, unix.TIOCGPGRP)
	return err == nil && group == syscall.Getpgrp()
}

func resizeEvents() (<-chan os.Signal, func()) {
	events := make(chan os.Signal, 1)
	signal.Notify(events, syscall.SIGWINCH)
	return events, func() { signal.Stop(events) }
}
