package transport

import (
	"context"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pion/logging"
	"github.com/pion/transport/v5/vnet"
	"github.com/pion/webrtc/v4"
)

// TestDeadPeerTimeoutClosesSilentPeer uses Pion's in-memory virtual network:
// no socket, host or service is contacted. After the channel works, every
// packet is dropped, as for a vanished device or network path. The peer with
// DeadPeerTimeout closes within that bound; the default peer is still open.
func TestDeadPeerTimeoutClosesSilentPeer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	router, err := vnet.NewRouter(&vnet.RouterConfig{CIDR: "10.0.0.0/24", LoggerFactory: &logging.DefaultLoggerFactory{Writer: io.Discard, DefaultLogLevel: logging.LogLevelDisabled}})
	if err != nil {
		t.Fatal(err)
	}
	var dropping atomic.Bool
	router.AddChunkFilter(func(vnet.Chunk) bool { return !dropping.Load() })
	peer := func(ip string, cfg Config) *Peer {
		n, err := vnet.NewNet(&vnet.NetConfig{StaticIPs: []string{ip}})
		if err != nil {
			t.Fatal(err)
		}
		if err := router.AddNet(n); err != nil {
			t.Fatal(err)
		}
		s := settings()
		s.SetNet(n)
		s.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4})
		p, err := newPeer(ctx, cfg, s)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = p.Close() })
		return p
	}
	const bound = 2 * time.Second
	watched, watchedRemote := peer("10.0.0.1", Config{DeadPeerTimeout: bound}), peer("10.0.0.2", Config{})
	control, controlRemote := peer("10.0.0.3", Config{}), peer("10.0.0.4", Config{})
	if err := router.Start(); err != nil {
		t.Fatal(err)
	}
	defer router.Stop()
	for _, pair := range [][2]*Peer{{watched, watchedRemote}, {control, controlRemote}} {
		ch, err := pair[0].CreateChannel("shell")
		if err != nil {
			t.Fatal(err)
		}
		negotiate(t, ctx, pair[0], pair[1], nil)
		if err := ch.Send(ctx, Message{Text: true, Data: []byte("ready")}); err != nil {
			t.Fatal(err)
		}
		remote, err := pair[1].Accept(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if m, err := remote.Receive(ctx); err != nil || string(m.Data) != "ready" {
			t.Fatal("channel did not work before the outage", err)
		}
	}
	// Idle but healthy: consent checks keep the watched peer open past its bound.
	time.Sleep(bound + 500*time.Millisecond)
	select {
	case <-watched.Done():
		t.Fatal("healthy idle peer closed by the liveness bound")
	default:
	}
	dropping.Store(true)
	started := time.Now()
	select {
	case <-watched.Done():
	case <-time.After(bound + 6*time.Second):
		t.Fatal("silent peer not closed within the configured bound")
	}
	if elapsed := time.Since(started); elapsed < bound/2 {
		t.Fatalf("closed too early: %v", elapsed)
	}
	select {
	case <-control.Done():
		t.Fatal("default peer closed as early as the configured one")
	default:
	}
}
