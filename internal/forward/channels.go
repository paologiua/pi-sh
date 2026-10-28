package forward

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"sync"

	"pi-sh/internal/forwardspec"
)

// Channels numbers channels as ssh's channel table does: every listening
// socket, the session and each forwarded connection take the lowest free id.
// Listening sockets are not listed by ~#, as in ssh. A nil *Channels is valid
// and lists nothing.
type Channels struct {
	mu      sync.Mutex
	used    map[int]bool
	listed  map[int]string
	changed chan struct{} // closed and replaced by Release
}

func NewChannels() *Channels {
	return &Channels{used: map[int]bool{}, listed: map[int]string{}, changed: make(chan struct{})}
}

func (c *Channels) alloc() int {
	id := 0
	for c.used[id] {
		id++
	}
	c.used[id] = true
	return id
}

// Reserve takes an id for a listening socket.
func (c *Channels) Reserve() int {
	if c == nil {
		return -1
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.alloc()
}

// Open takes an id for a listed channel with ssh's description.
func (c *Channels) Open(desc string) int {
	if c == nil {
		return -1
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.alloc()
	c.listed[id] = desc
	return id
}

// Describe replaces a listed channel's description.
func (c *Channels) Describe(id int, desc string) {
	if c == nil || id < 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.listed[id]; ok {
		c.listed[id] = desc
	}
}

// Release frees an id.
func (c *Channels) Release(id int) {
	if c == nil || id < 0 {
		return
	}
	c.mu.Lock()
	delete(c.used, id)
	delete(c.listed, id)
	close(c.changed)
	c.changed = make(chan struct{})
	c.mu.Unlock()
}

// ErrWaitStopped reports that WaitIdle ended because stop was closed.
var ErrWaitStopped = errors.New("forwarding stopped")

// WaitIdle returns nil once no listed channel remains, as ssh's client loop
// ends only when no channel is open; ctx or stop end it early.
func (c *Channels) WaitIdle(ctx context.Context, stop <-chan struct{}) error {
	if c == nil {
		return nil
	}
	for {
		c.mu.Lock()
		n, changed := len(c.listed), c.changed
		c.mu.Unlock()
		if n == 0 {
			return nil
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		case <-stop:
			return ErrWaitStopped
		}
	}
}

// Len is the number of listed channels.
func (c *Channels) Len() int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.listed)
}

// List returns ssh's ~# lines in id order, each "  #N description".
func (c *Channels) List() []string {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	ids := make([]int, 0, len(c.listed))
	for id := range c.listed {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = fmt.Sprintf("  #%d %s", id, c.listed[id])
	}
	return out
}

// carrierStatus replaces the internal state ssh prints in parentheses
// (descriptor numbers and channel flags, which pi-sh does not have).
const carrierStatus = " (Connect forwarding carrier)"

// hostPort splits a socket address as ssh prints it: a Unix-domain peer has
// an empty host and port 0.
func hostPort(a net.Addr) (string, int) {
	if a == nil {
		return "", 0
	}
	if a.Network() == "unix" {
		return "", 0
	}
	host, port, err := net.SplitHostPort(a.String())
	if err != nil {
		return a.String(), 0
	}
	p, _ := strconv.Atoi(port)
	return host, p
}

// localDescription is ssh's name for an accepted -L/-D connection
// (channel_connect_to_port/port_open_helper), observed on OpenSSH 10.2p1.
func localDescription(fwd forwardspec.Forward, target Target, conn net.Conn) string {
	kind, host, port := "direct-tcpip", target.Host, target.Port
	if target.Path != "" {
		kind, host, port = "direct-streamlocal@openssh.com", target.Path, forwardspec.PortStreamLocal
	}
	listenPort := fwd.Listen.Port
	originHost, originPort := hostPort(conn.RemoteAddr())
	localHost, localPort := hostPort(conn.LocalAddr())
	if fwd.Listen.Port == forwardspec.PortStreamLocal {
		listenPort, localHost, localPort = forwardspec.PortStreamLocal, fwd.Listen.Path, 0
	}
	return fmt.Sprintf("%s: listening port %d for %s port %d, connect from %s port %d to %s port %d%s",
		kind, listenPort, host, port, originHost, originPort, localHost, localPort, carrierStatus)
}
