package sftpcli

import (
	"errors"
	"os"
)

func (c *Console) remoteCopy(source, target string) (result error) {
	if version, ok := c.Client.HasExtension("copy-data"); !ok || version != "1" {
		return shown(os.ErrInvalid, "Server does not support copy-data extension")
	}
	from, err := c.Client.Open(source)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, from.Close()) }()
	info, err := from.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return shown(os.ErrInvalid, "Cannot copy non-regular file: %s", display(source))
	}
	dest, err := c.Client.Stat(target)
	create := errors.Is(err, os.ErrNotExist)
	if err != nil && !create {
		return err
	}
	if !create && !dest.Mode().IsRegular() {
		return shown(os.ErrInvalid, "Cannot copy to non-regular file: %s", display(target))
	}
	flags := os.O_WRONLY | os.O_TRUNC
	if create {
		flags |= os.O_CREATE | os.O_EXCL
	}
	to, err := c.Client.OpenFile(target, flags)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, to.Close()) }()
	if create {
		created, err := to.Stat()
		if err != nil {
			return err
		}
		mode := created.Mode().Perm() & info.Mode().Perm()
		if c.RemoteUmask != nil {
			if created.Mode().Perm()&0077 != 0 {
				return errors.New("remote server did not honor private creation")
			}
			mode = info.Mode().Perm() &^ *c.RemoteUmask
		}
		if err := to.Chmod(mode); err != nil {
			return err
		}
	}
	return c.Client.CopyData(from, to, 0, 0, 0)
}
