package sftpclient

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pkg/sftp"
)

func directoryFrame(r io.Reader) ([]byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(header[:])
	if n == 0 || n > 256<<10 {
		return nil, os.ErrInvalid
	}
	data := make([]byte, n)
	_, err := io.ReadFull(r, data)
	return data, err
}
func directorySend(w io.Writer, data []byte) error {
	b := binary.BigEndian.AppendUint32(nil, uint32(len(data)))
	b = append(b, data...)
	n, err := w.Write(b)
	if err == nil && n != len(b) {
		return io.ErrShortWrite
	}
	return err
}
func directoryString(b []byte, s string) []byte {
	b = binary.BigEndian.AppendUint32(b, uint32(len(s)))
	return append(b, s...)
}
func directoryStatus(id, code uint32) []byte {
	b := binary.BigEndian.AppendUint32([]byte{101}, id)
	b = binary.BigEndian.AppendUint32(b, code)
	return directoryString(directoryString(b, "secret-fixture-text"), "")
}

func TestDirectoryWireEOFMalformedRepliesAndCloseFailure(t *testing.T) {
	for _, mode := range []string{"clean", "stream-eof", "empty-name-packet", "invalid-name", "short-name-packet", "close-error", "close-eof", "short-close"} {
		t.Run(mode, func(t *testing.T) {
			local, remote := net.Pipe()
			var closeSeen atomic.Bool
			serverDone := make(chan struct{})
			go func() {
				defer close(serverDone)
				defer remote.Close()
				b, err := directoryFrame(remote)
				if err != nil || len(b) != 5 || b[0] != 1 {
					return
				}
				if directorySend(remote, []byte{2, 0, 0, 0, 3}) != nil {
					return
				}
				b, err = directoryFrame(remote)
				if err != nil || len(b) < 5 || b[0] != 11 {
					return
				}
				id := binary.BigEndian.Uint32(b[1:5])
				handle := directoryString(binary.BigEndian.AppendUint32([]byte{102}, id), "directory")
				if directorySend(remote, handle) != nil {
					return
				}
				b, err = directoryFrame(remote)
				if err != nil || len(b) < 5 || b[0] != 12 {
					return
				}
				id = binary.BigEndian.Uint32(b[1:5])
				if mode == "stream-eof" {
					return
				}
				reply := directoryStatus(id, 1)
				switch mode {
				case "empty-name-packet":
					reply = binary.BigEndian.AppendUint32(binary.BigEndian.AppendUint32([]byte{104}, id), 0)
				case "invalid-name":
					reply = binary.BigEndian.AppendUint32(binary.BigEndian.AppendUint32([]byte{104}, id), 1)
					reply = directoryString(directoryString(reply, "../escape"), "")
					reply = binary.BigEndian.AppendUint32(reply, 0)
				case "short-name-packet":
					reply = binary.BigEndian.AppendUint32(binary.BigEndian.AppendUint32([]byte{104}, id), 1)
				}
				if directorySend(remote, reply) != nil {
					return
				}
				b, err = directoryFrame(remote)
				if err != nil || len(b) < 5 || b[0] != 4 {
					return
				}
				closeSeen.Store(true)
				id = binary.BigEndian.Uint32(b[1:5])
				reply = directoryStatus(id, 0)
				if mode == "close-error" {
					reply = directoryStatus(id, 3)
				}
				if mode == "close-eof" {
					reply = directoryStatus(id, 1)
				}
				if mode == "short-close" {
					reply = binary.BigEndian.AppendUint32([]byte{101}, id)
				}
				_ = directorySend(remote, reply)
			}()
			client, err := sftp.NewClientPipe(local, local)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				client.Close()
				remote.Close()
				select {
				case <-serverDone:
				case <-time.After(time.Second):
					t.Error("wire fixture did not stop")
				}
			})
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			visits := 0
			err = client.ReadDirEach(ctx, "/", func(os.FileInfo) error { visits++; return nil })
			if visits != 0 {
				t.Fatal("invalid directory entry reached callback")
			}
			if mode == "clean" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("malformed/lost/failed reply became success")
			}
			if mode == "close-error" && !errors.Is(err, os.ErrPermission) {
				t.Fatal("CLOSE status lost", err)
			}
			if mode != "stream-eof" && !closeSeen.Load() {
				t.Fatal("directory CLOSE was not attempted")
			}
		})
	}
}
