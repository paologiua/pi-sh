// Package transport provides protocol-neutral WebRTC data channels. It does not
// authenticate, discover devices, contact Connect, or define a shell protocol.
package transport

import (
	"context"
	"errors"
	"io"
	"sync"

	"github.com/pion/logging"
	"github.com/pion/webrtc/v4"
	"pi-sh/internal/diag"
)

// State transitions are logged as enum names only; candidates, addresses, SDP
// and ICE credentials never reach diagnostics.
const (
	msgPeerState      diag.Message = "transport: peer connection state"
	msgICEState       diag.Message = "transport: ICE connection state"
	msgGatheringState diag.Message = "transport: ICE gathering state"
	msgSignalingState diag.Message = "transport: signaling state"
)

const (
	MaxMessageSize     = 64 * 1024
	MaxSendSize        = 16 * 1024
	maxChannels        = 8
	maxDescriptionSize = 256 * 1024
)

var (
	ErrClosed          = errors.New("WebRTC transport closed")
	ErrPeer            = errors.New("WebRTC peer operation failed")
	ErrDescription     = errors.New("invalid WebRTC session description")
	ErrChannelLimit    = errors.New("WebRTC channel limit exceeded")
	ErrMessageSize     = errors.New("WebRTC message exceeds local limit")
	ErrReceiveOverflow = errors.New("WebRTC receive queue exhausted")
)

// Config contains only transport configuration. ICE servers must come from an
// authorised source; none are hard-coded. Credentials are used in memory only.
type Config struct {
	ICEServers []webrtc.ICEServer
	RelayOnly  bool
}

type Peer struct {
	pc       *webrtc.PeerConnection
	done     chan struct{}
	incoming chan *Channel
	once     sync.Once
	mu       sync.Mutex
	channels []*Channel
}

func settings() webrtc.SettingEngine {
	s := webrtc.SettingEngine{}
	// Do not allow environment-controlled Pion logging to expose SDP, candidates
	// or credentials. Keep cryptographic verification at Pion's secure defaults.
	s.LoggerFactory = &logging.DefaultLoggerFactory{Writer: io.Discard, DefaultLogLevel: logging.LogLevelDisabled}
	s.SetSCTPMaxMessageSize(MaxMessageSize)
	s.SetSCTPMaxReceiveBufferSize(1024 * 1024)
	return s
}

func New(ctx context.Context, cfg Config) (*Peer, error) {
	return newPeer(ctx, cfg, settings())
}

func newPeer(ctx context.Context, cfg Config, s webrtc.SettingEngine) (*Peer, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c := webrtc.Configuration{ICEServers: cfg.ICEServers}
	if cfg.RelayOnly {
		c.ICETransportPolicy = webrtc.ICETransportPolicyRelay
	}
	pc, err := webrtc.NewAPI(webrtc.WithSettingEngine(s)).NewPeerConnection(c)
	if err != nil {
		return nil, ErrPeer
	}
	p := &Peer{pc: pc, done: make(chan struct{}), incoming: make(chan *Channel, maxChannels)}
	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		ch, err := p.add(dc)
		if err != nil {
			_ = dc.Close()
			return
		}
		select {
		case p.incoming <- ch:
		case <-p.done:
			_ = ch.Close()
		}
	})
	log := diag.From(ctx)
	pc.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		log.Debug3(msgICEState, diag.State("state", state.String()))
	})
	pc.OnICEGatheringStateChange(func(state webrtc.ICEGatheringState) {
		log.Debug3(msgGatheringState, diag.State("state", state.String()))
	})
	pc.OnSignalingStateChange(func(state webrtc.SignalingState) {
		log.Debug3(msgSignalingState, diag.State("state", state.String()))
	})
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		log.Debug2(msgPeerState, diag.State("state", state.String()))
		if state == webrtc.PeerConnectionStateFailed || state == webrtc.PeerConnectionStateClosed {
			_ = p.Close()
		}
	})
	go func() {
		select {
		case <-ctx.Done():
			_ = p.Close()
		case <-p.done:
		}
	}()
	return p, nil
}

// Accept returns an incoming data channel. Labels have no protocol meaning here.
func (p *Peer) Accept(ctx context.Context) (*Channel, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case <-p.done:
		return nil, ErrClosed
	default:
	}
	select {
	case c := <-p.incoming:
		return c, nil
	case <-p.done:
		return nil, ErrClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// CreateChannel creates an ordered, reliable channel. Other channel semantics
// require explicit protocol evidence before adding an application-level mapping.
func (p *Peer) CreateChannel(label string) (*Channel, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	select {
	case <-p.done:
		return nil, ErrClosed
	default:
	}
	if len(p.channels) >= maxChannels {
		return nil, ErrChannelLimit
	}
	dc, err := p.pc.CreateDataChannel(label, nil)
	if err != nil {
		return nil, ErrPeer
	}
	ch := newChannel(dc)
	p.channels = append(p.channels, ch)
	return ch, nil
}

func (p *Peer) add(dc *webrtc.DataChannel) (*Channel, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	select {
	case <-p.done:
		return nil, ErrClosed
	default:
	}
	if len(p.channels) >= maxChannels {
		return nil, ErrChannelLimit
	}
	ch := newChannel(dc)
	p.channels = append(p.channels, ch)
	return ch, nil
}

// Offer gathers candidates into a complete SDP offer. The result is sensitive
// in-memory signaling material: callers must not log or persist it. Negotiation
// methods are one-shot and must be called serially by the signaling coordinator.
func (p *Peer) Offer(ctx context.Context) (webrtc.SessionDescription, error) {
	if err := ctx.Err(); err != nil {
		return webrtc.SessionDescription{}, err
	}
	d, err := p.pc.CreateOffer(nil)
	if err != nil {
		return webrtc.SessionDescription{}, ErrPeer
	}
	return p.local(ctx, d)
}

func (p *Peer) Answer(ctx context.Context, offer webrtc.SessionDescription) (webrtc.SessionDescription, error) {
	if err := ctx.Err(); err != nil {
		return webrtc.SessionDescription{}, err
	}
	if offer.Type != webrtc.SDPTypeOffer || len(offer.SDP) > maxDescriptionSize {
		return webrtc.SessionDescription{}, ErrDescription
	}
	if err := p.pc.SetRemoteDescription(offer); err != nil {
		return webrtc.SessionDescription{}, ErrDescription
	}
	d, err := p.pc.CreateAnswer(nil)
	if err != nil {
		return webrtc.SessionDescription{}, ErrPeer
	}
	return p.local(ctx, d)
}

func (p *Peer) ApplyAnswer(answer webrtc.SessionDescription) error {
	if answer.Type != webrtc.SDPTypeAnswer || len(answer.SDP) > maxDescriptionSize {
		return ErrDescription
	}
	if err := p.pc.SetRemoteDescription(answer); err != nil {
		return ErrDescription
	}
	return nil
}

func (p *Peer) local(ctx context.Context, d webrtc.SessionDescription) (webrtc.SessionDescription, error) {
	complete := webrtc.GatheringCompletePromise(p.pc)
	if err := p.pc.SetLocalDescription(d); err != nil {
		return webrtc.SessionDescription{}, ErrPeer
	}
	select {
	case <-ctx.Done():
		_ = p.Close()
		return webrtc.SessionDescription{}, ctx.Err()
	case <-p.done:
		return webrtc.SessionDescription{}, ErrClosed
	case <-complete:
		d := p.pc.LocalDescription()
		if d == nil {
			return webrtc.SessionDescription{}, ErrPeer
		}
		return *d, nil
	}
}

func (p *Peer) Done() <-chan struct{} { return p.done }

// Close is idempotent and unblocks application readers and writers immediately.
// It may run in a Pion callback; therefore it uses non-graceful Pion Close.
func (p *Peer) Close() error {
	p.once.Do(func() {
		close(p.done)
		p.mu.Lock()
		for _, ch := range p.channels {
			ch.finish(ErrClosed)
		}
		p.mu.Unlock()
		_ = p.pc.Close()
	})
	return nil
}
