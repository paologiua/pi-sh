package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"pi-sh/internal/diag"
	"pi-sh/internal/localio"
	"pi-sh/internal/terminal"
	"pi-sh/internal/transport"
)

var errEscape = errors.New("local escape disconnect")

// Browser shell input is text. Reject invalid UTF-8 rather than silently
// replacing bytes; arbitrary binary stdin needs a separately verified path.
type textSender interface {
	Send(context.Context, transport.Message) error
}

func input(ctx context.Context, ch textSender, r io.Reader, escape rune, diagnostic io.Writer) (result error) {
	return inputWithActions(ctx, ch, r, escape, inputActions{diagnostic: diagnostic})
}

type inputActions struct {
	diagnostic io.Writer
	suspend    func(context.Context) error
	// interrupt makes the pending stdin Read return localio.ErrInterrupted.
	// With suspend, it lets an external SIGTSTP use the same handoff as ~^Z.
	interrupt func() bool
}

// suspendingReader performs external suspension requests at the read boundary,
// where no Read is pending. It never returns ErrInterrupted to its caller.
type suspendingReader struct {
	ctx     context.Context
	reader  io.Reader
	suspend func(context.Context) error
}

func (r suspendingReader) Read(p []byte) (int, error) {
	for {
		n, err := r.reader.Read(p)
		if !errors.Is(err, localio.ErrInterrupted) {
			return n, err
		}
		if err := r.suspend(r.ctx); err != nil {
			if cause := r.ctx.Err(); cause != nil {
				return 0, cause
			}
			return 0, err
		}
	}
}

// watchExternalSuspend routes SIGTSTP to the input boundary while this loop
// can perform the handoff. Outside that window the default stop action applies.
func watchExternalSuspend(ctx context.Context, actions inputActions) func() {
	if actions.suspend == nil || actions.interrupt == nil || !terminal.CanSuspend() {
		return func() {}
	}
	signals, stop := terminal.WatchSuspendSignals()
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		for {
			select {
			case <-signals:
				actions.interrupt()
			case <-done:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
	return func() { stop(); close(done); <-finished }
}

func writeEscapeDiagnostic(ctx context.Context, writer io.Writer, message string) error {
	if writer == nil {
		writer = io.Discard
	}
	n, err := io.WriteString(writer, message)
	if err != nil || n != len(message) {
		if cause := ctx.Err(); cause != nil {
			return cause
		}
		return errors.New("local escape diagnostic failed")
	}
	return nil
}

func inputWithActions(ctx context.Context, ch textSender, r io.Reader, escape rune, actions inputActions) (result error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	if file, ok := r.(*os.File); ok {
		streams, err := localio.Open(ctx, localio.Files{Stdin: file})
		if err != nil {
			return err
		}
		defer func() {
			result = localCleanupResult(result, streams.Close())
		}()
		r = streams.Stdin
	}
	if actions.suspend != nil && actions.interrupt != nil {
		defer watchExternalSuspend(ctx, actions)()
		r = suspendingReader{ctx: ctx, reader: r, suspend: actions.suspend}
	}
	reader := bufio.NewReader(r)
	// Coalesce only characters already available locally. One message per rune
	// can exhaust a peer's frame budget long before its byte budget on a paste.
	// Flush before a read could block; interactive keys never wait for a timer.
	const batchLimit = 4096
	var pending []byte
	flush := func() error {
		if len(pending) == 0 {
			return nil
		}
		data := pending
		pending = nil // Keep previously sent slices immutable for the sender.
		return ch.Send(ctx, transport.Message{Text: true, Data: data})
	}
	queue := func(c rune) error {
		if len(pending)+utf8.RuneLen(c) > batchLimit {
			if err := flush(); err != nil {
				return err
			}
		}
		pending = utf8.AppendRune(pending, c)
		return nil
	}
	lineStart, prefix := true, false
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if len(pending) != 0 {
			available, _ := reader.Peek(min(reader.Buffered(), utf8.UTFMax))
			if len(available) == 0 || !utf8.FullRune(available) {
				if err := flush(); err != nil {
					return err
				}
			}
		}
		c, n, err := reader.ReadRune()
		if cause := ctx.Err(); cause != nil {
			return cause
		}
		if err != nil {
			if errors.Is(err, io.EOF) && prefix {
				if e := queue(escape); e != nil {
					return e
				}
			}
			if e := flush(); e != nil {
				return e
			}
			return err
		}
		if c == utf8.RuneError && n == 1 {
			if err := flush(); err != nil {
				return err
			}
			return errors.New("binary stdin is not supported by the verified text-input path")
		}
		if prefix {
			prefix = false
			if c == '.' || c == '?' || c == 26 || localEscapes[c] {
				if err := flush(); err != nil {
					return err
				}
			}
			if c == '.' {
				return errEscape
			}
			if c == '?' {
				label := escapeLabel(escape)
				help := fmt.Sprintf("%s?\r\nSupported escape sequences:\r\n %s.   - disconnect\r\n %sV/v - decrease/increase verbosity (LogLevel)\r\n", label, label, label)
				if actions.suspend != nil {
					help += fmt.Sprintf(" %s^Z  - suspend pi-sh\r\n", label)
				}
				help += fmt.Sprintf(" %s#   - list open connections\r\n %s?   - this message\r\n %s%s  - send the escape character by typing it twice\r\n", label, label, label, label)
				help += fmt.Sprintf(" %sB %sC %sR %s& - refused: no equivalent over Connect\r\n", label, label, label, label)
				help += "(Note that escapes are only recognized immediately after newline.)\r\n"
				if err := writeEscapeDiagnostic(ctx, actions.diagnostic, help); err != nil {
					return err
				}
				// Local actions consume no remote byte and preserve line-start
				// state, so another escape can immediately follow the help.
				continue
			}
			if localEscapes[c] {
				if err := writeEscapeDiagnostic(ctx, actions.diagnostic, localEscape(ctx, escape, c)); err != nil {
					return err
				}
				continue
			}
			if c == 26 {
				if actions.suspend == nil {
					if err := writeEscapeDiagnostic(ctx, actions.diagnostic, "pi-sh: local suspension is unavailable\r\n"); err != nil {
						return err
					}
					continue
				}
				if err := writeEscapeDiagnostic(ctx, actions.diagnostic, fmt.Sprintf("%s^Z [suspend pi-sh]\r\n", escapeLabel(escape))); err != nil {
					return err
				}
				if err := actions.suspend(ctx); err != nil {
					if cause := ctx.Err(); cause != nil {
						return cause
					}
					return err
				}
				continue
			}
			if c != escape {
				if err := queue(escape); err != nil {
					return err
				}
			}
		} else if lineStart && c == escape {
			prefix = true
			continue
		}
		if err := queue(c); err != nil {
			return err
		}
		lineStart = c == '\n' || c == '\r'
	}
}

// localEscapes are consumed locally like OpenSSH's. Those without a Connect
// equivalent are refused with a notice instead of being sent as text.
var localEscapes = map[rune]bool{'B': true, 'C': true, 'R': true, 'V': true, 'v': true, '#': true, '&': true}

// localEscape performs ~V/~v/~# and returns the notice for every local action.
func localEscape(ctx context.Context, escape, c rune) string {
	label := escapeLabel(escape)
	switch c {
	case 'V', 'v':
		log := diag.From(ctx)
		if !log.OnStderr() {
			return fmt.Sprintf("%s%c [Logging to syslog or file]\r\n", label, c)
		}
		level := log.Level()
		if c == 'V' && level > diag.Quiet {
			level--
		}
		if c == 'v' && level < diag.Debug3 {
			level++
		}
		log.SetLevel(level)
		return fmt.Sprintf("%s%c [LogLevel %s]\r\n", label, c, level)
	case '#':
		// ssh's channel_open_message: listeners are not listed.
		return fmt.Sprintf("%s#\r\nThe following connections are open:\r\n%s\r\n", label, strings.Join(openChannels(ctx), "\r\n"))
	case 'C':
		// As OpenSSH with its default EnableEscapeCommandline=no.
		return "commandline disabled\r\n"
	case 'B':
		return fmt.Sprintf("%sB [BREAK is not supported: Connect has no break request]\r\n", label)
	case 'R':
		return fmt.Sprintf("%sR [rekeying is not supported: Connect DTLS has no client rekey request]\r\n", label)
	default: // '&'
		return fmt.Sprintf("%s& [backgrounding is not supported: pi-sh cannot fork]\r\n", label)
	}
}
