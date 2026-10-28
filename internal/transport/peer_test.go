package transport

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
)

// Only loopback IPv4 candidates are allowed. No STUN/TURN, LAN peers, remote
// signaling or Raspberry Pi service is involved in these real WebRTC tests.
func localPeer(t *testing.T, ctx context.Context) *Peer {
	t.Helper()
	s := settings()
	s.SetIncludeLoopbackCandidate(true)
	s.SetIPFilter(func(ip net.IP) bool { return ip.IsLoopback() })
	s.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4})
	p, err := newPeer(ctx, Config{}, s)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	return p
}

func TestOfferFailureIdentifiesOperation(t *testing.T) {
	p := localPeer(t, context.Background())
	_ = p.Close()
	_, err := p.Offer(context.Background())
	if !errors.Is(err, ErrPeer) || !strings.Contains(err.Error(), "operation=create-offer") {
		t.Fatalf("offer failure lacks safe operation context: %v", err)
	}
}

func TestPeerFailureDoesNotExposeUnderlyingError(t *testing.T) {
	for _, cause := range []error{
		errors.New("private session description password=secret"),
		fmt.Errorf("private session description password=secret: %w", syscall.ENOSYS),
	} {
		err := peerOperationError("create-offer", cause)
		if !errors.Is(err, ErrPeer) || strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "secret") {
			t.Fatalf("unsafe peer error: %v", err)
		}
		if errors.Unwrap(err) != ErrPeer {
			t.Fatal("underlying sensitive error retained")
		}
		if errors.Is(cause, syscall.ENOSYS) && !strings.Contains(err.Error(), fmt.Sprintf("errno=%d", uint64(syscall.ENOSYS))) {
			t.Fatalf("syscall error code missing: %v", err)
		}
	}
}

func negotiate(t *testing.T, ctx context.Context, a, b *Peer, modify func(*webrtc.SessionDescription)) {
	t.Helper()
	offer, err := a.Offer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	answer, err := b.Answer(ctx, offer)
	if err != nil {
		t.Fatal(err)
	}
	if modify != nil {
		modify(&answer)
	}
	if err := a.ApplyAnswer(answer); err != nil {
		t.Fatal(err)
	}
}

func TestEncryptedPeerMessages(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	a, b := localPeer(t, ctx), localPeer(t, ctx)
	x, err := a.CreateChannel("local-test")
	if err != nil {
		t.Fatal(err)
	}
	negotiate(t, ctx, a, b, nil)
	y, err := b.Accept(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if y.Label() != "local-test" {
		t.Fatal("channel label lost")
	}
	messages := []Message{{Data: []byte("hello 世界"), Text: true}, {Data: []byte{0, 255, 13, 10, 0, 128}}, {Data: []byte{}, Text: true}, {Data: bytes.Repeat([]byte{0xa5}, MaxSendSize)}}
	for _, m := range messages {
		if err := x.Send(ctx, m); err != nil {
			t.Fatal(err)
		}
		got, err := y.Receive(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if got.Text != m.Text || !bytes.Equal(got.Data, m.Data) {
			t.Fatal("message type, content or boundary lost")
		}
		if err := y.Send(ctx, got); err != nil {
			t.Fatal(err)
		}
		echo, err := x.Receive(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if echo.Text != m.Text || !bytes.Equal(echo.Data, m.Data) {
			t.Fatal("bidirectional message mismatch")
		}
	}
	if a.pc.SCTP().Transport().State() != webrtc.DTLSTransportStateConnected {
		t.Fatal("DTLS not connected")
	}
	if err := x.Send(ctx, Message{Data: make([]byte, MaxSendSize+1)}); !errors.Is(err, ErrMessageSize) {
		t.Fatal("oversized send accepted")
	}
	if err := y.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := x.Receive(ctx); !errors.Is(err, io.EOF) {
		t.Fatalf("remote close: %v", err)
	}
}

func TestFingerprintMismatchRejected(t *testing.T) {
	lifetime, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx, stop := context.WithTimeout(lifetime, 5*time.Second)
	defer stop()
	a, b := localPeer(t, lifetime), localPeer(t, lifetime)
	x, err := a.CreateChannel("authentication-test")
	if err != nil {
		t.Fatal(err)
	}
	negotiate(t, ctx, a, b, func(answer *webrtc.SessionDescription) {
		lines := strings.Split(answer.SDP, "\r\n")
		changed := false
		for i, l := range lines {
			if strings.HasPrefix(l, "a=fingerprint:sha-256 ") {
				lines[i] = "a=fingerprint:sha-256 " + strings.TrimSuffix(strings.Repeat("00:", 32), ":")
				changed = true
			}
		}
		if !changed {
			t.Fatal("no SHA-256 fingerprint to replace")
		}
		answer.SDP = strings.Join(lines, "\r\n")
	})
	// The owning lifetime is not cancelled by this test deadline. ErrClosed must
	// come from the failed handshake, not from cancelling the peer ourselves.
	err = x.Send(ctx, Message{Data: []byte("must not reach peer")})
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("expected handshake rejection, got %v", err)
	}
	if a.pc.SCTP().Transport().State() == webrtc.DTLSTransportStateConnected {
		t.Fatal("accepted wrong DTLS fingerprint")
	}
}

func TestCancellationAndLimits(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	p := localPeer(t, ctx)
	ch, err := p.CreateChannel("never-opened")
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < maxChannels; i++ {
		if _, err := p.CreateChannel("bounded"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := p.CreateChannel("overflow"); !errors.Is(err, ErrChannelLimit) {
		t.Fatal("channel limit not enforced")
	}
	wait, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	sent := make(chan error, 1)
	go func() { sent <- ch.Send(wait, Message{Data: []byte("not negotiated")}) }()
	cancel()
	select {
	case <-p.Done():
	case <-wait.Done():
		t.Fatal("peer cancellation did not close")
	}
	select {
	case err := <-sent:
		if !errors.Is(err, ErrClosed) {
			t.Fatal(err)
		}
	case <-wait.Done():
		t.Fatal("sender stuck")
	}
	if _, err := ch.Receive(wait); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	if _, err := p.Accept(wait); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := p.CreateChannel("after-close"); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
}

func TestReceiveQueueFailsWithoutSilentLoss(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := localPeer(t, ctx)
	ch, err := p.CreateChannel("queue-test")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < receiveQueueSize+1; i++ {
		ch.receive(webrtc.DataChannelMessage{Data: []byte{byte(i)}})
	}
	for i := 0; i < receiveQueueSize; i++ {
		m, err := ch.Receive(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(m.Data) != 1 || m.Data[0] != byte(i) {
			t.Fatal("queue corrupted or reordered")
		}
	}
	if _, err := ch.Receive(ctx); !errors.Is(err, ErrReceiveOverflow) {
		t.Fatalf("missing overflow error: %v", err)
	}
}

func TestInvalidDescriptionRedactsInput(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := localPeer(t, ctx)
	_, err := p.Answer(ctx, webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: "PRIVATE-TEST-SENTINEL"})
	if err != ErrDescription {
		t.Fatalf("raw description error escaped: %v", err)
	}
	if err := p.ApplyAnswer(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer}); err != ErrDescription {
		t.Fatal("wrong SDP type accepted")
	}
	_, err = p.Answer(ctx, webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: strings.Repeat("x", maxDescriptionSize+1)})
	if err != ErrDescription {
		t.Fatal("SDP size bound not enforced")
	}
}

func TestSendCancellationDoesNotClosePeer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	a, b := localPeer(t, ctx), localPeer(t, ctx)
	ch, err := a.CreateChannel("cancel-open-wait")
	if err != nil {
		t.Fatal(err)
	}
	short, stop := context.WithTimeout(ctx, 10*time.Millisecond)
	defer stop()
	if err := ch.Send(short, Message{Data: []byte("not yet open")}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	select {
	case <-a.Done():
		t.Fatal("message cancellation closed owning peer")
	default:
	}
	negotiate(t, ctx, a, b, nil)
	remote, err := b.Accept(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := ch.Send(ctx, Message{Data: []byte("after-cancellation")}); err != nil {
		t.Fatal(err)
	}
	m, err := remote.Receive(ctx)
	if err != nil || string(m.Data) != "after-cancellation" {
		t.Fatalf("reuse after cancellation: %v", err)
	}
}

func TestSustainedOrderedData(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	a, b := localPeer(t, ctx), localPeer(t, ctx)
	x, err := a.CreateChannel("ordered-data")
	if err != nil {
		t.Fatal(err)
	}
	negotiate(t, ctx, a, b, nil)
	y, err := b.Accept(ctx)
	if err != nil {
		t.Fatal(err)
	}
	const count = 96
	sent := make(chan error, 1)
	go func() {
		for i := 0; i < count; i++ {
			if err := x.Send(ctx, Message{Data: bytes.Repeat([]byte{byte(i)}, MaxSendSize)}); err != nil {
				sent <- err
				return
			}
			if x.dc.BufferedAmount() > sendHighWater {
				sent <- errors.New("send queue exceeded bound")
				return
			}
		}
		sent <- nil
	}()
	for i := 0; i < count; i++ {
		m, err := y.Receive(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if m.Text || !bytes.Equal(m.Data, bytes.Repeat([]byte{byte(i)}, MaxSendSize)) {
			t.Fatal("ordered transfer lost message data or boundaries")
		}
	}
	if err := <-sent; err != nil {
		t.Fatal(err)
	}
}

func TestReceiveSmallFragmentBurst(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	p := localPeer(t, ctx)
	ch, err := p.CreateChannel("fragmented-byte-stream")
	if err != nil {
		t.Fatal(err)
	}
	// A framed carrier may split a few KiB into many small messages. The
	// receive bound must account for bytes as well as hostile frame counts.
	const count = 2048
	for i := 0; i < count; i++ {
		ch.receive(webrtc.DataChannelMessage{Data: []byte{byte(i)}})
	}
	for i := 0; i < count; i++ {
		m, err := ch.Receive(ctx)
		if err != nil || len(m.Data) != 1 || m.Data[0] != byte(i) {
			t.Fatalf("valid small burst lost at byte %d: %v", i, err)
		}
	}
}

func TestReceiveByteBudgetAndAccounting(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	p := localPeer(t, ctx)
	ch, err := p.CreateChannel("byte-budget")
	if err != nil {
		t.Fatal(err)
	}
	count := receiveByteLimit / MaxMessageSize
	for i := 0; i < count+1; i++ {
		ch.receive(webrtc.DataChannelMessage{Data: bytes.Repeat([]byte{byte(i)}, MaxMessageSize)})
	}
	for i := 0; i < count; i++ {
		m, err := ch.Receive(ctx)
		if err != nil || len(m.Data) != MaxMessageSize || m.Data[0] != byte(i) {
			t.Fatal("bounded queue data changed", err)
		}
	}
	if _, err := ch.Receive(ctx); !errors.Is(err, ErrReceiveOverflow) {
		t.Fatal("byte budget was not enforced", err)
	}
	ch.mu.Lock()
	remaining := ch.queuedBytes
	ch.mu.Unlock()
	if remaining != 0 {
		t.Fatalf("queue byte accounting leaked %d bytes", remaining)
	}
}

// A peer that closes its connection after its last message (Pion's Close
// aborts the SCTP association with a User Initiated Abort cause, as the device
// agent did at the end of a command) ends the channel with io.EOF after the
// message. Pion runs OnError and OnClose in separate goroutines, so before the
// fix about half of the runs ended with ErrPeer instead.
func TestPeerCloseEndsChannelWithEOF(t *testing.T) {
	for i := range 20 {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		a, b := localPeer(t, ctx), localPeer(t, ctx)
		x, err := a.CreateChannel("local-test")
		if err != nil {
			t.Fatal(err)
		}
		negotiate(t, ctx, a, b, nil)
		y, err := b.Accept(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := y.Send(ctx, Message{Data: []byte("last")}); err != nil {
			t.Fatal(err)
		}
		if m, err := x.Receive(ctx); err != nil || string(m.Data) != "last" {
			t.Fatalf("run %d: %q %v", i, m.Data, err)
		}
		if err := b.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := x.Receive(ctx); !errors.Is(err, io.EOF) {
			t.Fatalf("run %d: peer close: %v", i, err)
		}
		cancel()
		_ = a.Close()
	}
}
