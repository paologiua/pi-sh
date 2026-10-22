//go:build darwin || linux

package shellbroker

import (
	"context"
	"os"
	"testing"

	"pi-sh/internal/transport"
)

type localBrokerChannel struct{ channel *transport.Channel }

func (s localBrokerChannel) Send(ctx context.Context, data []byte) error {
	return s.channel.Send(ctx, transport.Message{Text: true, Data: data})
}
func (s localBrokerChannel) Receive(ctx context.Context) ([]byte, error) {
	m, err := s.channel.Receive(ctx)
	return m.Data, err
}

// Only local Pion peers with empty ICE configuration; no service, credentials,
// SDP capture or remote installation. Both pumps stop with the fixture lifetime.
func localWebRTCPTY(t *testing.T, ctx context.Context, file *os.File) Stream {
	t.Helper()
	client, err := transport.New(ctx, transport.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	server, err := transport.New(ctx, transport.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { server.Close() })
	channel, err := client.CreateChannel("shell")
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
	go func() {
		for {
			message, err := remote.Receive(ctx)
			if err != nil {
				return
			}
			if n, err := file.Write(message.Data); err != nil || n != len(message.Data) {
				remote.Close()
				return
			}
		}
	}()
	go func() {
		defer remote.Close()
		for {
			buffer := make([]byte, 4096)
			n, err := file.Read(buffer)
			if n > 0 {
				if err := remote.Send(ctx, transport.Message{Text: true, Data: buffer[:n]}); err != nil {
					return
				}
			}
			if err != nil || n == 0 {
				return
			}
		}
	}()
	return localBrokerChannel{channel}
}
