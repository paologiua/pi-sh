package sftpcli

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pkg/sftp"
)

// Test-only wire adapter: add a fsync advertisement to the in-memory server,
// which will reject that extension. Production traffic has no such adapter.
type fsyncReplies struct {
	writer    io.Writer
	pending   []byte
	advertise bool
	status    uint32
	malformed bool
	drop      func()
}

func wireString(b []byte, s string) []byte {
	b = binary.BigEndian.AppendUint32(b, uint32(len(s)))
	return append(b, s...)
}
func (w *fsyncReplies) Write(p []byte) (int, error) {
	w.pending = append(w.pending, p...)
	for len(w.pending) >= 4 {
		n := int(binary.BigEndian.Uint32(w.pending[:4]))
		if n < 1 || n > 256<<10 {
			return 0, errors.New("invalid fixture packet")
		}
		if len(w.pending) < n+4 {
			break
		}
		body := append([]byte(nil), w.pending[4:n+4]...)
		w.pending = w.pending[n+4:]
		if body[0] == 2 && w.advertise { // SFTP VERSION
			body = wireString(wireString(body, "fsync@openssh.com"), "1")
		}
		if body[0] == 101 && len(body) >= 9 && binary.BigEndian.Uint32(body[5:9]) == 8 {
			if w.drop != nil {
				w.drop()
				return 0, io.ErrClosedPipe
			}
			// Exercise diagnostic redaction, independently from the numeric status.
			body = wireString(wireString(body[:9], "secret-fsync-server-payload"), "en")
			if w.status != 0 {
				binary.BigEndian.PutUint32(body[5:9], w.status)
			}
			if w.malformed {
				body[0] = 102
			} // wrong reply type, not an advisory status
		}
		packet := binary.BigEndian.AppendUint32(nil, uint32(len(body)))
		packet = append(packet, body...)
		if n, err := w.writer.Write(packet); err != nil {
			return 0, err
		} else if n != len(packet) {
			return 0, io.ErrShortWrite
		}
	}
	return len(p), nil
}

type fixtureStream struct {
	io.Reader
	io.Writer
	close func() error
}

type denyWrite struct {
	io.WriterAt
	rejectAt int64
}

func (d denyWrite) WriteAt(b []byte, offset int64) (int, error) {
	if offset >= d.rejectAt {
		return 0, os.ErrPermission
	}
	if int64(len(b)) > d.rejectAt-offset {
		n, err := d.WriterAt.WriteAt(b[:int(d.rejectAt-offset)], offset)
		if err != nil {
			return n, err
		}
		return n, os.ErrPermission
	}
	return d.WriterAt.WriteAt(b, offset)
}
func (d denyWrite) Close() error {
	if c, ok := d.WriterAt.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

type denyFilePut struct {
	sftp.FileWriter
	rejectAt int64
}

func (d denyFilePut) Filewrite(r *sftp.Request) (io.WriterAt, error) {
	w, err := d.FileWriter.Filewrite(r)
	if err != nil {
		return nil, err
	}
	return denyWrite{w, d.rejectAt}, nil
}
func fsyncHandlers(mode string) sftp.Handlers {
	h := sftp.InMemHandler()
	if mode == "write-error" {
		h.FilePut = denyFilePut{h.FilePut, 0}
	}
	if mode == "write-tail" {
		h.FilePut = denyFilePut{h.FilePut, 1024}
	}
	return h
}

func (s fixtureStream) Close() error {
	if s.close != nil {
		return s.close()
	}
	return nil
}

func fsyncFixtureClient(t *testing.T, mode string, options ...sftp.ClientOption) *sftp.Client {
	t.Helper()
	client, remote := net.Pipe()
	replies := &fsyncReplies{writer: remote, advertise: mode != "absent"}
	if mode == "permission" {
		replies.status = 3
	}
	if mode == "eof" {
		replies.status = 1
	}
	if mode == "malformed" {
		replies.malformed = true
	}
	if mode == "drop" {
		replies.drop = func() { remote.Close() }
	}
	server := sftp.NewRequestServer(fixtureStream{remote, replies, remote.Close}, fsyncHandlers(mode))
	done := make(chan struct{})
	go func() { defer close(done); server.Serve(); server.Close() }()
	c, err := sftp.NewClientPipe(client, client, options...)
	if err != nil {
		remote.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c.Close()
		server.Close()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("SFTP fixture did not close")
		}
	})
	return c
}

func TestFlushNegotiationAdvisoryAndTransportFailure(t *testing.T) {
	for _, mode := range []string{"absent", "advertised", "permission", "eof", "drop", "malformed", "write-error"} {
		for _, global := range []bool{false, true} {
			t.Run(mode+map[bool]string{false: "/command", true: "/global"}[global], func(t *testing.T) {
				client := fsyncFixtureClient(t, mode)
				dir := t.TempDir()
				data := make([]byte, 256*32)
				for i := range data {
					data[i] = byte(i)
				}
				if err := os.WriteFile(filepath.Join(dir, "input"), data, 0600); err != nil {
					t.Fatal(err)
				}
				flags := " -ff"
				if global {
					flags = ""
				}
				var diagnostic bytes.Buffer
				c := Console{Client: client, LocalDir: dir, RemoteDir: "/", Batch: true, Flush: global, Output: io.Discard, Errors: &diagnostic, Input: strings.NewReader("put" + flags + " input /remote\nget" + flags + " /remote download\nquit\n")}
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				err := c.Run(ctx)
				if mode == "write-error" {
					if err == nil || !strings.Contains(diagnostic.String(), "remote sync failed") {
						t.Fatal("failed upload did not attempt flush or copy error was erased", err, diagnostic.String())
					}
					return
				}
				if mode == "drop" || mode == "malformed" {
					if err == nil {
						t.Fatal("lost fsync reply became transfer success")
					}
					return
				}
				if err != nil {
					t.Fatal(err, diagnostic.String())
				}
				got, err := os.ReadFile(filepath.Join(dir, "download"))
				if err != nil || !bytes.Equal(got, data) {
					t.Fatal("flush changed data", err)
				}
				if mode == "absent" && diagnostic.Len() != 0 {
					t.Fatal("missing extension should be skipped silently", diagnostic.String())
				}
				if mode == "advertised" && !strings.Contains(diagnostic.String(), "SFTP status 8") {
					t.Fatal("server fsync failure not reported", diagnostic.String())
				}
				if (mode == "permission" || mode == "eof") && !strings.Contains(diagnostic.String(), "remote sync failed") {
					t.Fatal("normalized fsync status not reported", diagnostic.String())
				}
				if strings.Contains(diagnostic.String(), "secret") {
					t.Fatal("server-controlled fsync text disclosed")
				}
			})
		}
	}
}

// Spawned only by the local OpenSSH reference fixture. It serves memory over
// stdio, with a synthetic extension advertisement/failure; no socket or Pi.
func TestFsyncReferenceServer(t *testing.T) {
	mode := os.Getenv("PI_SH_TEST_FSYNC_HELPER")
	if mode == "" {
		return
	}
	replies := &fsyncReplies{writer: os.Stdout, advertise: mode != "absent"}
	if mode == "permission" {
		replies.status = 3
	}
	if mode == "eof" {
		replies.status = 1
	}
	if mode == "malformed" {
		replies.malformed = true
	}
	if mode == "drop" {
		replies.drop = func() { os.Exit(0) }
	}
	server := sftp.NewRequestServer(fixtureStream{Reader: os.Stdin, Writer: replies}, fsyncHandlers(mode))
	err := server.Serve()
	server.Close()
	if err != nil && !errors.Is(err, io.EOF) {
		os.Exit(1)
	}
	os.Exit(0)
}
