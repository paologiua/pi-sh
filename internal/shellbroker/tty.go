package shellbroker

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"pi-sh/internal/command"
	"pi-sh/internal/ttymode"
)

//go:embed tty.bash
var ttyBashSource string

//go:embed tty.pl
var ttyPerlSource string

// ttySource is the staged TTY adapter: tty.bash with the Perl supervisor as a
// single-quoted Bash word (tty.pl holds no apostrophe).
var ttySource = "_pt_perl_src='" + ttyPerlSource + "'\n" + ttyBashSource

// TTYPlan stages an adapter for the existing carrier PTY; it requests no new
// agent channel or subsystem. Local terminal modes and resize belong to callers.
type TTYPlan struct {
	plan   *Plan
	script string
}

type TTYSize struct{ Columns, Rows int }

// TTYSettings affect only the ephemeral carrier, never system configuration.
// Nil fields retain the carrier's existing value. Modes use portable meanings;
// platform-specific stty -g encodings must not be sent across OSes.
type TTYSettings struct {
	TERM  *string
	Size  *TTYSize
	Modes *ttymode.Settings
}

type TTYIO struct {
	Stdout io.Writer // merged remote stdout/stderr, with terminal transformations
	// Input starts only after remote terminal modes have been restored. It must
	// honor ctx for writes and arrange cancellation of any blocking local reads.
	// EOF means stop sending, not synthesize a terminal VEOF character.
	Input func(context.Context, Stream) error
	// IsInputTransportError identifies only a failed carrier send. Such a send
	// can race a completed command: drain the independent result reader briefly
	// before failing. Local input/diagnostic errors must not be classified here.
	IsInputTransportError func(error) bool
	// Ready runs after remote terminal restoration/initial sizing, before input
	// or output forwarding. It must return promptly and may start resize delivery.
	Ready func() error
	// StopIO must promptly unblock borrowed input/output on cancellation. When
	// provided, Run joins its workers after invoking it, before returning. A
	// blocking arbitrary callback without this hook retains caller-owned lifetime.
	StopIO func()
}

func NewTTY(text string) (*TTYPlan, error) {
	return NewTTYConfigured(text, TTYSettings{})
}

func NewTTYConfigured(text string, settings TTYSettings) (*TTYPlan, error) {
	p, err := newPlan(text, true)
	if err != nil {
		return nil, err
	}
	modeArgs, err := settings.Modes.SttyArgs()
	if err != nil {
		return nil, err
	}
	script := "_ps_columns=; _ps_rows=; _ps_modes=("
	for _, arg := range modeArgs {
		script += " " + quote(arg)
	}
	script += " );\n"
	if settings.TERM != nil {
		value := *settings.TERM
		if len(value) > 256 || !utf8.ValidString(value) || strings.IndexFunc(value, func(c rune) bool { return c < 32 || c == 127 }) >= 0 {
			return nil, errors.New("TERM must be UTF-8 text without controls and at most 256 bytes")
		}
		script += "export TERM=" + quote(value) + "\n"
	}
	if settings.Size != nil {
		s := *settings.Size
		if s.Columns < 0 || s.Columns > 65535 || s.Rows < 0 || s.Rows > 65535 {
			return nil, errors.New("terminal dimensions must be between 0 and 65535")
		}
		script += fmt.Sprintf("_ps_columns=%d; _ps_rows=%d\n", s.Columns, s.Rows)
	}
	// PI_SH_REMOTE_IMPL=bash keeps the Bash supervisor (docs/TESTING.md).
	if BashOnly() {
		script += "_pt_bash_impl=1\n"
	}
	return &TTYPlan{plan: p, script: script + ttySource}, nil
}

type ttyResult struct {
	code int
	err  error
}

type pendingTTYStream struct {
	Stream
	pending []byte
}

func (s *pendingTTYStream) Receive(ctx context.Context) ([]byte, error) {
	if len(s.pending) != 0 {
		b := s.pending
		s.pending = nil
		return b, nil
	}
	return s.Stream.Receive(ctx)
}

// Run owns no terminal or carrier. Post-commit failure explicitly reports that
// remote cleanup is unconfirmed: terminal input cannot safely carry a broker C
// record. Direct local HUP tests do not establish the agent's peer-close behavior.
func (p *TTYPlan) Run(ctx context.Context, stream Stream, stdio TTYIO) (int, error) {
	ctx, cancel := context.WithCancel(ctx)
	var workers sync.WaitGroup
	defer func() {
		cancel()
		if stdio.StopIO != nil {
			stdio.StopIO()
			workers.Wait()
		}
	}()
	w, err := p.plan.stage(ctx, stream, p.script)
	if err != nil {
		return 0, err
	}
	if err := w.send(fmt.Sprintf("G %d %d", len(p.script), len(p.plan.text))); err != nil {
		return 0, errors.Join(err, ErrCleanupUnconfirmed)
	}
	ready := make(chan struct{})
	results := make(chan ttyResult, 1)
	workers.Add(1)
	go func() {
		defer workers.Done()
		code, err := command.ReadResult(ctx, &pendingTTYStream{Stream: stream, pending: w.pending}, stdio.Stdout,
			[]byte(p.plan.prefix+"TR\x1f"), []byte(p.plan.prefix+"TX:"), 5*time.Second,
			func() error {
				if stdio.Ready != nil {
					if err := stdio.Ready(); err != nil {
						return err
					}
				}
				close(ready)
				return nil
			})
		results <- ttyResult{code, err}
	}()
	inputError := make(chan error, 1)
	if stdio.Input != nil {
		workers.Add(1)
		go func() {
			defer workers.Done()
			select {
			case <-ready:
			case <-ctx.Done():
				return
			}
			if ctx.Err() != nil {
				return
			}
			err := stdio.Input(ctx, stream)
			if err != nil && !errors.Is(err, io.EOF) {
				inputError <- err
			}
		}()
	}
	select {
	case result := <-results:
		if ctx.Err() != nil {
			return 0, errors.Join(ctx.Err(), ErrCleanupUnconfirmed)
		}
		if result.err != nil {
			return 0, errors.Join(result.err, ErrCleanupUnconfirmed)
		}
		return result.code, nil
	case err := <-inputError:
		if stdio.IsInputTransportError != nil && stdio.IsInputTransportError(err) {
			return drainTTYResult(ctx, results, err, time.Second)
		}
		return 0, errors.Join(err, ErrCleanupUnconfirmed)
	case <-ctx.Done():
		return 0, errors.Join(ctx.Err(), ErrCleanupUnconfirmed)
	}
}

// A send failure does not prove that a concurrently received command result
// failed. Only the existing parser's complete status AND clean EOF can win.
// Never infer completion from the error itself, and do not replay any bytes.
func drainTTYResult(ctx context.Context, results <-chan ttyResult, inputErr error, grace time.Duration) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, errors.Join(err, ErrCleanupUnconfirmed)
	}
	timer := time.NewTimer(grace)
	defer timer.Stop()
	select {
	case result := <-results:
		if err := ctx.Err(); err != nil {
			return 0, errors.Join(err, ErrCleanupUnconfirmed)
		}
		if result.err != nil {
			return 0, errors.Join(inputErr, result.err, ErrCleanupUnconfirmed)
		}
		return result.code, nil
	case <-ctx.Done():
		return 0, errors.Join(ctx.Err(), ErrCleanupUnconfirmed)
	case <-timer.C:
		if err := ctx.Err(); err != nil {
			return 0, errors.Join(err, ErrCleanupUnconfirmed)
		}
		return 0, errors.Join(inputErr, ErrCleanupUnconfirmed)
	}
}
