// Package ttymode describes terminal settings without OS-specific termios bits
// or a wire protocol. Names are a fixed vocabulary accepted by the remote stty
// adapter. Settings omitted by a producer retain the carrier's original value.
package ttymode

import (
	"errors"
	"sort"
	"strconv"
)

// Character values use 255 for disabled, matching the OpenSSH terminal-mode
// convention. This deliberately cannot distinguish a literal byte255 from it.
type Settings struct {
	Flags         map[string]bool
	Characters    map[string]uint32
	Baud          *Baud
	CharacterSize int // zero retains the carrier's size; otherwise 5 through 8
}

type Baud struct{ Input, Output uint32 }

var flags = map[string]bool{
	"icanon": true, "isig": true, "iexten": true, "echo": true,
	"echoe": true, "echok": true, "echonl": true, "echoctl": true,
	"echoke": true, "noflsh": true, "tostop": true, "pendin": true,
	"ignpar": true, "parmrk": true, "inpck": true, "istrip": true,
	"inlcr": true, "igncr": true, "icrnl": true, "ixon": true,
	"ixoff": true, "ixany": true, "imaxbel": true, "iutf8": true,
	"opost": true, "onlcr": true, "ocrnl": true, "onocr": true,
	"onlret": true, "parenb": true, "parodd": true,
	"iuclc": true, "olcuc": true, "xcase": true,
}

var characters = map[string]bool{
	"intr": true, "quit": true, "erase": true, "kill": true,
	"eof": true, "eol": true, "eol2": true, "start": true,
	"stop": true, "susp": true, "reprint": true, "werase": true,
	"lnext": true, "discard": true,
}

// LinuxBaud normalizes unsupported source rates to the OpenSSH/Linux fallback.
// This is a terminal metadata rate, not a network bandwidth setting.
func LinuxBaud(value uint32) uint32 {
	switch value {
	case 0, 50, 75, 110, 134, 150, 200, 300, 600, 1200, 1800, 2400, 4800, 9600, 19200, 38400, 57600, 115200, 230400:
		return value
	default:
		return 9600
	}
}

// SttyArgs validates the entire snapshot before any remote execution. It emits
// only fixed option names and bounded values; shell quoting belongs to the caller.
func (s *Settings) SttyArgs() ([]string, error) {
	if s == nil {
		return nil, nil
	}
	if len(s.Flags) > len(flags) || len(s.Characters) > len(characters) {
		return nil, errors.New("terminal mode snapshot exceeds supported vocabulary")
	}
	var args []string
	if s.CharacterSize != 0 {
		if s.CharacterSize < 5 || s.CharacterSize > 8 {
			return nil, errors.New("invalid terminal character size")
		}
		args = append(args, "cs"+strconv.Itoa(s.CharacterSize))
	}
	names := make([]string, 0, len(s.Flags))
	for name := range s.Flags {
		if !flags[name] {
			return nil, errors.New("unsupported terminal mode")
		}
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if s.Flags[name] {
			args = append(args, name)
		} else {
			args = append(args, "-"+name)
		}
	}
	names = names[:0]
	for name, value := range s.Characters {
		if !characters[name] || value > 255 {
			return nil, errors.New("unsupported terminal control character")
		}
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		value := s.Characters[name]
		var text string
		switch {
		case value == 255:
			text = "undef"
		case value < 32:
			text = "^" + string(rune(value+64))
		case value == 127:
			text = "^?"
		case value == '^':
			// A bare caret is itself stty syntax, not the literal byte.
			text = "0x5e"
		default:
			text = string([]byte{byte(value)})
		}
		args = append(args, name, text)
	}
	if s.Baud != nil {
		if LinuxBaud(s.Baud.Input) != s.Baud.Input || LinuxBaud(s.Baud.Output) != s.Baud.Output {
			return nil, errors.New("unsupported terminal baud rate")
		}
		args = append(args, "ospeed", strconv.FormatUint(uint64(s.Baud.Output), 10), "ispeed", strconv.FormatUint(uint64(s.Baud.Input), 10))
	}
	return args, nil
}
