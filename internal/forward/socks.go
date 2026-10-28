package forward

import (
	"bufio"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"time"
)

// socksTimeout bounds a stalled handshake; ssh has no explicit bound but
// tolerates partial requests indefinitely. This is a documented difference.
const socksTimeout = 2 * time.Minute

const maxSocksString = 1024

// socksHandshake implements OpenSSH's channel_decode_socks4/5 for one client:
// CONNECT only, no authentication, and an immediate success reply sent before
// the remote connection opens (a later remote failure closes the connection,
// exactly as ssh behaves). Unsupported requests close without a reply.
func socksHandshake(conn net.Conn) (Target, net.Conn, error) {
	version, t, wrapped, err := socksRequest(conn)
	if err == nil {
		err = socksReply(conn, version)
	}
	if err != nil {
		return Target{}, nil, err
	}
	return t, wrapped, nil
}

// socksRequest reads one CONNECT request without the final reply, which a
// remote dynamic forwarding sends only once connected. Data pipelined after
// the request stays readable through the returned connection.
func socksRequest(conn net.Conn) (byte, Target, net.Conn, error) {
	conn.SetDeadline(time.Now().Add(socksTimeout))
	defer conn.SetDeadline(time.Time{})
	r := bufio.NewReaderSize(conn, 512)
	version, err := r.ReadByte()
	if err != nil {
		return 0, Target{}, nil, err
	}
	var t Target
	switch version {
	case 4:
		t, err = socks4(r)
	case 5:
		t, err = socks5(r, conn)
	default:
		return 0, Target{}, nil, errors.New("unknown socks version")
	}
	if err != nil {
		return 0, Target{}, nil, err
	}
	return version, t, bufferedConn{conn, r}, nil
}

// socksReply sends the success reply ssh sends for each version.
func socksReply(conn net.Conn, version byte) error {
	reply := []byte{0, 90, 0, 0, 0, 0, 0, 0}
	if version == 5 {
		reply = []byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0}
	}
	_, err := conn.Write(reply)
	return err
}

// bufferedConn reads handshake leftovers first and keeps half-close.
type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (c bufferedConn) Read(b []byte) (int, error) { return c.r.Read(b) }
func (c bufferedConn) CloseWrite() error {
	if h, ok := c.Conn.(halfCloser); ok {
		return h.CloseWrite()
	}
	return nil
}

func readString(r *bufio.Reader) (string, error) {
	var out []byte
	for {
		b, err := r.ReadByte()
		if err != nil {
			return "", err
		}
		if b == 0 {
			return string(out), nil
		}
		if len(out) >= maxSocksString {
			return "", errors.New("SOCKS string too long")
		}
		out = append(out, b)
	}
}

func socks4(r *bufio.Reader) (Target, error) {
	var head [7]byte // command, port, IPv4
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return Target{}, err
	}
	if head[0] != 1 {
		return Target{}, errors.New("SOCKS4 command not supported")
	}
	if _, err := readString(r); err != nil { // user id, ignored as by ssh
		return Target{}, err
	}
	port := int(binary.BigEndian.Uint16(head[1:3]))
	ip := net.IPv4(head[3], head[4], head[5], head[6])
	host := ip.String()
	if head[3] == 0 && head[4] == 0 && head[5] == 0 && head[6] != 0 {
		name, err := readString(r) // SOCKS4A
		if err != nil {
			return Target{}, err
		}
		host = name
	}
	return Target{Host: host, Port: port}, nil
}

func socks5(r *bufio.Reader, w io.Writer) (Target, error) {
	n, err := r.ReadByte()
	if err != nil {
		return Target{}, err
	}
	methods := make([]byte, n)
	if _, err := io.ReadFull(r, methods); err != nil {
		return Target{}, err
	}
	found := false
	for _, m := range methods {
		found = found || m == 0
	}
	if !found {
		return Target{}, errors.New("SOCKS5 no-authentication method not offered")
	}
	if _, err := w.Write([]byte{5, 0}); err != nil {
		return Target{}, err
	}
	var head [4]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return Target{}, err
	}
	if head[0] != 5 || head[1] != 1 || head[2] != 0 {
		return Target{}, errors.New("SOCKS5 request not supported")
	}
	var host string
	switch head[3] {
	case 1:
		var a [4]byte
		if _, err := io.ReadFull(r, a[:]); err != nil {
			return Target{}, err
		}
		host = net.IP(a[:]).String()
	case 3:
		l, err := r.ReadByte()
		if err != nil {
			return Target{}, err
		}
		name := make([]byte, l)
		if _, err := io.ReadFull(r, name); err != nil {
			return Target{}, err
		}
		host = string(name)
	case 4:
		var a [16]byte
		if _, err := io.ReadFull(r, a[:]); err != nil {
			return Target{}, err
		}
		host = net.IP(a[:]).String()
	default:
		return Target{}, errors.New("SOCKS5 address type not supported")
	}
	var port [2]byte
	if _, err := io.ReadFull(r, port[:]); err != nil {
		return Target{}, err
	}
	return Target{Host: host, Port: int(binary.BigEndian.Uint16(port[:]))}, nil
}
