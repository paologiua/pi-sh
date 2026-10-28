package transport

import (
	"context"
	"errors"
	"io"
	"sync"

	"github.com/pion/sctp"
	"github.com/pion/webrtc/v4"
)

const receiveQueueSize = 4096
const receiveByteLimit = 256 * 1024
const sendHighWater = 256 * 1024

// Message preserves WebRTC message boundaries and its text/binary distinction.
// The caller owns Data. This type deliberately does not imply a byte stream.
type Message struct {
	Data []byte
	Text bool
}

type Channel struct {
	dc          *webrtc.DataChannel
	opened      chan struct{}
	done        chan struct{}
	received    chan Message
	low         chan struct{}
	sender      chan struct{}
	once        sync.Once
	openOnce    sync.Once
	mu          sync.Mutex
	room        *sync.Cond // signalled when queuedBytes falls or the channel ends
	closed      bool
	err         error
	queuedBytes int
}

func newChannel(dc *webrtc.DataChannel) *Channel {
	c := &Channel{dc: dc, opened: make(chan struct{}), done: make(chan struct{}), received: make(chan Message, receiveQueueSize), low: make(chan struct{}, 1), sender: make(chan struct{}, 1)}
	c.room = sync.NewCond(&c.mu)
	dc.SetBufferedAmountLowThreshold(sendHighWater / 2)
	dc.OnBufferedAmountLow(func() {
		select {
		case c.low <- struct{}{}:
		default:
		}
	})
	dc.OnOpen(func() { c.openOnce.Do(func() { close(c.opened) }) })
	dc.OnClose(func() { c.finish(io.EOF) })
	// The device agent ends a session by closing its peer connection, an SCTP
	// ABORT with a User Initiated Abort cause. That is a close, not a failure:
	// Pion runs OnError and OnClose in separate goroutines, so it must not
	// depend on which one finishes the channel first.
	dc.OnError(func(err error) {
		if errors.Is(err, sctp.ErrUserInitiatedAbort) {
			c.finish(io.EOF)
			return
		}
		c.finish(ErrPeer)
	})
	dc.OnMessage(c.receive)
	return c
}

func (c *Channel) Label() string { return c.dc.Label() }

func (c *Channel) receive(m webrtc.DataChannelMessage) {
	select {
	case <-c.done:
		return
	default:
	}
	if len(m.Data) > MaxMessageSize {
		c.finish(ErrMessageSize)
		_ = c.dc.Close()
		return
	}
	// A full queue waits for the consumer instead of failing the channel: Pion
	// then stops reading this SCTP stream, the receive window closes and the
	// peer slows down, as an ssh channel window would. Failing here ended
	// sessions whose device sent faster than a burst could be consumed
	// (64 forwarded downloads, 2026-10-28). The queue stays bounded by
	// receiveByteLimit and receiveQueueSize.
	c.mu.Lock()
	for c.queuedBytes > 0 && len(m.Data) > receiveByteLimit-c.queuedBytes && !c.closed {
		c.room.Wait()
	}
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.queuedBytes += len(m.Data)
	c.mu.Unlock()
	v := Message{Data: append([]byte(nil), m.Data...), Text: m.IsString}
	select {
	case c.received <- v:
	case <-c.done:
		c.consume(v)
	}
}

func (c *Channel) consume(m Message) {
	c.mu.Lock()
	c.queuedBytes -= len(m.Data)
	c.room.Broadcast()
	c.mu.Unlock()
}

// Receive drains already queued messages before returning the terminal error.
// A slow consumer holds the sender back (see receive); nothing is dropped.
func (c *Channel) Receive(ctx context.Context) (Message, error) {
	if err := ctx.Err(); err != nil {
		return Message{}, err
	}
	select {
	case m := <-c.received:
		c.consume(m)
		return m, nil
	default:
	}
	select {
	case m := <-c.received:
		c.consume(m)
		return m, nil
	case <-ctx.Done():
		return Message{}, ctx.Err()
	case <-c.done:
		select {
		case m := <-c.received:
			c.consume(m)
			return m, nil
		default:
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		return Message{}, c.err
	}
}

// Send bounds application buffering, waits for the channel to open and respects
// cancellation while waiting for a sender or buffer capacity. A successful call
// queues one message locally; it is not an acknowledgement of remote processing.
func (c *Channel) Send(ctx context.Context, m Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(m.Data) > MaxSendSize {
		return ErrMessageSize
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return ErrClosed
	case <-c.opened:
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return ErrClosed
	case c.sender <- struct{}{}:
	}
	defer func() { <-c.sender }()
	for c.dc.BufferedAmount()+uint64(len(m.Data)) > sendHighWater {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-c.done:
			return ErrClosed
		case <-c.low:
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-c.done:
		return ErrClosed
	default:
	}
	var err error
	if m.Text {
		err = c.dc.SendText(string(m.Data))
	} else {
		err = c.dc.Send(m.Data)
	}
	if err != nil {
		return ErrPeer
	}
	return nil
}

func (c *Channel) finish(err error) {
	c.once.Do(func() {
		c.mu.Lock()
		c.err, c.closed = err, true
		c.room.Broadcast()
		c.mu.Unlock()
		close(c.done)
	})
}

func (c *Channel) Close() error {
	c.finish(io.EOF)
	if err := c.dc.Close(); err != nil {
		return ErrPeer
	}
	return nil
}
