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
	"pi-sh/internal/terminal"
	"pi-sh/internal/transport"
)

func runInteractiveSession(parent context.Context, failures <-chan string, peer *transport.Peer, channel *transport.Channel,
	inputFile, outputFile, diagnosticFile *os.File, escape rune) (result error) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	tty, err := terminal.Open(ctx, inputFile, outputFile)
	if err != nil {
		return err
	}
	var streams *localio.Session
	var workers sync.WaitGroup
	defer func() {
		cancel()
		if streams != nil {
			result = localCleanupResult(result, streams.Close())
			if streams.Interruptible {
				workers.Wait()
			}
		}
		if err := tty.Close(); err != nil {
			result = localCleanupResult(result, fmt.Errorf("local terminal restoration failed: %w", err))
		}
	}()
	streams, err = localio.Open(ctx, localio.Files{Stdin: inputFile, Stdout: outputFile, Stderr: diagnosticFile})
	if err != nil {
		return err
	}
	diagnostic := streams.Stderr
	if diagnostic == nil {
		diagnostic = io.Discard
	}
	if escape < 0 {
		_, err = fmt.Fprint(diagnostic, "pi-sh: connected using native WebRTC; local escapes disabled\r\n")
	} else {
		_, err = fmt.Fprintf(diagnostic, "pi-sh: connected using native WebRTC; %s. immediately after Enter disconnects\r\n", escapeLabel(escape))
	}
	if err != nil {
		if cause := ctx.Err(); cause != nil {
			return cause
		}
		return errors.New("local connection notice failed")
	}
	resizeError := make(chan error, 1)
	workers.Add(1)
	go func() {
		defer workers.Done()
		for {
			r, err := peer.Accept(ctx)
			if err != nil {
				resizeError <- err
				return
			}
			if r.Label() != "resize" {
				_ = r.Close()
				continue
			}
			for size := range tty.Sizes() {
				data, _ := json.Marshal(struct {
					Columns int `json:"cols"`
					Rows    int `json:"rows"`
				}{size.Columns, size.Rows})
				if err := r.Send(ctx, transport.Message{Text: true, Data: data}); err != nil {
					resizeError <- err
					return
				}
			}
			return
		}
	}()
	outputError := make(chan error, 1)
	workers.Add(1)
	go func() {
		defer workers.Done()
		for {
			m, err := channel.Receive(ctx)
			if err != nil {
				outputError <- err
				return
			}
			if n, err := streams.Stdout.Write(m.Data); err != nil || n != len(m.Data) {
				outputError <- errors.New("local terminal output failed")
				return
			}
		}
	}()
	inputError := make(chan error, 1)
	workers.Add(1)
	go func() {
		defer workers.Done()
		inputError <- inputWithActions(ctx, channel, streams.Stdin, escape, sessionInputActions(tty, streams))
	}()
	return waitSession(ctx, cancel, tty, inputError, outputError, resizeError, failures)
}
