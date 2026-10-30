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

// Lchown sets numeric ownership on the link itself, only with the negotiated
// OpenSSH lsetstat v1 extension. It never silently follows the link.
func (c *Client) Lchown(path string, uid, gid uint32) error {
	return c.linkSetstat(path, sshFileXferAttrUIDGID, marshalUint32(marshalUint32(nil, uid), gid))
}

func (c *Client) linkSetstat(path string, flags uint32, attrs []byte) error {
	if version, ok := c.HasExtension("lsetstat@openssh.com"); !ok || version != "1" {
		return ErrSSHFxOpUnsupported
	}
	id := c.nextID()
	body := marshalString(nil, path)
	body = marshalUint32(body, flags)
	body = append(body, attrs...)
	typ, data, err := c.sendPacket(context.Background(), nil, &metadataExtension{ID: id, Name: "lsetstat@openssh.com", Body: body})
	if err != nil {
		return err
	}
	if typ != sshFxpStatus {
		return unimplementedPacketErr(typ)
	}
	return streamStatus(id, data, true)
}

// metadataExtension uses the same framing and request arbitration as other
// client operations. Replies are decoded by the checked local status parser.
type metadataExtension struct {
	ID   uint32
	Name string
	Body []byte
}

func (p *metadataExtension) id() uint32 { return p.ID }
func (p *metadataExtension) MarshalBinary() ([]byte, error) {
	b := make([]byte, 4)
	b = append(b, sshFxpExtended)
	b = marshalUint32(b, p.ID)
	b = marshalString(b, p.Name)
	return append(b, p.Body...), nil
}

// Lchmod changes permissions on the symbolic link itself when supported.
func (c *Client) Lchmod(path string, mode os.FileMode) error {
	return c.linkSetstat(path, sshFileXferAttrPermissions, marshalUint32(nil, toChmodPerm(mode)))
}

// CopyData copies regular-file bytes entirely on the server using copy-data v1.
// The caller owns both handles and must keep them open until this call returns.
// A zero length means through EOF, as specified by the extension.
func (c *Client) CopyData(source, target *File, offset, length, targetOffset uint64) error {
	if version, ok := c.HasExtension("copy-data"); !ok || version != "1" {
		return ErrSSHFxOpUnsupported
	}
	if source == nil || target == nil || source.c != c || target.c != c {
		return os.ErrInvalid
	}
	source.mu.RLock()
	from := source.handle
	source.mu.RUnlock()
	target.mu.RLock()
	to := target.handle
	target.mu.RUnlock()
	if from == "" || to == "" {
		return os.ErrClosed
	}
	id := c.nextID()
	body := marshalString(nil, from)
	body = marshalUint64(body, offset)
	body = marshalUint64(body, length)
	body = marshalString(body, to)
	body = marshalUint64(body, targetOffset)
	typ, data, err := c.sendPacket(context.Background(), nil, &metadataExtension{ID: id, Name: "copy-data", Body: body})
	if err != nil {
		return err
	}
	if typ != sshFxpStatus {
		return unimplementedPacketErr(typ)
	}
	return streamStatus(id, data, true)
}

// UserGroupNames resolves numeric IDs on the server. Request/reply lists are
// bounded, counted exactly, and never used as local account information.
func (c *Client) UserGroupNames(uids, gids []uint32) ([]string, []string, error) {
	if version, ok := c.HasExtension("users-groups-by-id@openssh.com"); !ok || version != "1" {
		return nil, nil, ErrSSHFxOpUnsupported
	}
	if len(uids) > 256 || len(gids) > 256 {
		return nil, nil, os.ErrInvalid
	}
	var ub, gb []byte
	for _, id := range uids {
		ub = marshalUint32(ub, id)
	}
	for _, id := range gids {
		gb = marshalUint32(gb, id)
	}
	id := c.nextID()
	body := marshalString(marshalString(nil, string(ub)), string(gb))
	typ, data, err := c.sendPacket(context.Background(), nil, &metadataExtension{ID: id, Name: "users-groups-by-id@openssh.com", Body: body})
	if err != nil {
		return nil, nil, err
	}
	if typ == sshFxpStatus {
		return nil, nil, streamStatus(id, data, false)
	}
	if typ != sshFxpExtendedReply {
		return nil, nil, unimplementedPacketErr(typ)
	}
	sid, data, err := unmarshalUint32Safe(data)
	if err != nil {
		return nil, nil, err
	}
	if sid != id {
		return nil, nil, &unexpectedIDErr{id, sid}
	}
	users, data, err := unmarshalStringSafe(data)
	if err != nil {
		return nil, nil, err
	}
	groups, rest, err := unmarshalStringSafe(data)
	if err != nil {
		return nil, nil, err
	}
	if len(rest) != 0 {
		return nil, nil, os.ErrInvalid
	}
	parse := func(raw string, count int) ([]string, error) {
		data := []byte(raw)
		names := make([]string, count)
		for i := range names {
			var err error
			names[i], data, err = unmarshalStringSafe(data)
			if err != nil {
				return nil, err
			}
		}
		if len(data) != 0 {
			return nil, os.ErrInvalid
		}
		return names, nil
	}
	unames, err := parse(users, len(uids))
	if err != nil {
		return nil, nil, err
	}
	gnames, err := parse(groups, len(gids))
	if err != nil {
		return nil, nil, err
	}
	return unames, gnames, nil
}
