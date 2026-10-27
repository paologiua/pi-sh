//go:build unix

package forward

import (
	"net"
	"sync"
	"syscall"
)

var umaskMu sync.Mutex

// listenUnixMasked binds with the process umask temporarily set to mask, as
// ssh does. Forwardings are bound during setup, before session I/O starts.
func listenUnixMasked(path string, mask uint32) (*net.UnixListener, error) {
	umaskMu.Lock()
	defer umaskMu.Unlock()
	old := syscall.Umask(int(mask))
	defer syscall.Umask(old)
	return net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
}
