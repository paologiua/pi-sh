package sftpcli

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"pi-sh/internal/progress"
)

type partialProgressWriter struct{}

func (partialProgressWriter) Write(p []byte) (int, error) { return min(256, len(p)), os.ErrPermission }

type brokenProgressOutput struct{}

func (brokenProgressOutput) Write([]byte) (int, error) { return 0, os.ErrClosed }

func TestProgressCountsOnlyWrittenBytesAndPreservesCopyError(t *testing.T) {
	var out bytes.Buffer
	c := Console{Progress: progress.New(&out, func() (int, bool) { return 80, true })}
	n, err := c.copyData(partialProgressWriter{}, bytes.NewReader(make([]byte, 1024)), "file", 1024, 0)
	if n != 256 || !errors.Is(err, os.ErrPermission) || !strings.Contains(out.String(), " 25%") || strings.Contains(out.String(), "100%") {
		t.Fatalf("partial write hidden: %d %v %q", n, err, out.String())
	}
}

func TestProgressDisplayFailureDoesNotChangeTransferResult(t *testing.T) {
	c := Console{Progress: progress.New(brokenProgressOutput{}, func() (int, bool) { return 80, true })}
	var dst bytes.Buffer
	data := bytes.Repeat([]byte{0, 255, 1}, 20000)
	n, err := c.copyData(&dst, bytes.NewReader(data), "file", int64(len(data)), 0)
	if err != nil || n != int64(len(data)) || !bytes.Equal(dst.Bytes(), data) {
		t.Fatal("display changed transfer", n, err)
	}
}
