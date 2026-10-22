// Package copycli implements literal local/remote SCP operands over SFTP.
// It never interpolates filenames in remote shell commands.
package copycli

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/pkg/sftp"
	"pi-sh/internal/connpolicy"
	"pi-sh/internal/sftpcli"
	"pi-sh/internal/sshdest"
)

type Operand struct{ Device, Path string }

// Parse follows OpenSSH scp operand syntax: [user@]host:path, [user@][addr]:path
// and scp://[user@]host[:port][/path]. A user, a port or an address literal
// has no Connect equivalent and is refused explicitly.
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
	switch {
	case d.User != "":
		return Operand{}, connpolicy.ErrUser
	case d.Port != "":
		return Operand{}, connpolicy.ErrPort
	case strings.ContainsAny(d.Host, "\r\n\t"):
		return Operand{}, errors.New("copy remote operand requires an exact Connect device/alias")
	}
	if err := connpolicy.Target(d.Host); err != nil {
		return Operand{}, err
	}
	return Operand{Device: d.Host, Path: d.Path}, nil
}

type Plan struct {
	Sources   []Operand
	Target    Operand
	Device    string
	Upload    bool
	Preserve  bool
	Recursive bool
	Errors    io.Writer
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
			if p.Device != "" && p.Device != source.Device {
				return nil, errors.New("download sources must use the same Connect device/alias")
			}
			p.Device = source.Device
		}
		p.Sources = append(p.Sources, source)
	}
	if p.Upload {
		p.Device = p.Target.Device
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

// Run copies each operand in order and stops at the first failing operand.
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
	console := sftpcli.Console{Client: client, RemoteUmask: remoteUmask, Preserve: p.Preserve, Errors: p.Errors, Program: "pi-cp"}
	for _, source := range p.Sources {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var err error
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
			return err
		}
	}
	return ctx.Err()
}
