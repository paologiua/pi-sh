// Copyright 2026 pi-sh contributors. All rights reserved.
// Local extension to github.com/pkg/sftp v1.13.11. See PI_SH_CHANGES.md.
// Redistribution and use follow the BSD-2-Clause terms in LICENSE.

package sftp

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
)

// ReadDirEach visits a directory in server order without accumulating its full
// listing. At most one bounded SFTP NAME packet is retained per active call.
// The callback may make requests on c, including another ReadDirEach; it is
// called synchronously and must return promptly or honor ctx itself.
//
// Entries have their literal server-provided basenames, without path cleaning.
// Dot entries are skipped; empty names, slash/NUL, malformed replies and unknown
// attribute flags fail. A callback error, ctx error, READDIR error or failed
// directory CLOSE is returned. There is no retry or replay. Existing ReadDir
// and ReadDirContext retain their upstream behavior.
func (c *Client) ReadDirEach(ctx context.Context, name string, visit func(os.FileInfo) error) (result error) {
	if visit == nil {
		return errors.New("sftp: nil directory visitor")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	id := c.nextID()
	typ, data, err := c.sendPacket(ctx, nil, &sshFxpOpendirPacket{ID: id, Path: name})
	if err != nil {
		return err
	}
	if typ == sshFxpStatus {
		return streamStatus(id, data, false)
	}
	if typ != sshFxpHandle {
		return unimplementedPacketErr(typ)
	}
	sid, data, err := unmarshalUint32Safe(data)
	if err != nil {
		return err
	}
	if sid != id {
		return &unexpectedIDErr{id, sid}
	}
	handle, rest, err := unmarshalStringSafe(data)
	if err != nil {
		return err
	}
	if len(rest) != 0 {
		return errors.New("sftp: invalid directory handle")
	}
	defer func() {
		id := c.nextID()
		typ, data, err := c.sendPacket(context.Background(), nil, &sshFxpClosePacket{ID: id, Handle: handle})
		if err == nil {
			if typ != sshFxpStatus {
				err = unimplementedPacketErr(typ)
			} else {
				err = streamStatus(id, data, true)
			}
		}
		result = errors.Join(result, err)
	}()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		id := c.nextID()
		typ, data, err := c.sendPacket(ctx, nil, &sshFxpReaddirPacket{ID: id, Handle: handle})
		if err != nil {
			return err
		}
		switch typ {
		case sshFxpName:
			if err := streamNames(ctx, id, data, visit); err != nil {
				return err
			}
		case sshFxpStatus:
			err := streamStatus(id, data, false)
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		default:
			return unimplementedPacketErr(typ)
		}
	}
}

func streamStatus(id uint32, data []byte, allowOK bool) error {
	sid, data, err := unmarshalUint32Safe(data)
	if err != nil {
		return err
	}
	if sid != id {
		return &unexpectedIDErr{id, sid}
	}
	code, data, err := unmarshalUint32Safe(data)
	if err != nil {
		return err
	}
	// Validate, then discard server-controlled message and language text.
	_, data, err = unmarshalStringSafe(data)
	if err != nil {
		return err
	}
	_, data, err = unmarshalStringSafe(data)
	if err != nil {
		return err
	}
	if len(data) != 0 {
		return errors.New("sftp: trailing directory status data")
	}
	if code == sshFxOk {
		if allowOK {
			return nil
		}
		return errors.New("sftp: unexpected directory success status")
	}
	return normaliseError(&StatusError{Code: code})
}

func streamNames(ctx context.Context, id uint32, data []byte, visit func(os.FileInfo) error) error {
	sid, data, err := unmarshalUint32Safe(data)
	if err != nil {
		return err
	}
	if sid != id {
		return &unexpectedIDErr{id, sid}
	}
	count, data, err := unmarshalUint32Safe(data)
	if err != nil {
		return err
	}
	// Each entry needs at least two length prefixes and an attribute-flags word.
	if count == 0 || uint64(count) > uint64(len(data)/12) {
		return errors.New("sftp: invalid directory entry count")
	}
	for i := uint32(0); i < count; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		var filename string
		filename, data, err = unmarshalStringSafe(data)
		if err != nil {
			return err
		}
		_, data, err = unmarshalStringSafe(data)
		if err != nil {
			return err
		}
		flags, _, err := unmarshalUint32Safe(data)
		if err != nil {
			return err
		}
		if flags & ^uint32(sshFileXferAttrAll) != 0 {
			return errors.New("sftp: unknown directory attribute flags")
		}
		var attr *FileStat
		attr, data, err = unmarshalAttrs(data)
		if err != nil {
			return err
		}
		if filename == "." || filename == ".." {
			continue
		}
		if filename == "" || strings.ContainsAny(filename, "/\x00") {
			return errors.New("sftp: invalid directory entry name")
		}
		if err := visit(fileInfoFromStat(attr, filename)); err != nil {
			return err
		}
	}
	if len(data) != 0 {
		return errors.New("sftp: trailing directory entry data")
	}
	return nil
}
