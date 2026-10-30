package sftpcli

import (
	"context"
	"errors"
	"fmt"
	"github.com/pkg/sftp"
	"io"
	"os"
)

// Reproduce OpenSSH's fixed status vocabulary without printing server-supplied
// status messages, protocol bodies or inherited process state.
func statusText(err error) string {
	var status *sftp.StatusError
	if errors.As(err, &status) {
		labels := []string{"No error", "End of file", "No such file or directory", "Permission denied", "Failure", "Bad message", "No connection", "Connection lost", "Operation unsupported"}
		if int(status.Code) < len(labels) {
			return labels[status.Code]
		}
		return "Unknown status"
	}
	switch {
	case errors.Is(err, os.ErrNotExist):
		return "No such file or directory"
	case errors.Is(err, os.ErrPermission):
		return "Permission denied"
	case errors.Is(err, os.ErrExist):
		return "File exists"
	case errors.Is(err, io.EOF):
		return "End of file"
	}
	var local *os.PathError
	if errors.As(err, &local) {
		return local.Err.Error()
	}
	return "Failure"
}

func operationError(err error, format string, args ...any) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return shown(err, "%s: %s", fmt.Sprintf(format, args...), statusText(err))
}
