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
	s.Baud = &ttymode.Baud{Input: normalize(t.Ispeed), Output: normalize(t.Ospeed)}
}
