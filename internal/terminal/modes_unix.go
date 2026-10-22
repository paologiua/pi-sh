//go:build darwin || linux

package terminal

import (
	"errors"
	"os"
	"runtime"

	"golang.org/x/sys/unix"
	"pi-sh/internal/ttymode"
)

// CaptureModes must precede Open/MakeRaw. It translates meanings, never the
// OS-specific stty -g string. The target carrier is the existing Linux Pi PTY.
func CaptureModes(file *os.File) (*ttymode.Settings, error) {
	defer runtime.KeepAlive(file)
	if file == nil {
		return nil, ErrNotTerminal
	}
	fd, err := descriptor(file)
	if err != nil {
		return nil, errors.New("local terminal mode query failed")
	}
	t, err := unix.IoctlGetTermios(fd, modeReadRequest)
	if err != nil {
		return nil, errors.New("local terminal mode query failed")
	}
	s := &ttymode.Settings{Flags: make(map[string]bool), Characters: make(map[string]uint32)}
	groups := []struct {
		value uint64
		bits  map[string]uint64
	}{
		{uint64(t.Lflag), map[string]uint64{
			"icanon": unix.ICANON, "isig": unix.ISIG, "iexten": unix.IEXTEN, "echo": unix.ECHO,
			"echoe": unix.ECHOE, "echok": unix.ECHOK, "echonl": unix.ECHONL, "echoctl": unix.ECHOCTL,
			"echoke": unix.ECHOKE, "noflsh": unix.NOFLSH, "tostop": unix.TOSTOP, "pendin": unix.PENDIN,
		}},
		{uint64(t.Iflag), map[string]uint64{
			"ignpar": unix.IGNPAR, "parmrk": unix.PARMRK, "inpck": unix.INPCK, "istrip": unix.ISTRIP,
			"inlcr": unix.INLCR, "igncr": unix.IGNCR, "icrnl": unix.ICRNL, "ixon": unix.IXON,
			"ixoff": unix.IXOFF, "ixany": unix.IXANY, "imaxbel": unix.IMAXBEL, "iutf8": unix.IUTF8,
		}},
		{uint64(t.Oflag), map[string]uint64{
			"opost": unix.OPOST, "onlcr": unix.ONLCR, "ocrnl": unix.OCRNL, "onocr": unix.ONOCR, "onlret": unix.ONLRET,
		}},
		{uint64(t.Cflag), map[string]uint64{"parenb": unix.PARENB, "parodd": unix.PARODD}},
	}
	for _, g := range groups {
		for name, mask := range g.bits {
			s.Flags[name] = g.value&mask != 0
		}
	}
	for name, index := range map[string]int{
		"intr": unix.VINTR, "quit": unix.VQUIT, "erase": unix.VERASE, "kill": unix.VKILL,
		"eof": unix.VEOF, "eol": unix.VEOL, "eol2": unix.VEOL2, "start": unix.VSTART,
		"stop": unix.VSTOP, "susp": unix.VSUSP, "reprint": unix.VREPRINT, "werase": unix.VWERASE,
		"lnext": unix.VLNEXT, "discard": unix.VDISCARD,
	} {
		value := uint32(t.Cc[index])
		if value == modeDisabledCharacter {
			value = 255
		}
		s.Characters[name] = value
	}
	// OpenSSH sends CS7/CS8 as bit-presence flags, not a CSIZE selector. Since
	// their masks overlap, any nonzero conventional size ultimately sets CS8.
	s.CharacterSize = 5
	if t.Cflag&unix.CS8 != 0 {
		s.CharacterSize = 8
	}
	capturePlatformModes(t, s)
	return s, nil
}
