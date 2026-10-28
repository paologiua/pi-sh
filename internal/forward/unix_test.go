//go:build unix

package forward

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"pi-sh/internal/forwardspec"
)

func TestUnixListenerMaskUnlinkAndRemotePath(t *testing.T) {
	dir, err := os.MkdirTemp("", "pf")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	remote := filepath.Join(dir, "r")
	halfCloseServer(t, "unix", remote)
	local := filepath.Join(dir, "l")
	fwd, ok := forwardspec.Local(local+":"+remote, nil)
	if !ok {
		t.Fatal("unix spec")
	}
	cfg := Config{Forwards: []forwardspec.Forward{fwd}, BindMask: 0o077}
	f, cancel := startForwarder(t, cfg, Log{})
	info, err := os.Stat(local)
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatal("StreamLocalBindMask not applied", info.Mode(), err)
	}
	c, err := net.Dial("unix", local)
	if err != nil {
		t.Fatal(err)
	}
	if reply := exchange(t, c, []byte("abc")); !bytes.Equal(reply, []byte("cba")) {
		t.Fatalf("%q", reply)
	}
	c.Close()
	cancel()
	f.Close()
	// ssh leaves the socket path behind; reuse needs StreamLocalBindUnlink.
	if _, err := os.Lstat(local); err != nil {
		t.Fatal("socket path removed on close", err)
	}
	if _, err := Listen(context.Background(), Config{Forwards: []forwardspec.Forward{fwd}, BindMask: 0o177, ExitOnFail: true}, Log{}); err == nil {
		t.Fatal("stale socket reused without StreamLocalBindUnlink")
	}
	g, err := Listen(context.Background(), Config{Forwards: []forwardspec.Forward{fwd}, BindMask: 0o177, BindUnlink: true, ExitOnFail: true}, Log{})
	if err != nil {
		t.Fatal("StreamLocalBindUnlink did not replace the stale socket", err)
	}
	g.Close()
}

// ssh's descriptions with Unix-domain sockets (OpenSSH 10.2p1): a local socket
// listener has port -2 and an empty origin; a socket target is
// direct-streamlocal@openssh.com with port -2.
func TestChannelsDescribeUnixSockets(t *testing.T) {
	dir, err := os.MkdirTemp("", "pf")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	target := holdServer(t, "tcp4", "127.0.0.1:0")
	tp := target.Addr().(*net.TCPAddr).Port
	remote := filepath.Join(dir, "r")
	holdServer(t, "unix", remote)
	local := filepath.Join(dir, "l")
	lp := freePort(t)
	a, _ := forwardspec.Local(fmt.Sprintf("%s:127.0.0.1:%d", local, tp), nil)
	b, _ := forwardspec.Local(fmt.Sprintf("127.0.0.1:%d:%s", lp, remote), nil)
	chans := NewChannels()
	startForwarder(t, Config{Forwards: []forwardspec.Forward{a, b}, Channels: chans}, Log{})
	c1, err := net.Dial("unix", local)
	if err != nil {
		t.Fatal(err)
	}
	defer c1.Close()
	waitChannels(t, chans, 1)
	c2, err := net.Dial("tcp4", "127.0.0.1:"+strconv.Itoa(lp))
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()
	got := waitChannels(t, chans, 2)
	want := []string{
		fmt.Sprintf("  #2 direct-tcpip: listening port -2 for 127.0.0.1 port %d, connect from  port 0 to %s port 0 (Connect forwarding carrier)", tp, local),
		fmt.Sprintf("  #3 direct-streamlocal@openssh.com: listening port %d for %s port -2, connect from 127.0.0.1 port %d to 127.0.0.1 port %d (Connect forwarding carrier)", lp, remote, originPort(c2), lp),
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
