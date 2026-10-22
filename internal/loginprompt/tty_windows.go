package loginprompt

import "os"

func openTerminal() (*os.File, *os.File, error) {
	in, err := os.OpenFile("CONIN$", os.O_RDWR, 0)
	if err != nil {
		return nil, nil, err
	}
	out, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0)
	if err != nil {
		in.Close()
		return nil, nil, err
	}
	return in, out, nil
}
