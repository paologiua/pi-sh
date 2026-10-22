package sftpcli

import "io"

// copyFileData keeps error arbitration in io.CopyBuffer rather than a file's
// optional WriterTo/ReaderFrom fast path. In the pinned pkg/sftp sequential
// ReadFrom, the final short source read can hide that chunk's WRITE status error.
// Plain Write always checks it, even when the source also reached EOF.
func copyFileData(dst io.Writer, src io.Reader) (int64, error) {
	return io.CopyBuffer(struct{ io.Writer }{dst}, struct{ io.Reader }{src}, make([]byte, 32<<10))
}
