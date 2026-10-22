//go:build unix

package forward

import (
	"bytes"
	"context"
	"net"
	"os"
	"path/filepath"
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
