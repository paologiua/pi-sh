package sftpcli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/pkg/sftp"
	"pi-sh/internal/sftpclient"
)

type preservedMetadata struct {
	mode         os.FileMode
	atime, mtime time.Time
}

func (c *Console) metadataWarning(operation string, err error) {
	if err == nil || c.Errors == nil {
		return
	}
	program := c.Program
	if program == "" {
		program = "pi-sftp"
	}
	fmt.Fprintf(c.Errors, "%s: %s: %v\n", program, operation, sftpclient.SafeError(err))
}

// Metadata status failures are advisory in the OpenSSH reference. Protocol and
// connection failures remain errors; the subsequent CLOSE must also succeed.
func (c *Console) remoteMetadataResult(operation string, err error) error {
	if err == nil {
		return nil
	}
	var status *sftp.StatusError
	if errors.As(err, &status) || errors.Is(err, os.ErrPermission) || errors.Is(err, os.ErrNotExist) || errors.Is(err, io.EOF) {
		c.metadataWarning(operation, err)
		return nil
	}
	return err
}

func (c *Console) preserveLocal(target *os.File, name string, source os.FileInfo) {
	// OpenSSH regular-file SFTP copies retain only permission bits, not set-ID,
	// sticky bits, ownership or ACLs. A metadata failure does not fail the copy.
	c.metadataWarning("local chmod failed", target.Chmod(source.Mode().Perm()))
	if stat, ok := source.Sys().(*sftp.FileStat); ok {
		c.metadataWarning("local set times failed", os.Chtimes(name, stat.AccessTime(), stat.ModTime()))
	} else {
		c.metadataWarning("source timestamps unavailable", os.ErrInvalid)
	}
}

func (c *Console) preserveRemote(target *sftp.File, source preservedMetadata) error {
	return c.remoteMetadataResult("remote set metadata failed", target.SetPermissionsAndTimes(source.mode, source.atime, source.mtime))
}
