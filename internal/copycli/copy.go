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
	return p, nil
}

func (p *Plan) Run(ctx context.Context, client *sftp.Client, remoteUmask *os.FileMode) error {
	if len(p.Sources) > 1 {
		var info os.FileInfo
		var err error
		if p.Upload {
			info, err = client.Stat(p.Target.Path)
		} else {
			info, err = os.Stat(p.Target.Path)
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
		if p.Recursive {
			if err := console.CopyTree(ctx, p.Upload, source.Path, p.Target.Path); err != nil {
				return err
			}
			continue
		}
		name := "get"
		if p.Upload {
			name = "put"
		}
		if _, err := console.Execute(ctx, []string{name, "--", source.Path, p.Target.Path}); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
	}
	return ctx.Err()
}
