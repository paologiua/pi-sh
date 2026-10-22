//go:build darwin || linux

package terminal

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

func terminalFD(t *testing.T, f *os.File) int {
	t.Helper()
	fd, err := descriptor(f)
	if err != nil {
		t.Fatal(err)
	}
	return fd
}

func TestSuspendCapturesChangedShellModesAndForcesSize(t *testing.T) {
	master, file := openPTY(t)
	fd := terminalFD(t, file)
	before, err := term.GetState(fd)
	if err != nil {
		t.Fatal(err)
	}
	session, err := Open(context.Background(), file, file)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	<-session.Sizes()
	var changed *term.State
	session.stopProcess = func(context.Context) error {
		now, err := term.GetState(fd)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(now, before) {
			return errors.New("terminal was raw during suspension")
		}
		modes, err := unix.IoctlGetTermios(fd, modeReadRequest)
		if err != nil {
			return err
		}
		modes.Lflag ^= unix.ECHO
		modes.Cc[unix.VEOF] = 7
		if err := unix.IoctlSetTermios(fd, modeWriteRequest, modes); err != nil {
			return err
		}
		changed, err = term.GetState(fd)
		if err != nil {
			return err
		}
		return pty.Setsize(master, &pty.Winsize{Rows: 31, Cols: 97})
	}
	if err := session.Suspend(context.Background()); err != nil {
		t.Fatal(err)
	}
	now, err := term.GetState(fd)
	if err != nil || reflect.DeepEqual(now, changed) {
		t.Fatal("raw mode was not reacquired", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	nextSize(t, ctx, session, Size{Columns: 97, Rows: 31})
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	after, err := term.GetState(fd)
	if err != nil || !reflect.DeepEqual(after, changed) {
		t.Fatal("resume did not preserve shell's new baseline", err)
	}
}

func TestCancelledSuspendDoesNotReacquireRaw(t *testing.T) {
	_, file := openPTY(t)
	fd := terminalFD(t, file)
	before, err := term.GetState(fd)
	if err != nil {
		t.Fatal(err)
	}
	session, err := Open(context.Background(), file, file)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	operation, cancel := context.WithCancel(context.Background())
	defer cancel()
	session.stopProcess = func(context.Context) error { cancel(); return nil }
	if err := session.Suspend(operation); !errors.Is(err, context.Canceled) {
		t.Fatal("suspend lost cancellation", err)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	after, err := term.GetState(fd)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("cancelled resume changed cooked modes", err)
	}
}

func TestSuspendFailureKeepsCookedAndReportsError(t *testing.T) {
	_, file := openPTY(t)
	fd := terminalFD(t, file)
	before, err := term.GetState(fd)
	if err != nil {
		t.Fatal(err)
	}
	session, err := Open(context.Background(), file, file)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	failure := errors.New("fixture stop refused")
	session.stopProcess = func(context.Context) error { return failure }
	if err := session.Suspend(context.Background()); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if err := session.Close(); !errors.Is(err, failure) {
		t.Fatal("terminal lost stop error", err)
	}
	after, err := term.GetState(fd)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("stop failure left raw modes", err)
	}
}

func TestNativeSuspendHelper(t *testing.T) {
	if os.Getenv("PI_SH_NATIVE_SUSPEND_HELPER") != "1" {
		return
	}
	file := os.NewFile(3, "suspend-terminal")
	defer file.Close()
	session, err := Open(context.Background(), file, file)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	<-session.Sizes()
	fmt.Println("suspend-ready")
	if err := session.Suspend(context.Background()); err != nil {
		t.Fatal(err)
	}
	size := <-session.Sizes()
	fmt.Printf("resumed:%d:%d\n", size.Columns, size.Rows)
	if _, err := bufio.NewReader(os.Stdin).ReadString('\n'); err != nil {
		t.Fatal(err)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestNativeSuspendRestoresAndReacquiresAcrossCONT(t *testing.T) {
	master, file := openPTY(t)
	fd := terminalFD(t, file)
	before, err := term.GetState(fd)
	if err != nil {
		t.Fatal(err)
	}
	controlReader, controlWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer controlReader.Close()
	defer controlWriter.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestNativeSuspendHelper$")
	cmd.Env = append(os.Environ(), "PI_SH_NATIVE_SUSPEND_HELPER=1")
	cmd.Stdin = controlReader
	cmd.ExtraFiles = []*os.File{file}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	finished := false
	defer func() {
		if !finished {
			cmd.Process.Signal(syscall.SIGCONT)
			cmd.Process.Kill()
			<-waited
		}
	}()
	reader := bufio.NewReader(output)
	line, err := reader.ReadString('\n')
	if err != nil || line != "suspend-ready\n" {
		t.Fatal("helper did not acquire raw terminal", line, err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		state, e := exec.Command("ps", "-p", fmt.Sprint(cmd.Process.Pid), "-o", "stat=").Output()
		if e == nil && strings.HasPrefix(strings.TrimSpace(string(state)), "T") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("helper never reached native stopped state")
		}
		time.Sleep(time.Millisecond)
	}
	cooked, err := term.GetState(fd)
	if err != nil || !reflect.DeepEqual(cooked, before) {
		t.Fatal("native stopped process left terminal raw", err)
	}
	modes, err := unix.IoctlGetTermios(fd, modeReadRequest)
	if err != nil {
		t.Fatal(err)
	}
	modes.Lflag ^= unix.ECHO
	modes.Cc[unix.VEOF] = 7
	if err := unix.IoctlSetTermios(fd, modeWriteRequest, modes); err != nil {
		t.Fatal(err)
	}
	changed, err := term.GetState(fd)
	if err != nil {
		t.Fatal(err)
	}
	if err := pty.Setsize(master, &pty.Winsize{Rows: 31, Cols: 97}); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Process.Signal(syscall.SIGCONT); err != nil {
		t.Fatal(err)
	}
	line, err = reader.ReadString('\n')
	if err != nil || line != "resumed:97:31\n" {
		t.Fatal("native resume did not refresh size", line, err)
	}
	raw, err := term.GetState(fd)
	if err != nil || reflect.DeepEqual(raw, changed) {
		t.Fatal("native resume did not acquire raw", err)
	}
	if _, err := controlWriter.WriteString("finish\n"); err != nil {
		t.Fatal(err)
	}
	if err := <-waited; err != nil {
		t.Fatal("native suspend helper failed", err)
	}
	finished = true
	after, err := term.GetState(fd)
	if err != nil || !reflect.DeepEqual(after, changed) {
		t.Fatal("native close lost changed shell modes", err)
	}
}

func TestCloseJoinsSuspensionWithoutReacquiringRaw(t *testing.T) {
	_, file := openPTY(t)
	fd := terminalFD(t, file)
	before, err := term.GetState(fd)
	if err != nil {
		t.Fatal(err)
	}
	session, err := Open(context.Background(), file, file)
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	session.stopProcess = func(context.Context) error { close(entered); <-release; return nil }
	suspended := make(chan error, 1)
	go func() { suspended <- session.Suspend(context.Background()) }()
	<-entered
	closed := make(chan error, 1)
	go func() { closed <- session.Close() }()
	// Close must mark shutdown before the simulated SIGCONT arrives.
	<-session.stop
	select {
	case err := <-closed:
		t.Fatal("Close did not join transition", err)
	default:
	}
	close(release)
	if err := <-suspended; !errors.Is(err, ErrClosed) {
		t.Fatal("suspension did not observe Close", err)
	}
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
	after, err := term.GetState(fd)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("Close reacquired raw modes", err)
	}
}
