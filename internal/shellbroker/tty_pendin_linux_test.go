//go:build linux

package shellbroker

import (
	"context"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
)

func TestGNUCarrierPendinChangesOnlyRequestedKernelBit(t *testing.T) {
	version, err := exec.Command("stty", "--version").Output()
	if err != nil || !strings.HasPrefix(string(version), "stty (GNU coreutils)") {
		t.Skip("GNU stty fixture required")
	}
	master, file, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer file.Close()
	fd := int(file.Fd())
	source := strings.TrimSuffix(ttySource, "_pt_main\n")
	for _, enabled := range []bool{true, false} {
		before, err := unix.IoctlGetTermios(fd, unix.TCGETS)
		if err != nil {
			t.Fatal(err)
		}
		arg := "-pendin"
		if enabled {
			arg = "pendin"
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		command := exec.CommandContext(ctx, modernTTYBash(t), "--noprofile", "--norc", "-c", source+"\n_pt_stty_modes \"$1\"", "pendin-kernel", arg)
		command.Stdin = file
		out, err := command.CombinedOutput()
		cancel()
		if err != nil || len(out) != 0 {
			t.Fatalf("%s: %v %q", arg, err, out)
		}
		after, err := unix.IoctlGetTermios(fd, unix.TCGETS)
		if err != nil {
			t.Fatal(err)
		}
		expected := *before
		if enabled {
			expected.Lflag |= unix.PENDIN
		} else {
			expected.Lflag &^= unix.PENDIN
		}
		if !reflect.DeepEqual(*after, expected) {
			t.Fatalf("%s changed unexpected kernel state: got%+v want%+v", arg, *after, expected)
		}
	}
}
