//go:build darwin || linux

package fwdbroker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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

	"pi-sh/internal/forward"
	"pi-sh/internal/forwardspec"
)

// TestMain lets this test binary act as a stand-in connector named "nc" with
// the OpenBSD netcat behaviour the adapter relies on (-N half-close, -v
// success line, -U Unix sockets, exit 1 on connect failure). It runs only
// locally; the device's real connector is checked by the probe scripts.
func TestMain(m *testing.M) {
	if filepath.Base(os.Args[0]) == "nc" {
		os.Exit(fakeNetcat(os.Args[1:]))
	}
	os.Exit(m.Run())
}

func fakeNetcat(args []string) int {
	verbose, unix := false, false
	var operands []string
	for i, a := range args {
		if a == "--" {
			operands = args[i+1:]
			break
		}
		switch a {
		case "-h":
			fmt.Fprintln(os.Stderr, "OpenBSD netcat (pi-sh test double)")
			return 1
		case "-v":
			verbose = true
		case "-U":
			unix = true
		case "-N":
		default:
			return 2
		}
	}
	var conn net.Conn
	var err error
	if unix && len(operands) == 1 {
		conn, err = net.Dial("unix", operands[0])
	} else if len(operands) == 2 {
		conn, err = net.Dial("tcp", net.JoinHostPort(operands[0], operands[1]))
	} else {
		return 2
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "nc: connect to %s failed: Connection refused\n", strings.Join(operands, " port "))
		return 1
	}
	if verbose && !unix {
		fmt.Fprintf(os.Stderr, "Connection to %s %s port [tcp/*] succeeded!\n", operands[0], operands[1])
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		io.Copy(conn, os.Stdin)
		conn.(interface{ CloseWrite() error }).CloseWrite()
	}()
	go func() {
		defer wg.Done()
		io.Copy(os.Stdout, conn)
		os.Stdout.Close()
	}()
	wg.Wait()
	return 0
}

type readResult struct {
	data []byte
	err  error
}

type ptyStream struct {
	file   *os.File
	frames chan readResult
}

func (s ptyStream) Send(ctx context.Context, b []byte) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	_, err := s.file.Write(b)
	return err
}

func (s ptyStream) Receive(ctx context.Context) ([]byte, error) {
	select {
	case f := <-s.frames:
		return f.data, f.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// startMux runs the adapter in a local interactive /bin/sh over a real PTY,
// with PATH restricted to bash, cat, stty and (optionally) the test connector.
func startMux(t *testing.T, withConnector bool) (*Mux, error) {
	t.Helper()
	bash := os.Getenv("PI_SH_TEST_BASH")
	if bash == "" {
		bash, _ = exec.LookPath("bash")
	}
	if bash == "" || exec.Command(bash, "-c", "((BASH_VERSINFO[0]>4||(BASH_VERSINFO[0]==4&&BASH_VERSINFO[1]>=3)))").Run() != nil {
		t.Skip("Bash >=4.3 required; set PI_SH_TEST_BASH")
	}
	tools := t.TempDir()
	link := func(name, target string) {
		if err := os.Symlink(target, filepath.Join(tools, name)); err != nil {
			t.Fatal(err)
		}
	}
	link("bash", bash)
	for _, name := range []string{"cat", "stty"} {
		path, err := exec.LookPath(name)
		if err != nil {
			t.Skip(name + " unavailable")
		}
		link(name, path)
	}
	if withConnector {
		self, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		link("nc", self)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	cmd := exec.Command("/bin/sh", "-i")
	cmd.Env = []string{"PATH=" + tools, "HOME=" + tools, "HISTFILE=/dev/null", "ENV=/dev/null", "BASH_ENV=/dev/null", "SHELL=/bin/sh", "TERM=dumb"}
	file, err := pty.Start(cmd)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	s := ptyStream{file: file, frames: make(chan readResult, 64)}
	go func() {
		for {
			b := make([]byte, 4096)
			n, err := file.Read(b)
			if errors.Is(err, syscall.EIO) {
				err = io.EOF
			}
			select {
			case s.frames <- readResult{b[:n], err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
	m, err := Start(ctx, s)
	t.Cleanup(func() {
		if m != nil {
			if err := m.Close(); err != nil {
				t.Error("adapter cleanup:", err)
			}
		}
		cancel()
		file.Close()
		cmd.Process.Signal(syscall.SIGHUP)
		cmd.Wait()
	})
	return m, err
}

func reverseServer(t *testing.T, network, address string) net.Listener {
	l, err := net.Listen(network, address)
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
				request, err := io.ReadAll(c)
				if err != nil {
					return
				}
				for i, j := 0, len(request)-1; i < j; i, j = i+1, j-1 {
					request[i], request[j] = request[j], request[i]
				}
				c.Write(request)
			}()
		}
	}()
	return l
}

func reversed(b []byte) []byte {
	out := append([]byte(nil), b...)
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

func tcpTarget(l net.Listener) forward.Target {
	a := l.Addr().(*net.TCPAddr)
	return forward.Target{Host: a.IP.String(), Port: a.Port}
}

func roundTrip(ctx context.Context, m *Mux, target forward.Target, request []byte) ([]byte, error) {
	s, err := m.Dial(ctx, target)
	if err != nil {
		return nil, err
	}
	defer s.Close()
	go func() {
		s.Write(request)
		s.CloseWrite()
	}()
	return io.ReadAll(s)
}

func TestAdapterHalfCloseBinaryAndConcurrency(t *testing.T) {
	m, err := startMux(t, true)
	if err != nil {
		t.Fatal(err)
	}
	if m.Connector().Name != "openbsd-nc" || len(m.Detected["nc"]) == 0 {
		t.Fatal("connector selection", m.Connector().Name, m.Detected)
	}
	server := reverseServer(t, "tcp4", "127.0.0.1:0")
	ctx := context.Background()
	request := make([]byte, 256*16)
	for i := range request {
		request[i] = byte(i)
	}
	reply, err := roundTrip(ctx, m, tcpTarget(server), request)
	if err != nil || !bytes.Equal(reply, reversed(request)) {
		t.Fatalf("binary half-close round trip: %d bytes, %v", len(reply), err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r := append(bytes.Repeat([]byte{byte(i)}, 700+i*37), 0, 255, byte(i))
			got, err := roundTrip(ctx, m, tcpTarget(server), r)
			if err != nil || !bytes.Equal(got, reversed(r)) {
				errs <- fmt.Errorf("stream %d: %d bytes, %v", i, len(got), err)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestAdapterIPv6AndUnixTargets(t *testing.T) {
	m, err := startMux(t, true)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if l, err := net.Listen("tcp6", "[::1]:0"); err == nil {
		l.Close()
		server := reverseServer(t, "tcp6", "[::1]:0")
		if got, err := roundTrip(ctx, m, tcpTarget(server), []byte("ipv6")); err != nil || string(got) != "6vpi" {
			t.Fatalf("%q %v", got, err)
		}
	}
	dir, err := os.MkdirTemp("", "fw")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "s")
	reverseServer(t, "unix", path)
	if got, err := roundTrip(ctx, m, forward.Target{Path: path}, []byte("unix")); err != nil || string(got) != "xinu" {
		t.Fatalf("%q %v", got, err)
	}
}

func TestAdapterRemoteFirstCloseAndLateInput(t *testing.T) {
	m, err := startMux(t, true)
	if err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	late := make(chan string, 1)
	go func() {
		c, err := l.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		c.Write([]byte("banner"))
		c.(*net.TCPConn).CloseWrite()
		got, _ := io.ReadAll(c)
		late <- string(got)
	}()
	s, err := m.Dial(context.Background(), tcpTarget(l))
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(s)
	if err != nil || string(got) != "banner" {
		t.Fatal(got, err)
	}
	if _, err := s.Write([]byte("after-remote-eof")); err != nil {
		t.Fatal(err)
	}
	s.CloseWrite()
	select {
	case got := <-late:
		if got != "after-remote-eof" {
			t.Fatal(got)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("input after remote EOF not delivered")
	}
}

func TestAdapterOpenFailure(t *testing.T) {
	m, err := startMux(t, true)
	if err != nil {
		t.Fatal(err)
	}
	l, _ := net.Listen("tcp4", "127.0.0.1:0")
	target := tcpTarget(l)
	l.Close()
	_, err = m.Dial(context.Background(), target)
	var open *forward.OpenError
	if !errors.As(err, &open) || !strings.Contains(open.Detail, "Connection refused") {
		t.Fatal(err)
	}
	if _, err := m.Dial(context.Background(), forward.Target{Host: "-oProxy", Port: 1}); !errors.As(err, &open) {
		t.Fatal("option-like host accepted", err)
	}
	// The adapter remains usable after a failure.
	server := reverseServer(t, "tcp4", "127.0.0.1:0")
	if got, err := roundTrip(context.Background(), m, tcpTarget(server), []byte("ok")); err != nil || string(got) != "ko" {
		t.Fatal(got, err)
	}
}

// An unread stream must not stall others (per-stream windows), and closing
// streams and the adapter must terminate the remote connectors.
func TestAdapterBackpressureIsolationAndCleanup(t *testing.T) {
	m, err := startMux(t, true)
	if err != nil {
		t.Fatal(err)
	}
	flood, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer flood.Close()
	closed := make(chan struct{}, 2)
	go func() {
		for {
			c, err := flood.Accept()
			if err != nil {
				return
			}
			go func() {
				big := bytes.Repeat([]byte("x"), 64<<10)
				for {
					c.SetWriteDeadline(time.Now().Add(20 * time.Second))
					if _, err := c.Write(big); err != nil {
						break
					}
				}
				c.Close()
				closed <- struct{}{}
			}()
		}
	}()
	ctx := context.Background()
	stalled, err := m.Dial(ctx, tcpTarget(flood))
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 100)
	if _, err := io.ReadFull(stalled, buf); err != nil {
		t.Fatal(err)
	}
	server := reverseServer(t, "tcp4", "127.0.0.1:0")
	start := time.Now()
	if got, err := roundTrip(ctx, m, tcpTarget(server), []byte("independent")); err != nil || string(got) != "tnednepedni" {
		t.Fatal(got, err)
	}
	if time.Since(start) > 10*time.Second {
		t.Fatal("unread stream delayed another stream")
	}
	stalled.Close()
	select {
	case <-closed:
	case <-time.After(15 * time.Second):
		t.Fatal("closing a stream did not terminate its remote connection")
	}
	second, err := m.Dial(ctx, tcpTarget(flood))
	if err != nil {
		t.Fatal(err)
	}
	io.ReadFull(second, buf)
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-closed:
	case <-time.After(15 * time.Second):
		t.Fatal("adapter close did not terminate remote connections")
	}
}

func TestAdapterWithoutConnectorRefuses(t *testing.T) {
	m, err := startMux(t, false)
	if !errors.Is(err, ErrNoConnector) || m != nil {
		t.Fatal(err)
	}
}

// The whole local side over the adapter: SOCKS5 and -L listeners.
func TestEndToEndLocalAndDynamicForwarding(t *testing.T) {
	m, err := startMux(t, true)
	if err != nil {
		t.Fatal(err)
	}
	server := reverseServer(t, "tcp4", "127.0.0.1:0")
	sp := server.Addr().(*net.TCPAddr).Port
	free := func() int {
		l, _ := net.Listen("tcp4", "127.0.0.1:0")
		defer l.Close()
		return l.Addr().(*net.TCPAddr).Port
	}
	lp, dp := free(), free()
	local, _ := forwardspec.Local(fmt.Sprintf("127.0.0.1:%d:127.0.0.1:%d", lp, sp), nil)
	dynamic, _ := forwardspec.Dynamic(fmt.Sprintf("127.0.0.1:%d", dp), nil)
	ctx, cancel := context.WithCancel(context.Background())
	f, err := forward.Listen(ctx, forward.Config{Forwards: []forwardspec.Forward{local, dynamic}, BindMask: 0o177}, forward.Log{})
	if err != nil {
		t.Fatal(err)
	}
	served := make(chan struct{})
	go func() { f.Serve(ctx, m); close(served) }()
	defer func() { cancel(); <-served }()
	exchange := func(c net.Conn, request string) string {
		c.SetDeadline(time.Now().Add(20 * time.Second))
		c.Write([]byte(request))
		c.(*net.TCPConn).CloseWrite()
		got, _ := io.ReadAll(c)
		return string(got)
	}
	c, err := net.Dial("tcp4", "127.0.0.1:"+strconv.Itoa(lp))
	if err != nil {
		t.Fatal(err)
	}
	if got := exchange(c, "local"); got != "lacol" {
		t.Fatal(got)
	}
	c.Close()
	c, err = net.Dial("tcp4", "127.0.0.1:"+strconv.Itoa(dp))
	if err != nil {
		t.Fatal(err)
	}
	c.SetDeadline(time.Now().Add(20 * time.Second))
	c.Write(append([]byte{5, 1, 0, 5, 1, 0, 1, 127, 0, 0, 1}, byte(sp>>8), byte(sp)))
	reply := make([]byte, 12)
	if _, err := io.ReadFull(c, reply); err != nil || reply[1] != 0 || reply[3] != 0 {
		t.Fatal(reply, err)
	}
	if got := exchange(c, "socks"); got != "skcos" {
		t.Fatal(got)
	}
	c.Close()
	var out bytes.Buffer
	if err := forward.Stdio(context.Background(), m, forward.Target{Host: "127.0.0.1", Port: sp}, strings.NewReader("stdio"), &out, forward.Log{}); err != nil || out.String() != "oidts" {
		t.Fatal(out.String(), err)
	}
}
