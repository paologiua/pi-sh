//go:build darwin || linux

package localio

import (
	"context"
	"errors"
	"io"
	"os"
	"runtime"
	"sync"

	"golang.org/x/sys/unix"
)

type lease struct {
	file      *os.File
	fd, flags int
	changed   bool
}
type unixStreams struct {
	operations          sync.RWMutex
	ctx                 context.Context
	leases              []*lease
	endpoints           []*endpoint
	wakeRead, wakeWrite *os.File
	wakeFD              int
	woken               chan struct{}
}
type endpoint struct {
	owner *unixStreams
	fd    int
	mu    sync.Mutex
	// unpollable marks a valid descriptor that poll rejects with POLLNVAL, as
	// Darwin does for /dev/null. Its I/O is attempted directly instead.
	unpollable bool
}

// Open snapshots every distinct descriptor BEFORE changing any flag: stdin,
// stdout and stderr can alias the same open-file description. Independent
// wrappers would otherwise remember each other's temporary O_NONBLOCK setting.
func Open(ctx context.Context, files Files) (*Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	u := &unixStreams{}
	s := &Session{Interruptible: true}
	seen := make(map[int]*lease)
	for i, file := range []*os.File{files.Stdin, files.Stdout, files.Stderr} {
		if file == nil {
			continue
		}
		raw, err := file.SyscallConn()
		if err != nil {
			return nil, ErrSetup
		}
		fd, flags := -1, 0
		var controlErr error
		err = raw.Control(func(value uintptr) { fd = int(value); flags, controlErr = unix.FcntlInt(value, unix.F_GETFL, 0) })
		if err != nil || controlErr != nil {
			return nil, ErrSetup
		}
		if seen[fd] == nil {
			l := &lease{file: file, fd: fd, flags: flags}
			seen[fd] = l
			u.leases = append(u.leases, l)
		}
		e := &endpoint{owner: u, fd: fd}
		u.endpoints = append(u.endpoints, e)
		switch i {
		case 0:
			s.Stdin = e
		case 1:
			s.Stdout = e
		case 2:
			s.Stderr = e
		}
	}
	var err error
	u.wakeRead, u.wakeWrite, err = os.Pipe()
	if err != nil {
		return nil, ErrSetup
	}
	u.wakeFD = int(u.wakeRead.Fd())
	for _, l := range u.leases {
		if l.flags&unix.O_NONBLOCK != 0 {
			continue
		}
		if _, err := unix.FcntlInt(uintptr(l.fd), unix.F_SETFL, l.flags|unix.O_NONBLOCK); err != nil {
			restore := u.restore()
			u.wakeRead.Close()
			u.wakeWrite.Close()
			return nil, errors.Join(ErrSetup, restore)
		}
		l.changed = true
	}
	u.ctx, s.cancel = context.WithCancel(ctx)
	u.woken = make(chan struct{})
	context.AfterFunc(u.ctx, func() {
		// One byte fits the private empty pipe; never drain it. All concurrent
		// pollers must observe the same cancellation readiness.
		_, _ = u.wakeWrite.Write([]byte{1})
		close(u.woken)
	})
	s.finish = func() error {
		<-u.woken
		u.operations.Lock()
		defer u.operations.Unlock()
		for _, e := range u.endpoints {
			e.mu.Lock()
		}
		defer func() {
			for i := len(u.endpoints) - 1; i >= 0; i-- {
				u.endpoints[i].mu.Unlock()
			}
		}()
		err := u.restore()
		return errors.Join(err, u.wakeRead.Close(), u.wakeWrite.Close())
	}
	s.suspend = func(action func() error) (result error) {
		// The input filter calls between reads. Writer preference prevents new
		// operations overtaking this drain while active writes finish normally.
		u.operations.Lock()
		defer u.operations.Unlock()
		if err := u.ctx.Err(); err != nil {
			return err
		}
		if err := u.restore(); err != nil {
			return err
		}
		if err := u.ctx.Err(); err != nil {
			return err
		}
		if err := action(); err != nil {
			return err
		}
		if err := u.ctx.Err(); err != nil {
			return err
		}
		// The local shell may have changed status flags while the client was
		// stopped. Snapshot every alias before acquiring any descriptor again.
		for _, l := range u.leases {
			flags, err := unix.FcntlInt(uintptr(l.fd), unix.F_GETFL, 0)
			if err != nil {
				return ErrSetup
			}
			l.flags = flags
		}
		for _, l := range u.leases {
			if l.flags&unix.O_NONBLOCK == 0 {
				if _, err := unix.FcntlInt(uintptr(l.fd), unix.F_SETFL, l.flags|unix.O_NONBLOCK); err != nil {
					return errors.Join(ErrSetup, u.restore())
				}
				l.changed = true
			}
		}
		return nil
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(err, s.Close())
	}
	return s, nil
}

func (u *unixStreams) restore() error {
	var result error
	for _, l := range u.leases {
		if l.changed {
			// Restore only the flag we own. I/O can change kernel bookkeeping
			// bits (e.g. Darwin FWASWRITTEN) without a caller changing flags.
			current, err := unix.FcntlInt(uintptr(l.fd), unix.F_GETFL, 0)
			if err == nil {
				_, err = unix.FcntlInt(uintptr(l.fd), unix.F_SETFL, (current&^unix.O_NONBLOCK)|(l.flags&unix.O_NONBLOCK))
			}
			if err != nil {
				result = ErrRestore
			} else {
				l.changed = false
			}
		}
		runtime.KeepAlive(l.file)
	}
	return result
}

func (e *endpoint) wait(events int16) error {
	for {
		if err := e.owner.ctx.Err(); err != nil {
			return err
		}
		if e.unpollable {
			return nil
		}
		fds := []unix.PollFd{{Fd: int32(e.fd), Events: events}, {Fd: int32(e.owner.wakeFD), Events: unix.POLLIN}}
		_, err := unix.Poll(fds, -1)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return err
		}
		if err := e.owner.ctx.Err(); err != nil {
			return err
		}
		if fds[0].Revents&unix.POLLNVAL != 0 {
			if _, err := unix.FcntlInt(uintptr(e.fd), unix.F_GETFL, 0); err != nil {
				return os.ErrClosed
			}
			e.unpollable = true
			return nil
		}
		if fds[0].Revents&(events|unix.POLLHUP|unix.POLLERR) != 0 {
			return nil
		}
	}
}

func (e *endpoint) Read(p []byte) (int, error) {
	e.owner.operations.RLock()
	defer e.owner.operations.RUnlock()
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(p) == 0 {
		return 0, nil
	}
	for {
		if err := e.wait(unix.POLLIN); err != nil {
			return 0, err
		}
		if err := e.owner.ctx.Err(); err != nil {
			return 0, err
		}
		n, err := unix.Read(e.fd, p)
		if err == unix.EINTR || err == unix.EAGAIN || err == unix.EWOULDBLOCK {
			e.pause(err)
			continue
		}
		if err != nil {
			return max(0, n), err
		}
		if n == 0 {
			return 0, io.EOF
		}
		return n, nil
	}
}

func (e *endpoint) Write(p []byte) (int, error) {
	e.owner.operations.RLock()
	defer e.owner.operations.RUnlock()
	e.mu.Lock()
	defer e.mu.Unlock()
	total := 0
	for total < len(p) {
		if err := e.wait(unix.POLLOUT); err != nil {
			return total, err
		}
		if err := e.owner.ctx.Err(); err != nil {
			return total, err
		}
		end := total + min(len(p)-total, 32*1024)
		n, err := unix.Write(e.fd, p[total:end])
		if n > 0 {
			total += n
		}
		if err == unix.EINTR || err == unix.EAGAIN || err == unix.EWOULDBLOCK {
			e.pause(err)
			continue
		}
		if err != nil {
			return total, err
		}
		if n == 0 {
			return total, io.ErrShortWrite
		}
	}
	return total, nil
}

// pause waits briefly after EAGAIN on an unpollable descriptor; cancellation
// still wakes it through the private pipe.
func (e *endpoint) pause(err error) {
	if !e.unpollable || err == unix.EINTR {
		return
	}
	_, _ = unix.Poll([]unix.PollFd{{Fd: int32(e.owner.wakeFD), Events: unix.POLLIN}}, 10)
}
