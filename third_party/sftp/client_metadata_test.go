package sftp

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

type metadataRequest struct {
	handle                    string
	flags, mode, atime, mtime uint32
}

func metadataReply(w io.Writer, typ fxp, body []byte) error {
	packet := binary.BigEndian.AppendUint32(nil, uint32(len(body)+1))
	packet = append(packet, byte(typ))
	packet = append(packet, body...)
	n, err := w.Write(packet)
	if err == nil && n != len(packet) {
		return io.ErrShortWrite
	}
	return err
}
func metadataStatus(id, code uint32) []byte {
	body := binary.BigEndian.AppendUint32(nil, id)
	body = binary.BigEndian.AppendUint32(body, code)
	return streamTestString(streamTestString(body, "private-fixture-status"), "")
}

func TestHandlePermissionsAndTimesWireAndFailures(t *testing.T) {
	for _, tc := range []struct {
		name         string
		atime, mtime int64
		code         uint32
		want         error
	}{
		{"ok", 1600000001, 1600000013, 0, nil},
		{"v3-conversion", -1, 1<<32 + 9, 0, nil},
		{"permission", 1600000001, 1600000013, 3, os.ErrPermission},
		{"status-eof", 1600000001, 1600000013, 1, io.EOF},
		{"malformed", 1600000001, 1600000013, 0, os.ErrInvalid},
		{"wrong-type", 1600000001, 1600000013, 0, os.ErrInvalid},
		{"stream-loss", 1600000001, 1600000013, 0, os.ErrInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			local, remote := net.Pipe()
			record := make(chan metadataRequest, 1)
			serverDone := make(chan error, 1)
			go func() {
				serverDone <- func() error {
					defer remote.Close()
					typ, _, err := recvPacket(remote, nil, 0)
					if err != nil {
						return err
					}
					if typ != sshFxpInit {
						return fmt.Errorf("expected INIT")
					}
					if err := metadataReply(remote, sshFxpVersion, []byte{0, 0, 0, 3}); err != nil {
						return err
					}
					typ, data, err := recvPacket(remote, nil, 0)
					if err != nil {
						return err
					}
					if typ != sshFxpOpen {
						return fmt.Errorf("expected OPEN")
					}
					id, _, err := unmarshalUint32Safe(data)
					if err != nil {
						return err
					}
					if err := metadataReply(remote, sshFxpHandle, streamTestString(binary.BigEndian.AppendUint32(nil, id), "opened-file")); err != nil {
						return err
					}
					typ, data, err = recvPacket(remote, nil, 0)
					if err != nil {
						return err
					}
					if typ != sshFxpFsetstat {
						return fmt.Errorf("expected one handle FSETSTAT, got %d", typ)
					}
					id, data, err = unmarshalUint32Safe(data)
					if err != nil {
						return err
					}
					handle, data, err := unmarshalStringSafe(data)
					if err != nil {
						return err
					}
					flags, data, err := unmarshalUint32Safe(data)
					if err != nil {
						return err
					}
					mode, data, err := unmarshalUint32Safe(data)
					if err != nil {
						return err
					}
					atime, data, err := unmarshalUint32Safe(data)
					if err != nil {
						return err
					}
					mtime, data, err := unmarshalUint32Safe(data)
					if err != nil {
						return err
					}
					if len(data) != 0 {
						return fmt.Errorf("unexpected metadata fields")
					}
					record <- metadataRequest{handle, flags, mode, atime, mtime}
					if tc.name == "stream-loss" {
						return nil
					}
					replyType, reply := fxp(sshFxpStatus), metadataStatus(id, tc.code)
					if tc.name == "malformed" {
						reply = binary.BigEndian.AppendUint32(nil, id)
					}
					if tc.name == "wrong-type" {
						replyType = sshFxpAttrs
					}
					if err := metadataReply(remote, replyType, reply); err != nil {
						return err
					}
					typ, data, err = recvPacket(remote, nil, 0)
					if err != nil {
						return err
					}
					if typ != sshFxpClose {
						return fmt.Errorf("expected CLOSE, got %d", typ)
					}
					id, _, err = unmarshalUint32Safe(data)
					if err != nil {
						return err
					}
					return metadataReply(remote, sshFxpStatus, metadataStatus(id, 0))
				}()
			}()
			client, err := NewClientPipe(local, local)
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			file, err := client.OpenFile("/synthetic", os.O_WRONLY)
			if err != nil {
				t.Fatal(err)
			}
			err = file.SetPermissionsAndTimes(0640, time.Unix(tc.atime, 987654321), time.Unix(tc.mtime, 123456789))
			if tc.want == nil && err != nil || tc.want != nil && err == nil {
				t.Fatal("metadata result changed", err)
			}
			if tc.want != nil && tc.want != os.ErrInvalid && !errors.Is(err, tc.want) {
				t.Fatal("status classification lost", err)
			}
			if err != nil && strings.Contains(err.Error(), "private-fixture") {
				t.Fatal("server-controlled status text exposed")
			}
			got := <-record
			if got.handle != "opened-file" || got.flags != sshFileXferAttrPermissions|sshFileXferAttrACmodTime || got.mode != 0640 || got.atime != uint32(tc.atime) || got.mtime != uint32(tc.mtime) {
				t.Fatal("metadata request differs", got)
			}
			if tc.name != "stream-loss" {
				if err := file.Close(); err != nil {
					t.Fatal(err)
				}
				if err := file.SetPermissionsAndTimes(0600, time.Now(), time.Now()); !errors.Is(err, os.ErrClosed) {
					t.Fatal("closed handle accepted", err)
				}
			}
			if err := <-serverDone; err != nil {
				t.Fatal(err)
			}
		})
	}
	if err := (*File)(nil).SetPermissionsAndTimes(0600, time.Now(), time.Now()); !errors.Is(err, os.ErrInvalid) {
		t.Fatal("nil file accepted", err)
	}
}
