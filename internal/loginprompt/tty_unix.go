//go:build !windows

package loginprompt

import "os"

func openTerminal() (*os.File, *os.File, error) {
	f, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	return f, f, err
}
