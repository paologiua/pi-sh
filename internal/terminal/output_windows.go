package terminal

import "golang.org/x/sys/windows"

func prepareOutput(fd int) (func() error, error) {
	h := windows.Handle(fd)
	var original uint32
	if err := windows.GetConsoleMode(h, &original); err != nil {
		return nil, err
	}
	mode := original | windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING | windows.ENABLE_PROCESSED_OUTPUT
	if err := windows.SetConsoleMode(h, mode); err != nil {
		return nil, err
	}
	return func() error { return windows.SetConsoleMode(h, original) }, nil
}
