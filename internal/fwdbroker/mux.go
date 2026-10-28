// Package fwdbroker multiplexes forwarded connections over one Connect shell
// carrier using an in-memory Bash adapter. Each connection runs a connector
// program that already exists on the device (see Connector); nothing is
// installed or written remotely. The framing is pi-sh application data inside
// the evidenced shell channel, not a native Connect forwarding protocol.
package fwdbroker

import (
	"context"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"pi-sh/internal/forward"
	"pi-sh/internal/shellbroker"
)

//go:embed forward.bash
var source string

const (
	// MaxStreams bounds concurrent remote connectors (and their processes).
	MaxStreams  = 64
	inputChunk  = 2048
	inputWindow = 8
	outWindow   = 8
	outChunk    = 2048
	maxStderr   = 4096
	// maxOpen and maxWord are the adapter's record and argv-word bounds
	// (forward.bash), checked so an oversized open fails that connection
	// instead of the adapter.
	maxOpen     = 4224
	maxWord     = 2048
	quitTimeout = 2 * time.Second
)

var (
	ErrProtocol    = errors.New("invalid or incomplete forwarding adapter exchange")
	ErrClosed      = errors.New("forwarding adapter closed")
	ErrNoConnector = errors.New("no supported connector (Perl with Socket, OpenBSD netcat, Nmap ncat or socat) is present on the device; pi-sh does not install software")
)

// Mux is a started adapter. It implements forward.Dialer.
type Mux struct {
	records   *shellbroker.Records
	connector Connector
	Detected  map[string][]string

	mu      sync.Mutex
	streams map[int]*stream
	next    int
	slots   chan struct{}
	done    chan struct{}
	err     error
	quit    chan struct{}
	once    sync.Once
}

// ErrStageTimeout reports a device that did not finish staging the adapter and
// detecting connectors in time.
var ErrStageTimeout = errors.New("forwarding adapter setup timed out")

// stageTimeout bounds staging and connector detection (a test may shorten it).
var stageTimeout = 30 * time.Second

// Start stages the adapter over the carrier and selects a connector. ctx must
// remain live while the Mux is used; setup (a signal, for example) or
// stageTimeout abandon the staging, after which the caller closes the
// carrier, which also ends the abandoned exchange. The carrier is
// caller-owned.
func Start(setup, ctx context.Context, carrier shellbroker.Stream) (*Mux, error) {
	type result struct {
		m   *Mux
		err error
	}
	done := make(chan result, 1)
	go func() {
		m, err := start(ctx, carrier)
		done <- result{m, err}
	}()
	timer := time.NewTimer(stageTimeout)
	defer timer.Stop()
	select {
	case r := <-done:
		return r.m, r.err
	case <-setup.Done():
		return nil, setup.Err()
	case <-timer.C:
		return nil, ErrStageTimeout
	}
}

func start(ctx context.Context, carrier shellbroker.Stream) (*Mux, error) {
	adapter, err := shellbroker.NewAdapter(source, []string{"cat"}, "forwarding adapter requires existing Bash >=4.3 and cat")
	if err != nil {
		return nil, err
	}
	records, err := adapter.Start(ctx, carrier)
	if err != nil {
		return nil, err
	}
	m := &Mux{records: records, Detected: map[string][]string{}, streams: map[int]*stream{},
		slots: make(chan struct{}, MaxStreams), done: make(chan struct{}), quit: make(chan struct{})}
	for {
		text, err := records.Next()
		if err != nil {
			return nil, err
		}
		if text == "R" {
			break
		}
		f := strings.Fields(text)
		if len(f) < 2 || len(f) > 3 || f[0] != "T" || len(m.Detected) > 8 {
			return nil, ErrProtocol
		}
		if len(f) == 3 {
			line, err := hex.DecodeString(f[2])
			if err != nil || len(m.Detected[f[1]]) >= 3 {
				return nil, ErrProtocol
			}
			m.Detected[f[1]] = append(m.Detected[f[1]], string(line))
		} else if _, ok := m.Detected[f[1]]; !ok {
			m.Detected[f[1]] = nil
		}
	}
	go m.receive()
	detected := m.Detected
	// Diagnostic override (docs/TESTING.md): consider only this tool, which
	// must still be recognized on the device.
	if tool := os.Getenv("PI_SH_FORWARD_CONNECTOR"); tool != "" {
		detected = map[string][]string{tool: m.Detected[tool]}
	}
	connector, ok := Select(detected)
	if !ok {
		m.Close()
		return nil, ErrNoConnector
	}
	m.connector = connector
	return m, nil
}

// Connector reports the selected remote connector.
func (m *Mux) Connector() Connector { return m.connector }

// Done is closed when the adapter or carrier fails or the Mux is closed.
func (m *Mux) Done() <-chan struct{} { return m.done }

// Err reports why Done was closed.
func (m *Mux) Err() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.err
}

func (m *Mux) fail(err error) {
	m.once.Do(func() {
		m.mu.Lock()
		m.err = err
		m.mu.Unlock()
		close(m.done)
	})
}

// Close asks the adapter to kill and reap every connector, waiting a bounded
// time for its confirmation. Streams fail afterwards.
func (m *Mux) Close() error {
	select {
	case <-m.done:
		return nil
	default:
	}
	err := m.records.Send("Q")
	if err == nil {
		select {
		case <-m.quit:
		case <-m.done:
		case <-time.After(quitTimeout):
			err = errors.New("forwarding adapter cleanup was not confirmed")
		}
	}
	m.fail(ErrClosed)
	return err
}

func (m *Mux) receive() {
	for {
		text, err := m.records.Next()
		if err != nil {
			m.fail(fmt.Errorf("forwarding carrier failed: %w", err))
			return
		}
		if text == "D" {
			close(m.quit)
			m.fail(ErrClosed)
			return
		}
		if err := m.dispatch(text); err != nil {
			m.fail(err)
			return
		}
	}
}

func (m *Mux) dispatch(text string) error {
	// The adapter's own failure ("Z protocol") has two fields; check it first.
	if reason, ok := strings.CutPrefix(text, "Z "); ok {
		if reason != "protocol" {
			reason = "unrecognized reason" // never echo device-chosen text
		}
		return errors.New("forwarding adapter failed: " + reason)
	}
	f := strings.Fields(text)
	if len(f) < 2 {
		return ErrProtocol
	}
	id, err := strconv.Atoi(f[1])
	if err != nil {
		return ErrProtocol
	}
	m.mu.Lock()
	s := m.streams[id]
	m.mu.Unlock()
	if s == nil {
		if f[0] == "d" && len(f) == 2 && id > 0 && id <= m.lastID() {
			return nil // a close crossed the connector's exit
		}
		return ErrProtocol
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case f[0] == "o" && len(f) == 4:
		seq, err := strconv.Atoi(f[2])
		data, herr := hex.DecodeString(f[3])
		if err != nil || herr != nil || len(data) == 0 || len(data) > outChunk || seq != s.outSeq+1 || s.outEOF {
			return ErrProtocol
		}
		s.outSeq = seq
		s.gotData = true
		select {
		case s.events <- data:
		default:
			return ErrProtocol // the adapter exceeded its window
		}
	case f[0] == "f" && len(f) == 2:
		if s.outEOF {
			return ErrProtocol
		}
		s.outEOF = true
		close(s.events)
	case f[0] == "e" && len(f) == 3:
		data, err := hex.DecodeString(f[2])
		if err != nil {
			return ErrProtocol
		}
		if len(s.stderr)+len(data) <= maxStderr {
			s.stderr = append(s.stderr, data...)
		}
		if !s.ready && s.readyText != "" && strings.Contains(string(s.stderr), s.readyText) {
			s.ready = true
			close(s.readyCh)
		}
	case f[0] == "a" && len(f) == 3:
		seq, err := strconv.Atoi(f[2])
		if err != nil || seq != s.inAcked+1 || seq > s.inSent {
			return ErrProtocol
		}
		s.inAcked = seq
		s.signal()
	case f[0] == "k" && len(f) == 2:
		if !s.inClosed {
			s.inClosed = true
			close(s.inClosedCh)
		}
	case f[0] == "x" && len(f) == 3:
		code, err := strconv.Atoi(f[2])
		if err != nil || code < 0 || code > 255 || s.exited {
			return ErrProtocol
		}
		s.status, s.exited = code, true
		close(s.exitCh)
		m.finish(s)
	case f[0] == "d" && len(f) == 2:
		m.finish(s)
	default:
		return ErrProtocol
	}
	return nil
}

func (m *Mux) lastID() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.next
}

// finish forgets an adapter-side stream (called with s.mu held).
func (m *Mux) finish(s *stream) {
	if s.released {
		return
	}
	s.released = true
	close(s.goneCh)
	if !s.inClosed {
		s.inClosed = true
		close(s.inClosedCh)
	}
	if !s.outEOF {
		s.outEOF = true
		close(s.events)
	}
	m.mu.Lock()
	delete(m.streams, s.id)
	m.mu.Unlock()
	<-m.slots
}

// Dial starts a connector for target. With a connector that reports
// readiness, Dial waits for it and returns an *forward.OpenError on failure.
func (m *Mux) Dial(ctx context.Context, target forward.Target) (forward.Stream, error) {
	argv, err := m.connector.Argv(target)
	if err != nil {
		return nil, &forward.OpenError{Detail: err.Error()}
	}
	s, err := m.start(ctx, argv, m.connector.ReadyText(target), m.connector.Hex)
	if err != nil {
		return nil, err // never a typed nil in the interface
	}
	return s, nil
}

// start runs argv as one stream; with readyText it waits for that stderr
// marker and reports an *forward.OpenError if the program exits first. A hex
// program reads and writes the adapter's hex records itself (forward.bash).
func (m *Mux) start(ctx context.Context, argv []string, readyText string, hexStdio bool) (*stream, error) {
	select {
	case m.slots <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-m.done:
		return nil, m.Err()
	}
	words := make([]string, len(argv))
	size := len("O 1234567890 r")
	for i, a := range argv {
		words[i] = hex.EncodeToString([]byte(a))
		size += 1 + len(words[i])
		if len(a) > maxWord {
			size = maxOpen + 1
		}
	}
	if size > maxOpen {
		<-m.slots
		return nil, &forward.OpenError{Detail: "remote target too long for the forwarding adapter"}
	}
	m.mu.Lock()
	m.next++
	s := &stream{mux: m, id: m.next, readyText: readyText, events: make(chan []byte, outWindow+1), readyCh: make(chan struct{}),
		exitCh: make(chan struct{}), inClosedCh: make(chan struct{}), goneCh: make(chan struct{}), wake: make(chan struct{}, 1)}
	m.streams[s.id] = s
	m.mu.Unlock()
	mode := "r"
	if hexStdio {
		mode = "h"
	}
	if err := m.records.Send(fmt.Sprintf("O %d %s %s", s.id, mode, strings.Join(words, " "))); err != nil {
		m.fail(err)
		return nil, err
	}
	if s.readyText == "" {
		return s, nil
	}
	select {
	case <-s.readyCh:
		return s, nil
	case <-s.exitCh:
		select {
		case <-s.readyCh:
			return s, nil
		default:
		}
		return nil, s.openError()
	case <-ctx.Done():
		s.Close()
		return nil, ctx.Err()
	case <-m.done:
		return nil, m.Err()
	}
}

type stream struct {
	mux       *Mux
	id        int
	readyText string

	mu                          sync.Mutex
	events                      chan []byte
	pending                     []byte
	outSeq, outRead             int
	outEOF, gotData, ready      bool
	readyCh, exitCh, inClosedCh chan struct{}
	goneCh                      chan struct{} // closed when the adapter forgot the stream
	inClosed, exited, released  bool
	status                      int
	stderr                      []byte
	inSent, inAcked             int
	wake                        chan struct{}
	wroteEOF, closed            bool
	writeMu                     sync.Mutex
}

func (s *stream) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *stream) openError() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return &forward.OpenError{Detail: classify(string(s.stderr), s.status)}
}

// Remote connector text is not echoed into local diagnostics; only these
// recognized system reasons (the ones ssh's server-side reason would carry)
// are reported.
var reasons = []string{
	"Connection refused", "Connection timed out", "No route to host", "Network is unreachable",
	"Name or service not known", "Temporary failure in name resolution", "No address associated with hostname",
	"No such file or directory", "Permission denied", "Connection reset by peer",
}

func classify(stderr string, status int) string {
	for _, r := range reasons {
		if strings.Contains(stderr, r) {
			return r
		}
	}
	if status == 127 {
		return "remote connector program not found"
	}
	return fmt.Sprintf("remote connector exited with status %d", status)
}

// Read delivers remote output; each block is acknowledged once fully read.
func (s *stream) Read(b []byte) (int, error) {
	if len(s.pending) == 0 {
		select {
		case data, ok := <-s.events:
			if !ok {
				return 0, s.eof()
			}
			s.pending = data
		case <-s.mux.done:
			return 0, s.mux.Err()
		}
	}
	n := copy(b, s.pending)
	s.pending = s.pending[n:]
	if len(s.pending) == 0 {
		s.outRead++
		s.mu.Lock()
		live := !s.released
		s.mu.Unlock()
		if live {
			if err := s.mux.records.Send(fmt.Sprintf("A %d %d", s.id, s.outRead)); err != nil {
				s.mux.fail(err)
			}
		}
	}
	return n, nil
}

// eof distinguishes a stream that opened from one whose connector failed.
func (s *stream) eof() error {
	s.mu.Lock()
	opened := s.ready || s.gotData
	s.mu.Unlock()
	if opened {
		return io.EOF
	}
	select {
	case <-s.exitCh:
	case <-s.goneCh:
		// Closed (C) before it opened or exited: nothing more will come.
		s.mu.Lock()
		exited := s.exited
		s.mu.Unlock()
		if !exited {
			return io.EOF
		}
	case <-s.mux.done:
		return s.mux.Err()
	}
	s.mu.Lock()
	failed := s.exited && s.status != 0
	s.mu.Unlock()
	if failed {
		return s.openError()
	}
	return io.EOF
}

var errInputClosed = errors.New("remote connector stopped reading input")

func (s *stream) Write(b []byte) (int, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	written := 0
	for len(b) > 0 {
		for {
			s.mu.Lock()
			closed, room := s.inClosed, s.inSent-s.inAcked < inputWindow
			s.mu.Unlock()
			if closed {
				return written, errInputClosed
			}
			if room {
				break
			}
			select {
			case <-s.wake:
			case <-s.inClosedCh:
			case <-s.mux.done:
				return written, s.mux.Err()
			}
		}
		chunk := b[:min(len(b), inputChunk)]
		s.mu.Lock()
		s.inSent++
		seq := s.inSent
		s.mu.Unlock()
		if err := s.mux.records.Send(fmt.Sprintf("I %d %d %d %s", s.id, seq, 2*len(chunk), hex.EncodeToString(chunk))); err != nil {
			s.mux.fail(err)
			return written, err
		}
		written += len(chunk)
		b = b[len(chunk):]
	}
	return written, nil
}

// CloseWrite sends EOF; the adapter closes the connector's stdin after all
// earlier input, and the connector shuts down the socket's write side.
func (s *stream) CloseWrite() error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.mu.Lock()
	skip := s.wroteEOF || s.inClosed
	s.wroteEOF = true
	s.mu.Unlock()
	if skip {
		return nil
	}
	return s.mux.records.Send(fmt.Sprintf("F %d", s.id))
}

// Close kills the connector unless it already exited. After EOF in both
// directions it asks the adapter to let the connector finish writing queued
// input first (bounded), as sshd drains a channel on CHANNEL_CLOSE.
func (s *stream) Close() error {
	s.mu.Lock()
	skip := s.closed || s.released
	graceful := s.wroteEOF && s.outEOF
	s.closed = true
	s.mu.Unlock()
	if skip {
		return nil
	}
	select {
	case <-s.mux.done:
		return nil
	default:
	}
	if graceful {
		return s.mux.records.Send(fmt.Sprintf("G %d", s.id))
	}
	return s.mux.records.Send(fmt.Sprintf("C %d", s.id))
}
