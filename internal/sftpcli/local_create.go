package sftpcli

import (
	"errors"
	"os"
)

// A console-local mask avoids changing the umask of unrelated Go goroutines.
// Once selected by lumask it replaces, rather than intersects, the inherited mask.
func (c *Console) openLocalFile(name string, flags int, mode os.FileMode) (*os.File, error) {
	if c.LocalUmask == nil || flags&os.O_CREATE == 0 {
		return os.OpenFile(name, flags, mode)
	}
	file, err := os.OpenFile(name, flags|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) {
		return os.OpenFile(name, flags&^(os.O_CREATE|os.O_EXCL), mode)
	}
	if err != nil {
		return nil, err
	}
	if err = file.Chmod(mode &^ *c.LocalUmask); err != nil {
		return nil, errors.Join(err, file.Close())
	}
	return file, nil
}

func (c *Console) makeLocalDir(name string, mode os.FileMode) error {
	if c.LocalUmask == nil {
		return os.Mkdir(name, mode)
	}
	if err := os.Mkdir(name, 0700); err != nil {
		return err
	}
	return os.Chmod(name, mode&^*c.LocalUmask)
}
