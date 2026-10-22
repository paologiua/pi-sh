package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"unicode/utf8"

	"pi-sh/internal/localio"
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
			if c == '.' || c == '?' || c == 26 {
				if err := flush(); err != nil {
					return err
				}
			}
			if c == '.' {
				return errEscape
			}
			if c == '?' {
				label := escapeLabel(escape)
				help := fmt.Sprintf("%s?\r\nSupported escape sequences:\r\n %s. - disconnect\r\n %s? - this message\r\n", label, label, label)
				if actions.suspend != nil {
					help += fmt.Sprintf(" %s^Z - suspend pi-sh\r\n", label)
				}
				help += fmt.Sprintf(" %s%s - send a literal escape character\r\nEscapes apply at the start of a line.\r\n", label, label)
				if err := writeEscapeDiagnostic(ctx, actions.diagnostic, help); err != nil {
					return err
				}
				// Local actions consume no remote byte and preserve line-start
				// state, so another escape can immediately follow the help.
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
