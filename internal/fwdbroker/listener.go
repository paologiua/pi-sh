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

// perlLoader decodes the adapter's hex input lines (hin appends the bytes to
// $p), reads a length-prefixed program and runs it with the remaining
// arguments; the program keeps using hin and $p for later input. The
// listener is too large for one argv word of the adapter's open record.
const perlLoader = `my($l,$p)=("","");` +
	`sub hin{my $n=sysread(STDIN,$l,65536,length$l);$n or return $n;my $k=rindex($l,"\n")+1;` +
	`if($k){(my $h=substr($l,0,$k,""))=~tr/\n//d;$p.=pack("H*",$h)}1}` +
	`while(length$p<8||length$p<8+substr($p,0,8)){hin()or exit 2}` +
	`my $n=substr($p,0,8,"");eval substr($p,0,$n,"");exit 3`

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
	s, err := m.start(ctx, []string{"perl", "-e", perlLoader, "--", host, strconv.Itoa(port)}, "", true)
	if err != nil {
		return nil, err
	}
	if _, err := s.Write([]byte(fmt.Sprintf("%08d", len(listenerProgram)) + listenerProgram)); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}
