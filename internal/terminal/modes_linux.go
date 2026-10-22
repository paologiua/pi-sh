package terminal

import (
	"golang.org/x/sys/unix"
	"pi-sh/internal/ttymode"
)

const modeReadRequest = unix.TCGETS
const modeDisabledCharacter = 0

func capturePlatformModes(t *unix.Termios, s *ttymode.Settings) {
	s.Flags["iuclc"] = t.Iflag&unix.IUCLC != 0
	s.Flags["olcuc"] = t.Oflag&unix.OLCUC != 0
	s.Flags["xcase"] = t.Lflag&unix.XCASE != 0
	// TCGETS encodes legacy speeds in CBAUD/CIBAUD. Ispeed/Ospeed are not
	// portable output fields for this ioctl. An input code of zero follows output.
	rates := map[uint32]uint32{
		unix.B0: 0, unix.B50: 50, unix.B75: 75, unix.B110: 110, unix.B134: 134,
		unix.B150: 150, unix.B200: 200, unix.B300: 300, unix.B600: 600, unix.B1200: 1200,
		unix.B1800: 1800, unix.B2400: 2400, unix.B4800: 4800, unix.B9600: 9600,
		unix.B19200: 19200, unix.B38400: 38400, unix.B57600: 57600, unix.B115200: 115200, unix.B230400: 230400,
	}
	decode := func(code uint32) uint32 {
		if rate, ok := rates[code]; ok {
			return rate
		}
		return 9600
	}
	output := uint32(t.Cflag) & unix.CBAUD
	input := (uint32(t.Cflag) & unix.CIBAUD) >> 16
	if input == 0 {
		input = output
	}
	s.Baud = &ttymode.Baud{Input: decode(input), Output: decode(output)}
}
