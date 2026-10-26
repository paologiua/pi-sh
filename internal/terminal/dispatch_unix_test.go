//go:build darwin || linux

package terminal

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

// After a watcher has existed, SIGTSTP is permanently caught by the runtime.
// A watcher must receive it instead of the process stopping; without one, a
// nonorphaned process must still stop, as the default action would.
func TestExternalSuspendDispatchHelper(t *testing.T) {
	if os.Getenv("PI_SH_SUSPEND_DISPATCH_HELPER") != "1" {
		return
	}
	requests, stop := WatchSuspendSignals()
	fmt.Println("watching")
	<-requests
	fmt.Println("routed")
	stop()
	fmt.Println("default")
	if _, err := bufio.NewReader(os.Stdin).ReadString('\n'); err != nil {
		t.Fatal(err)
	}
	fmt.Println("finished")
}

func TestExternalSuspendDispatchAfterWatcherEnds(t *testing.T) {
	controlReader, controlWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer controlReader.Close()
	defer controlWriter.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestExternalSuspendDispatchHelper$")
	cmd.Env = append(os.Environ(), "PI_SH_SUSPEND_DISPATCH_HELPER=1")
	cmd.Stdin = controlReader
	// A separate group whose parent (this test) shares the session is not
	// orphaned, so the default SIGTSTP action would stop it.
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
	expect := func(want string) {
		t.Helper()
		line, err := reader.ReadString('\n')
		if err != nil || line != want+"\n" {
			t.Fatalf("helper said %q (%v), want %q", line, err, want)
		}
	}
	stopped := func() bool {
		state, err := exec.Command("ps", "-p", fmt.Sprint(cmd.Process.Pid), "-o", "stat=").Output()
		return err == nil && strings.HasPrefix(strings.TrimSpace(string(state)), "T")
	}
	expect("watching")
	if err := cmd.Process.Signal(syscall.SIGTSTP); err != nil {
		t.Fatal(err)
	}
	expect("routed")
	expect("default")
	if stopped() {
		t.Fatal("a routed SIGTSTP stopped the process")
	}
	if err := cmd.Process.Signal(syscall.SIGTSTP); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for !stopped() {
		if time.Now().After(deadline) {
			t.Fatal("SIGTSTP without a watcher did not stop the process")
		}
		time.Sleep(time.Millisecond)
	}
	if err := cmd.Process.Signal(syscall.SIGCONT); err != nil {
		t.Fatal(err)
	}
	if _, err := controlWriter.WriteString("finish\n"); err != nil {
		t.Fatal(err)
	}
	expect("finished")
	if err := <-waited; err != nil {
		t.Fatal("dispatch helper failed", err)
	}
	finished = true
}
