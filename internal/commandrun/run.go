// Package commandrun arbitrates remote completion, cancellation and authorization
// loss. It never turns cleanup or a connection close into command success.
package commandrun

import (
	"context"
	"errors"
	"fmt"
	"time"

	"pi-sh/internal/shellbroker"
)

// ErrAuthorization identifies a failed session check without inspecting or
// exposing its source-provided diagnostic text.
var ErrAuthorization = errors.New("session authorization check failed")

type outcome struct {
	code int
	err  error
}

func Run(ctx context.Context, failures <-chan string, run func(context.Context) (int, error), grace time.Duration) (int, error) {
	// A queued failure is already authoritative. Do not invoke an adapter (which
	// can send bootstrap/launch bytes) before checking known session failure.
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	select {
	case message := <-failures:
		return 0, fmt.Errorf("%w: %s", ErrAuthorization, message)
	default:
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	result := make(chan outcome, 1)
	go func() { code, err := run(ctx); result <- outcome{code, err} }()
	preserve := func(cause, err error) error {
		if errors.Is(err, shellbroker.ErrCleanupUnconfirmed) {
			return errors.Join(cause, shellbroker.ErrCleanupUnconfirmed)
		}
		return cause
	}
	stop := func(cause error) error {
		cancel()
		if grace <= 0 {
			return cause
		}
		timer := time.NewTimer(grace)
		defer timer.Stop()
		select {
		case r := <-result:
			return preserve(cause, r.err)
		case <-timer.C:
			return errors.Join(cause, shellbroker.ErrCleanupUnconfirmed)
		}
	}
	select {
	case r := <-result:
		if ctx.Err() != nil {
			return 0, preserve(ctx.Err(), r.err)
		}
		select {
		case message := <-failures:
			return 0, preserve(fmt.Errorf("%w: %s", ErrAuthorization, message), r.err)
		default:
		}
		return r.code, r.err
	case message := <-failures:
		return 0, stop(fmt.Errorf("%w: %s", ErrAuthorization, message))
	case <-ctx.Done():
		return 0, stop(ctx.Err())
	}
}
