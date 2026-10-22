package transport

import (
	"context"
	"io"
	"sync"

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
	err         error
	queuedBytes int
}

func newChannel(dc *webrtc.DataChannel) *Channel {
	c := &Channel{dc: dc, opened: make(chan struct{}), done: make(chan struct{}), received: make(chan Message, receiveQueueSize), low: make(chan struct{}, 1), sender: make(chan struct{}, 1)}
	dc.SetBufferedAmountLowThreshold(sendHighWater / 2)
	dc.OnBufferedAmountLow(func() {
		select {
		case c.low <- struct{}{}:
		default:
		}
	})
	dc.OnOpen(func() { c.openOnce.Do(func() { close(c.opened) }) })
	dc.OnClose(func() { c.finish(io.EOF) })
	dc.OnError(func(error) { c.finish(ErrPeer) })
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
	c.mu.Lock()
	if len(m.Data) > receiveByteLimit-c.queuedBytes {
		c.mu.Unlock()
		c.finish(ErrReceiveOverflow)
		_ = c.dc.Close()
		return
	}
	c.queuedBytes += len(m.Data)
	c.mu.Unlock()
	v := Message{Data: append([]byte(nil), m.Data...), Text: m.IsString}
	select {
	case c.received <- v:
	case <-c.done:
		c.consume(v)
	default:
		c.consume(v)
		c.finish(ErrReceiveOverflow)
		_ = c.dc.Close()
	}
}

func (c *Channel) consume(m Message) { c.mu.Lock(); c.queuedBytes -= len(m.Data); c.mu.Unlock() }

// Receive drains already queued messages before returning the terminal error.
// A slow consumer exhausts the bounded queue and fails the channel explicitly.
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
	c.once.Do(func() { c.mu.Lock(); c.err = err; c.mu.Unlock(); close(c.done) })
}

func (c *Channel) Close() error {
	c.finish(io.EOF)
	if err := c.dc.Close(); err != nil {
		return ErrPeer
	}
	return nil
}
