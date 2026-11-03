package terminal

import (
	"golang.org/x/sys/unix"
	"pi-sh/internal/ttymode"
	"testing"
)

const modeWriteRequest = unix.TCSETS

func TestLinuxSpeedCodesAndInputFallback(t *testing.T) {
	for _, tc := range []struct {
		output, input         uint32
		wantInput, wantOutput uint32
	}{
		{unix.B19200, 0, 19200, 19200},
		{unix.B9600, unix.B38400, 38400, 9600},
		{unix.B460800, unix.B460800, 9600, 9600},
	} {
		tio := &unix.Termios{Cflag: tc.output | tc.input<<16}
		s := &ttymode.Settings{Flags: map[string]bool{}}
		capturePlatformModes(tio, s)
		if s.Baud.Input != tc.wantInput || s.Baud.Output != tc.wantOutput {
			t.Fatalf("speed %x/%x became %+v", tc.output, tc.input, s.Baud)
		}
	}
}

// iSH reports c_cflag 0: CS5 and B0. Neither can be applied to the Pi's Linux
// PTY (which forces CS8), so capture retains the carrier's size and rate.
func TestLinuxZeroControlFlagsRetainCarrierSizeAndRate(t *testing.T) {
	s := &ttymode.Settings{Flags: map[string]bool{}}
	capturePlatformModes(&unix.Termios{}, s)
	if s.Baud != nil {
		t.Fatalf("B0 captured as %+v", s.Baud)
	}
	if size := characterSize(0); size != 0 {
		t.Fatalf("CSIZE 0 became cs%d", size)
	}
	for _, flag := range []uint32{unix.CS6, unix.CS7, unix.CS8} {
		if size := characterSize(uint64(flag)); size != 8 {
			t.Fatalf("CSIZE %x became cs%d", flag, size)
		}
	}
	s.CharacterSize = characterSize(0)
	args, err := s.SttyArgs()
	if err != nil {
		t.Fatal(err)
	}
	for _, arg := range args {
		if arg == "cs5" || arg == "ospeed" || arg == "ispeed" {
			t.Fatalf("carrier would be asked for %q: %q", arg, args)
		}
	}
}
