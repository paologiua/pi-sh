//go:build !darwin && !linux

package terminal

import (
	"os"
	"pi-sh/internal/ttymode"
)

// Windows console modes are not POSIX terminal modes. Retain the carrier's
// settings; do not invent a termios snapshot from a console bitfield.
func CaptureModes(*os.File) (*ttymode.Settings, error) { return nil, nil }
