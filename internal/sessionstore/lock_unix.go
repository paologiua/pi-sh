//go:build darwin || linux || freebsd || openbsd || netbsd || dragonfly

package sessionstore

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// flock locks belong to the open file description, so two descriptors in
// the same process exclude each other as well as other processes.
func lockFile(f *os.File) error {
	for {
		err := unix.Flock(int(f.Fd()), unix.LOCK_EX)
		if !errors.Is(err, unix.EINTR) {
			return err
		}
	}
}

func unlockFile(f *os.File) error {
	return unix.Flock(int(f.Fd()), unix.LOCK_UN)
}
