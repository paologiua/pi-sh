// Package sftpclient carries SFTP over the existing shell broker's binary pipes.
// It neither authenticates using SSH nor installs a server on the remote device.
package sftpclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/pkg/sftp"
	"pi-sh/internal/shellbroker"
)

type Runner func(context.Context, string, shellbroker.IO) (int, error)
type Options struct {
	Server   string
	ReadOnly bool
}
type result struct {
	code int
	err  error
}

var errProcessTimeout = errors.New("remote SFTP server did not exit within the shutdown grace")

// Session owns its protocol pipes, not the caller's transport or authentication.
type Session struct {
	Client *sftp.Client
	// OriginalUmask is captured before making only the server process's umask
	// private. Callers apply it to explicit file/directory permissions.
	OriginalUmask os.FileMode
	cancel        context.CancelFunc
	inR           *io.PipeReader
	inW           *io.PipeWriter
	outR          *io.PipeReader
	outW          *io.PipeWriter
	done          chan struct{}
	result        result
	once          sync.Once
	err           error
}

func ServerCommand(server string) (string, error) {
	return serverCommand(server, false)
}
func serverCommand(server string, readOnly bool) (string, error) {
	flags := "-e"
	if readOnly {
		flags += " -R"
	}
	if server == "" {
		return `for _ps_sftp in /usr/lib/openssh/sftp-server /usr/libexec/openssh/sftp-server /usr/libexec/sftp-server; do if test -x "$_ps_sftp"; then exec "$_ps_sftp" ` + flags + `; fi; done; exit 127`, nil
	}
	if !path.IsAbs(server) || !utf8.ValidString(server) || len(server) > 4096 || strings.IndexFunc(server, func(r rune) bool { return r < 32 || r == 127 }) >= 0 {
		return "", errors.New("SFTP server must be an existing absolute Unix executable path without control characters")
	}
	return "exec '" + strings.ReplaceAll(server, "'", "'\\''") + "' " + flags, nil
}

// Open negotiates SFTP v3. The caller must authenticate before invoking Open;
// negotiation is bounded to 30s and the process lifetime follows ctx thereafter.
func Open(ctx context.Context, run Runner, options Options) (*Session, error) {
	command, err := serverCommand(options.Server, options.ReadOnly)
	if err != nil {
		return nil, err
	}
	// The prefix is private setup metadata, consumed before SFTP negotiation.
	// No persistent setting changes: only this child server inherits umask 077.
	command = `printf 'PSU%s\n' "$(umask)" || exit 125; umask 077 || exit 125; ` + command
	owned, cancel := context.WithCancel(ctx)
	s := &Session{cancel: cancel, done: make(chan struct{})}
	s.inR, s.inW = io.Pipe()
	s.outR, s.outW = io.Pipe()
	go func() {
		code, err := run(owned, command, shellbroker.IO{Stdin: s.inR, Stdout: s.outW, Stderr: io.Discard})
		s.result = result{code, err}
		s.outW.CloseWithError(s.commandError())
		s.inR.CloseWithError(io.ErrClosedPipe)
		close(s.done)
	}()
	go func() {
		select {
		case <-owned.Done():
			s.breakPipes(owned.Err())
		case <-s.done:
		}
	}()
	timer := time.AfterFunc(30*time.Second, cancel)
	s.OriginalUmask, err = readUmask(s.outR)
	var client *sftp.Client
	if err == nil {
		client, err = sftp.NewClientPipe(s.outR, s.inW, sftp.MaxPacketChecked(1024), sftp.MaxConcurrentRequestsPerFile(4))
	}
	withinDeadline := timer.Stop()
	if err != nil || !withinDeadline || owned.Err() != nil {
		cancel()
		s.breakPipes(io.ErrClosedPipe)
		if client != nil {
			client.Close()
		}
		finish := s.wait(4 * time.Second)
		if ctx.Err() != nil {
			return nil, errors.Join(ctx.Err(), finish)
		}
		return nil, errors.Join(errors.New("SFTP negotiation failed; an existing compatible remote server is required"), finish)
	}
	s.Client = client
	return s, nil
}

func readUmask(r io.Reader) (os.FileMode, error) {
	var line [9]byte
	for n := 0; n < len(line); n++ {
		if _, err := io.ReadFull(r, line[n:n+1]); err != nil {
			return 0, err
		}
		if line[n] != '\n' {
			continue
		}
		text := string(line[:n])
		if !strings.HasPrefix(text, "PSU") || (len(text) != 6 && len(text) != 7) {
			break
		}
		digits := text[3:]
		if strings.IndexFunc(digits, func(r rune) bool { return r < '0' || r > '7' }) >= 0 {
			break
		}
		mask, err := strconv.ParseUint(digits, 8, 9)
		if err == nil {
			return os.FileMode(mask), nil
		}
		break
	}
	return 0, errors.New("invalid remote creation mask")
}

func (s *Session) commandError() error {
	if s.result.err != nil {
		return s.result.err
	}
	if s.result.code != 0 {
		return fmt.Errorf("remote SFTP server exited with status %d", s.result.code)
	}
	return nil
}

func (s *Session) breakPipes(err error) {
	s.inR.CloseWithError(err)
	s.inW.CloseWithError(err)
	s.outR.CloseWithError(err)
	s.outW.CloseWithError(err)
}

func (s *Session) wait(d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-s.done:
		return s.commandError()
	case <-timer.C:
		return errProcessTimeout
	}
}

// Close sends stdin EOF first, verifies command completion, and cancels with a
// bounded cleanup grace if the server does not exit. It is idempotent.
func (s *Session) Close() error {
	s.once.Do(func() {
		s.inW.Close()
		s.err = s.wait(3 * time.Second)
		if errors.Is(s.err, errProcessTimeout) {
			s.cancel()
			s.breakPipes(io.ErrClosedPipe)
			finish := s.wait(4 * time.Second)
			if errors.Is(finish, errProcessTimeout) {
				finish = shellbroker.ErrCleanupUnconfirmed
			}
			s.err = errors.Join(s.err, finish)
		}
		s.outR.Close()
		if s.Client != nil {
			s.Client.Close()
		}
		s.cancel()
		s.breakPipes(io.ErrClosedPipe)
	})
	return s.err
}

// SafeError discards server-controlled text, paths and packet payloads. Only a
// numeric protocol status or a fixed class reaches CLI diagnostics.
func SafeError(err error) error {
	if err == nil {
		return nil
	}
	for _, class := range []error{context.Canceled, context.DeadlineExceeded, fs.ErrNotExist, fs.ErrPermission, fs.ErrExist, fs.ErrInvalid} {
		if errors.Is(err, class) {
			return class
		}
	}
	var status *sftp.StatusError
	if errors.As(err, &status) {
		return fmt.Errorf("remote operation failed (SFTP status %d)", status.Code)
	}
	if errors.Is(err, io.EOF) {
		return io.EOF
	}
	return errors.New("file operation failed")
}
