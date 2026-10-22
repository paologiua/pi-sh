// Package platform exercises local runtime facilities needed by a future Connect
// client. It never contacts Raspberry Pi infrastructure or a remote device.
package platform

import (
	"context"
	"fmt"
	"io"
	"net"
	"runtime"
	"time"
)

type Result struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Error  string `json:"error,omitempty"`
}

type Report struct {
	OS      string   `json:"os"`
	Arch    string   `json:"arch"`
	Go      string   `json:"go"`
	Results []Result `json:"results"`
}

// Check performs bounded loopback-only checks. Success is not proof of iSH or
// WebRTC compatibility: external DNS, ICE, DTLS, SCTP and TTY need separate tests.
func Check(ctx context.Context) Report {
	r := Report{OS: runtime.GOOS, Arch: runtime.GOARCH, Go: runtime.Version()}
	for _, p := range []struct {
		name string
		run  func(context.Context) error
	}{
		{"tcp-loopback", tcp}, {"udp-loopback", udp}, {"goroutine-timer", timer},
	} {
		c, cancel := context.WithTimeout(ctx, 3*time.Second)
		err := p.run(c)
		cancel()
		v := Result{Name: p.name, Passed: err == nil}
		if err != nil {
			v.Error = err.Error()
		}
		r.Results = append(r.Results, v)
	}
	return r
}

func tcp(ctx context.Context) error {
	l, err := (&net.ListenConfig{}).Listen(ctx, "tcp4", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer l.Close()
	stop := context.AfterFunc(ctx, func() { _ = l.Close() })
	defer stop()
	done := make(chan error, 1)
	go func() {
		c, err := l.Accept()
		if err != nil {
			done <- err
			return
		}
		defer c.Close()
		deadline, _ := ctx.Deadline()
		_ = c.SetDeadline(deadline)
		b := make([]byte, 1)
		_, err = io.ReadFull(c, b)
		if err == nil {
			_, err = c.Write(b)
		}
		done <- err
	}()
	c, err := (&net.Dialer{}).DialContext(ctx, "tcp4", l.Addr().String())
	if err != nil {
		return err
	}
	defer c.Close()
	deadline, _ := ctx.Deadline()
	_ = c.SetDeadline(deadline)
	if _, err = c.Write([]byte{42}); err != nil {
		return err
	}
	b := make([]byte, 1)
	if _, err = io.ReadFull(c, b); err != nil {
		return err
	}
	if b[0] != 42 {
		return fmt.Errorf("loopback payload mismatch")
	}
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func udp(ctx context.Context) error {
	l, err := (&net.ListenConfig{}).ListenPacket(ctx, "udp4", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer l.Close()
	stop := context.AfterFunc(ctx, func() { _ = l.Close() })
	defer stop()
	deadline, _ := ctx.Deadline()
	_ = l.SetDeadline(deadline)
	c, err := (&net.Dialer{}).DialContext(ctx, "udp4", l.LocalAddr().String())
	if err != nil {
		return err
	}
	defer c.Close()
	_ = c.SetDeadline(deadline)
	if _, err = c.Write([]byte{42}); err != nil {
		return err
	}
	b := make([]byte, 2)
	n, peer, err := l.ReadFrom(b)
	if err != nil {
		return err
	}
	if n != 1 || b[0] != 42 {
		return fmt.Errorf("loopback payload mismatch")
	}
	if _, err = l.WriteTo(b[:n], peer); err != nil {
		return err
	}
	n, err = c.Read(b)
	if err != nil {
		return err
	}
	if n != 1 || b[0] != 42 {
		return fmt.Errorf("loopback reply mismatch")
	}
	return nil
}

func timer(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		t := time.NewTimer(time.Millisecond)
		defer t.Stop()
		select {
		case <-t.C:
			close(done)
		case <-ctx.Done():
		}
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
