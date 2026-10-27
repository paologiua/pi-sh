// Package forward implements the local side of OpenSSH-style forwarding:
// -L listeners (TCP and Unix-domain), the -D SOCKS4/4a/5 proxy and -W stdio
// forwarding. Remote connections are opened through a Dialer, so the package
// has no knowledge of Connect or of the remote connector in use.
package forward

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"sync"
	"sync/atomic"

	"pi-sh/internal/forwardspec"
)

// Target is a remote TCP host/port or a remote Unix-socket path.
type Target struct {
	Host string
	Port int
	Path string
}

func (t Target) String() string {
	if t.Path != "" {
		return t.Path
	}
	return net.JoinHostPort(t.Host, strconv.Itoa(t.Port))
}

// Stream is one forwarded remote connection. Read returns io.EOF after the
// remote side's output ends, or an *OpenError when the remote connection
// could not be established. CloseWrite sends EOF while reads continue; Close
// aborts both directions.
type Stream interface {
	io.ReadWriter
	CloseWrite() error
	Close() error
}

// Dialer opens remote streams. Dial may return before the remote connect
// completes; a later Read then reports any *OpenError.
type Dialer interface {
	Dial(ctx context.Context, target Target) (Stream, error)
}

// OpenError reports a remote connection that never opened (ssh prints
// "open failed: connect failed").
type OpenError struct{ Detail string }

func (e *OpenError) Error() string {
	if e.Detail == "" {
		return "connect failed"
	}
	return "connect failed: " + e.Detail
}

// Log receives diagnostics at OpenSSH-like levels.
type Log struct {
	Error, Verbose, Debug func(string)
}

func (l Log) error(format string, args ...any) {
	if l.Error != nil {
		l.Error(fmt.Sprintf(format, args...))
	}
}
func (l Log) verbose(format string, args ...any) {
	if l.Verbose != nil {
		l.Verbose(fmt.Sprintf(format, args...))
	}
}
func (l Log) debug(format string, args ...any) {
	if l.Debug != nil {
		l.Debug(fmt.Sprintf(format, args...))
	}
}

// ErrLocalForwarding is ssh's fatal ExitOnForwardFailure result.
var ErrLocalForwarding = errors.New("Could not request local forwarding.")

// Config selects local forwardings and binding policy.
type Config struct {
	Forwards     []forwardspec.Forward // -L and -D entries
	GatewayPorts bool
	BindUnlink   bool
	BindMask     uint32
	ExitOnFail   bool
}

// Forwarder owns the listeners of one session.
type Forwarder struct {
	log       Log
	listeners []listener
	channel   atomic.Int64
	wg        sync.WaitGroup
}

type listener struct {
	net.Listener
	forward forwardspec.Forward
}

// Listen binds every forwarding as ssh_init_forwarding does: a forwarding
// succeeds if at least one address binds. With ExitOnFail any failure is
// fatal; otherwise failure of all forwardings is reported and tolerated.
func Listen(ctx context.Context, cfg Config, log Log) (*Forwarder, error) {
	f := &Forwarder{log: log}
	success := 0
	for _, fwd := range cfg.Forwards {
		if fwd.Dynamic() {
			log.debug("Local connections to %s forwarded via SOCKS", fwd.Listen.String())
		} else {
			log.debug("Local connections to %s forwarded to remote address %s", fwd.Listen.String(), fwd.Connect.String())
		}
		ls := bind(ctx, fwd, cfg, log)
		for _, l := range ls {
			f.listeners = append(f.listeners, listener{l, fwd})
		}
		if len(ls) > 0 {
			success++
		}
	}
	n := len(cfg.Forwards)
	if n > 0 && success != n && cfg.ExitOnFail {
		f.Close()
		return nil, ErrLocalForwarding
	}
	if n > 0 && success == 0 {
		log.error("%s", ErrLocalForwarding.Error())
	}
	return f, nil
}

// Addrs returns the bound listener addresses (for tests and diagnostics).
func (f *Forwarder) Addrs() []net.Addr {
	var out []net.Addr
	for _, l := range f.listeners {
		out = append(out, l.Addr())
	}
	return out
}

// Close stops accepting. Active connections end with Serve's context.
func (f *Forwarder) Close() {
	for _, l := range f.listeners {
		l.Close()
	}
}

// Serve accepts until ctx ends, then closes listeners and waits for every
// connection handler (which ctx also cancels) to finish.
func (f *Forwarder) Serve(ctx context.Context, dialer Dialer) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(ctx, f.Close)
	defer stop()
	var accepts sync.WaitGroup
	for _, l := range f.listeners {
		accepts.Add(1)
		go func(l listener) {
			defer accepts.Done()
			for {
				conn, err := l.Accept()
				if err != nil {
					if ctx.Err() == nil && !errors.Is(err, net.ErrClosed) {
						f.log.error("accept %s: %v", l.Addr(), err)
					}
					return
				}
				id := f.channel.Add(1)
				f.wg.Add(1)
				go func() {
					defer f.wg.Done()
					f.handle(ctx, dialer, l.forward, conn, id)
				}()
			}
		}(l)
	}
	<-ctx.Done()
	f.Close()
	accepts.Wait()
	f.wg.Wait()
}

func (f *Forwarder) handle(ctx context.Context, dialer Dialer, fwd forwardspec.Forward, conn net.Conn, id int64) {
	defer conn.Close()
	var target Target
	if fwd.Dynamic() {
		t, wrapped, err := socksHandshake(conn)
		if err != nil {
			f.log.debug("channel %d: dynamic request failed: %v", id, err)
			return
		}
		target, conn = t, wrapped
	} else if fwd.Connect.Port == forwardspec.PortStreamLocal {
		target = Target{Path: fwd.Connect.Path}
	} else {
		target = Target{Host: fwd.Connect.Host, Port: fwd.Connect.Port}
	}
	f.log.debug("channel %d: new forwarded connection to %s", id, target)
	stream, err := dialer.Dial(ctx, target)
	if err != nil {
		f.log.error("channel %d: open failed: %v", id, openFailure(err))
		return
	}
	if err := Pipe(ctx, conn, stream); err != nil {
		var open *OpenError
		if errors.As(err, &open) {
			f.log.error("channel %d: open failed: %v", id, open)
			return
		}
		f.log.debug("channel %d: %v", id, err)
	}
}

func openFailure(err error) error {
	var open *OpenError
	if errors.As(err, &open) {
		return open
	}
	return &OpenError{Detail: err.Error()}
}

// halfCloser is implemented by *net.TCPConn and *net.UnixConn.
type halfCloser interface{ CloseWrite() error }

// Pipe relays local and remote with independent half-close in each direction:
// local EOF becomes remote CloseWrite and remote EOF becomes local CloseWrite.
// It returns when both directions finished or either failed, closing both.
func Pipe(ctx context.Context, local io.ReadWriteCloser, remote Stream) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(ctx, func() { local.Close(); remote.Close() })
	defer stop()
	results := make(chan error, 2)
	go func() {
		_, err := io.Copy(remote, local)
		if err == nil {
			err = remote.CloseWrite()
		}
		results <- err
	}()
	go func() {
		_, err := io.Copy(local, remote)
		if err == nil {
			if h, ok := local.(halfCloser); ok {
				err = h.CloseWrite()
			}
		}
		results <- err
	}()
	var first error
	for i := 0; i < 2; i++ {
		err := <-results
		if err != nil && first == nil {
			first = err
			// Abort the other direction instead of waiting for a peer that
			// may never finish.
			local.Close()
			remote.Close()
		}
	}
	local.Close()
	remote.Close()
	return first
}

// ErrStdioForwarding is ssh's -W failure message (exit status 255).
var ErrStdioForwarding = errors.New("stdio forwarding failed")

// Stdio implements -W. Local stdin EOF is sent as remote EOF; the session ends
// when the remote output ends, as ssh exits when the channel closes. The
// stdin reader is caller-owned and may remain blocked after return.
func Stdio(ctx context.Context, dialer Dialer, target Target, stdin io.Reader, stdout io.Writer, log Log) error {
	stream, err := dialer.Dial(ctx, target)
	if err != nil {
		log.error("channel 0: open failed: %v", openFailure(err))
		return ErrStdioForwarding
	}
	defer stream.Close()
	input := make(chan error, 1)
	go func() {
		_, err := io.Copy(stream, stdin)
		if err == nil {
			err = stream.CloseWrite()
		}
		input <- err
	}()
	_, err = io.Copy(stdout, stream)
	if err != nil {
		var open *OpenError
		if errors.As(err, &open) {
			log.error("channel 0: open failed: %v", open)
			return ErrStdioForwarding
		}
		return err
	}
	select {
	case err := <-input:
		if err != nil && !errors.Is(err, os.ErrClosed) && !errors.Is(err, net.ErrClosed) {
			log.debug("stdio input: %v", err)
		}
	default:
	}
	return nil
}
