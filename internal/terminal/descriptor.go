package terminal

import (
	"os"
	"runtime"

	"golang.org/x/term"
)

// descriptor borrows the handle without File.Fd changing its blocking mode.
// Callers must keep the file alive and open for the entire borrowing period.
func descriptor(file *os.File) (int, error) {
	if file == nil {
		return 0, ErrNotTerminal
	}
	raw, err := file.SyscallConn()
	if err != nil {
		return 0, err
	}
	var fd int
	if err := raw.Control(func(value uintptr) { fd = int(value) }); err != nil {
		return 0, err
	}
	return fd, nil
}

// IsTerminal queries a file without changing its descriptor's status flags.
func IsTerminal(file *os.File) bool {
	defer runtime.KeepAlive(file)
	fd, err := descriptor(file)
	return err == nil && term.IsTerminal(fd)
}
