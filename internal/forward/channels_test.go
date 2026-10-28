package forward

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"pi-sh/internal/forwardspec"
)

// holdServer accepts connections and keeps them open until the test ends.
func holdServer(t *testing.T, network, address string) net.Listener {
	l, err := net.Listen(network, address)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		var held []net.Conn
		defer func() {
			for _, c := range held {
				c.Close()
			}
		}()
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			held = append(held, c)
		}
	}()
	return l
}

func waitChannels(t *testing.T, c *Channels, n int) []string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		list := c.List()
		if len(list) == n {
			return list
		}
		if time.Now().After(deadline) {
			t.Fatalf("want %d channels, have %q", n, list)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func originPort(c net.Conn) int { return c.LocalAddr().(*net.TCPAddr).Port }

// The ~# list as OpenSSH 10.2p1 printed it for `-D 127.0.0.1:P -L Q:h:p`:
// listening sockets take ids 0..2 unlisted, the session #3, a SOCKS
// connection without a request "dynamic-tcpip", a -L connection ssh's
// direct-tcpip description; a freed id is reused first.
func TestChannelsNumberAndDescribeLikeSSH(t *testing.T) {
	if l, err := net.Listen("tcp6", "[::1]:0"); err != nil {
		t.Skip("IPv6 loopback unavailable; localhost binds one socket")
	} else {
		l.Close()
	}
	target := holdServer(t, "tcp4", "127.0.0.1:0")
	tp := target.Addr().(*net.TCPAddr).Port
	dp, lp := freePort(t), freePort(t)
	dyn, _ := forwardspec.Dynamic("127.0.0.1:"+strconv.Itoa(dp), nil)
	loc, ok := forwardspec.Local(fmt.Sprintf("%d:127.0.0.1:%d", lp, tp), nil)
	if !ok {
		t.Fatal("spec")
	}
	chans := NewChannels()
	f, _ := startForwarder(t, Config{Forwards: []forwardspec.Forward{dyn, loc}, Channels: chans}, Log{})
	if n := len(f.Addrs()); n != 3 {
		t.Fatalf("%d listening sockets", n)
	}
	if id := chans.Open("client-session (Connect shell channel)"); id != 3 {
		t.Fatalf("session id %d", id)
	}
	pending, err := net.Dial("tcp4", "127.0.0.1:"+strconv.Itoa(dp))
	if err != nil {
		t.Fatal(err)
	}
	defer pending.Close()
	waitChannels(t, chans, 2)
	plain, err := net.Dial("tcp4", "127.0.0.1:"+strconv.Itoa(lp))
	if err != nil {
		t.Fatal(err)
	}
	defer plain.Close()
	want := []string{
		"  #3 client-session (Connect shell channel)",
		"  #4 dynamic-tcpip (Connect forwarding carrier)",
		fmt.Sprintf("  #5 direct-tcpip: listening port %d for 127.0.0.1 port %d, connect from 127.0.0.1 port %d to 127.0.0.1 port %d (Connect forwarding carrier)", lp, tp, originPort(plain), lp),
	}
	if got := waitChannels(t, chans, 3); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	// After its SOCKS request the connection is described like -L.
	pending.Write([]byte{5, 1, 0})
	reply := make([]byte, 2)
	if _, err := pending.Read(reply); err != nil {
		t.Fatal(err)
	}
	pending.Write([]byte{5, 1, 0, 1, 127, 0, 0, 1, byte(tp >> 8), byte(tp)})
	described := fmt.Sprintf("  #4 direct-tcpip: listening port %d for 127.0.0.1 port %d, connect from 127.0.0.1 port %d to 127.0.0.1 port %d (Connect forwarding carrier)", dp, tp, originPort(pending), dp)
	deadline := time.Now().Add(5 * time.Second)
	for chans.List()[1] != described {
		if time.Now().After(deadline) {
			t.Fatalf("%q", chans.List())
		}
		time.Sleep(5 * time.Millisecond)
	}
	pending.Close()
	waitChannels(t, chans, 2)
	again, err := net.Dial("tcp4", "127.0.0.1:"+strconv.Itoa(lp))
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	if got := waitChannels(t, chans, 3); !strings.HasPrefix(got[1], "  #4 direct-tcpip: ") {
		t.Fatalf("freed id not reused: %q", got)
	}
}

// Closing the forwarder frees the listeners' ids.
func TestChannelsReleaseListeners(t *testing.T) {
	loc, _ := forwardspec.Local(fmt.Sprintf("127.0.0.1:%d:127.0.0.1:1", freePort(t)), nil)
	chans := NewChannels()
	f, err := Listen(context.Background(), Config{Forwards: []forwardspec.Forward{loc}, Channels: chans}, Log{})
	if err != nil {
		t.Fatal(err)
	}
	if id := chans.Reserve(); id != 1 {
		t.Fatal(id)
	}
	f.Close()
	f.Close()
	if id := chans.Reserve(); id != 0 {
		t.Fatal(id)
	}
	var none *Channels
	if none.Open("x") != -1 || none.List() != nil {
		t.Fatal("nil table")
	}
}

// A -R peer from the device is listed by ~# only as an address and a port:
// anything else (here a terminal escape sequence) is a protocol error.
func TestParsePeerRejectsDeviceText(t *testing.T) {
	for in, want := range map[string]string{"127.0.0.1 5000": "127.0.0.1 5000", "::1 1": "::1 1", "::ffff:7f00:1 2": "::ffff:127.0.0.1 2"} {
		if got, ok := parsePeer(in); !ok || got != want {
			t.Errorf("%q: %q %v", in, got, ok)
		}
	}
	for _, in := range []string{"\x1b]52;c;AAAA\a 1", "127.0.0.1", "127.0.0.1 65536", "127.0.0.1 01", "host 22", "127.0.0.1 2 3"} {
		if got, ok := parsePeer(in); ok {
			t.Errorf("%q accepted as %q", in, got)
		}
	}
}
