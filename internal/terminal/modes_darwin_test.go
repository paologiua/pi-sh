package terminal

import (
	"golang.org/x/sys/unix"
	"pi-sh/internal/ttymode"
	"testing"
)

const modeWriteRequest = unix.TIOCSETA

func TestDarwinSpeedNormalizationDoesNotTruncate(t *testing.T) {
	tio := &unix.Termios{Ispeed: 1<<32 + 19200, Ospeed: 230400}
	s := &ttymode.Settings{}
	capturePlatformModes(tio, s)
	if s.Baud.Input != 9600 || s.Baud.Output != 230400 {
		t.Fatal("speed truncated instead of normalizing", s.Baud)
	}
}
