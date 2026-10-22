// Package terminal manages only the local terminal. It defines no Connect
// framing, remote PTY allocation, shell protocol, or command execution behavior.
package terminal

import (
	"context"
	"errors"
	"os"
	"runtime"
	"sync"

	"golang.org/x/term"
)

var ErrNotTerminal = errors.New("input and display must be local terminals")

type Size struct{ Columns, Rows int }

// Session owns terminal modes until Close or context cancellation. Callers must
// keep input and display open, and must not start overlapping sessions on them.
// It never owns, reads, writes or closes the supplied file descriptors.
type Session struct {
	stop        chan struct{}
	done        chan struct{}
	sizes       chan Size
	once        sync.Once
	err         error // written before done closes, read only after receiving done
	suspends    chan suspendRequest
	stopProcess func(context.Context) error
}

// Open sets input raw and publishes display dimensions. Pass actual file
// handles, not assumed descriptor numbers: Windows console handles differ.
// input and display may be the same Unix TTY; Windows needs a console output
// handle for display sizing and virtual-terminal output mode.
func Open(ctx context.Context, input, display *os.File) (*Session, error) {
	defer runtime.KeepAlive(input)
	defer runtime.KeepAlive(display)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	inputFD, inputErr := descriptor(input)
	displayFD, displayErr := descriptor(display)
	if inputErr != nil || displayErr != nil || !term.IsTerminal(inputFD) || !term.IsTerminal(displayFD) {
		return nil, ErrNotTerminal
	}
	initial, err := dimensions(displayFD)
	if err != nil {
		return nil, err
	}
	restore, err := acquireRaw(inputFD, displayFD)
	if err != nil {
		return nil, err
	}
	events, stopEvents := resizeEvents()
	s := &Session{stop: make(chan struct{}), done: make(chan struct{}), sizes: make(chan Size, 1), suspends: make(chan suspendRequest), stopProcess: SuspendProcess}
	s.sizes <- initial
	go func() {
		defer func() {
			stopEvents()
			if restore != nil {
				s.err = errors.Join(s.err, restore())
			}
			// Borrowing descriptors must not allow os.File finalizers to close
			// them while the session still needs to restore their modes.
			runtime.KeepAlive(input)
			runtime.KeepAlive(display)
			close(s.sizes)
			close(s.done)
		}()
		last := initial
		// Requery once after subscription, so a resize during setup is not lost.
		update := func(force bool) bool {
			next, err := dimensions(displayFD)
			if err != nil {
				s.err = err
				return false
			}
			if force || next != last {
				// Keep the latest dimensions, even if the consumer is slow.
				select {
				case <-s.sizes:
				default:
				}
				s.sizes <- next
				last = next
			}
			return true
		}
		if !update(false) {
			return
		}
		for {
			select {
			case <-ctx.Done():
				return
			case <-s.stop:
				return
			case <-events:
				if !update(false) {
					return
				}
			case request := <-s.suspends:
				ending := func() error {
					if err := request.ctx.Err(); err != nil {
						return err
					}
					if err := ctx.Err(); err != nil {
						return err
					}
					select {
					case <-s.stop:
						return ErrClosed
					default:
						return nil
					}
				}
				if err := ending(); err != nil {
					request.result <- err
					return
				}
				if err := restore(); err != nil {
					s.err = err
					request.result <- err
					return
				}
				restore = nil
				if err := ending(); err != nil {
					request.result <- err
					return
				}
				if err := s.stopProcess(request.ctx); err != nil {
					if request.ctx.Err() == nil && ctx.Err() == nil {
						s.err = err
					}
					request.result <- err
					return
				}
				if err := ending(); err != nil {
					request.result <- err
					return
				}
				var err error
				restore, err = acquireRaw(inputFD, displayFD)
				if err != nil {
					s.err = err
					request.result <- err
					return
				}
				if !update(true) {
					request.result <- s.err
					return
				}
				request.result <- nil
			}
		}
	}()
	return s, nil
}

func acquireRaw(inputFD, displayFD int) (func() error, error) {
	state, err := term.MakeRaw(inputFD)
	if err != nil {
		return nil, err
	}
	restoreOutput, err := prepareOutput(displayFD)
	if err != nil {
		return nil, errors.Join(err, term.Restore(inputFD, state))
	}
	return func() error { return errors.Join(restoreOutput(), term.Restore(inputFD, state)) }, nil
}

func dimensions(fd int) (Size, error) {
	w, h, err := term.GetSize(fd)
	if err != nil {
		return Size{}, err
	}
	return Size{Columns: w, Rows: h}, nil
}

// Sizes emits the current dimensions and subsequent changes, coalescing to the
// latest value. It closes when the session stops. No remote resize is sent here.
func (s *Session) Sizes() <-chan Size { return s.sizes }

// Done closes only after mode restoration was attempted and size watching stopped.
func (s *Session) Done() <-chan struct{} { return s.done }

// Wait waits for cancellation or Close and returns mode/query errors, if any.
// Context cancellation itself is an ordinary end of terminal ownership.
func (s *Session) Wait() error { <-s.done; return s.err }

// Close requests shutdown, waits for mode restoration, and returns any errors.
// It is safe to call concurrently and repeatedly. Descriptors must remain open
// until Close returns; os.Exit, SIGKILL and process crashes cannot run cleanup.
func (s *Session) Close() error {
	s.once.Do(func() { close(s.stop) })
	return s.Wait()
}
