package sftpcli

import (
	"context"
	"io"
	"pi-sh/internal/progress"
)

func (c *Console) copyData(dst io.Writer, src io.Reader, name string, size, offset int64) (int64, error) {
	return c.copyDataMode(dst, src, name, size, offset, false)
}

func (c *Console) copyDataMode(dst io.Writer, src io.Reader, name string, size, offset int64, sequential bool) (int64, error) {
	var meter *progress.Transfer
	if c.Progress != nil && !c.ProgressDisabled {
		meter = c.Progress.Start(name, size, offset)
		defer meter.Stop()
	}
	if !sequential && c.Requests > 1 {
		reader, rok := src.(io.ReaderAt)
		writer, wok := dst.(io.WriterAt)
		if rok && wok {
			ctx := c.Context
			if ctx == nil {
				ctx = context.Background()
			}
			chunk := c.BufferSize
			if chunk <= 0 {
				chunk = 32768
			}
			return parallelCopy(ctx, writer, reader, size, offset, chunk, c.Requests, meter)
		}
	}
	if meter != nil {
		dst = progressWriter{dst, meter}
	}
	return copyFileDataBuffer(dst, src, c.BufferSize)
}

type progressWriter struct {
	writer io.Writer
	meter  *progress.Transfer
}

func (w progressWriter) Write(data []byte) (int, error) {
	n, err := w.writer.Write(data)
	if n > 0 && n <= len(data) {
		w.meter.Add(n)
	}
	return n, err
}

// copyFileData keeps error arbitration in io.CopyBuffer rather than a file's
// optional WriterTo/ReaderFrom fast path. In the pinned pkg/sftp sequential
// ReadFrom, the final short source read can hide that chunk's WRITE status error.
// Plain Write always checks it, even when the source also reached EOF.
func copyFileData(dst io.Writer, src io.Reader) (int64, error) {
	return copyFileDataBuffer(dst, src, 0)
}

func copyFileDataBuffer(dst io.Writer, src io.Reader, size int) (int64, error) {
	if size <= 0 {
		size = 32 << 10
	}
	return io.CopyBuffer(struct{ io.Writer }{dst}, struct{ io.Reader }{src}, make([]byte, size))
}
