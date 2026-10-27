//go:build darwin || linux

package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/pion/webrtc/v4"
	"pi-sh/internal/browserauth"
	"pi-sh/internal/connpolicy"
	"pi-sh/internal/diag"
	"pi-sh/internal/forward"
	"pi-sh/internal/shellbroker"
	"pi-sh/internal/transport"
)

// agentSource stands in for Connect and the device agent: every Exchange
// answers with a new local Pion peer whose shell channel is bridged to a new
// interactive /bin/sh on a PTY, so each Establish gets its own session, as the
// device accepted two concurrent sessions natively. PATH holds bash, cat,
// stty and this test binary as "nc" (and as "setsid" where none exists).
type agentSource struct {
	t        *testing.T
	lifetime context.Context
	tools    string
	mu       sync.Mutex
	sessions int
	peers    []*transport.Peer
	shells   []*exec.Cmd
}

func newAgentSource(t *testing.T, lifetime context.Context) *agentSource {
	t.Helper()
	bash := os.Getenv("PI_SH_TEST_BASH")
	if bash == "" {
		bash, _ = exec.LookPath("bash")
	}
	if bash == "" || exec.Command(bash, "-c", "((BASH_VERSINFO[0]>4||(BASH_VERSINFO[0]==4&&BASH_VERSINFO[1]>=3)))").Run() != nil {
		t.Skip("local Bash >=4.3 required")
	}
	tools := t.TempDir()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	links := map[string]string{"bash": bash, "nc": self, "setsid": self}
	if path, err := exec.LookPath("setsid"); err == nil {
		links["setsid"] = path
	}
	for _, name := range []string{"cat", "stty"} {
		path, err := exec.LookPath(name)
		if err != nil {
			t.Skip(name + " unavailable")
		}
		links[name] = path
	}
	for name, target := range links {
		if err := os.Symlink(target, filepath.Join(tools, name)); err != nil {
			t.Fatal(err)
		}
	}
	s := &agentSource{t: t, lifetime: lifetime, tools: tools}
	t.Cleanup(s.shutdown)
	return s
}

func (*agentSource) Configuration(context.Context) (browserauth.Configuration, error) {
	return browserauth.Configuration{}, nil
}
func (*agentSource) Failures() <-chan string { return nil }
func (*agentSource) Close() error            { return nil }

func (s *agentSource) Exchange(ctx context.Context, offer webrtc.SessionDescription) (webrtc.SessionDescription, error) {
	peer, err := transport.New(s.lifetime, transport.Config{})
	if err != nil {
		return webrtc.SessionDescription{}, err
	}
	answer, err := peer.Answer(ctx, offer)
	if err != nil {
		peer.Close()
		return answer, err
	}
	s.mu.Lock()
	s.sessions++
	s.peers = append(s.peers, peer)
	s.mu.Unlock()
	go s.serve(peer)
	return answer, nil
}

func (s *agentSource) serve(peer *transport.Peer) {
	ch, err := peer.Accept(s.lifetime)
	if err != nil {
		return
	}
	cmd := exec.Command("/bin/sh", "-i")
	cmd.Env = []string{"PATH=" + s.tools, "HOME=" + s.tools, "HISTFILE=/dev/null", "ENV=/dev/null", "BASH_ENV=/dev/null", "SHELL=/bin/sh", "TERM=dumb"}
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 24, Cols: 80})
	if err != nil {
		return
	}
	s.mu.Lock()
	s.shells = append(s.shells, cmd)
	s.mu.Unlock()
	go func() {
		for {
			m, err := ch.Receive(s.lifetime)
			if err != nil {
				f.Close()
				return
			}
			if _, err := f.Write(m.Data); err != nil {
				return
			}
		}
	}()
	buffer := make([]byte, 4096)
	for {
		n, err := f.Read(buffer)
		if n > 0 {
			if ch.Send(s.lifetime, transport.Message{Data: append([]byte(nil), buffer[:n]...)}) != nil {
				return
			}
		}
		if err != nil {
			ch.Close()
			return
		}
	}
}

func (s *agentSource) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessions
}

func (s *agentSource) shutdown() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, cmd := range s.shells {
		cmd.Process.Signal(syscall.SIGHUP)
		cmd.Wait()
	}
	for _, p := range s.peers {
		p.Close()
	}
}

// echoServer replies with each connection's input reversed after its EOF,
// then closes: it needs a correct half-close in both directions.
func echoServer(t *testing.T) (net.Listener, int) {
	t.Helper()
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				data, _ := io.ReadAll(c)
				for i, j := 0, len(data)-1; i < j; i, j = i+1, j-1 {
					data[i], data[j] = data[j], data[i]
				}
				c.Write(data)
			}()
		}
	}()
	return l, l.Addr().(*net.TCPAddr).Port
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func reverse(b []byte) []byte {
	out := append([]byte(nil), b...)
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

func binaryPayload(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i * 7)
	}
	return b
}

// -W relays binary stdin to a remote connection and back, half-closing after
// local EOF, then exits 0; a refused target prints ssh's two lines and 255.
func TestStdioForwardingThroughCLI(t *testing.T) {
	lifetime, stop := context.WithTimeout(context.Background(), 60*time.Second)
	defer stop()
	source := newAgentSource(t, lifetime)
	_, port := echoServer(t)
	var logged bytes.Buffer
	ctx := diag.With(lifetime, diag.Default(&logged, "pi-sh"))
	opts, err := parseOptions([]string{"--browser", "-W", "127.0.0.1:" + strconv.Itoa(port), "pi"})
	if err != nil {
		t.Fatal(err)
	}
	payload := binaryPayload(5000)
	var out bytes.Buffer
	code, err := runForwarding(ctx, connpolicy.Default(), source, opts, bytes.NewReader(payload), &out)
	if code != 0 || err != nil || !bytes.Equal(out.Bytes(), reverse(payload)) {
		t.Fatalf("code %d err %v: %d bytes, log %q", code, err, out.Len(), logged.String())
	}
	opts, err = parseOptions([]string{"--browser", "-W", "127.0.0.1:" + strconv.Itoa(freePort(t)), "pi"})
	if err != nil {
		t.Fatal(err)
	}
	logged.Reset()
	code, err = runForwarding(ctx, connpolicy.Default(), source, opts, strings.NewReader(""), io.Discard)
	// OpenSSH 10.2: "channel 0: open failed: connect failed: Connection refused",
	// then fatal "stdio forwarding failed", exit 255.
	if code != 255 || !errors.Is(err, forward.ErrStdioForwarding) || !strings.Contains(logged.String(), "channel 0: open failed: connect failed: Connection refused") {
		t.Fatalf("refused: code %d err %v log %q", code, err, logged.String())
	}
}

// -N -L and -D serve concurrent local clients through one carrier until the
// session context ends; then every connector is reaped (status 255 without
// a classifying signal).
func TestNoSessionLocalForwardingThroughCLI(t *testing.T) {
	lifetime, stop := context.WithTimeout(context.Background(), 60*time.Second)
	defer stop()
	source := newAgentSource(t, lifetime)
	_, port := echoServer(t)
	local := freePort(t)
	var logged bytes.Buffer
	ctx, cancel := context.WithCancel(diag.With(lifetime, diag.Default(&logged, "pi-sh")))
	defer cancel()
	opts, err := parseOptions([]string{"--browser", "-N", "-oExitOnForwardFailure=yes", "-L", "127.0.0.1:" + strconv.Itoa(local) + ":127.0.0.1:" + strconv.Itoa(port), "pi", "ignored"})
	if err != nil {
		t.Fatal(err)
	}
	type result struct {
		code int
		err  error
	}
	done := make(chan result, 1)
	go func() {
		code, err := runForwarding(ctx, connpolicy.Default(), source, opts, nil, io.Discard)
		done <- result{code, err}
	}()
	var conn net.Conn
	for deadline := time.Now().Add(20 * time.Second); ; {
		conn, err = net.Dial("tcp4", "127.0.0.1:"+strconv.Itoa(local))
		if err == nil || time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("listener never ready: %v %q", err, logged.String())
	}
	conn.Close()
	var wg sync.WaitGroup
	errs := make(chan error, 3)
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c, err := net.Dial("tcp4", "127.0.0.1:"+strconv.Itoa(local))
			if err != nil {
				errs <- err
				return
			}
			defer c.Close()
			payload := binaryPayload(1500 + i*700)
			if _, err := c.Write(payload); err != nil {
				errs <- err
				return
			}
			c.(*net.TCPConn).CloseWrite()
			got, err := io.ReadAll(c)
			if err != nil || !bytes.Equal(got, reverse(payload)) {
				errs <- errors.New("stream " + strconv.Itoa(i) + " differs: " + strconv.Itoa(len(got)) + " bytes")
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if n := source.count(); n != 1 {
		t.Errorf("-N used %d Connect sessions, want 1", n)
	}
	cancel()
	select {
	case r := <-done:
		if r.code != 255 || r.err != nil || strings.Contains(logged.String(), "not confirmed") {
			t.Fatalf("end: code %d err %v log %q", r.code, r.err, logged.String())
		}
	case <-lifetime.Done():
		t.Fatal("forwarding did not stop")
	}
	if c, err := net.Dial("tcp4", "127.0.0.1:"+strconv.Itoa(local)); err == nil {
		c.Close()
		t.Fatal("listener still open after stop")
	}
}

// With ExitOnForwardFailure, a local bind failure is fatal before anything
// runs (ssh: "Could not request local forwarding.").
func TestExitOnForwardFailureThroughCLI(t *testing.T) {
	lifetime, stop := context.WithTimeout(context.Background(), 60*time.Second)
	defer stop()
	source := newAgentSource(t, lifetime)
	busy, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	port := busy.Addr().(*net.TCPAddr).Port
	ctx := diag.With(lifetime, diag.Default(io.Discard, "pi-sh"))
	opts, err := parseOptions([]string{"--browser", "-N", "-oExitOnForwardFailure=yes", "-L", "127.0.0.1:" + strconv.Itoa(port) + ":127.0.0.1:1", "pi"})
	if err != nil {
		t.Fatal(err)
	}
	code, err := runForwarding(ctx, connpolicy.Default(), source, opts, nil, io.Discard)
	if code != 255 || err == nil || err.Error() != "Could not request local forwarding." {
		t.Fatalf("code %d err %v", code, err)
	}
}

// -L next to a command uses a second Connect session for the forwarding
// carrier; a forwarded connection works while the command runs, and the
// command's own status is kept.
func TestLocalForwardingAlongsideCommand(t *testing.T) {
	lifetime, stop := context.WithTimeout(context.Background(), 60*time.Second)
	defer stop()
	source := newAgentSource(t, lifetime)
	_, port := echoServer(t)
	local := freePort(t)
	var logged bytes.Buffer
	ctx := diag.With(lifetime, diag.Default(&logged, "pi-sh"))
	opts, err := parseOptions([]string{"--browser", "--experimental-command", "-T", "-L", "127.0.0.1:" + strconv.Itoa(local) + ":127.0.0.1:" + strconv.Itoa(port), "pi", "sleep 2; exit 7"})
	if err != nil {
		t.Fatal(err)
	}
	f, listeners, err := startForwarding(ctx, opts.policy, source, opts)
	if err != nil {
		t.Fatal(err)
	}
	f.serve(ctx, listeners)
	plan, err := shellbroker.New("sleep 2; exit 7")
	if err != nil {
		t.Fatal(err)
	}
	commandDone := make(chan error, 1)
	go func() { commandDone <- connect(ctx, source, -1, nil, plan, nil, strings.NewReader("")) }()
	time.Sleep(300 * time.Millisecond)
	c, err := net.Dial("tcp4", "127.0.0.1:"+strconv.Itoa(local))
	if err != nil {
		t.Fatal(err)
	}
	payload := binaryPayload(3000)
	c.Write(payload)
	c.(*net.TCPConn).CloseWrite()
	got, err := io.ReadAll(c)
	c.Close()
	if err != nil || !bytes.Equal(got, reverse(payload)) {
		t.Fatalf("forwarded during command: %v, %d bytes", err, len(got))
	}
	var status *commandStatus
	if err := <-commandDone; !errors.As(err, &status) || status.code != 7 {
		t.Fatalf("command: %v", err)
	}
	f.stop(ctx)
	if n := source.count(); n != 2 {
		t.Errorf("used %d Connect sessions, want 2", n)
	}
	if strings.Contains(logged.String(), "not confirmed") {
		t.Fatalf("cleanup: %q", logged.String())
	}
}
