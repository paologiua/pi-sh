//go:build darwin || linux

package shellbroker

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

type guardedBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (b *guardedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(p)
}
func (b *guardedBuffer) text() string { b.mu.Lock(); defer b.mu.Unlock(); return b.buffer.String() }

func localSSHReference(t *testing.T) func(string, []byte) (int, []byte, []byte) {
	command := localSSHCommand(t)
	return func(text string, input []byte) (int, []byte, []byte) { return runSSHReference(t, command(text, input)) }
}

func runSSHReference(t *testing.T, cmd *exec.Cmd) (int, []byte, []byte) {
	t.Helper()
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			code = exit.ExitCode()
		} else {
			t.Fatal(err)
		}
	}
	return code, out.Bytes(), stderr.Bytes()
}

func localSSHCommand(t *testing.T) func(string, []byte) *exec.Cmd {
	t.Helper()
	ssh, err := exec.LookPath("ssh")
	if err != nil {
		t.Skip("OpenSSH client not available")
	}
	keygen, err := exec.LookPath("ssh-keygen")
	if err != nil {
		t.Skip("OpenSSH key generator not available")
	}
	sshd, err := exec.LookPath("sshd")
	if err != nil {
		if _, e := os.Stat("/usr/sbin/sshd"); e == nil {
			sshd = "/usr/sbin/sshd"
		} else {
			t.Skip("OpenSSH server not available")
		}
	}
	account, err := user.Current()
	if err != nil {
		t.Skip("local account unavailable")
	}
	dir := t.TempDir()
	for _, name := range []string{"host", "user"} {
		if output, err := exec.Command(keygen, "-q", "-t", "ed25519", "-N", "", "-f", filepath.Join(dir, name)).CombinedOutput(); err != nil {
			t.Fatalf("test key generation: %v %s", err, output)
		}
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	hostPub, err := os.ReadFile(filepath.Join(dir, "host.pub"))
	if err != nil {
		t.Fatal(err)
	}
	fields := strings.Fields(string(hostPub))
	known := filepath.Join(dir, "known")
	if err = os.WriteFile(known, []byte(fmt.Sprintf("[127.0.0.1]:%d %s %s\n", port, fields[0], fields[1])), 0600); err != nil {
		t.Fatal(err)
	}
	config := fmt.Sprintf("ListenAddress 127.0.0.1\nPort %d\nHostKey %s/host\nPidFile %s/pid\nAuthorizedKeysFile %s/user.pub\nPasswordAuthentication no\nKbdInteractiveAuthentication no\nPubkeyAuthentication yes\nUsePAM no\nStrictModes yes\nPrintMotd no\nLogLevel ERROR\n", port, dir, dir, dir)
	path := filepath.Join(dir, "config")
	if err = os.WriteFile(path, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	var log guardedBuffer
	server := exec.Command(sshd, "-D", "-e", "-f", path)
	server.Stderr = &log
	if err = server.Start(); err != nil {
		t.Skipf("local OpenSSH server cannot start: %v", err)
	}
	stopped := make(chan error, 1)
	go func() { stopped <- server.Wait() }()
	t.Cleanup(func() {
		server.Process.Signal(os.Interrupt)
		select {
		case <-stopped:
		case <-time.After(2 * time.Second):
			server.Process.Kill()
			<-stopped
		}
	})
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	deadline := time.Now().Add(3 * time.Second)
	for {
		conn, e := net.DialTimeout("tcp4", address, 100*time.Millisecond)
		if e == nil {
			conn.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Skipf("local OpenSSH server unavailable: %s", log.text())
		}
		time.Sleep(10 * time.Millisecond)
	}
	command := func(text string, input []byte) *exec.Cmd {
		cmd := exec.Command(ssh, "-F", "/dev/null", "-T", "-o", "BatchMode=yes", "-o", "ConnectTimeout=3", "-o", "StrictHostKeyChecking=yes", "-o", "UserKnownHostsFile="+known, "-o", "GlobalKnownHostsFile=/dev/null", "-o", "IdentityAgent=none", "-o", "IdentitiesOnly=yes", "-i", filepath.Join(dir, "user"), "-p", strconv.Itoa(port), account.Username+"@127.0.0.1", text)
		cmd.Stdin = bytes.NewReader(input)
		return cmd
	}
	if code, _, stderr := runSSHReference(t, command("true", nil)); code != 0 {
		t.Skipf("local reference account not permitted by sshd: %s", stderr)
	}
	version, _ := exec.Command(ssh, "-V").CombinedOutput()
	t.Logf("OpenSSH reference: %s", strings.TrimSpace(string(version)))
	return command
}

func TestOpenSSHLocalSignalsTerminateClient(t *testing.T) {
	command := localSSHCommand(t)
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT} {
		t.Run(sig.String(), func(t *testing.T) {
			cmd := command("trap 'exit 42' INT; trap 'exit 43' TERM; printf 'local-signal-ready\\n'; read -r ignored", nil)
			read, write, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer read.Close()
			defer write.Close()
			cmd.Stdin = read // Hold stdin open until the client has terminated.
			var out, stderr guardedBuffer
			cmd.Stdout, cmd.Stderr = &out, &stderr
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer cmd.Process.Kill()
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			deadline := time.Now().Add(5 * time.Second)
			for !strings.Contains(out.text(), "local-signal-ready\n") {
				select {
				case err := <-done:
					t.Fatalf("client ended before signal: %v %s", err, stderr.text())
				default:
				}
				if time.Now().After(deadline) {
					t.Fatal("reference command never became ready")
				}
				time.Sleep(5 * time.Millisecond)
			}
			if err := cmd.Process.Signal(sig); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				failure, ok := err.(*exec.ExitError)
				if !ok || failure.ExitCode() != 255 {
					t.Fatalf("local signal produced %v, not client failure 255", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("OpenSSH did not terminate on local signal")
			}
		})
	}
}

func TestOpenSSHDifferentialCommandBehavior(t *testing.T) {
	reference := localSSHReference(t)
	all := make([]byte, 256)
	octals := ""
	for i := range all {
		all[i] = byte(i)
		octals += fmt.Sprintf("\\%03o", i)
	}
	for _, tc := range []struct {
		name, text string
		input      []byte
	}{
		{"every-byte-stdin", "cat", bytes.Repeat(all, 8)},
		{"separate-binary-streams", "printf '" + octals + "'; printf '" + octals + "' >&2", nil},
		{"input-eof-then-output", "cat; printf after >&2", []byte("binary\x00\xff\r\n")},
		{"no-controlling-tty", "test ! -t 0 && test ! -t 1 && test ! -t 2 && ! ( : </dev/tty ) 2>/dev/null", nil},
		{"exit-0", "exit 0", nil}, {"exit-1", "exit 1", nil}, {"exit-23", "exit 23", nil},
		{"exit-126", "exit 126", nil}, {"exit-127", "exit 127", nil}, {"exit-143", "exit 143", nil}, {"exit-255", "exit 255", nil},
		{"signal-term", "kill -TERM $$", nil}, {"signal-int", "kill -INT $$", nil}, {"signal-kill", "kill -KILL $$", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wantCode, wantOut, wantErr := reference(tc.text, tc.input)
			var out, stderr bytes.Buffer
			code, err := runLocal(t, tc.text, bytes.NewReader(tc.input), &out, &stderr)
			if err != nil || code != wantCode || !bytes.Equal(out.Bytes(), wantOut) || !bytes.Equal(stderr.Bytes(), wantErr) {
				t.Fatalf("broker code=%d err=%v out=%q stderr=%q; ssh code=%d out=%q stderr=%q", code, err, out.Bytes()[:min(out.Len(), 80)], stderr.Bytes()[:min(stderr.Len(), 80)], wantCode, wantOut[:min(len(wantOut), 80)], wantErr[:min(len(wantErr), 80)])
			}
		})
	}
}
