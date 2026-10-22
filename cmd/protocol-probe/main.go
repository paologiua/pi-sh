// protocol-probe is a developer-only, stdin/stdout JSON signaling harness for
// an authorised browser-assisted native WebRTC shell check. It is not pi-sh.
// Signaling material must be piped in memory, never saved or copied into logs.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/pion/webrtc/v4"
	"golang.org/x/term"
	"pi-sh/internal/transport"
)

func main() { os.Exit(app()) }

func app() int {
	// A terminal must not echo ICE credentials or descriptions sent to this
	// harness. Pipes are preferred; PTY input is explicitly made raw.
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		state, err := term.MakeRaw(fd)
		if err != nil {
			return 1
		}
		defer term.Restore(fd, state)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	ctx, stop := context.WithTimeout(ctx, 120*time.Second)
	defer stop()
	if err := run(ctx, os.Stdin, os.Stdout); err != nil {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"kind": "error", "error": err.Error()})
		return 1
	}
	return 0
}

func run(ctx context.Context, input io.Reader, output io.Writer) error {
	dec := json.NewDecoder(io.LimitReader(input, 1024*1024))
	enc := json.NewEncoder(output)
	var cfg webrtc.Configuration
	if err := decode(ctx, dec, &cfg); err != nil {
		return errors.New("invalid ICE configuration input")
	}
	p, err := transport.New(ctx, transport.Config{ICEServers: cfg.ICEServers})
	if err != nil {
		return err
	}
	defer p.Close()
	ch, err := p.CreateChannel("shell")
	if err != nil {
		return err
	}
	offer, err := p.Offer(ctx)
	if err != nil {
		return err
	}
	if err := enc.Encode(map[string]any{"kind": "offer", "offer": offer}); err != nil {
		return errors.New("signaling output failed")
	}
	var answer webrtc.SessionDescription
	if err := decode(ctx, dec, &answer); err != nil {
		return errors.New("invalid answer input")
	}
	if err := p.ApplyAnswer(answer); err != nil {
		return err
	}
	resizeSent := make(chan error, 1)
	go func() {
		r, err := p.Accept(ctx)
		if err != nil {
			resizeSent <- err
			return
		}
		if r.Label() != "resize" {
			resizeSent <- errors.New("unexpected remote channel")
			return
		}
		resizeSent <- r.Send(ctx, transport.Message{Text: true, Data: []byte(`{"cols":100,"rows":32}`)})
	}()
	results := make(chan string, 32)
	readErr := make(chan error, 1)
	go func() {
		var pending []byte
		for {
			m, err := ch.Receive(ctx)
			if err != nil {
				readErr <- err
				return
			}
			pending = append(pending, m.Data...)
			if len(pending) > 128*1024 {
				readErr <- errors.New("shell output exceeded probe limit")
				return
			}
			for {
				i := bytes.IndexByte(pending, '\n')
				if i < 0 {
					break
				}
				line := strings.TrimSpace(string(pending[:i]))
				pending = pending[i+1:]
				// Emit only known marker/size lines, never raw terminal contents.
				if strings.HasPrefix(line, "PI_SH_NATIVE_") || line == "32 100" {
					select {
					case results <- line:
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}()
	send := func(s string) error { return ch.Send(ctx, transport.Message{Text: true, Data: []byte(s)}) }
	await := func(want string) error {
		for {
			select {
			case line := <-results:
				if line == want {
					return nil
				}
			case <-readErr:
				return errors.New("remote shell closed before expected marker")
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	if err := send(" unset HISTFILE; printf '\\nPI_SH_NATIVE_A\\n'; test -t 0 && printf 'PI_SH_NATIVE_TTY\\n'; test -n \"$CONNECT_TTY\" && printf 'PI_SH_NATIVE_CONNECT\\n'\r"); err != nil {
		return err
	}
	for _, marker := range []string{"PI_SH_NATIVE_A", "PI_SH_NATIVE_TTY", "PI_SH_NATIVE_CONNECT"} {
		if err := await(marker); err != nil {
			return err
		}
	}
	if err := enc.Encode(map[string]any{"kind": "native-shell", "interactiveRoundTrip": true, "tty": true, "connectTTY": true}); err != nil {
		return err
	}
	select {
	case err := <-resizeSent:
		if err != nil {
			return err
		}
	case <-ctx.Done():
		return ctx.Err()
	}
	if err := send("printf '\\nPI_SH_NATIVE_SIZE\\n'; stty size; printf 'PI_SH_NATIVE_B\\n'\r"); err != nil {
		return err
	}
	if err := await("PI_SH_NATIVE_SIZE"); err != nil {
		return err
	}
	if err := await("32 100"); err != nil {
		return err
	}
	if err := await("PI_SH_NATIVE_B"); err != nil {
		return err
	}
	if err := send("exit\r"); err != nil {
		return err
	}
	select {
	case err := <-readErr:
		if !errors.Is(err, io.EOF) && !errors.Is(err, transport.ErrClosed) {
			return err
		}
	case <-ctx.Done():
		return ctx.Err()
	}
	return enc.Encode(map[string]any{"kind": "verified", "nativeWebRTC": true, "twoShellRoundTrips": true, "resize": true, "remoteClose": true})
}

// The harness is a dedicated process. On cancellation its caller exits after
// restoring local mode; an outstanding stdin read cannot hold that exit open.
func decode(ctx context.Context, dec *json.Decoder, value any) error {
	done := make(chan error, 1)
	go func() { done <- dec.Decode(value) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
