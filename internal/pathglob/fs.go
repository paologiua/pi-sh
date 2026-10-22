package pathglob

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"

	"github.com/pkg/sftp"
)

// Remote walks an SFTP server. Directories are read with the bounded
// ReadDirEach iterator, so only one NAME packet per open directory is held.
// SFTP status replies (no such file, permission denied, not a directory) mean
// "no match"; transport and protocol failures stop the expansion.
func Remote(client *sftp.Client) FS { return remoteFS{client} }

type remoteFS struct{ client *sftp.Client }

func remoteError(err error) error {
	if err == nil {
		return nil
	}
	var status *sftp.StatusError
	if errors.As(err, &status) || errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission) {
		return err
	}
	return Fatal(err)
}

func (r remoteFS) ReadDir(ctx context.Context, dir string, visit func(string) error) error {
	// A visitor failure is reported to the expander by the visitor itself.
	return remoteError(r.client.ReadDirEach(ctx, dir, func(info os.FileInfo) error { return visit(info.Name()) }))
}

func (r remoteFS) Lstat(_ context.Context, name string) (fs.FileInfo, error) {
	info, err := r.client.Lstat(name)
	return info, remoteError(err)
}

func (r remoteFS) Stat(_ context.Context, name string) (fs.FileInfo, error) {
	info, err := r.client.Stat(name)
	return info, remoteError(err)
}

// Local walks the client filesystem with '/' separators, reading directories
// in fixed-size batches. Callers must not use it with Windows paths, where '\'
// is a separator rather than the glob escape.
func Local() FS { return localFS{} }

type localFS struct{}

func (localFS) ReadDir(ctx context.Context, dir string, visit func(string) error) (result error) {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer func() {
		if err := f.Close(); result == nil {
			result = err
		}
	}()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		entries, err := f.ReadDir(64)
		for _, entry := range entries {
			if err := visit(entry.Name()); err != nil {
				return err
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func (localFS) Lstat(_ context.Context, name string) (fs.FileInfo, error) { return os.Lstat(name) }
func (localFS) Stat(_ context.Context, name string) (fs.FileInfo, error)  { return os.Stat(name) }
