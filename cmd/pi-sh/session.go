package main

import (
	"context"
	"errors"
	"io"

	"pi-sh/internal/shellbroker"
	"pi-sh/internal/transport"
)

// Cleanup failure must not retain a status/escape that app would treat as
// success, or an EOF that the adapter would ignore as ordinary input completion.
func localCleanupResult(result, failure error) error {
	if failure == nil {
		return result
	}
	var status *commandStatus
	if errors.As(result, &status) || errors.Is(result, errEscape) || errors.Is(result, io.EOF) {
		if errors.Is(result, shellbroker.ErrCleanupUnconfirmed) {
			return errors.Join(failure, shellbroker.ErrCleanupUnconfirmed)
		}
		return failure
	}
	return errors.Join(result, failure)
}

type localTerminal interface {
	Done() <-chan struct{}
	Wait() error
}

// waitSession arbitrates terminal restoration, I/O and authorization failures.
func waitSession(ctx context.Context, cancel context.CancelFunc, tty localTerminal,
	inputError, outputError, resizeError <-chan error, authorizationFailure <-chan string) (result error) {
	intentional := false
	defer func() {
		if err := ctx.Err(); err != nil && !intentional {
			result = err
		}
	}()
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case err := <-inputError:
		if errors.Is(err, errEscape) {
			if cause := ctx.Err(); cause != nil {
				return cause
			}
			intentional = true
			cancel()
			// As ssh after ~.: no remote status exists, so app() exits 255.
			return errEscape
		}
		return err
	case err := <-outputError:
		if errors.Is(err, io.EOF) || errors.Is(err, transport.ErrClosed) {
			return errors.New("remote connection closed; Connect has not supplied a remote exit status")
		}
		return err
	case message := <-authorizationFailure:
		return errors.New("session authorization check failed: " + message)
	case err := <-resizeError:
		return err
	case <-tty.Done():
		// Done only certifies that restoration finished, not a successful
		// connection. Cancellation can make both select cases ready together.
		if err := errors.Join(tty.Wait(), ctx.Err()); err != nil {
			return err
		}
		return errors.New("local terminal stopped without a disconnect request")
	case <-ctx.Done():
		return ctx.Err()
	}
}
