package sftpcli

import (
	"io"
	"pi-sh/internal/progress"
)

func (c *Console) copyData(dst io.Writer, src io.Reader, name string, size, offset int64) (int64, error) {
	if c.Progress == nil {
		return copyFileData(dst, src)
	}
	meter := c.Progress.Start(name, size, offset)
	defer meter.Stop()
	return copyFileData(progressWriter{dst, meter}, src)
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
	return io.CopyBuffer(struct{ io.Writer }{dst}, struct{ io.Reader }{src}, make([]byte, 32<<10))
}
