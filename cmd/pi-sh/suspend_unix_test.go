//go:build darwin || linux

package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
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
	"pi-sh/internal/localio"
	"pi-sh/internal/terminal"
	"pi-sh/internal/transport"
)

// Only the subprocess enters native SIGTSTP; the test runner never stops itself.
func TestSuspendTTYSessionHelper(t *testing.T) {
	mode := os.Getenv("PI_SH_SUSPEND_SESSION_HELPER")
	if mode == "" {
		return
	}
	local := os.NewFile(3, "local-terminal")
	output := os.NewFile(4, "session-output")
	defer local.Close()
	defer output.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client, channel, _, applied := ttyPeers(t, ctx)
	go func() {
		started := false
		for {
			var size terminal.Size
			select {
			case <-ctx.Done():
				return
			case size = <-applied:
			}
			if mode == "interactive" && !started && size.Columns == 91 {
				started = true
				if err := channel.Send(ctx, transport.Message{Text: true, Data: []byte("printf 'fixture-%s\\n' ready; IFS= read -r x; printf 'seen:%s\\n' \"$x\"; stty size\r")}); err != nil {
					return
				}
			}
			if size.Columns == 97 && size.Rows == 31 {
				fmt.Println("resize-resumed")
				return
			}
		}
	}()
	if mode == "interactive" {
		err := runInteractiveSession(ctx, nil, client, channel, local, local, local, '~')
		if err != nil {
			t.Fatalf("interactive session: %v", err)
		}
	} else {
		err := runTTYSession(ctx, nil, client, channel, ttyCommandRequest{term: "xterm", text: "printf 'fixture-ready\\n'; IFS= read -r x; printf 'seen:%s\\n' \"$x\"; stty size; exit 23"}, local, local, output, output, '~')
		var status *commandStatus
		if !errors.As(err, &status) || status.code != 23 {
			t.Fatalf("session status: %v", err)
		}

	}
	fmt.Println("session-finished")
}

func suspendFileFD(t *testing.T, f *os.File) int {
	t.Helper()
	raw, err := f.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	fd := -1
	if err := raw.Control(func(value uintptr) { fd = int(value) }); err != nil {
		t.Fatal(err)
	}
	return fd
}

func TestSuspendTTYSessionOverWebRTC(t *testing.T) {
	for _, mode := range []string{"tty", "interactive"} {
		t.Run(mode, func(t *testing.T) { testSuspendSession(t, mode) })
	}
}

func testSuspendSession(t *testing.T, mode string) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	master, local, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer local.Close()
	fd := suspendFileFD(t, local)
	masterFD := suspendFileFD(t, master)
	if err := unix.SetNonblock(fd, false); err != nil {
		t.Fatal(err)
	}
	before, err := term.GetState(fd)
	if err != nil {
		t.Fatal(err)
	}
	originalFlags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.IoctlSetWinsize(masterFD, unix.TIOCSWINSZ, &unix.Winsize{Row: 29, Col: 91}); err != nil {
		t.Fatal(err)
	}
	readOutput, output, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer readOutput.Close()
	defer output.Close()
	observed := &ttyOutput{ready: make(chan struct{})}
	drained := make(chan struct{})
	parentIO, err := localio.Open(ctx, localio.Files{Stdin: master, Stdout: master})
	if err != nil {
		t.Fatal(err)
	}
	defer parentIO.Close()
	var sessionOutput io.Reader = readOutput
	if mode == "interactive" {
		sessionOutput = parentIO.Stdin
	}
	go func() { defer close(drained); io.Copy(observed, sessionOutput) }()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSuspendTTYSessionHelper$")
	cmd.Env = append(os.Environ(), "PI_SH_SUSPEND_SESSION_HELPER="+mode)
	cmd.ExtraFiles = []*os.File{local, output}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	control, err := cmd.StdoutPipe()
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
	select {
	case <-observed.ready:
	case <-ctx.Done():
		t.Fatal("remote command never ready", observed.text())
	}
	if _, err := parentIO.Stdout.Write([]byte("~\x1a")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		state, e := exec.Command("ps", "-p", fmt.Sprint(cmd.Process.Pid), "-o", "stat=").Output()
		if e == nil && strings.HasPrefix(strings.TrimSpace(string(state)), "T") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("CLI never stopped", observed.text())
		}
		time.Sleep(time.Millisecond)
	}
	cooked, err := term.GetState(fd)
	if err != nil || !reflect.DeepEqual(cooked, before) {
		t.Fatal("stopped CLI retained raw terminal", err)
	}
	flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
	if err != nil || flags&unix.O_NONBLOCK != originalFlags&unix.O_NONBLOCK {
		t.Fatal("stopped CLI retained borrowed flags", flags, originalFlags, err)
	}
	// Model changes made by the shell while pi-sh has handed the terminal back.
	changeModes := exec.Command("stty", "-echo", "eof", "^G")
	changeModes.Stdin = local
	if out, err := changeModes.CombinedOutput(); err != nil {
		t.Fatalf("shell modes: %v %q", err, out)
	}
	changed, err := term.GetState(fd)
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.SetNonblock(fd, true); err != nil {
		t.Fatal(err)
	}
	if err := unix.IoctlSetWinsize(masterFD, unix.TIOCSWINSZ, &unix.Winsize{Row: 31, Col: 97}); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Process.Signal(syscall.SIGCONT); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(control)
	line, err := reader.ReadString('\n')
	if err != nil || line != "resize-resumed\n" {
		t.Fatal("resize after resume", line, err)
	}
	raw, err := term.GetState(fd)
	if err != nil || reflect.DeepEqual(raw, changed) {
		t.Fatal("CLI did not reacquire raw modes", err)
	}
	if _, err := parentIO.Stdout.Write([]byte("x\n")); err != nil {
		t.Fatal(err)
	}
	if mode == "interactive" {
		deadline := time.Now().Add(3 * time.Second)
		for !strings.Contains(strings.ReplaceAll(observed.text(), "\r\n", "\n"), "seen:x\n31 97\n") {
			if time.Now().After(deadline) {
				t.Fatal("interactive resumed output", observed.text())
			}
			time.Sleep(time.Millisecond)
		}
		if _, err := parentIO.Stdout.Write([]byte("~.")); err != nil {
			t.Fatal(err)
		}
	}
	line, err = reader.ReadString('\n')
	if err != nil || line != "session-finished\n" {
		t.Fatal("completion", line, err, observed.text())
	}
	if err := <-waited; err != nil {
		t.Fatal("CLI child", err)
	}
	finished = true
	output.Close()
	if mode == "interactive" {
		parentIO.Cancel()
	}
	<-drained
	after, err := term.GetState(fd)
	if err != nil || !reflect.DeepEqual(after, changed) {
		t.Fatal("CLI lost shell's updated terminal baseline", err)
	}
	flags, err = unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
	if err != nil || flags&unix.O_NONBLOCK == 0 {
		t.Fatal("CLI lost shell's updated descriptor baseline", flags, err)
	}
	text := strings.ReplaceAll(observed.text(), "\r\n", "\n")
	if !strings.Contains(text, "seen:x\n31 97\n") || !strings.Contains(text, "[suspend pi-sh]") {
		t.Fatal("resume output", text)
	}
}
