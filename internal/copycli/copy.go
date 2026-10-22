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
	"pi-sh/internal/sftpcli"
)

type Operand struct{ Device, Path string }

func Parse(s string) (Operand, error) {
	if s == "" || strings.IndexByte(s, 0) >= 0 {
		return Operand{}, errors.New("empty or invalid copy operand")
	}
	if strings.Contains(s, "://") {
		return Operand{}, errors.New("copy URIs are not implemented")
	}
	// Windows absolute drive paths are local even when a script is parsed on Unix.
	if len(s) >= 3 && s[1] == ':' && ((s[0] >= 'a' && s[0] <= 'z') || (s[0] >= 'A' && s[0] <= 'Z')) && (s[2] == '/' || s[2] == '\\') {
		return Operand{Path: s}, nil
	}
	colon := strings.IndexByte(s, ':')
	slash := strings.IndexByte(s, '/')
	if colon < 0 || (slash >= 0 && slash < colon) {
		return Operand{Path: s}, nil
	}
	device, p := s[:colon], s[colon+1:]
	if device == "" || strings.ContainsAny(device, "@[]\r\n\t") {
		return Operand{}, errors.New("copy remote operand requires an exact Connect device/alias, without user or address syntax")
	}
	if p == "" {
		p = "."
	}
	return Operand{Device: device, Path: p}, nil
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
