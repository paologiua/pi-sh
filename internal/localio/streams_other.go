//go:build !darwin && !linux

package localio

import (
	"context"
	"io"
	"os"
)

// This fallback deliberately makes no synchronous Windows console/pipe I/O
// cancellation claim. It preserves existing behavior pending native validation.
func Open(ctx context.Context, files Files) (*Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	s := &Session{cancel: cancel, finish: func() error { return nil }}
	if files.Stdin != nil {
		s.Stdin = &fallback{ctx: ctx, file: files.Stdin}
	}
	if files.Stdout != nil {
		s.Stdout = &fallback{ctx: ctx, file: files.Stdout}
	}
	if files.Stderr != nil {
		s.Stderr = &fallback{ctx: ctx, file: files.Stderr}
	}
	return s, nil
}

type fallback struct {
	ctx  context.Context
	file *os.File
}

func (f *fallback) Read(p []byte) (int, error) {
	if err := f.ctx.Err(); err != nil {
		return 0, err
	}
	return f.file.Read(p)
}
func (f *fallback) Write(p []byte) (int, error) {
	if err := f.ctx.Err(); err != nil {
		return 0, err
	}
	return f.file.Write(p)
}

var _ io.ReadWriter = (*fallback)(nil)
