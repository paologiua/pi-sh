package sftpclient

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"io/fs"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"pi-sh/internal/shellbroker"
)

type serverPipes struct {
	io.Reader
	io.Writer
}

func (p serverPipes) Close() error {
	if c, ok := p.Reader.(io.Closer); ok {
		c.Close()
	}
	if c, ok := p.Writer.(io.Closer); ok {
		c.Close()
	}
	return nil
}

func memoryRunner(ctx context.Context, _ string, stdio shellbroker.IO) (int, error) {
	io.WriteString(stdio.Stdout, "PSU0022\n")
	server := sftp.NewRequestServer(serverPipes{stdio.Stdin, stdio.Stdout}, sftp.InMemHandler())
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			server.Close()
		case <-done:
		}
	}()
	err := server.Serve()
	server.Close()
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	if err == io.EOF {
		err = nil
	}
	return 0, err
}

func TestSFTPPipeRoundTripAndCleanEOF(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := Open(ctx, memoryRunner, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	data := make([]byte, 256*32)
	for i := range data {
		data[i] = byte(i)
	}
	file, err := session.Client.Create("/binary ' 世界")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.Write(data); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	file, err = session.Client.Open("/binary ' 世界")
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(file)
	if err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, got) {
		t.Fatal("binary SFTP data changed")
	}
	if err = session.Close(); err != nil {
		t.Fatal(err)
	}
	if err = session.Close(); err != nil {
		t.Fatal("close is not idempotent")
	}
}

func TestSFTPHandshakeCancellationAndInvalidPackets(t *testing.T) {
	for _, bad := range []bool{false, true} {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		run := func(ctx context.Context, _ string, stdio shellbroker.IO) (int, error) {
			if !bad {
				<-ctx.Done()
				return 0, ctx.Err()
			}
			io.WriteString(stdio.Stdout, "PSU0022\n")
			init := make([]byte, 9)
			if _, err := io.ReadFull(stdio.Stdin, init); err != nil {
				return 0, err
			}
			header := make([]byte, 4)
			binary.BigEndian.PutUint32(header, 1<<30)
			_, err := stdio.Stdout.Write(header)
			return 0, err
		}
		start := time.Now()
		session, err := Open(ctx, run, Options{})
		cancel()
		if err == nil || session != nil {
			t.Fatal("invalid/absent server accepted")
		}
		if !bad && !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("cancellation lost: %v", err)
		}
		if time.Since(start) > time.Second {
			t.Fatal("negotiation was not cancellable/bounded")
		}
	}
}

func TestSFTPServerCommandAndErrorRedaction(t *testing.T) {
	for _, path := range []string{"relative", "/bad\npath", "/bad\x00path", strings.Repeat("/", 4097)} {
		if _, err := ServerCommand(path); err == nil {
			t.Fatal("invalid server path accepted")
		}
	}
	command, err := ServerCommand("/existing ' server")
	if err != nil || !strings.Contains(command, "'\\''") {
		t.Fatal("server path not quoted")
	}
	err = SafeError(errors.New("secret-packet-contents"))
	if strings.Contains(err.Error(), "secret") {
		t.Fatal("raw library error disclosed")
	}
	err = SafeError(&os.PathError{Op: "read", Path: "secret-path", Err: fs.ErrPermission})
	if !errors.Is(err, fs.ErrPermission) || strings.Contains(err.Error(), "secret") {
		t.Fatal("safe file error class lost or path disclosed")
	}
	err = SafeError(&sftp.StatusError{Code: 3})
	if err == nil || !strings.Contains(err.Error(), "3") {
		t.Fatal("protocol status lost")
	}
}

func TestCreationMaskPrefixIsBoundedAndStrict(t *testing.T) {
	for _, prefix := range []string{"PSU022\n", "PSU0022\n", "PSU0777\n"} {
		r := strings.NewReader(prefix + "next-packet")
		mask, err := readUmask(r)
		if err != nil || mask > 0777 {
			t.Fatal("valid mask rejected", err)
		}
		remaining, _ := io.ReadAll(r)
		if string(remaining) != "next-packet" {
			t.Fatal("prefix consumed protocol bytes")
		}
	}
	for _, prefix := range []string{"", "PSU+22\n", "PSU0899\n", "PSU7777\n", "PSU22\n", "PSU00022\n", "PSU0022\r\n", strings.Repeat("x", 1024)} {
		if _, err := readUmask(strings.NewReader(prefix)); err == nil {
			t.Fatalf("invalid mask prefix accepted: %q", prefix)
		}
	}
}
