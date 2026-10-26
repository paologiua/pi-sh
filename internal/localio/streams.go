// Package localio coordinates cancellable borrowed standard-stream operations.
// Callers retain file ownership and must not close/use the same descriptors or
// change their flags until Close returns, except inside WithSuspended action.
// Open all aliases together in one set.
package localio

import (
	"context"
	"errors"
	"io"
	"os"
	"sync"
)

var ErrSetup = errors.New("local standard-stream setup failed")
var ErrRestore = errors.New("local file status flag restoration failed")
var ErrSuspendUnsupported = errors.New("local stream suspension is not supported")

type Files struct{ Stdin, Stdout, Stderr *os.File }

type Session struct {
	Stdin          io.Reader
	Stdout, Stderr io.Writer
	// Interruptible is true for the Unix readiness backend. Regular filesystem
	// syscalls retain OS latency limits; arbitrary blocking io.Reader/Writer
	// implementations are outside this API. Other platforms retain old I/O.
	Interruptible bool
	cancel        context.CancelFunc
	finish        func() error
	suspend       func(func() error) error
	interrupt     func()
	once          sync.Once
	err           error
}

// ErrInterrupted is returned by one stdin Read after InterruptRead. No input
// byte is consumed by that Read, and the session remains usable.
var ErrInterrupted = errors.New("local stdin read interrupted for a local action")

// InterruptRead makes the pending or next stdin Read return ErrInterrupted, so
// the single reader can reach an input boundary (for WithSuspended) without
// cancelling the session. Repeated requests before that Read coalesce. It
// reports false when the backend cannot interrupt a blocked Read.
func (s *Session) InterruptRead() bool {
	if s.interrupt == nil {
		return false
	}
	s.interrupt()
	return true
}

// Cancel wakes owned operations without restoring flags or closing caller files.
func (s *Session) Cancel() { s.cancel() }

// WithSuspended drains active file operations, restores borrowed status flags,
// calls action, then reacquires them. Call only at an input boundary with no
// pending Read: a pending read must complete or be cancelled before handover.
// Cancellation interrupts pollable output drain. Files must remain open, but
// their status flags may change during action and become the new baseline.
func (s *Session) WithSuspended(action func() error) error {
	if s.suspend == nil {
		return ErrSuspendUnsupported
	}
	return s.suspend(action)
}

// Close cancels and quiesces owned Unix descriptor operations before restoring
// flags. A caller's regular-filesystem syscall can still stall inside its kernel.
// It never closes caller files. Repeated/concurrent calls share one result.
func (s *Session) Close() error {
	s.once.Do(func() { s.cancel(); s.err = s.finish() })
	return s.err
}
