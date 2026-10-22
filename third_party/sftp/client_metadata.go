// Copyright 2026 pi-sh contributors. All rights reserved.
// Local extension to github.com/pkg/sftp v1.13.11. See PI_SH_CHANGES.md.
// Redistribution and use follow the BSD-2-Clause terms in LICENSE.

package sftp

import (
	"context"
	"os"
	"time"
)

// SetPermissionsAndTimes sends mode, access time and modification time together
// in one FSETSTAT identifying this open handle. Renaming/replacing the pathname
// cannot redirect this request to the replacement file. No size, uid/gid or
// extended attributes are sent. This is not a filesystem transaction: a server
// may partially apply attributes before returning an error.
//
// Mode uses the same conversion as File.Chmod. Times use SFTP v3 unsigned32-bit
// seconds, discarding fractions, with the same conversion as Client.Chtimes.
func (f *File) SetPermissionsAndTimes(mode os.FileMode, atime, mtime time.Time) error {
	if f == nil {
		return os.ErrInvalid
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	if f.handle == "" {
		return os.ErrClosed
	}
	id := f.c.nextID()
	typ, data, err := f.c.sendPacket(context.Background(), nil, &sshFxpFsetstatPacket{
		ID: id, Handle: f.handle, Flags: sshFileXferAttrPermissions | sshFileXferAttrACmodTime,
		Attrs: &FileStat{Mode: toChmodPerm(mode), Atime: uint32(atime.Unix()), Mtime: uint32(mtime.Unix())},
	})
	if err != nil {
		return err
	}
	if typ != sshFxpStatus {
		return unimplementedPacketErr(typ)
	}
	return streamStatus(id, data, true)
}
