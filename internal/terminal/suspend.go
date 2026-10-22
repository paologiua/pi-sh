package terminal

import (
	"context"
	"errors"
)

var ErrSuspendUnsupported = errors.New("local process suspension is not supported")
var ErrClosed = errors.New("local terminal ownership has ended")

type suspendRequest struct {
	ctx    context.Context
	result chan error
}

// CanSuspend reports build support; actual stop semantics depend on job control.
func CanSuspend() bool { return canStopProcess }

// SuspendProcess requests suspension of only this process, never its group.
// Use Session.Suspend when the process owns raw terminal modes.
func SuspendProcess(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !canStopProcess {
		return ErrSuspendUnsupported
	}
	if err := stopProcess(); err != nil {
		return err
	}
	return ctx.Err()
}

// Suspend restores modes before stopping and captures the shell's current modes
// on continue. Its context prevents reacquisition after this operation ends.
func (s *Session) Suspend(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !CanSuspend() {
		return ErrSuspendUnsupported
	}
	request := suspendRequest{ctx: ctx, result: make(chan error, 1)}
	select {
	case s.suspends <- request:
	case <-s.done:
		return errors.Join(ErrClosed, s.Wait(), ctx.Err())
	case <-ctx.Done():
		return ctx.Err()
	}
	// Once delivered, join the transition so it cannot later change modes
	// after this caller has relinquished the borrowed files.
	select {
	case err := <-request.result:
		return err
	case <-s.done:
		select {
		case err := <-request.result:
			return err
		default:
			return errors.Join(ErrClosed, s.Wait(), ctx.Err())
		}
	}
}
