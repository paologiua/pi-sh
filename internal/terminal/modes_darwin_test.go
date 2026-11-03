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

// Ospeed 0 (B0) is a hang-up request; input speed 0 follows the output speed.
func TestDarwinZeroSpeedsRetainCarrierRate(t *testing.T) {
	s := &ttymode.Settings{}
	capturePlatformModes(&unix.Termios{}, s)
	if s.Baud != nil {
		t.Fatalf("B0 captured as %+v", s.Baud)
	}
	capturePlatformModes(&unix.Termios{Ospeed: 19200}, s)
	if s.Baud == nil || s.Baud.Input != 19200 || s.Baud.Output != 19200 {
		t.Fatalf("input speed 0 became %+v", s.Baud)
	}
	if characterSize(0) != 0 || characterSize(unix.CS7) != 8 || characterSize(unix.CS8) != 8 {
		t.Fatal("character size selection")
	}
}
