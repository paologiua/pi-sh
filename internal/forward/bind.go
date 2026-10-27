package forward

import (
	"context"
	"errors"
	"io/fs"
	"net"
	"os"
	"strconv"

	"pi-sh/internal/forwardspec"
)

// bindAddresses is channel_fwd_bind_addr for a client (is_client=1) followed
// by the getaddrinfo selection of channel_setup_fwd_listener_tcpip: nil host
// and no wildcard means both loopback addresses, wildcard means both
// unspecified addresses, otherwise the named/literal address is resolved.
func bindAddresses(ctx context.Context, e forwardspec.Endpoint, gateway bool) ([]net.IP, error) {
	wildcard := false
	host := ""
	useHost := false
	switch {
	case !e.HasHost:
		wildcard = gateway
	case e.Host == "" || e.Host == "*":
		wildcard = true
	case e.Host != "localhost":
		host, useHost = e.Host, true
	}
	if !useHost {
		if wildcard {
			return []net.IP{net.IPv4zero, net.IPv6unspecified}, nil
		}
		return []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback}, nil
	}
	if ip := net.ParseIP(host); ip != nil {
		return []net.IP{ip}, nil
	}
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	var ips []net.IP
	for _, a := range addrs {
		if a.Zone == "" {
			ips = append(ips, a.IP)
		}
	}
	return ips, nil
}

func bind(ctx context.Context, fwd forwardspec.Forward, cfg Config, log Log) []net.Listener {
	if fwd.Listen.Port == forwardspec.PortStreamLocal {
		l, err := listenUnix(fwd.Listen.Path, cfg.BindUnlink, cfg.BindMask)
		if err != nil {
			log.error("%v", err)
			return nil
		}
		return []net.Listener{l}
	}
	ips, err := bindAddresses(ctx, fwd.Listen, cfg.GatewayPorts)
	if err != nil {
		log.error("getaddrinfo(%.64s): %v", fwd.Listen.Host, err)
		return nil
	}
	var out []net.Listener
	port := strconv.Itoa(fwd.Listen.Port)
	for _, ip := range ips {
		network := "tcp4"
		if ip.To4() == nil {
			network = "tcp6" // Go sets IPV6_V6ONLY for tcp6, as ssh does
		}
		address := net.JoinHostPort(ip.String(), port)
		l, err := (&net.ListenConfig{}).Listen(ctx, network, address)
		if err != nil {
			// The kernel may lack an address family; ssh logs this at verbose.
			log.verbose("bind [%s]:%s: %v", ip, port, bindCause(err))
			continue
		}
		log.debug("Local forwarding listening on %s port %s.", ip, port)
		out = append(out, l)
	}
	if len(out) == 0 {
		log.error("channel_setup_fwd_listener_tcpip: cannot listen to port: %d", fwd.Listen.Port)
	}
	return out
}

func bindCause(err error) error {
	var op *net.OpError
	if errors.As(err, &op) {
		var sys *os.SyscallError
		if errors.As(op.Err, &sys) {
			return sys.Err
		}
		return op.Err
	}
	return err
}

// listenUnix follows unix_listener: optional unlink of an existing path, bind
// under StreamLocalBindMask, listen. The socket file is left in place when the
// listener closes, as ssh leaves it (StreamLocalBindUnlink handles reuse).
func listenUnix(path string, unlink bool, mask uint32) (net.Listener, error) {
	if unlink {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, errors.New("unlink(" + path + "): " + bindCause(err).Error())
		}
	}
	l, err := listenUnixMasked(path, mask)
	if err != nil {
		return nil, errors.New("bind: " + bindCause(err).Error() + "; cannot listen to path: " + path)
	}
	l.SetUnlinkOnClose(false)
	return l, nil
}
