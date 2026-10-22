//go:build unix

package terminal

import (
	"os"
	"os/signal"
	"syscall"
)

func resizeEvents() (<-chan os.Signal, func()) {
	events := make(chan os.Signal, 1)
	signal.Notify(events, syscall.SIGWINCH)
	return events, func() { signal.Stop(events) }
}
