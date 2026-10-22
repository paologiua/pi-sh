// Package command implements an explicitly experimental command adapter over
// an existing interactive PTY. Its markers are application data, not Connect
// protocol messages or an independently authenticated remote exit-status channel.
package command

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const maxBootstrap = 2048 // Conservative bound below common PTY canonical-line limits.
const maxPreamble = 64 * 1024

var ErrStatus = errors.New("experimental command ended without a verified adapter status")

// Stream preserves the bytes of existing shell messages. The caller supplies
// authentication, authorization lifetime, cancellation and transport ownership.
type Stream interface {
	Send(context.Context, []byte) error
	Receive(context.Context) ([]byte, error)
}

type Plan struct {
	bootstrap string
	ready     []byte
	exit      []byte
	closeWait time.Duration
}

func New(text string) (*Plan, error) {
	if !utf8.ValidString(text) || strings.IndexFunc(text, func(c rune) bool { return c < 32 || c == 127 }) >= 0 {
		return nil, errors.New("experimental command requires UTF-8 text without literal control characters")
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, errors.New("command marker generation failed")
	}
	nonce := hex.EncodeToString(random[:])
	base := "PI_SH:" + nonce + ":"
	p := &Plan{ready: []byte("\x1e" + base + "READY\x1f"), exit: []byte("\x1e" + base + "EXIT:"), closeWait: 5 * time.Second}
	// Replace only this session's login shell with a noninteractive supervisor.
	// No trap is installed in the user's interactive shell. Disable history
	// persistence before replacement. PTY state changes live only for this PTY.
	// Fail before READY if line-ending/echo control is unavailable.
	script := "stty -echo -onlcr || exit 125; printf '\\036" + base + "READY\\037'; " +
		"/bin/sh -c " + quote(text) + " </dev/null; s=$?; printf '\\036" + base + "EXIT:%d\\037' \"$s\"; exit \"$s\""
	p.bootstrap = " unset HISTFILE; exec /bin/sh -c " + quote(script) + "\r"
	if len(p.bootstrap) > maxBootstrap {
		return nil, errors.New("experimental command exceeds conservative PTY bootstrap limit")
	}
	return p, nil
}

func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }

// Run forwards only output between READY and EXIT. It requires a valid framed
// status followed by EOF; truncation, framing ambiguity or cancellation fail
// closed. Output before a late failure may already have reached the caller.
// stdout/stderr remain merged, stdin is /dev/null, and commands still have a PTY.
func (p *Plan) Run(ctx context.Context, stream Stream, output io.Writer) (int, error) {
	if err := stream.Send(ctx, []byte(p.bootstrap)); err != nil {
		return 0, err
	}
	return p.readResult(ctx, stream, output, nil)
}

// ReadResult parses a caller-staged PTY adapter's bounded output/result frames.
// onReady runs once after readiness, before any application output is forwarded.
// It must return promptly; the caller owns terminal/input/transport lifecycle.
func ReadResult(ctx context.Context, stream Stream, output io.Writer, ready, exit []byte, closeWait time.Duration, onReady func() error) (int, error) {
	if len(ready) == 0 || len(ready) > 256 || len(exit) == 0 || len(exit) > 256 || bytes.Equal(ready, exit) || closeWait <= 0 || closeWait > time.Minute {
		return 0, errors.New("invalid adapter result framing")
	}
	p := &Plan{ready: bytes.Clone(ready), exit: bytes.Clone(exit), closeWait: closeWait}
	return p.readResult(ctx, stream, output, onReady)
}

func (p *Plan) readResult(ctx context.Context, stream Stream, output io.Writer, onReady func() error) (int, error) {
	if output == nil {
		output = io.Discard
	}
	var pending []byte
	started, finished := false, false
	discarded, status := 0, 0
	receiveCtx := ctx
	for {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		chunk, err := stream.Receive(receiveCtx)
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		if receiveCtx.Err() != nil {
			return 0, fmt.Errorf("%w: channel did not close after completion", ErrStatus)
		}
		if err != nil {
			if errors.Is(err, io.EOF) && started && finished {
				return status, nil
			}
			if errors.Is(err, io.EOF) {
				return 0, ErrStatus
			}
			return 0, err
		}
		if finished {
			if len(chunk) != 0 {
				return 0, ErrStatus
			}
			continue
		}
		pending = append(pending, chunk...)
		if !started {
			i := bytes.Index(pending, p.ready)
			if i < 0 {
				n := max(0, len(pending)-len(p.ready)+1)
				discarded += n
				pending = pending[n:]
				if discarded > maxPreamble {
					return 0, errors.New("command bootstrap output exceeded local limit")
				}
				continue
			}
			if discarded+i > maxPreamble {
				return 0, errors.New("command bootstrap output exceeded local limit")
			}
			pending = pending[i+len(p.ready):]
			started = true
			if onReady != nil {
				if err := onReady(); err != nil {
					return 0, err
				}
			}
		}
		i := bytes.Index(pending, p.exit)
		if i < 0 {
			// Retain only a possible partial marker. Holding an arbitrary marker-
			// sized tail deadlocks short interactive prompts waiting for input.
			keep := min(len(pending), len(p.exit)-1)
			for keep > 0 && !bytes.HasSuffix(pending, p.exit[:keep]) {
				keep--
			}
			n := len(pending) - keep
			if err := write(output, pending[:n]); err != nil {
				return 0, err
			}
			pending = pending[n:]
			continue
		}
		if err := write(output, pending[:i]); err != nil {
			return 0, err
		}
		pending = pending[i:]
		end := bytes.IndexByte(pending[len(p.exit):], '\x1f')
		if end < 0 {
			if len(pending) > len(p.exit)+3 {
				return 0, ErrStatus
			}
			continue
		}
		digits := pending[len(p.exit) : len(p.exit)+end]
		if len(digits) < 1 || len(digits) > 3 || bytes.IndexFunc(digits, func(c rune) bool { return c < '0' || c > '9' }) >= 0 {
			return 0, ErrStatus
		}
		status, err = strconv.Atoi(string(digits))
		if err != nil || status > 255 || strconv.Itoa(status) != string(digits) {
			return 0, ErrStatus
		}
		if len(pending) != len(p.exit)+end+1 {
			return 0, ErrStatus
		}
		pending = nil
		finished = true
		closeCtx, stop := context.WithTimeout(ctx, p.closeWait)
		defer stop() // Completion is parsed only once; release its timer on every return.
		receiveCtx = closeCtx
	}
}

func write(w io.Writer, b []byte) error {
	if len(b) == 0 {
		return nil
	}
	n, err := w.Write(b)
	if err != nil {
		return fmt.Errorf("command output write failed: %w", err)
	}
	if n != len(b) {
		return io.ErrShortWrite
	}
	return nil
}
