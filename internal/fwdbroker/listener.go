package fwdbroker

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"net/netip"
	"strconv"

	"pi-sh/internal/forward"
)

// listenerProgram is the -R listener; its frame protocol is documented in
// the file and implemented on the local side by forward.Remote.
//
//go:embed listener.pl
var listenerProgram string

// perlLoader reads a length-prefixed program from stdin with sysread, so no
// input past it is buffered, and runs it with the remaining arguments. The
// listener is too large for one argv word of the adapter's open record.
const perlLoader = `my($n,$p)=("","");` +
	`while(length$n<8){sysread(STDIN,$n,8-length$n,length$n)or exit 2}` +
	`while(length$p<$n){sysread(STDIN,$p,$n-length$p,length$p)or exit 2}` +
	`eval$p;exit 3`

// ErrNoListener reports a device without the Perl that -R needs.
var ErrNoListener = errors.New("remote forwarding needs Perl (perl-base) on the device; pi-sh does not install software")

// CanListen reports whether -R listeners can run on the device.
func (m *Mux) CanListen() bool {
	_, ok := Select(map[string][]string{"perl": m.Detected["perl"]})
	return ok
}

// Listen starts a remote listener. host follows sshd's GatewayPorts
// clientspecified rules: "localhost" binds the loopback addresses, "*" every
// address, anything else that address. The returned stream carries the
// listener's frames (forward.Remote).
func (m *Mux) Listen(ctx context.Context, host string, port int) (forward.Stream, error) {
	if !m.CanListen() {
		return nil, ErrNoListener
	}
	if port < 0 || port > 65535 {
		return nil, errors.New("invalid remote listen port")
	}
	if host != "localhost" && host != "*" {
		if err := checkHostPort(forward.Target{Host: host, Port: 1}); err != nil {
			return nil, err
		}
		if a, err := netip.ParseAddr(host); err == nil && a.Zone() != "" {
			return nil, errors.New("scoped IPv6 listen addresses are not supported")
		}
	}
	s, err := m.start(ctx, []string{"perl", "-e", perlLoader, "--", host, strconv.Itoa(port)}, "")
	if err != nil {
		return nil, err
	}
	if _, err := s.Write([]byte(fmt.Sprintf("%08d", len(listenerProgram)) + listenerProgram)); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}
