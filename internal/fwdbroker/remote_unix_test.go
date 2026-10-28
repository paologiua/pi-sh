//go:build unix

package fwdbroker

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"pi-sh/internal/forward"
	"pi-sh/internal/forwardspec"
)

type logLines struct {
	mu    sync.Mutex
	lines []string
}

func (l *logLines) add(level string) func(string) {
	return func(s string) {
		l.mu.Lock()
		l.lines = append(l.lines, level+": "+s)
		l.mu.Unlock()
	}
}

func (l *logLines) log() forward.Log {
	return forward.Log{Error: l.add("error"), Info: l.add("info"), Debug: l.add("debug")}
}

func (l *logLines) has(text string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, line := range l.lines {
		if strings.Contains(line, text) {
			return true
		}
	}
	return false
}

func (l *logLines) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.lines, "\n")
}

// allocatedPort reads ssh's "Allocated port N" line.
func (l *logLines) allocatedPort(t *testing.T) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, line := range l.lines {
		if rest, ok := strings.CutPrefix(line, "info: Allocated port "); ok {
			port, err := strconv.Atoi(strings.Fields(rest)[0])
			if err == nil {
				return port
			}
		}
	}
	t.Fatalf("no allocated port in\n%s", l.lines)
	return 0
}

func remoteForward(t *testing.T, spec string) forwardspec.Forward {
	t.Helper()
	f, ok := forwardspec.Remote(spec, nil)
	if !ok {
		t.Fatal("bad spec", spec)
	}
	return f
}

// serveRemote starts -R forwardings over a Perl-capable adapter and serves
// them until the test ends.
func serveRemote(t *testing.T, m *Mux, cfg forward.RemoteConfig, logs *logLines) (*forward.Remote, error) {
	t.Helper()
	r, err := forward.ListenRemote(context.Background(), m, cfg, logs.log())
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.Serve(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	return r, nil
}

func exchange(t *testing.T, address string, request []byte) []byte {
	t.Helper()
	c, err := net.DialTimeout("tcp", address, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(30 * time.Second))
	go func() {
		c.Write(request)
		c.(*net.TCPConn).CloseWrite()
	}()
	got, err := io.ReadAll(c)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestRemoteForwardingHalfCloseConcurrencyAndAllocation(t *testing.T) {
	m, err := startMux(t, "perl")
	if err != nil {
		t.Fatal(err)
	}
	target := reverseServer(t, "tcp4", "127.0.0.1:0")
	tp := target.Addr().(*net.TCPAddr).Port
	logs := &logLines{}
	if _, err := serveRemote(t, m, forward.RemoteConfig{Forwards: []forwardspec.Forward{remoteForward(t, fmt.Sprintf("0:127.0.0.1:%d", tp))}}, logs); err != nil {
		t.Fatal(err)
	}
	port := logs.allocatedPort(t)
	if !logs.has(fmt.Sprintf("Allocated port %d for remote forward to 127.0.0.1:%d", port, tp)) ||
		!logs.has("remote forward success for: listen 0, connect 127.0.0.1:") {
		t.Fatal(logs)
	}
	// Loopback only by default: both families when available, never a wildcard.
	if c, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), 5*time.Second); err != nil {
		t.Fatal(err)
	} else {
		c.Close()
	}
	request := make([]byte, 200_000)
	for i := range request {
		request[i] = byte(i * 7)
	}
	address := "127.0.0.1:" + strconv.Itoa(port)
	if got := exchange(t, address, request); !bytes.Equal(got, reversed(request)) {
		t.Fatalf("binary half-close: %d bytes", len(got))
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r := bytes.Repeat([]byte{byte(i)}, 3000+i*1111)
			if got := exchange(t, address, r); !bytes.Equal(got, reversed(r)) {
				t.Errorf("stream %d: %d bytes", i, len(got))
			}
		}(i)
	}
	wg.Wait()
	if !logs.has("client_request_forwarded_tcpip: listen localhost port " + strconv.Itoa(port) + ", originator 127.0.0.1 port ") {
		t.Fatal(logs)
	}
}

// A -R connection is listed by ~# under its originator address, as ssh names
// a forwarded-tcpip channel; it leaves the table when it ends.
func TestRemoteForwardingListsChannels(t *testing.T) {
	m, err := startMux(t, "perl")
	if err != nil {
		t.Fatal(err)
	}
	target := reverseServer(t, "tcp4", "127.0.0.1:0")
	logs := &logLines{}
	chans := forward.NewChannels()
	chans.Open("client-session (Connect shell channel)")
	cfg := forward.RemoteConfig{Forwards: []forwardspec.Forward{remoteForward(t, fmt.Sprintf("0:127.0.0.1:%d", target.Addr().(*net.TCPAddr).Port))}, Channels: chans}
	if _, err := serveRemote(t, m, cfg, logs); err != nil {
		t.Fatal(err)
	}
	c, err := net.Dial("tcp4", "127.0.0.1:"+strconv.Itoa(logs.allocatedPort(t)))
	if err != nil {
		t.Fatal(err)
	}
	want := "  #0 client-session (Connect shell channel)\n  #1 127.0.0.1 (Connect forwarding carrier)"
	for deadline := time.Now().Add(10 * time.Second); strings.Join(chans.List(), "\n") != want; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("%q", chans.List())
		}
	}
	c.Close()
	for deadline := time.Now().Add(10 * time.Second); len(chans.List()) != 1; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("not released: %q", chans.List())
		}
	}
}

func TestRemoteDynamicForwarding(t *testing.T) {
	m, err := startMux(t, "perl")
	if err != nil {
		t.Fatal(err)
	}
	target := reverseServer(t, "tcp4", "127.0.0.1:0")
	tp := target.Addr().(*net.TCPAddr).Port
	closed, _ := net.Listen("tcp4", "127.0.0.1:0")
	cp := closed.Addr().(*net.TCPAddr).Port
	closed.Close()
	logs := &logLines{}
	cfg := forward.RemoteConfig{
		Forwards:   []forwardspec.Forward{remoteForward(t, "0")},
		PermitOpen: []string{"127.0.0.1:" + strconv.Itoa(tp), "127.0.0.1:" + strconv.Itoa(cp)},
	}
	if _, err := serveRemote(t, m, cfg, logs); err != nil {
		t.Fatal(err)
	}
	address := "127.0.0.1:" + strconv.Itoa(logs.allocatedPort(t))
	socks := func(port int) (reply []byte, conn net.Conn) {
		c, err := net.DialTimeout("tcp", address, 5*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		c.SetDeadline(time.Now().Add(20 * time.Second))
		c.Write([]byte{5, 1, 0})
		method := make([]byte, 2)
		if _, err := io.ReadFull(c, method); err != nil || method[1] != 0 {
			t.Fatal(method, err)
		}
		req := append([]byte{5, 1, 0, 1, 127, 0, 0, 1}, 0, 0)
		binary.BigEndian.PutUint16(req[8:], uint16(port))
		c.Write(req)
		reply, _ = io.ReadAll(io.LimitReader(c, 10))
		return reply, c
	}
	// Connected: the success reply comes only after the local connect.
	reply, c := socks(tp)
	if len(reply) != 10 || reply[1] != 0 {
		t.Fatal(reply)
	}
	c.Write([]byte("dyn"))
	c.(*net.TCPConn).CloseWrite()
	if got, _ := io.ReadAll(c); string(got) != "nyd" {
		t.Fatalf("%q", got)
	}
	c.Close()
	// Refused locally: closed without a reply, ssh's error line.
	if reply, c := socks(cp); len(reply) != 0 {
		t.Fatal(reply)
	} else {
		c.Close()
	}
	if !logs.has(fmt.Sprintf("error: connect_to 127.0.0.1 port %d: failed.", cp)) {
		t.Fatal(logs)
	}
	// Not in PermitRemoteOpen: closed without a reply.
	if reply, c := socks(9); len(reply) != 0 {
		t.Fatal(reply)
	} else {
		c.Close()
	}
	if !logs.has("rdynamic_connect_finish: requested forward not permitted") {
		t.Fatal(logs)
	}
}

func TestRemoteForwardingFailures(t *testing.T) {
	m, err := startMux(t, "perl")
	if err != nil {
		t.Fatal(err)
	}
	busy, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	bp := busy.Addr().(*net.TCPAddr).Port
	busySpec := remoteForward(t, fmt.Sprintf("127.0.0.1:%d:127.0.0.1:1", bp))
	logs := &logLines{}
	r, err := forward.ListenRemote(context.Background(), m, forward.RemoteConfig{Forwards: []forwardspec.Forward{busySpec}}, logs.log())
	if err != nil || !logs.has(fmt.Sprintf("info: Warning: remote port forwarding failed for listen port %d", bp)) {
		t.Fatal(err, logs)
	}
	r.Close()
	_, err = forward.ListenRemote(context.Background(), m, forward.RemoteConfig{Forwards: []forwardspec.Forward{busySpec}, ExitOnFail: true}, logs.log())
	var rf *forward.RemoteForwardError
	if !errors.As(err, &rf) || err.Error() != fmt.Sprintf("Error: remote port forwarding failed for listen port %d", bp) {
		t.Fatal(err)
	}
	// Privileged ports are refused to non-root users, as by sshd.
	if !runningAsRoot() {
		logs := &logLines{}
		if _, err := forward.ListenRemote(context.Background(), m, forward.RemoteConfig{Forwards: []forwardspec.Forward{remoteForward(t, "80:127.0.0.1:1")}}, logs.log()); err != nil || !logs.has("failed for listen port 80") {
			t.Fatal(err, logs)
		}
	}
	// The adapter stays usable and no listener remains.
	if got, err := roundTrip(context.Background(), m, tcpTarget(reverseServer(t, "tcp4", "127.0.0.1:0")), []byte("ok")); err != nil || string(got) != "ko" {
		t.Fatal(got, err)
	}
}

func TestRemoteForwardingNeedsPerl(t *testing.T) {
	m, err := startMux(t, "nc")
	if err != nil {
		t.Fatal(err)
	}
	logs := &logLines{}
	_, err = forward.ListenRemote(context.Background(), m, forward.RemoteConfig{Forwards: []forwardspec.Forward{remoteForward(t, "0:127.0.0.1:1")}, ExitOnFail: true}, logs.log())
	if err == nil || !logs.has("needs Perl") {
		t.Fatal(err, logs)
	}
}

// Closing the remote forwarding ends its listener on the device side.
func TestRemoteForwardingCloseReleasesPort(t *testing.T) {
	m, err := startMux(t, "perl")
	if err != nil {
		t.Fatal(err)
	}
	logs := &logLines{}
	r, err := forward.ListenRemote(context.Background(), m, forward.RemoteConfig{Forwards: []forwardspec.Forward{remoteForward(t, "0:127.0.0.1:1")}}, logs.log())
	if err != nil {
		t.Fatal(err)
	}
	port := logs.allocatedPort(t)
	r.Close()
	deadline := time.Now().Add(10 * time.Second)
	for {
		l, err := net.Listen("tcp4", "127.0.0.1:"+strconv.Itoa(port))
		if err == nil {
			l.Close()
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("remote listener still bound after Close")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func runningAsRoot() bool { return os.Geteuid() == 0 }

// Bind addresses follow sshd's GatewayPorts clientspecified: no host is the
// loopback addresses, "*" every address, an address only that address.
func TestRemoteForwardingBindAddresses(t *testing.T) {
	m, err := startMux(t, "perl")
	if err != nil {
		t.Fatal(err)
	}
	var external string
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil && !n.IP.IsLoopback() && !n.IP.IsLinkLocalUnicast() {
			external = n.IP.String()
			break
		}
	}
	reachable := func(host string, port int) bool {
		c, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), 2*time.Second)
		if err == nil {
			c.Close()
		}
		return err == nil
	}
	ipv6 := false
	if l, err := net.Listen("tcp6", "[::1]:0"); err == nil {
		l.Close()
		ipv6 = true
	}
	for _, tc := range []struct {
		spec              string
		loop4, loop6, ext bool
	}{
		{"0:127.0.0.1:1", true, true, false},
		{"*:0:127.0.0.1:1", true, true, true},
		{"127.0.0.1:0:127.0.0.1:1", true, false, false},
	} {
		logs := &logLines{}
		r, err := forward.ListenRemote(context.Background(), m, forward.RemoteConfig{Forwards: []forwardspec.Forward{remoteForward(t, tc.spec)}}, logs.log())
		if err != nil {
			t.Fatal(tc.spec, err)
		}
		port := logs.allocatedPort(t)
		if reachable("127.0.0.1", port) != tc.loop4 {
			t.Errorf("%s: 127.0.0.1", tc.spec)
		}
		if ipv6 && reachable("::1", port) != tc.loop6 {
			t.Errorf("%s: ::1", tc.spec)
		}
		if external != "" && reachable(external, port) != tc.ext {
			t.Errorf("%s: %s", tc.spec, external)
		}
		r.Close()
	}
}
