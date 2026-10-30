// Package copycli implements literal local/remote SCP operands over SFTP.
// It never interpolates filenames in remote shell commands.
package copycli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/pkg/sftp"
	"pi-sh/internal/account"
	"pi-sh/internal/connpolicy"
	"pi-sh/internal/progress"
	"pi-sh/internal/sftpcli"
	"pi-sh/internal/sshdest"
)

// ErrLocalSource marks a missing local upload operand. As with scp it ends
// the copy, and pi-cp exits 255.
var ErrLocalSource = errors.New("stat local")

// Operand is a local path (Device empty) or a remote device path. Account is
// the Raspberry Pi ID account from email::device:path or an scp:// user.
type Operand struct{ Device, Path, Account string }

// Parse follows OpenSSH scp operand syntax: [user@]host:path, [user@][addr]:path
// and scp://[user@]host[:port][/path], plus the pi-sh account form
// email::device:path. A Unix-style user, a port or an address literal has no
// Connect equivalent and is refused explicitly. A URI user is the account
// when it is an email address (percent-encode '@' as %40).
func Parse(s string) (Operand, error) {
	if s == "" || strings.IndexByte(s, 0) >= 0 {
		return Operand{}, errors.New("empty or invalid copy operand")
	}
	d, remote, err := sshdest.Copy(s)
	if err != nil {
		return Operand{}, err
	}
	if !remote {
		return Operand{Path: s}, nil
	}
	if err := connpolicy.DestinationUser(d); err != nil {
		return Operand{}, err
	}
	switch {
	case d.User != "" && account.Validate(d.User) != nil:
		return Operand{}, connpolicy.ErrUser
	case d.Port != "":
		return Operand{}, connpolicy.ErrPort
	case strings.ContainsAny(d.Host, "\r\n\t"):
		return Operand{}, errors.New("copy remote operand requires an exact Connect device/alias")
	}
	if err := connpolicy.Target(d.Host); err != nil {
		return Operand{}, err
	}
	return Operand{Device: d.Host, Path: d.Path, Account: d.User}, nil
}

type Plan struct {
	Sources []Operand
	Target  Operand
	Device  string
	// Account is the remote operands' common account, or "" (see Parse).
	Account              string
	Upload               bool
	Preserve             bool
	Recursive            bool
	Errors               io.Writer
	Progress             *progress.Display
	Bandwidth            uint64
	BufferSize, Requests int
}

func New(args []string) (*Plan, error) {
	if len(args) < 2 {
		return nil, errors.New("copy requires source(s) and destination")
	}
	p := &Plan{}
	var err error
	p.Target, err = Parse(args[len(args)-1])
	if err != nil {
		return nil, err
	}
	p.Upload = p.Target.Device != ""
	for _, text := range args[:len(args)-1] {
		source, err := Parse(text)
		if err != nil {
			return nil, err
		}
		if p.Upload && source.Device != "" {
			return nil, errors.New("remote-to-remote copy is not implemented")
		}
		if !p.Upload && source.Device == "" {
			return nil, errors.New("local-only and mixed-direction copies are not implemented")
		}
		if !p.Upload {
			if p.Device != "" && (p.Device != source.Device || p.Account != source.Account) {
				return nil, errors.New("download sources must use the same Connect device/alias and account")
			}
			p.Device, p.Account = source.Device, source.Account
		}
		p.Sources = append(p.Sources, source)
	}
	if p.Upload {
		p.Device, p.Account = p.Target.Device, p.Target.Account
	}
	// Reject unsupported ~user remote paths before any login.
	remote := p.Sources
	if p.Upload {
		remote = []Operand{p.Target}
	}
	for _, operand := range remote {
		if _, err := remotePath(operand.Path); err != nil {
			return nil, err
		}
	}
	return p, nil
}

// Run copies each operand in order. Like scp, a failing operand is reported
// and the rest are still copied; the last failure is returned. A lost
// connection or cancellation stops the copy.
// Remote download operands are globs like scp's SFTP mode; local operands and
// the remote upload target are literal (a local shell expands local globs).
// Remote paths get scp's "~" and "~/" handling.
func (p *Plan) Run(ctx context.Context, client *sftp.Client, remoteUmask *os.FileMode) error {
	target := p.Target.Path
	if p.Upload {
		var err error
		if target, err = remotePath(target); err != nil {
			return err
		}
	}
	if len(p.Sources) > 1 {
		var info os.FileInfo
		var err error
		if p.Upload {
			info, err = client.Stat(target)
		} else {
			info, err = os.Stat(target)
		}
		if err != nil || !info.IsDir() {
			return errors.New("multiple sources require an existing destination directory")
		}
	}
	console := sftpcli.Console{Client: client, BufferSize: p.BufferSize, Requests: p.Requests, Context: ctx, Bandwidth: p.Bandwidth, RemoteUmask: remoteUmask, Preserve: p.Preserve, Errors: p.Errors, Program: "pi-cp", Progress: p.Progress}
	var last error
	for _, source := range p.Sources {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var err error
		if p.Upload {
			// scp's SFTP-mode upload stats each local operand in turn and
			// ends the copy when one is missing.
			if _, serr := os.Stat(source.Path); serr != nil {
				var pe *os.PathError
				if errors.As(serr, &pe) {
					serr = pe.Err
				}
				if last != nil {
					p.report(last)
				}
				return fmt.Errorf("%w \"%s\": %w", ErrLocalSource, display(source.Path), serr)
			}
		}
		switch {
		case !p.Upload:
			err = p.download(ctx, client, &console, source.Path)
		case p.Recursive:
			err = console.CopyTree(ctx, true, source.Path, target)
		default:
			err = console.CopyFile(true, source.Path, target)
		}
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			// A lost connection ends the copy; any other failure is reported
			// and the next operand is tried, as scp does.
			if errors.Is(err, sftp.ErrSSHFxConnectionLost) {
				return err
			}
			if last != nil {
				p.report(last)
			}
			last = err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return last
}
