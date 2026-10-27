package forward

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"pi-sh/internal/forwardspec"
)

// netDialer stands in for the remote side: it dials locally and maps a refused
// connection to an OpenError delivered by Read, as the adapter does.
type netDialer struct{ opened chan Target }

type netStream struct {
	net.Conn
	openErr error
}

func (s *netStream) Read(b []byte) (int, error) {
	if s.openErr != nil {
		return 0, s.openErr
	}
	return s.Conn.Read(b)
}
func (s *netStream) Write(b []byte) (int, error) {
	if s.openErr != nil {
		return len(b), nil
	}
	return s.Conn.Write(b)
}
func (s *netStream) CloseWrite() error {
	if s.openErr != nil {
		return nil
	}
	return s.Conn.(halfCloser).CloseWrite()
}
func (s *netStream) Close() error {
	if s.Conn == nil {
		return nil
	}
	return s.Conn.Close()
}

func (d netDialer) Dial(ctx context.Context, t Target) (Stream, error) {
	if d.opened != nil {
		d.opened <- t
	}
	network, address := "tcp", t.String()
	if t.Path != "" {
		network = "unix"
	}
	c, err := (&net.Dialer{}).DialContext(ctx, network, address)
	if err != nil {
		return &netStream{openErr: &OpenError{Detail: "Connection refused"}}, nil
	}
	return &netStream{Conn: c}, nil
}

// halfCloseServer reads a request to EOF, then replies with the reversed bytes.
func halfCloseServer(t *testing.T, network, address string) net.Listener {
	l, err := net.Listen(network, address)
	if err != nil {
		t.Fatal(err)
	}
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
	t.Cleanup(func() { l.Close() })
	return l
}

func allBytes(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i)
	}
	return b
}

func reversed(b []byte) []byte {
	out := append([]byte(nil), b...)
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

func freePort(t *testing.T) int {
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func startForwarder(t *testing.T, cfg Config, log Log) (*Forwarder, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	f, err := Listen(ctx, cfg, log)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { f.Serve(ctx, netDialer{}); close(done) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("Serve did not finish after cancellation")
		}
	})
	return f, cancel
}

func exchange(t *testing.T, c net.Conn, request []byte) []byte {
	t.Helper()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.Write(request); err != nil {
		t.Fatal(err)
	}
	if err := c.(halfCloser).CloseWrite(); err != nil {
		t.Fatal(err)
	}
	reply, err := io.ReadAll(c)
	if err != nil {
		t.Fatal(err)
	}
	return reply
}

func TestLocalForwardHalfCloseBinaryConcurrent(t *testing.T) {
	server := halfCloseServer(t, "tcp4", "127.0.0.1:0")
	port := freePort(t)
	spec := fmt.Sprintf("127.0.0.1:%d:127.0.0.1:%d", port, server.Addr().(*net.TCPAddr).Port)
	fwd, ok := forwardspec.Local(spec, nil)
	if !ok {
		t.Fatal(spec)
	}
	startForwarder(t, Config{Forwards: []forwardspec.Forward{fwd}, BindMask: 0o177}, Log{})
	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c, err := net.Dial("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
			if err != nil {
				errs <- err
				return
			}
			defer c.Close()
			request := append(allBytes(256*8), byte(i))
			c.SetDeadline(time.Now().Add(5 * time.Second))
			c.Write(request)
			c.(halfCloser).CloseWrite()
			reply, err := io.ReadAll(c)
			if err != nil || !bytes.Equal(reply, reversed(request)) {
				errs <- fmt.Errorf("stream %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestRemoteFirstCloseReachesLocalClient(t *testing.T) {
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		c, err := l.Accept()
		if err != nil {
			return
		}
		c.Write([]byte("banner"))
		c.(halfCloser).CloseWrite()
		// Keep reading: the client may still send after our FIN.
		got, _ := io.ReadAll(c)
		c.Write(nil)
		c.Close()
		_ = got
	}()
	port := freePort(t)
	fwd, _ := forwardspec.Local(fmt.Sprintf("%d:127.0.0.1:%d", port, l.Addr().(*net.TCPAddr).Port), nil)
	startForwarder(t, Config{Forwards: []forwardspec.Forward{fwd}}, Log{})
	c, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	got, err := io.ReadAll(c) // local stdin side still open
	if err != nil || string(got) != "banner" {
		t.Fatal(got, err)
	}
	if _, err := c.Write([]byte("after-fin")); err != nil {
		t.Fatal("write after remote half-close failed", err)
	}
}

func TestOpenFailureClosesLocalAndLogs(t *testing.T) {
	dead := freePort(t)
	port := freePort(t)
	fwd, _ := forwardspec.Local(fmt.Sprintf("%d:127.0.0.1:%d", port, dead), nil)
	var mu sync.Mutex
	var logged []string
	startForwarder(t, Config{Forwards: []forwardspec.Forward{fwd}}, Log{Error: func(s string) { mu.Lock(); logged = append(logged, s); mu.Unlock() }})
	c, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		t.Fatal(err)
	}
	c.SetDeadline(time.Now().Add(5 * time.Second))
	c.Write([]byte("x"))
	// Closing with unread input may surface as a reset; either way no data.
	got, err := io.ReadAll(c)
	if len(got) != 0 || (err != nil && !strings.Contains(err.Error(), "reset")) {
		t.Fatal(got, err)
	}
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(logged) != 1 || !strings.Contains(logged[0], "open failed: connect failed: Connection refused") {
		t.Fatal(logged)
	}
}

func socksDial(t *testing.T, port int, handshake []byte, wantReply []byte) net.Conn {
	t.Helper()
	c, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		t.Fatal(err)
	}
	c.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.Write(handshake); err != nil {
		t.Fatal(err)
	}
	reply := make([]byte, len(wantReply))
	if _, err := io.ReadFull(c, reply); err != nil || !bytes.Equal(reply, wantReply) {
		t.Fatalf("reply %x %v", reply, err)
	}
	return c
}

func TestSOCKSVariants(t *testing.T) {
	server4 := halfCloseServer(t, "tcp4", "127.0.0.1:0")
	p4 := uint16(server4.Addr().(*net.TCPAddr).Port)
	server6, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Skip("IPv6 loopback unavailable")
	}
	server6.Close()
	server6 = halfCloseServer(t, "tcp6", "[::1]:0")
	p6 := uint16(server6.Addr().(*net.TCPAddr).Port)
	port := freePort(t)
	fwd, _ := forwardspec.Dynamic(strconv.Itoa(port), nil)
	opened := make(chan Target, 16)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f, err := Listen(ctx, Config{Forwards: []forwardspec.Forward{fwd}}, Log{})
	if err != nil {
		t.Fatal(err)
	}
	go f.Serve(ctx, netDialer{opened: opened})
	pb := func(p uint16) []byte { return binary.BigEndian.AppendUint16(nil, p) }
	cat := func(parts ...[]byte) []byte { return bytes.Join(parts, nil) }
	s4ok := []byte{0, 90, 0, 0, 0, 0, 0, 0}
	s5ok := []byte{5, 0, 5, 0, 0, 1, 0, 0, 0, 0, 0, 0}
	for _, tc := range []struct {
		name            string
		handshake, want []byte
		target          string
	}{
		{"socks4", cat([]byte{4, 1}, pb(p4), []byte{127, 0, 0, 1}, []byte("user\x00")), s4ok, "127.0.0.1"},
		{"socks4a", cat([]byte{4, 1}, pb(p4), []byte{0, 0, 0, 1}, []byte("\x00localhost\x00")), s4ok, "localhost"},
		{"socks5-ipv4", cat([]byte{5, 2, 2, 0, 5, 1, 0, 1, 127, 0, 0, 1}, pb(p4)), s5ok, "127.0.0.1"},
		{"socks5-domain", cat([]byte{5, 1, 0, 5, 1, 0, 3, 9}, []byte("localhost"), pb(p4)), s5ok, "localhost"},
		{"socks5-ipv6", cat([]byte{5, 1, 0, 5, 1, 0, 4}, net.IPv6loopback, pb(p6)), s5ok, "::1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.target == "localhost" {
				if addrs, err := net.LookupHost("localhost"); err != nil || len(addrs) == 0 {
					t.Skip("localhost does not resolve")
				}
			}
			c := socksDial(t, port, tc.handshake, tc.want)
			defer c.Close()
			got := <-opened
			if got.Host != tc.target {
				t.Fatal(got)
			}
			request := allBytes(1000)
			reply := exchange(t, c, request)
			if tc.target == "localhost" && len(reply) == 0 {
				t.Skip("localhost resolved to an address without the fixture")
			}
			if !bytes.Equal(reply, reversed(request)) {
				t.Fatal("relay changed bytes")
			}
		})
	}
	// Pipelined data after a SOCKS4 request is relayed, not lost.
	c := socksDial(t, port, cat([]byte{4, 1}, pb(p4), []byte{127, 0, 0, 1, 0}, []byte("early")), s4ok)
	<-opened
	if reply := exchange(t, c, []byte("+late")); string(reply) != "etal+ylrae" {
		t.Fatalf("%q", reply)
	}
	c.Close()
	// Unsupported requests close without a reply, as in ssh.
	for _, bad := range [][]byte{{6}, {5, 1, 2}, cat([]byte{5, 1, 0, 5, 2, 0, 1, 127, 0, 0, 1}, pb(p4)), cat([]byte{4, 2}, pb(p4), []byte{127, 0, 0, 1, 0})} {
		c, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if err != nil {
			t.Fatal(err)
		}
		c.SetDeadline(time.Now().Add(5 * time.Second))
		c.Write(bad)
		rest, err := io.ReadAll(c)
		c.Close()
		if err != nil {
			t.Fatal(err)
		}
		if bad[0] == 5 && len(bad) > 3 {
			if !bytes.Equal(rest, []byte{5, 0}) {
				t.Fatalf("%x: %x", bad, rest)
			}
		} else if len(rest) != 0 {
			t.Fatalf("%x: unexpected reply %x", bad, rest)
		}
	}
}

func TestBindAddressPolicy(t *testing.T) {
	loop := "127.0.0.1 ::1"
	wild := "0.0.0.0 ::"
	for _, tc := range []struct {
		spec    string
		gateway bool
		want    string
	}{
		{"8080:h:1", false, loop}, {"8080:h:1", true, wild},
		{"localhost:8080:h:1", false, loop}, {"localhost:8080:h:1", true, loop},
		{"*:8080:h:1", false, wild}, {":8080:h:1", false, wild},
		{"127.0.0.1:8080:h:1", true, "127.0.0.1"}, {"[::1]:8080:h:1", false, "::1"},
		{"192.0.2.1:8080:h:1", false, "192.0.2.1"},
	} {
		f, ok := forwardspec.Local(tc.spec, nil)
		if !ok {
			t.Fatal(tc.spec)
		}
		ips, err := bindAddresses(context.Background(), f.Listen, tc.gateway)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, ip := range ips {
			got = append(got, ip.String())
		}
		if strings.Join(got, " ") != tc.want {
			t.Errorf("%s gateway=%v: %v", tc.spec, tc.gateway, got)
		}
	}
}

func TestExitOnForwardFailure(t *testing.T) {
	busy, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	port := busy.Addr().(*net.TCPAddr).Port
	bad, _ := forwardspec.Local(fmt.Sprintf("127.0.0.1:%d:h:1", port), nil)
	good, _ := forwardspec.Local(fmt.Sprintf("127.0.0.1:%d:h:1", freePort(t)), nil)
	var errorsLogged []string
	log := Log{Error: func(s string) { errorsLogged = append(errorsLogged, s) }}
	if _, err := Listen(context.Background(), Config{Forwards: []forwardspec.Forward{good, bad}, ExitOnFail: true}, log); !errors.Is(err, ErrLocalForwarding) {
		t.Fatal("ExitOnForwardFailure did not fail", err)
	}
	errorsLogged = nil
	f, err := Listen(context.Background(), Config{Forwards: []forwardspec.Forward{good, bad}}, log)
	if err != nil || len(f.Addrs()) != 1 {
		t.Fatal("partial failure must continue", err)
	}
	f.Close()
	if len(errorsLogged) != 1 || !strings.Contains(errorsLogged[0], "cannot listen to port") {
		t.Fatal(errorsLogged)
	}
	errorsLogged = nil
	f, err = Listen(context.Background(), Config{Forwards: []forwardspec.Forward{bad}}, log)
	if err != nil || len(f.Addrs()) != 0 || errorsLogged[len(errorsLogged)-1] != "Could not request local forwarding." {
		t.Fatal("total failure without ExitOnForwardFailure is an error message only", err, errorsLogged)
	}
}

func TestStdioForwarding(t *testing.T) {
	server := halfCloseServer(t, "tcp4", "127.0.0.1:0")
	target := Target{Host: "127.0.0.1", Port: server.Addr().(*net.TCPAddr).Port}
	var out bytes.Buffer
	request := allBytes(4096)
	if err := Stdio(context.Background(), netDialer{}, target, bytes.NewReader(request), &out, Log{}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out.Bytes(), reversed(request)) {
		t.Fatal("stdio relay changed bytes")
	}
	var logged string
	err := Stdio(context.Background(), netDialer{}, Target{Host: "127.0.0.1", Port: freePort(t)}, bytes.NewReader(nil), &out, Log{Error: func(s string) { logged = s }})
	if !errors.Is(err, ErrStdioForwarding) || !strings.Contains(logged, "channel 0: open failed: connect failed") {
		t.Fatal(err, logged)
	}
}
