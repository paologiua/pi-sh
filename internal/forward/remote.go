package forward

import (
	"bufio"
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
	"time"

	"pi-sh/internal/forwardspec"
)

// Listener starts a remote listener whose stream carries the frames described
// in fwdbroker/listener.pl: pack("aNN", type, id, length) . data.
type Listener interface {
	Listen(ctx context.Context, host string, port int) (Stream, error)
}

// RemoteConfig selects -R forwardings.
type RemoteConfig struct {
	Forwards   []forwardspec.Forward
	ExitOnFail bool
	// PermitOpen is PermitRemoteOpen; empty or "any" allows every
	// destination of a remote dynamic forwarding.
	PermitOpen []string
	// Channels, when set, numbers accepted connections for ~#.
	Channels *Channels
}

const (
	// remoteWindow must equal $W in listener.pl.
	remoteWindow   = 65536
	remoteMaxFrame = 16384
	remoteSetup    = 60 * time.Second
)

// RemoteForwardError is ssh's "remote port forwarding failed" result;
// with ExitOnForwardFailure it is fatal (exit 255).
type RemoteForwardError struct{ Port int }

func (e *RemoteForwardError) Error() string {
	return fmt.Sprintf("Error: remote port forwarding failed for listen port %d", e.Port)
}

// Remote owns the session's remote listeners.
type Remote struct {
	log       Log
	listeners []*remoteListener
	wg        sync.WaitGroup
}

// ListenRemote requests every -R forwarding in order, logging as ssh does
// when each reply arrives. A failure is a warning, or fatal with ExitOnFail.
func ListenRemote(ctx context.Context, lst Listener, cfg RemoteConfig, log Log) (*Remote, error) {
	r := &Remote{log: log}
	for _, fwd := range cfg.Forwards {
		listenHost := "LOCALHOST"
		if fwd.Listen.HasHost {
			listenHost = fwd.Listen.Host
		}
		log.debug("Remote connections from %s:%d forwarded to local address %s", listenHost, fwd.Listen.Port, connectString(fwd.Connect))
		l, err := startRemote(ctx, lst, fwd, cfg.PermitOpen, cfg.Channels, log)
		shown := ""
		if fwd.Listen.HasHost {
			shown = fwd.Listen.Host + ":"
		}
		if err != nil {
			log.debug("remote forward failure for: listen %s%d, connect %s", shown, fwd.Listen.Port, connectString(fwd.Connect))
			log.debug("remote listener: %v", err)
			if errors.Is(err, errNoRemoteListener) {
				log.error("%v", err)
			}
			if cfg.ExitOnFail {
				r.Close()
				return nil, &RemoteForwardError{Port: fwd.Listen.Port}
			}
			log.info("Warning: remote port forwarding failed for listen port %d", fwd.Listen.Port)
			continue
		}
		log.debug("remote forward success for: listen %s%d, connect %s", shown, fwd.Listen.Port, connectString(fwd.Connect))
		if fwd.Listen.Port == 0 {
			log.info("Allocated port %d for remote forward to %s", l.port, connectString(fwd.Connect))
		}
		r.listeners = append(r.listeners, l)
	}
	return r, nil
}

// errNoRemoteListener wraps a Listener refusal that applies to every
// forwarding (no Perl on the device).
var errNoRemoteListener = errors.New("remote listener unavailable")

// connectString is ssh's "host:port" for a forwarding's connect side, with
// "socks:0" for dynamic and "path:-2" for a Unix socket.
func connectString(e forwardspec.Endpoint) string {
	if e.Port == forwardspec.PortStreamLocal {
		return e.Path + ":" + strconv.Itoa(e.Port)
	}
	return e.Host + ":" + strconv.Itoa(e.Port)
}

// Serve handles remote connections until ctx ends, then closes the
// listeners and waits for every handler.
func (r *Remote) Serve(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	for _, l := range r.listeners {
		r.wg.Add(1)
		go func(l *remoteListener) {
			defer r.wg.Done()
			l.run(ctx, &r.wg)
		}(l)
	}
	<-ctx.Done()
	r.Close()
	r.wg.Wait()
}

// Close ends every listener and its connections.
func (r *Remote) Close() {
	for _, l := range r.listeners {
		l.close()
	}
}

type remoteListener struct {
	fwd    forwardspec.Forward
	bind   string
	port   int
	permit []string
	log    Log
	stream Stream
	reader *bufio.Reader
	chans  *Channels

	wmu   sync.Mutex
	mu    sync.Mutex
	conns map[uint32]*rconn
	dead  bool
}

// bindHost is OpenSSH's channel_rfwd_bind_host: no host means loopback,
// an empty host or "*" every address.
func bindHost(e forwardspec.Endpoint) string {
	switch {
	case !e.HasHost:
		return "localhost"
	case e.Host == "" || e.Host == "*":
		return "*"
	}
	return e.Host
}

func startRemote(ctx context.Context, lst Listener, fwd forwardspec.Forward, permit []string, chans *Channels, log Log) (*remoteListener, error) {
	stream, err := lst.Listen(ctx, bindHost(fwd.Listen), fwd.Listen.Port)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errNoRemoteListener, err)
	}
	l := &remoteListener{fwd: fwd, bind: bindHost(fwd.Listen), permit: permit, log: log, stream: stream, chans: chans,
		reader: bufio.NewReaderSize(stream, remoteMaxFrame+9), conns: map[uint32]*rconn{}}
	type reply struct {
		t    byte
		data []byte
		err  error
	}
	first := make(chan reply, 1)
	go func() {
		t, _, data, err := l.frame()
		first <- reply{t, data, err}
	}()
	select {
	case rep := <-first:
		switch {
		case rep.err != nil:
			err = rep.err
		case rep.t == 'L':
			if l.port, err = strconv.Atoi(string(rep.data)); err == nil && l.port > 0 && l.port <= 65535 {
				return l, nil
			}
			err = errors.New("invalid listener reply")
		case rep.t == 'F':
			// The listener's own reason (strerror or a fixed phrase).
			err = errors.New(string(rep.data))
		default:
			err = errors.New("invalid listener reply")
		}
	case <-time.After(remoteSetup):
		err = errors.New("remote listener did not reply")
	case <-ctx.Done():
		err = ctx.Err()
	}
	stream.Close()
	return nil, err
}

func (l *remoteListener) frame() (byte, uint32, []byte, error) {
	var h [9]byte
	if _, err := io.ReadFull(l.reader, h[:]); err != nil {
		return 0, 0, nil, err
	}
	n := binary.BigEndian.Uint32(h[5:])
	if n > remoteMaxFrame {
		return 0, 0, nil, errors.New("remote listener frame too large")
	}
	data := make([]byte, n)
	if _, err := io.ReadFull(l.reader, data); err != nil {
		return 0, 0, nil, err
	}
	return h[0], binary.BigEndian.Uint32(h[1:5]), data, nil
}

func (l *remoteListener) send(t byte, id uint32, data []byte) error {
	b := make([]byte, 9+len(data))
	b[0] = t
	binary.BigEndian.PutUint32(b[1:5], id)
	binary.BigEndian.PutUint32(b[5:9], uint32(len(data)))
	copy(b[9:], data)
	l.wmu.Lock()
	defer l.wmu.Unlock()
	_, err := l.stream.Write(b)
	return err
}

func (l *remoteListener) close() {
	l.stream.Close()
	l.mu.Lock()
	l.dead = true
	conns := l.conns
	l.conns = map[uint32]*rconn{}
	l.mu.Unlock()
	for _, c := range conns {
		c.gone()
	}
}

// run dispatches frames until the listener stream ends; handlers are added
// to wg.
func (l *remoteListener) run(ctx context.Context, wg *sync.WaitGroup) {
	defer l.close()
	for {
		t, id, data, err := l.frame()
		if err != nil {
			if ctx.Err() == nil && !errors.Is(err, io.EOF) {
				l.log.debug("remote listener for port %d: %v", l.port, err)
			}
			return
		}
		if t == 'N' {
			c := newRconn(l, id, string(data))
			l.mu.Lock()
			if l.dead || l.conns[id] != nil {
				l.mu.Unlock()
				return
			}
			l.conns[id] = c
			l.mu.Unlock()
			// ssh names a forwarded-tcpip channel after its originator address.
			host, _, _ := strings.Cut(c.peer, " ")
			ch := l.chans.Open(host + carrierStatus)
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer l.chans.Release(ch)
				l.handle(ctx, c)
			}()
			continue
		}
		l.mu.Lock()
		c := l.conns[id]
		l.mu.Unlock()
		if c == nil {
			continue // a close crossed this frame
		}
		var ok bool
		switch t {
		case 'D':
			ok = c.push(data)
		case 'E':
			ok = c.pushEOF()
		case 'A':
			ok = len(data) == 4 && c.grant(int(binary.BigEndian.Uint32(data)))
		case 'C':
			l.forget(id)
			c.gone()
			ok = true
		}
		if !ok {
			l.log.debug("remote listener for port %d: protocol error", l.port)
			return
		}
	}
}

func (l *remoteListener) forget(id uint32) {
	l.mu.Lock()
	delete(l.conns, id)
	l.mu.Unlock()
}

// handle connects one remote connection to its local destination, as ssh's
// client_request_forwarded_tcpip and rdynamic_connect_finish do.
func (l *remoteListener) handle(ctx context.Context, c *rconn) {
	defer c.Close()
	host, port, _ := strings.Cut(c.peer, " ")
	l.log.debug("client_request_forwarded_tcpip: listen %s port %d, originator %s port %s", l.bind, l.port, host, port)
	var local net.Conn
	var err error
	var remote net.Conn = c
	if l.fwd.Dynamic() {
		var version byte
		var target Target
		var wrapped net.Conn
		version, target, wrapped, err = socksRequest(c)
		if err != nil {
			l.log.debug("channel %d: dynamic request failed: %v", c.id, err)
			return
		}
		if !permitted(l.permit, target) {
			l.log.debug("rdynamic_connect_finish: requested forward not permitted")
			return
		}
		if local, err = dialLocal(ctx, target); err != nil {
			l.log.error("connect_to %s port %d: failed.", target.Host, target.Port)
			return
		}
		// A remote dynamic forwarding replies only once connected.
		if err := socksReply(c, version); err != nil {
			local.Close()
			return
		}
		remote = wrapped
	} else {
		target := Target{Host: l.fwd.Connect.Host, Port: l.fwd.Connect.Port}
		if l.fwd.Connect.Port == forwardspec.PortStreamLocal {
			target = Target{Path: l.fwd.Connect.Path}
		}
		if local, err = dialLocal(ctx, target); err != nil {
			if target.Path != "" {
				l.log.error("connect to %s port %d failed: %s", target.Path, forwardspec.PortStreamLocal, causeText(err))
			} else {
				l.log.error("connect_to %s port %d: failed.", target.Host, target.Port)
			}
			return
		}
	}
	stream, ok := local.(Stream)
	if !ok {
		local.Close()
		return
	}
	if err := Pipe(ctx, remote, stream); err != nil {
		l.log.debug("channel %d: %v", c.id, err)
	}
}

func dialLocal(ctx context.Context, t Target) (net.Conn, error) {
	var d net.Dialer
	if t.Path != "" {
		return d.DialContext(ctx, "unix", t.Path)
	}
	return d.DialContext(ctx, "tcp", net.JoinHostPort(t.Host, strconv.Itoa(t.Port)))
}

// causeText is the strerror part of a local dial error.
func causeText(err error) string {
	var sys *os.SyscallError
	if errors.As(err, &sys) {
		return sys.Err.Error()
	}
	return err.Error()
}

// permitted applies PermitRemoteOpen: host and port must match, each
// exactly (host case-insensitively) or as "*".
func permitted(list []string, t Target) bool {
	if len(list) == 0 {
		return true
	}
	for _, p := range list {
		switch p {
		case "any":
			return true
		case "none":
			return false
		}
		i := strings.LastIndexByte(p, ':')
		if i < 0 {
			continue
		}
		host, port := strings.Trim(p[:i], "[]"), p[i+1:]
		if (host == "*" || strings.EqualFold(host, t.Host)) && (port == "*" || port == strconv.Itoa(t.Port)) {
			return true
		}
	}
	return false
}

// rconn is one remote connection as a net.Conn with half-close. Received
// data is bounded by the listener's per-connection window; consumed bytes
// are acknowledged so the listener reads more.
type rconn struct {
	l    *remoteListener
	id   uint32
	peer string

	mu       sync.Mutex
	cond     *sync.Cond
	buf      []byte
	eof      bool
	closed   bool // Close called or listener dropped the connection
	removed  bool // the listener no longer knows the id
	wclosed  bool
	credit   int
	consumed int
	deadline time.Time
	timer    *time.Timer
}

func newRconn(l *remoteListener, id uint32, peer string) *rconn {
	c := &rconn{l: l, id: id, peer: peer, credit: remoteWindow}
	c.cond = sync.NewCond(&c.mu)
	return c
}

func (c *rconn) push(data []byte) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.eof || len(c.buf)+c.consumed+len(data) > remoteWindow {
		return false
	}
	c.buf = append(c.buf, data...)
	c.cond.Broadcast()
	return true
}

func (c *rconn) pushEOF() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.eof = true
	c.cond.Broadcast()
	return true
}

func (c *rconn) grant(n int) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.credit += n
	if c.credit > remoteWindow {
		return false
	}
	c.cond.Broadcast()
	return true
}

// gone marks a connection the listener dropped: buffered data stays
// readable, then reads end; writes fail.
func (c *rconn) gone() {
	c.mu.Lock()
	c.removed, c.eof = true, true
	c.cond.Broadcast()
	c.mu.Unlock()
}

func (c *rconn) expired() bool {
	return !c.deadline.IsZero() && !time.Now().Before(c.deadline)
}

func (c *rconn) Read(b []byte) (int, error) {
	c.mu.Lock()
	for len(c.buf) == 0 && !c.eof && !c.closed && !c.expired() {
		c.cond.Wait()
	}
	switch {
	case c.closed:
		c.mu.Unlock()
		return 0, net.ErrClosed
	case len(c.buf) == 0 && c.eof:
		c.mu.Unlock()
		return 0, io.EOF
	case len(c.buf) == 0:
		c.mu.Unlock()
		return 0, os.ErrDeadlineExceeded
	}
	n := copy(b, c.buf)
	c.buf = c.buf[n:]
	c.consumed += n
	ack := 0
	if c.consumed >= remoteWindow/4 || len(c.buf) == 0 {
		ack, c.consumed = c.consumed, 0
	}
	removed := c.removed
	c.mu.Unlock()
	if ack > 0 && !removed {
		var a [4]byte
		binary.BigEndian.PutUint32(a[:], uint32(ack))
		c.l.send('A', c.id, a[:])
	}
	return n, nil
}

func (c *rconn) Write(b []byte) (int, error) {
	written := 0
	for written < len(b) {
		c.mu.Lock()
		for c.credit == 0 && !c.closed && !c.removed && !c.wclosed {
			c.cond.Wait()
		}
		if c.closed || c.removed || c.wclosed {
			c.mu.Unlock()
			return written, net.ErrClosed
		}
		n := min(len(b)-written, c.credit, remoteMaxFrame)
		c.credit -= n
		c.mu.Unlock()
		if err := c.l.send('D', c.id, b[written:written+n]); err != nil {
			return written, err
		}
		written += n
	}
	return written, nil
}

func (c *rconn) CloseWrite() error {
	c.mu.Lock()
	done := c.wclosed || c.closed || c.removed
	c.wclosed = true
	c.cond.Broadcast()
	c.mu.Unlock()
	if done {
		return nil
	}
	return c.l.send('E', c.id, nil)
}

func (c *rconn) Close() error {
	c.mu.Lock()
	done := c.closed
	c.closed = true
	removed := c.removed
	if c.timer != nil {
		c.timer.Stop()
	}
	c.cond.Broadcast()
	c.mu.Unlock()
	if done {
		return nil
	}
	c.l.forget(c.id)
	if !removed {
		return c.l.send('C', c.id, nil)
	}
	return nil
}

func (c *rconn) SetDeadline(t time.Time) error { return c.SetReadDeadline(t) }

func (c *rconn) SetReadDeadline(t time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deadline = t
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
	if !t.IsZero() {
		c.timer = time.AfterFunc(time.Until(t), func() {
			c.mu.Lock()
			c.cond.Broadcast()
			c.mu.Unlock()
		})
	}
	c.cond.Broadcast()
	return nil
}

func (c *rconn) SetWriteDeadline(time.Time) error { return nil }

type remoteAddr string

func (a remoteAddr) Network() string { return "tcp" }
func (a remoteAddr) String() string  { return string(a) }

func (c *rconn) LocalAddr() net.Addr  { return remoteAddr(c.l.bind + " " + strconv.Itoa(c.l.port)) }
func (c *rconn) RemoteAddr() net.Addr { return remoteAddr(c.peer) }
