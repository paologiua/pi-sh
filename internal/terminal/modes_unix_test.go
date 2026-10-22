//go:build darwin || linux

package terminal

import (
	"context"
	"os"
	"reflect"
	"testing"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
)

func modePTY(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	m, s, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close(); s.Close() })
	return m, s
}

func TestCaptureModesPrecedesRawAndSurvivesRestoration(t *testing.T) {
	_, file := modePTY(t)
	tio, err := unix.IoctlGetTermios(int(file.Fd()), modeReadRequest)
	if err != nil {
		t.Fatal(err)
	}
	tio.Lflag &^= unix.ECHO | unix.ICANON | unix.ISIG
	tio.Oflag &^= unix.OPOST
	tio.Iflag &^= unix.ICRNL
	tio.Cc[unix.VINTR] = 29
	tio.Cc[unix.VEOF] = modeDisabledCharacter
	if err := unix.IoctlSetTermios(int(file.Fd()), modeWriteRequest, tio); err != nil {
		t.Fatal(err)
	}
	before, err := CaptureModes(file)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"echo", "icanon", "isig", "opost", "icrnl"} {
		if before.Flags[name] {
			t.Fatal("mode not captured", name)
		}
	}
	if len(before.Flags) < 31 || len(before.Characters) != 14 || before.Characters["intr"] != 29 || before.Characters["eof"] != 255 {
		t.Fatal("incomplete/incorrect mode capture", before)
	}
	session, err := Open(context.Background(), file, file)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	after, err := CaptureModes(file)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("mode snapshot/restoration changed", err)
	}
}
