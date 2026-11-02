//go:build !(darwin || linux || freebsd || openbsd || netbsd || dragonfly || windows)

package sessionstore

import (
	"errors"
	"os"
)

// Without a verified file lock, concurrent writers could lose entries, so
// saving sessions is refused rather than done unsafely.
var errNoLock = errors.New("saved sessions are not supported on this platform: no file locking")

func lockFile(*os.File) error   { return errNoLock }
func unlockFile(*os.File) error { return nil }
