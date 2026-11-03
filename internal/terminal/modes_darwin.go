package terminal

import (
	"golang.org/x/sys/unix"
	"pi-sh/internal/ttymode"
)

const modeReadRequest = unix.TIOCGETA
const modeDisabledCharacter = 255

func capturePlatformModes(t *unix.Termios, s *ttymode.Settings) {
	normalize := func(value uint64) uint32 {
		if value > uint64(^uint32(0)) {
			return 9600
		}
		return ttymode.LinuxBaud(uint32(value))
	}
	if t.Ospeed == 0 {
		return // B0 is a hang-up request, not a rate: retain the carrier's rate.
	}
	input := t.Ispeed
	if input == 0 {
		input = t.Ospeed // termios: input speed 0 follows the output speed
	}
	s.Baud = &ttymode.Baud{Input: normalize(input), Output: normalize(t.Ospeed)}
}
