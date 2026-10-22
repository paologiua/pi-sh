//go:build darwin || linux

package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/creack/pty"
	"pi-sh/internal/command"
	"pi-sh/internal/transport"
)

// This simulator joins actual local WebRTC peers to a local shell PTY. It
// exercises the adapter and CLI coordinator, not Connect HTTP/signaling APIs.
func TestCommandOverLocalWebRTCAndPTY(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client, err := transport.New(ctx, transport.Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server, err := transport.New(ctx, transport.Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	ch, err := client.CreateChannel("shell")
	if err != nil {
		t.Fatal(err)
	}
	offer, err := client.Offer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	answer, err := server.Answer(ctx, offer)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.ApplyAnswer(answer); err != nil {
		t.Fatal(err)
	}
	remote, err := server.Accept(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, "/bin/sh", "-i")
	cmd.Env = append(os.Environ(), "HISTFILE=/dev/null", "ENV=/dev/null", "BASH_ENV=/dev/null")
	file, err := pty.Start(cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	defer func() { cancel(); _ = cmd.Wait() }()
	go func() {
		for {
			message, err := remote.Receive(ctx)
			if err != nil {
				return
			}
			if _, err := file.Write(message.Data); err != nil {
				return
			}
		}
	}()
	go func() {
		defer remote.Close()
		buffer := make([]byte, 1024)
		for {
			n, err := file.Read(buffer)
			if n > 0 {
				if err := remote.Send(ctx, transport.Message{Data: buffer[:n]}); err != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	plan, err := command.New("printf 'simulated\\n'; printf 'error' >&2; exit 23")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	drained := make(chan error, 1)
	go func() { _, err := io.Copy(&out, reader); drained <- err }()
	err = runLegacyFileCommand(ctx, nil, ch, plan, writer)
	if err := writer.Close(); err != nil {
		t.Fatal("caller output closed", err)
	}
	if err := <-drained; err != nil {
		t.Fatal(err)
	}
	var status *commandStatus
	if !errors.As(err, &status) || status.code != 23 || out.String() != "simulated\nerror" {
		t.Fatalf("adapter result %v, output %q", err, out.String())
	}
}

func TestCommandCancellationAndAuthorization(t *testing.T) {
	for _, authorization := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		peer, err := transport.New(ctx, transport.Config{})
		if err != nil {
			t.Fatal(err)
		}
		ch, err := peer.CreateChannel("shell") // Never opens: send must cancel.
		if err != nil {
			t.Fatal(err)
		}
		failures := make(chan string, 1)
		if authorization {
			failures <- "test authorization expired"
		} else {
			cancel()
		}
		plan, _ := command.New("true")
		err = runCommand(ctx, failures, ch, plan, io.Discard)
		cancel()
		peer.Close()
		var status *commandStatus
		if err == nil || errors.As(err, &status) {
			t.Fatal("session failure reported command status")
		}
	}
}
