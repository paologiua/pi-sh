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
