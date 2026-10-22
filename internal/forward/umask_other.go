//go:build !unix

package forward

import (
	"errors"
	"net"
)

// Windows has no umask; a non-default StreamLocalBindMask cannot be honoured
// and is refused rather than silently ignored.
func listenUnixMasked(path string, mask uint32) (*net.UnixListener, error) {
	if mask != 0o177 {
		return nil, errors.New("StreamLocalBindMask is not supported on this platform")
	}
	return net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
}
