package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"pi-sh/internal/localio"
	"pi-sh/internal/shellbroker"
	"pi-sh/internal/terminal"
	"pi-sh/internal/transport"
	"pi-sh/internal/ttymode"
)

type ttyCommandRequest struct {
	text, term string
	stdinNull  bool
}

// runTTYSession owns local terminal state and resize delivery. The adapter owns
// the remote in-memory launch and framing; the coordinator owns authorization.
// EOF stops input without synthesizing a terminal VEOF character.
func runTTYSession(ctx context.Context, failures <-chan string, peer *transport.Peer, channel *transport.Channel,
	request ttyCommandRequest, stdin io.Reader, inputFile, outputFile, diagnosticFile *os.File, escape rune) (result error) {
	ctx, cancel := context.WithCancelCause(ctx)
	var tty *terminal.Session
	var streams *localio.Session
	var monitors sync.WaitGroup
	defer func() {
		cancel(nil)
		monitors.Wait()
		if streams != nil {
			result = localCleanupResult(result, streams.Close())
		}
		if tty != nil {
			if err := tty.Close(); err != nil {
				// A restoration failure must not retain a successful commandStatus
				// that app() would otherwise prioritize over the joined failure.
				restoration := fmt.Errorf("local terminal restoration failed: %w", err)
				result = localCleanupResult(result, restoration)
			}
		}
	}()
	initial := terminal.Size{}
	var sizes <-chan terminal.Size
	var modes *ttymode.Settings
	if !request.stdinNull && terminal.IsTerminal(inputFile) {
		display := outputFile
		if !terminal.IsTerminal(display) {
			display = inputFile
		}
		var err error
		modes, err = terminal.CaptureModes(inputFile)
		if err != nil {
			return err
		}
		tty, err = terminal.Open(ctx, inputFile, display)
		if err != nil {
			return err
		}
		initial = <-tty.Sizes()
		sizes = tty.Sizes()
		monitors.Add(1)
		go func() {
			defer monitors.Done()
			select {
			case <-ctx.Done():
			case <-tty.Done():
				// An input operation ending during suspension can finish terminal
				// ownership normally. Only a terminal failure invalidates status.
				if err := tty.Wait(); err != nil && ctx.Err() == nil {
					cancel(errors.Join(errors.New("local terminal stopped"), err))
				}
			}
		}()
	}
	plan, err := shellbroker.NewTTYConfigured(request.text, shellbroker.TTYSettings{
		TERM: &request.term, Size: &shellbroker.TTYSize{Columns: initial.Columns, Rows: initial.Rows},
		Modes: modes,
	})
	if err != nil {
		return err
	}
	var inputFileForIO *os.File
	if file, ok := stdin.(*os.File); ok && !request.stdinNull {
		inputFileForIO = file
	}
	streams, err = localio.Open(ctx, localio.Files{Stdin: inputFileForIO, Stdout: outputFile, Stderr: diagnosticFile})
	if err != nil {
		return err
	}
	if streams.Stdin != nil {
		stdin = streams.Stdin
	}
	var stopIO func()
	if streams.Interruptible && (inputFileForIO != nil || request.stdinNull || stdin == nil) {
		stopIO = streams.Cancel
	}
	ready := make(chan struct{})
	monitors.Add(1)
	go func() {
		defer monitors.Done()
		select {
		case <-ctx.Done():
			return
		case <-ready:
		}
		if err := sendTTYResizes(ctx, peer, sizes); err != nil && ctx.Err() == nil {
			cancel(errors.New("terminal resize channel failed"))
		}
	}()
	err = coordinateCommand(ctx, failures, func(ctx context.Context) (int, error) {
		stop := context.AfterFunc(ctx, streams.Cancel)
		defer stop()
		return plan.Run(ctx, commandStream{channel}, shellbroker.TTYIO{
			Stdout: streams.Stdout,
			StopIO: stopIO,
			IsInputTransportError: func(err error) bool {
				return errors.Is(err, transport.ErrPeer) || errors.Is(err, transport.ErrClosed)
			},
			Ready: func() error { close(ready); return nil },
			Input: func(ctx context.Context, _ shellbroker.Stream) error {
				if request.stdinNull || stdin == nil {
					return io.EOF
				}
				return inputWithActions(ctx, channel, stdin, escape, sessionInputActions(tty, streams))
			},
		})
	}, true)
	if cause := context.Cause(ctx); cause != nil {
		// Preserve the resize/terminal cause alongside cleanup evidence.
		if errors.Is(err, shellbroker.ErrCleanupUnconfirmed) {
			return errors.Join(cause, shellbroker.ErrCleanupUnconfirmed)
		}
		return cause
	}
	if errors.Is(err, errEscape) {
		// Intentional disconnect is local success, but cleanup is not proved.
		return errors.Join(errEscape, shellbroker.ErrCleanupUnconfirmed)
	}
	return err
}

func sendTTYResizes(ctx context.Context, peer *transport.Peer, sizes <-chan terminal.Size) error {
	for {
		channel, err := peer.Accept(ctx)
		if err != nil {
			return err
		}
		if channel.Label() != "resize" {
			_ = channel.Close()
			continue
		}
		send := func(size terminal.Size) error {
			data, _ := json.Marshal(struct {
				Columns int `json:"cols"`
				Rows    int `json:"rows"`
			}{size.Columns, size.Rows})
			return channel.Send(ctx, transport.Message{Text: true, Data: data})
		}
		// The adapter already applied initial size before command launch. Do not
		// resend that stale snapshot after a more recent resize has been queued.
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case size, ok := <-sizes:
				if !ok {
					return nil
				}
				if err := send(size); err != nil {
					return err
				}
			}
		}
	}
}
