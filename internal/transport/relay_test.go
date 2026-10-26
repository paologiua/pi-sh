package transport

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/pion/logging"
	piontransport "github.com/pion/transport/v5"
	"github.com/pion/transport/v5/stdnet"
	"github.com/pion/turn/v5"
	"github.com/pion/webrtc/v4"
)

// Reproduce iSH's EINVAL at the actual interface enumeration seam used by ICE.
type deniedInterfacesNet struct{ *stdnet.Net }

func (*deniedInterfacesNet) Interfaces() ([]*piontransport.Interface, error) {
	return nil, syscall.EINVAL
}

func TestRelayWithoutInterfacesEncryptedSession(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	logger := &logging.DefaultLoggerFactory{Writer: io.Discard, DefaultLogLevel: logging.LogLevelDisabled}
	listener, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	const user, password, realm = "local-test", "local-test-password", "local-test.invalid"
	server, err := turn.NewServer(turn.ServerConfig{
		LoggerFactory: logger,
		Realm:         realm,
		AuthHandler: func(attrs *turn.RequestAttributes) (string, []byte, bool) {
			return user, turn.GenerateAuthKey(user, realm, password), attrs.Username == user
		},
		PacketConnConfigs: []turn.PacketConnConfig{{
			PacketConn: listener,
			RelayAddressGenerator: &turn.RelayAddressGeneratorStatic{
				RelayAddress: net.IPv4(127, 0, 0, 1), Address: "127.0.0.1",
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	s := settings()
	s.SetNet(&deniedInterfacesNet{Net: &stdnet.Net{}})
	s.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4})
	cfg := Config{RelayWithoutInterfaces: true, ICEServers: []webrtc.ICEServer{{
		URLs:     []string{"turn:" + listener.LocalAddr().String() + "?transport=udp"},
		Username: user, Credential: password,
	}}}
	p, err := newPeer(ctx, cfg, s)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	ch, err := p.CreateChannel("shell")
	if err != nil {
		t.Fatal(err)
	}
	offer, err := p.Offer(ctx)
	if err != nil {
		t.Fatalf("offer must work despite unavailable interface enumeration: %v", err)
	}
	count := 0
	for line := range strings.SplitSeq(offer.SDP, "\r\n") {
		if strings.HasPrefix(line, "a=candidate:") {
			count++
			if !strings.Contains(line, " typ relay ") {
				t.Fatal("non-relay candidate in interface-free mode")
			}
		}
	}
	if count == 0 {
		t.Fatal("no relay candidate")
	}
	remote := localPeer(t, ctx)
	answer, err := remote.Answer(ctx, offer)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.ApplyAnswer(answer); err != nil {
		t.Fatal(err)
	}
	if err := ch.Send(ctx, Message{Data: []byte("relay-test")}); err != nil {
		t.Fatal(err)
	}
	rch, err := remote.Accept(ctx)
	if err != nil {
		t.Fatal(err)
	}
	m, err := rch.Receive(ctx)
	if err != nil || string(m.Data) != "relay-test" {
		t.Fatal("relay payload missing", err)
	}
	if err := rch.Send(ctx, Message{Data: []byte("relay-reply")}); err != nil {
		t.Fatal(err)
	}
	m, err = ch.Receive(ctx)
	if err != nil || string(m.Data) != "relay-reply" {
		t.Fatal("relay response missing", err)
	}
	if p.pc.SCTP().Transport().State() != webrtc.DTLSTransportStateConnected {
		t.Fatal("relay session did not establish DTLS")
	}
}

func TestDefaultTransportReportsDeniedInterfaces(t *testing.T) {
	s := settings()
	s.SetNet(&deniedInterfacesNet{Net: &stdnet.Net{}})
	p, err := newPeer(context.Background(), Config{}, s)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	if _, err := p.CreateChannel("shell"); err != nil {
		t.Fatal(err)
	}
	_, err = p.Offer(context.Background())
	if !errors.Is(err, ErrPeer) || !strings.Contains(err.Error(), "operation=create-offer errno=22") {
		t.Fatalf("default path must preserve the reproduced iSH failure: %v", err)
	}
}

func TestRelayWithoutInterfacesRequiresAuthenticatedTURN(t *testing.T) {
	for _, server := range []webrtc.ICEServer{
		{},
		{URLs: []string{"stun:127.0.0.1:3478"}, Username: "test", Credential: "test"},
		{URLs: []string{"turn:127.0.0.1:3478"}, Username: "test"},
		{URLs: []string{"turn:127.0.0.1:3478"}, Credential: "test"},
	} {
		_, err := newPeer(context.Background(), Config{RelayWithoutInterfaces: true, ICEServers: []webrtc.ICEServer{server}}, settings())
		if !errors.Is(err, ErrRelayRequired) {
			t.Fatalf("missing authenticated TURN must fail explicitly: %v", err)
		}
	}
}

func TestRelayModeEnvironmentIsExplicit(t *testing.T) {
	t.Setenv("PI_SH_ISH_RELAY", "1")
	if _, err := New(context.Background(), Config{}); !errors.Is(err, ErrRelayRequired) {
		t.Fatalf("environment did not select relay mode: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, value := range []string{"", "0"} {
		t.Setenv("PI_SH_ISH_RELAY", value)
		if _, err := New(ctx, Config{}); !errors.Is(err, context.Canceled) {
			t.Fatalf("relay mode must remain opt-in: %v", err)
		}
	}
	t.Setenv("PI_SH_ISH_RELAY", "private-invalid-value")
	if _, err := New(context.Background(), Config{}); err == nil || strings.Contains(err.Error(), "private-invalid-value") {
		t.Fatal("invalid environment value must fail without echoing its content")
	}
}

func TestRelayModeRejectsOfferWithoutRelayCandidate(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	p := localPeer(t, ctx)
	// Simulate an ICE gathering result with host candidates only. The mode must
	// reject it before the caller can send an unusable offer to Connect.
	p.relayWithoutInterfaces = true
	if _, err := p.CreateChannel("shell"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Offer(ctx); !errors.Is(err, ErrRelayUnavailable) {
		t.Fatalf("no relay candidate must be reported: %v", err)
	}
}
