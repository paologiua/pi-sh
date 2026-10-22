package sftpcli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pkg/sftp"
)

func TestPreserveOptions(t *testing.T) {
	args, options, err := transferFlags([]string{"-apfp", "--", "-p"}, transferOptions{})
	if err != nil || !options.preserve || !options.resume || !options.flush || len(args) != 1 || args[0] != "-p" {
		t.Fatal(args, options, err)
	}
	_, options, err = transferFlags([]string{"source", "-p"}, transferOptions{})
	if err != nil || options.preserve {
		t.Fatal("operand reinterpreted")
	}
}

func TestLocalAccessTimeSnapshot(t *testing.T) {
	name := filepath.Join(t.TempDir(), "source")
	if err := os.WriteFile(name, []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	atime, mtime := time.Unix(1600000001, 0), time.Unix(1600000013, 0)
	if err := os.Chtimes(name, atime, mtime); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.ReadFile(name); err != nil {
		t.Fatal(err)
	}
	got, ok := localAccessTime(info)
	if !ok || got.Unix() != atime.Unix() || info.ModTime().Unix() != mtime.Unix() {
		t.Fatal("source snapshot unavailable", got, ok)
	}
}

func TestMetadataStatusFailureClassificationAndRedaction(t *testing.T) {
	var diagnostic bytes.Buffer
	c := Console{Errors: &diagnostic, Program: "pi-cp"}
	for _, err := range []error{&sftp.StatusError{Code: 8}, os.ErrPermission, os.ErrNotExist, io.EOF} {
		if err := c.remoteMetadataResult("remote metadata failed", fmt.Errorf("secret-payload: %w", err)); err != nil {
			t.Fatal("advisory status became fatal", err)
		}
	}
	if strings.Contains(diagnostic.String(), "secret") || !strings.Contains(diagnostic.String(), "pi-cp:") {
		t.Fatal("unsafe diagnostic")
	}
	fatal := errors.New("secret-malformed-protocol")
	if !errors.Is(c.remoteMetadataResult("remote metadata failed", fatal), fatal) {
		t.Fatal("protocol failure ignored")
	}
	if strings.Contains(diagnostic.String(), "secret") {
		t.Fatal("protocol details logged")
	}
}

type capturePreservedMetadata struct {
	sftp.FileCmder
	mu         sync.Mutex
	attributes []sftp.FileStat
}

func (c *capturePreservedMetadata) Filecmd(r *sftp.Request) error {
	if r.Method == "Setstat" {
		c.mu.Lock()
		c.attributes = append(c.attributes, *r.Attributes())
		c.mu.Unlock()
	}
	return c.FileCmder.Filecmd(r)
}

func TestUploadFailureCannotBeErasedByPreservation(t *testing.T) {
	var capture *capturePreservedMetadata
	client := memoryClientWith(t, func(h *sftp.Handlers) {
		h.FilePut = denyFilePut{h.FilePut, 1024}
		capture = &capturePreservedMetadata{FileCmder: h.FileCmd}
		h.FileCmd = capture
	})
	dir := t.TempDir()
	name := filepath.Join(dir, "source")
	if err := os.WriteFile(name, bytes.Repeat([]byte("x"), 8193), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(name, 0640); err != nil {
		t.Fatal(err)
	}
	atime, mtime := time.Unix(1600000001, 0), time.Unix(1600000013, 0)
	if err := os.Chtimes(name, atime, mtime); err != nil {
		t.Fatal(err)
	}
	writeResumeRemote(t, client, "/target", nil)
	c := Console{Client: client, Preserve: true}
	if _, err := c.Execute(context.Background(), []string{"put", name, "/target"}); err == nil {
		t.Fatal("failed data copy hidden")
	}
	info, err := client.Stat("/target")
	if err != nil || info.Size() != 1024 {
		t.Fatal("partial copy status or size", err)
	}
	capture.mu.Lock()
	defer capture.mu.Unlock()
	attrs := capture.attributes
	if len(attrs) != 1 || attrs[0].Mode != 0640 || attrs[0].Atime != uint32(atime.Unix()) || attrs[0].Mtime != uint32(mtime.Unix()) {
		t.Fatal("final upload metadata not attempted after failed data copy", attrs)
	}
}

func TestLocalMetadataErrorsWarnWithoutPaths(t *testing.T) {
	client := memoryClient(t)
	writeResumeRemote(t, client, "/source", nil)
	info, err := client.Stat("/source")
	if err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(t.TempDir(), "secret-filename")
	f, err := os.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	if err := os.Remove(name); err != nil {
		t.Fatal(err)
	}
	var diagnostic bytes.Buffer
	c := Console{Errors: &diagnostic}
	c.preserveLocal(f, name, info)
	if strings.Contains(diagnostic.String(), "secret") || !strings.Contains(diagnostic.String(), "local chmod failed") || !strings.Contains(diagnostic.String(), "local set times failed") {
		t.Fatal("metadata failure diagnostic", diagnostic.String())
	}
}
