package main

import (
	"context"
	"errors"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"pi-sh/internal/connectsession"
	"pi-sh/internal/connpolicy"
	"pi-sh/internal/diag"
	"pi-sh/internal/forward"
	"pi-sh/internal/forwardspec"
	"pi-sh/internal/fwdbroker"
	"pi-sh/internal/transport"
)

// Forwarding messages are produced locally from the user's own specifications
// and the adapter's classified reasons; Failure still applies Sanitize.
func forwardLog(log *diag.Logger) forward.Log {
	at := func(level diag.Level) func(string) {
		return func(s string) { log.Failure(level, errors.New(s)) }
	}
	return forward.Log{Error: at(diag.Error), Info: at(diag.Info), Verbose: at(diag.Verbose), Debug: at(diag.Debug1)}
}

// forwarding is one dedicated Connect carrier running the in-memory
// forwarding adapter, plus the local listeners it serves. It is separate
// from the session's channel, as Connect accepted two concurrent sessions on
// the device (docs/FORWARDING.md, 2026-10-27).
type forwarding struct {
	peer   *transport.Peer
	mux    *fwdbroker.Mux
	remote *forward.Remote
	cancel context.CancelFunc
	served sync.WaitGroup
}

// startForwarding connects the carrier and stages the adapter. With local
// forwardings it binds them as ssh does after authentication; bind failures
// are fatal only with ExitOnForwardFailure (forward.Listen).
func startForwarding(ctx context.Context, policy connpolicy.Policy, source sessionSource, opts options, chans *forward.Channels) (*forwarding, *forward.Forwarder, error) {
	log := diag.From(ctx)
	peer, ch, err := connectsession.Establish(ctx, context.WithoutCancel(ctx), source, policy)
	if err != nil {
		return nil, nil, err
	}
	mux, err := fwdbroker.Start(context.WithoutCancel(ctx), commandStream{ch})
	if err != nil {
		peer.Close()
		return nil, nil, err
	}
	log.Debug(msgForwardCarrier, diag.State("connector", mux.Connector().Name))
	f := &forwarding{peer: peer, mux: mux}
	var listeners *forward.Forwarder
	if len(opts.fwd.Local) > 0 {
		listeners, err = forward.Listen(ctx, forward.Config{
			Forwards: opts.fwd.Local, GatewayPorts: opts.fwd.GatewayPorts, BindUnlink: opts.fwd.BindUnlink,
			BindMask: opts.fwd.BindMask, ExitOnFail: opts.fwd.ExitOnForwardFailure, Channels: chans,
		}, forwardLog(log))
		if err != nil {
			f.stop(ctx)
			return nil, nil, err
		}
	}
	// Remote listeners follow local ones, as in ssh_init_forwarding.
	if len(opts.fwd.Remote) > 0 {
		f.remote, err = forward.ListenRemote(ctx, mux, forward.RemoteConfig{
			Forwards: opts.fwd.Remote, ExitOnFail: opts.fwd.ExitOnForwardFailure, PermitOpen: opts.fwd.PermitRemoteOpen, Channels: chans,
		}, forwardLog(log))
		if err != nil {
			if listeners != nil {
				listeners.Close()
			}
			f.stop(ctx)
			return nil, nil, err
		}
	}
	return f, listeners, nil
}

// serve accepts local and remote connections until stop or carrier failure;
// listeners may be nil.
func (f *forwarding) serve(ctx context.Context, listeners *forward.Forwarder) {
	ctx, f.cancel = context.WithCancel(ctx)
	if listeners != nil {
		f.served.Add(1)
		go func() {
			defer f.served.Done()
			listeners.Serve(ctx, f.mux)
		}()
	}
	if f.remote != nil {
		f.served.Add(1)
		go func() {
			defer f.served.Done()
			f.remote.Serve(ctx)
		}()
	}
}

// stop closes listeners and active streams, asks the adapter to kill and reap
// every connector, then closes the carrier. An unconfirmed cleanup is
// reported, never hidden.
func (f *forwarding) stop(ctx context.Context) {
	if f.cancel != nil {
		f.cancel()
	}
	f.served.Wait()
	if f.remote != nil {
		f.remote.Close()
	}
	if err := f.mux.Close(); err != nil {
		diag.From(ctx).Failure(diag.Error, err)
	}
	f.peer.Close()
}

// runForwarding handles sessions with forwarding but no shell or command:
// -W relays stdin/stdout to one remote connection; -N serves -L/-D/-R until a
// signal or carrier failure.
func runForwarding(ctx context.Context, policy connpolicy.Policy, source sessionSource, opts options, stdin io.Reader, stdout io.Writer) (int, error) {
	// -N and -W read no escapes, so no channel table is kept.
	f, listeners, err := startForwarding(ctx, policy, source, opts, nil)
	if err != nil {
		return 255, err
	}
	defer f.stop(ctx)
	if opts.fwd.StdioSet {
		target := forward.Target{Host: opts.fwd.Stdio.Host, Port: opts.fwd.Stdio.Port}
		if opts.fwd.Stdio.Port == forwardspec.PortStreamLocal {
			target = forward.Target{Path: opts.fwd.Stdio.Path}
		}
		if stdin == nil {
			stdin = eofReader{}
		}
		// ssh prints the open failure, then "stdio forwarding failed" (fatal).
		if err := forward.Stdio(ctx, f.mux, target, stdin, stdout, forwardLog(diag.From(ctx))); err != nil {
			return 255, err
		}
		return 0, nil
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, terminationSignals()...)
	defer signal.Stop(signals)
	f.serve(ctx, listeners)
	select {
	case <-ctx.Done():
		// The app's signal context ends together with this channel's delivery.
		select {
		case sig := <-signals:
			return noSessionSignalStatus(sig), nil
		case <-time.After(100 * time.Millisecond):
			return 255, nil
		}
	case sig := <-signals:
		return noSessionSignalStatus(sig), nil
	case <-f.mux.Done():
		return 255, f.mux.Err()
	}
}

// noSessionSignalStatus is what OpenSSH 10.2 `ssh -N` returned in the local
// reference: 0 after SIGTERM, 255 after SIGINT or SIGHUP.
func noSessionSignalStatus(sig os.Signal) int {
	if sig == syscall.SIGTERM {
		return 0
	}
	return 255
}

type eofReader struct{}

func (eofReader) Read([]byte) (int, error) { return 0, io.EOF }
