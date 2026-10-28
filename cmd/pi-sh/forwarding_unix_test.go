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
	"pi-sh/internal/commandrun"
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
// stty, perl and this test binary as "nc" (and as "setsid" where none exists).
type agentSource struct {
	t        *testing.T
	lifetime context.Context
	tools    string
	mu       sync.Mutex
	sessions int
	peers    []*transport.Peer
	shells   []*exec.Cmd
	failures chan string
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
	// The -R listener runs on Perl, as on the device.
	if path, err := exec.LookPath("perl"); err == nil {
		links["perl"] = path
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
func (s *agentSource) Failures() <-chan string { return s.failures }
func (*agentSource) Close() error              { return nil }

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
	// As with ssh -W, escapes are not interpreted: "\r~#" and "~." are data.
	payload := append([]byte("\r~#\r~.\n"), binaryPayload(5000)...)
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
	f, listeners, err := startForwarding(ctx, opts.policy, source, opts, nil)
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

// -N -R through the CLI: the Perl listener (here on this machine) accepts,
// pi-sh connects to the local target, and stop releases the remote port.
func TestNoSessionRemoteForwardingThroughCLI(t *testing.T) {
	if _, err := exec.LookPath("perl"); err != nil {
		t.Skip("perl unavailable")
	}
	lifetime, stop := context.WithTimeout(context.Background(), 60*time.Second)
	defer stop()
	source := newAgentSource(t, lifetime)
	_, port := echoServer(t)
	remote := freePort(t)
	var logged bytes.Buffer
	ctx, cancel := context.WithCancel(diag.With(lifetime, diag.Default(&logged, "pi-sh")))
	defer cancel()
	opts, err := parseOptions([]string{"--browser", "-N", "-oExitOnForwardFailure=yes", "-R", "127.0.0.1:" + strconv.Itoa(remote) + ":127.0.0.1:" + strconv.Itoa(port), "pi"})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan int, 1)
	go func() {
		code, _ := runForwarding(ctx, connpolicy.Default(), source, opts, nil, io.Discard)
		done <- code
	}()
	var conn net.Conn
	for deadline := time.Now().Add(20 * time.Second); ; {
		conn, err = net.Dial("tcp4", "127.0.0.1:"+strconv.Itoa(remote))
		if err == nil || time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("remote listener never ready: %v %q", err, logged.String())
	}
	payload := binaryPayload(40000)
	go func() {
		conn.Write(payload)
		conn.(*net.TCPConn).CloseWrite()
	}()
	got, err := io.ReadAll(conn)
	conn.Close()
	if err != nil || !bytes.Equal(got, reverse(payload)) {
		t.Fatalf("%d bytes, %v", len(got), err)
	}
	cancel()
	select {
	case code := <-done:
		if code != 255 || strings.Contains(logged.String(), "not confirmed") {
			t.Fatalf("end: code %d log %q", code, logged.String())
		}
	case <-lifetime.Done():
		t.Fatal("forwarding did not stop")
	}
	for deadline := time.Now().Add(10 * time.Second); ; {
		l, err := net.Listen("tcp4", "127.0.0.1:"+strconv.Itoa(remote))
		if err == nil {
			l.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("remote listener still bound after stop")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// ExitOnForwardFailure makes a refused remote listen fatal with ssh's line.
func TestRemoteForwardFailureIsFatalThroughCLI(t *testing.T) {
	if _, err := exec.LookPath("perl"); err != nil {
		t.Skip("perl unavailable")
	}
	lifetime, stop := context.WithTimeout(context.Background(), 60*time.Second)
	defer stop()
	source := newAgentSource(t, lifetime)
	busy, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	bp := strconv.Itoa(busy.Addr().(*net.TCPAddr).Port)
	opts, err := parseOptions([]string{"--browser", "-N", "-oExitOnForwardFailure=yes", "-R", "127.0.0.1:" + bp + ":127.0.0.1:1", "pi"})
	if err != nil {
		t.Fatal(err)
	}
	code, err := runForwarding(lifetime, connpolicy.Default(), source, opts, nil, io.Discard)
	if code != 255 || err == nil || err.Error() != "Error: remote port forwarding failed for listen port "+bp {
		t.Fatal(code, err)
	}
}

// After the command ends, pi-sh keeps serving forwarded connections until they
// close, as ssh 10.2 does (silently): the open connection keeps working, a new
// one is still forwarded, and the wait ends when both close. A signal (the
// app context ending) ends the wait with false, which app() turns into 255.
func TestSessionEndWaitsForForwardedConnections(t *testing.T) {
	lifetime, stop := context.WithTimeout(context.Background(), 60*time.Second)
	defer stop()
	source := newAgentSource(t, lifetime)
	_, port := echoServer(t)
	local := freePort(t)
	var logged bytes.Buffer
	ctx := diag.With(lifetime, diag.Default(&logged, "pi-sh"))
	opts, err := parseOptions([]string{"--browser", "--experimental-command", "-T", "-L", "127.0.0.1:" + strconv.Itoa(local) + ":127.0.0.1:" + strconv.Itoa(port), "pi", "exit 7"})
	if err != nil {
		t.Fatal(err)
	}
	chans := forward.NewChannels()
	f, listeners, err := startForwarding(ctx, opts.policy, source, opts, chans)
	if err != nil {
		t.Fatal(err)
	}
	defer f.stop(ctx)
	session := chans.Open(sessionChannel)
	f.serve(ctx, listeners)
	address := "127.0.0.1:" + strconv.Itoa(local)
	held, err := net.Dial("tcp4", address)
	if err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(10 * time.Second); chans.Len() != 2; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal(chans.List())
		}
	}
	plan, err := shellbroker.New("exit 7")
	if err != nil {
		t.Fatal(err)
	}
	err = connect(ctx, source, -1, nil, plan, nil, strings.NewReader(""))
	var status *commandStatus
	if !errors.As(err, &status) || status.code != 7 || !sessionEnded(err) {
		t.Fatalf("command: %v", err)
	}
	chans.Release(session)
	waited := make(chan bool, 1)
	go func() { waited <- f.await(ctx, chans, nil) }()
	// A connection opened during the wait is still forwarded.
	late, err := net.Dial("tcp4", address)
	if err != nil {
		t.Fatal(err)
	}
	late.Write([]byte("late"))
	late.(*net.TCPConn).CloseWrite()
	if got, err := io.ReadAll(late); err != nil || string(got) != "etal" {
		t.Fatalf("during the wait: %q %v", got, err)
	}
	late.Close()
	select {
	case <-waited:
		t.Fatal("wait ended with a connection open")
	case <-time.After(300 * time.Millisecond):
	}
	held.Write([]byte("held"))
	held.(*net.TCPConn).CloseWrite()
	if got, err := io.ReadAll(held); err != nil || string(got) != "dleh" {
		t.Fatalf("held connection: %q %v", got, err)
	}
	held.Close()
	select {
	case ok := <-waited:
		if !ok {
			t.Fatal("wait reported a signal")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("wait did not end after the connections closed")
	}
	// A signal during the wait: the context ends, await reports false.
	again, err := net.Dial("tcp4", address)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	for deadline := time.Now().Add(10 * time.Second); chans.Len() != 1; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal(chans.List())
		}
	}
	signalled, cancel := context.WithCancel(ctx)
	go func() { time.Sleep(100 * time.Millisecond); cancel() }()
	if f.await(signalled, chans, nil) {
		t.Fatal("signal did not end the wait")
	}
	if sessionEnded(errEscape) || sessionEnded(transport.ErrClosed) {
		t.Fatal("~. or a transport failure must not wait")
	}
}

// Lost authorization ends -N forwarding (and the end-of-session wait) with
// 255 and the session's authorization error. Before, only a session read the
// source's failures, so -N kept serving after the device was de-authorized.
func TestForwardingEndsOnLostAuthorization(t *testing.T) {
	lifetime, stop := context.WithTimeout(context.Background(), 60*time.Second)
	defer stop()
	source := newAgentSource(t, lifetime)
	source.failures = make(chan string, 1)
	_, port := echoServer(t)
	local := freePort(t)
	var logged bytes.Buffer
	ctx := diag.With(lifetime, diag.Default(&logged, "pi-sh"))
	opts, err := parseOptions([]string{"--browser", "-N", "-L", "127.0.0.1:" + strconv.Itoa(local) + ":127.0.0.1:" + strconv.Itoa(port), "pi"})
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
	for deadline := time.Now().Add(20 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if c, err := net.Dial("tcp4", "127.0.0.1:"+strconv.Itoa(local)); err == nil {
			c.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("listener not ready")
		}
	}
	source.failures <- "browser companion heartbeat expired"
	select {
	case r := <-done:
		if r.code != 255 || !errors.Is(r.err, commandrun.ErrAuthorization) {
			t.Fatalf("code %d err %v", r.code, r.err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("-N kept serving after the authorization failure")
	}

	// The same during the wait after a session.
	chans := forward.NewChannels()
	opts, _ = parseOptions([]string{"--browser", "--experimental-command", "-T", "-L", "127.0.0.1:" + strconv.Itoa(freePort(t)) + ":127.0.0.1:" + strconv.Itoa(port), "pi", "true"})
	f, listeners, err := startForwarding(ctx, opts.policy, source, opts, chans)
	if err != nil {
		t.Fatal(err)
	}
	defer f.stop(ctx)
	f.serve(ctx, listeners)
	chans.Open("held")
	failures := make(chan string, 1)
	failures <- "direct Connect authorization check failed"
	if f.await(ctx, chans, failures) {
		t.Fatal("wait survived the authorization failure")
	}
	if !strings.Contains(logged.String(), "session authorization check failed") {
		t.Fatalf("not reported: %q", logged.String())
	}
}

// A carrier that fails while the session runs is reported once. Before,
// nothing watched it and stop's Close returned nil, so forwarding stopped
// silently and every later connection just failed.
func TestForwardingCarrierFailureIsReported(t *testing.T) {
	lifetime, stop := context.WithTimeout(context.Background(), 60*time.Second)
	defer stop()
	source := newAgentSource(t, lifetime)
	_, port := echoServer(t)
	var logged guardedLog
	ctx := diag.With(lifetime, diag.Default(&logged, "pi-sh"))
	opts, err := parseOptions([]string{"--browser", "--experimental-command", "-T", "-L", "127.0.0.1:" + strconv.Itoa(freePort(t)) + ":127.0.0.1:" + strconv.Itoa(port), "pi", "true"})
	if err != nil {
		t.Fatal(err)
	}
	f, listeners, err := startForwarding(ctx, opts.policy, source, opts, forward.NewChannels())
	if err != nil {
		t.Fatal(err)
	}
	f.serve(ctx, listeners)
	f.watch(ctx)
	f.peer.Close()
	for deadline := time.Now().Add(10 * time.Second); !strings.Contains(logged.String(), "forwarding carrier failed"); time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("not reported: %q", logged.String())
		}
	}
	f.stop(ctx)
	if n := strings.Count(logged.String(), "forwarding carrier failed"); n != 1 {
		t.Fatalf("reported %d times: %q", n, logged.String())
	}
}

type guardedLog struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (g *guardedLog) Write(p []byte) (int, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.b.Write(p)
}

func (g *guardedLog) String() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.b.String()
}
