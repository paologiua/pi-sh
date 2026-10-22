package sftpcli

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/pkg/sftp"
	"pi-sh/internal/sftpclient"
)

// CopyTree implements scp -r semantics, which follow source symlinks. It is
// deliberately separate from sftp get/put: their recursive symlink policy differs.
// Destination paths remain literal and are never evaluated by a shell.
func (c *Console) CopyTree(ctx context.Context, upload bool, source, target string) error {
	return sftpclient.SafeError(c.copyTreeRoot(ctx, upload, source, target))
}

func (c *Console) treeStat(upload, source bool, name string) (os.FileInfo, error) {
	if upload == source {
		return os.Stat(name)
	}
	return c.Client.Stat(name)
}

// treeAppend preserves symlink/.. traversal in the caller's path. path.Join and
// filepath.Join would clean these components before the filesystem sees them.
func treeAppend(root, name string, local bool) string {
	sep := "/"
	if local {
		sep = string(os.PathSeparator)
	}
	if strings.HasSuffix(root, sep) {
		return root + name
	}
	return root + sep + name
}

func treeName(name string, local bool) bool {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\x00") {
		return false
	}
	return !local || (filepath.Base(name) == name && !filepath.IsAbs(name) && filepath.VolumeName(name) == "")
}

func treeBase(name string, local bool) string {
	if local {
		name = filepath.ToSlash(name)
	}
	name = strings.TrimRight(name, "/")
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		name = name[i+1:]
	}
	return name
}

func (c *Console) copyTreeRoot(ctx context.Context, upload bool, source, target string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	info, err := c.treeStat(upload, true, source)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		if !info.Mode().IsRegular() {
			return errors.New("copy source is not a regular file or directory")
		}
		return c.treeFile(upload, source, target)
	}
	dest, err := c.treeStat(upload, false, target)
	if err == nil {
		if !dest.IsDir() {
			return errors.New("directory copy requires a directory destination")
		}
		base := treeBase(source, upload)
		if base != "." {
			if !treeName(base, !upload) {
				return errors.New("source directory basename cannot be used at the destination")
			}
			target = treeAppend(target, base, !upload)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return c.treeDir(ctx, upload, source, target, info, nil, nil)
}

func (c *Console) treeFile(upload bool, source, target string) error {
	options := transferOptions{preserve: c.Preserve}
	if upload {
		return c.put(source, target, options)
	}
	return c.get(source, target, options)
}

// The depth bound matches OpenSSH 10.2's SFTP directory-copy bound. Ancestor
// identities also reject cycles before producing 64 repeated destination trees.
func (c *Console) treeDir(ctx context.Context, upload bool, source, target string, info os.FileInfo, localAncestors []os.FileInfo, remoteAncestors []string) (result error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(localAncestors)+len(remoteAncestors) >= 64 {
		return errors.New("directory copy exceeds maximum depth")
	}
	if upload {
		for _, ancestor := range localAncestors {
			if os.SameFile(info, ancestor) {
				return errors.New("directory copy contains a symbolic-link cycle")
			}
		}
		localAncestors = append(localAncestors, info)
	} else {
		canonical, err := c.Client.RealPath(source)
		if err != nil {
			return err
		}
		for _, ancestor := range remoteAncestors {
			if canonical == ancestor {
				return errors.New("directory copy contains a symbolic-link cycle")
			}
		}
		remoteAncestors = append(remoteAncestors, canonical)
	}

	// Capture timestamps before enumeration changes directory atime.
	var metadata preservedMetadata
	if upload && c.Preserve {
		var ok bool
		metadata.atime, ok = localAccessTime(info)
		if !ok {
			return errors.New("source access time unavailable for preservation")
		}
		metadata.mtime, metadata.mode = info.ModTime(), info.Mode().Perm()
	}
	dest, err := c.treeStat(upload, false, target)
	created := errors.Is(err, os.ErrNotExist)
	if err != nil && !created {
		return err
	}
	mode := info.Mode().Perm()
	if created {
		if upload {
			err = c.Client.Mkdir(target)
		} else {
			err = os.Mkdir(target, mode|0300)
		}
		if err != nil {
			return err // No fallback when another actor won the creation race.
		}
		dest, err = c.treeStat(upload, false, target)
		if err != nil {
			return err
		}
		if upload && c.RemoteUmask != nil {
			if dest.Mode().Perm()&0077 != 0 {
				return errors.New("remote server did not honor private directory creation")
			}
		}
	}
	if !dest.IsDir() {
		return errors.New("directory copy conflicts with a non-directory destination")
	}
	adjustLocalMode := mode|0300 != mode
	if upload || adjustLocalMode || c.Preserve {
		// OpenSSH uploads restore source directory permissions even without -p.
		// Downloads chmod only when temporary owner write/execute bits were needed;
		// otherwise an existing directory retains its mode even with -p.
		if created {
			if upload {
				err = c.Client.Chmod(target, mode|0300)
			}
			if err != nil {
				return err
			}
		}
		defer func() {
			if upload {
				result = errors.Join(result, c.remoteMetadataResult("remote directory chmod failed", c.Client.Chmod(target, mode)))
				if c.Preserve {
					result = errors.Join(result, c.remoteMetadataResult("remote directory set times failed", c.Client.Chtimes(target, metadata.atime, metadata.mtime)))
				}
			} else {
				if adjustLocalMode {
					c.metadataWarning("local directory chmod failed", os.Chmod(target, mode))
				}
				if c.Preserve {
					if stat, ok := info.Sys().(*sftp.FileStat); ok {
						c.metadataWarning("local directory set times failed", os.Chtimes(target, stat.AccessTime(), info.ModTime()))
					} else {
						c.metadataWarning("source directory timestamps unavailable", os.ErrInvalid)
					}
				}
			}
		}()
	}

	visit := func(name string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !treeName(name, !upload) || !treeName(name, upload) {
			return errors.New("directory entry cannot be used as a destination filename")
		}
		child := treeAppend(source, name, upload)
		destination := treeAppend(target, name, !upload)
		childInfo, err := c.treeStat(upload, true, child)
		if err != nil {
			return err
		}
		if childInfo.IsDir() {
			return c.treeDir(ctx, upload, child, destination, childInfo, localAncestors, remoteAncestors)
		}
		if !childInfo.Mode().IsRegular() {
			return errors.New("directory entry is not a regular file or directory")
		}
		if dest, err := c.treeStat(upload, false, destination); err == nil && dest.IsDir() {
			return errors.New("regular file conflicts with a directory destination")
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return c.treeFile(upload, child, destination)
	}
	if upload {
		dir, err := os.Open(source)
		if err != nil {
			return err
		}
		defer func() { result = errors.Join(result, dir.Close()) }()
		for {
			entries, readErr := dir.ReadDir(64)
			for _, entry := range entries {
				if err := visit(entry.Name()); err != nil {
					return err
				}
			}
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			if readErr != nil {
				return readErr
			}
		}
	}
	return c.Client.ReadDirEach(ctx, source, func(entry os.FileInfo) error {
		return visit(entry.Name())
	})
}
