package sftpcli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"sync"
	"testing"
)

type failingOffsetWriter struct {
	mu      sync.Mutex
	fail    int64
	offsets []int64
}

func (w *failingOffsetWriter) WriteAt(p []byte, offset int64) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.offsets = append(w.offsets, offset)
	if offset == w.fail {
		return 0, os.ErrPermission
	}
	return len(p), nil
}

func TestParallelCopyFailureRetainsContiguousPrefix(t *testing.T) {
	writer := &failingOffsetWriter{fail: 1024}
	n, err := parallelCopy(context.Background(), writer, bytes.NewReader(make([]byte, 3072)), 3072, 0, 1024, 3, nil)
	if n != 1024 || !errors.Is(err, os.ErrPermission) {
		t.Fatal("failure became success or counted a hole", n, err)
	}
	if len(writer.offsets) != 3 {
		t.Fatal("in-flight requests were not joined")
	}
}

func TestParallelCopyShortReadAndCancellation(t *testing.T) {
	writer := &failingOffsetWriter{fail: -1}
	n, err := parallelCopy(context.Background(), writer, bytes.NewReader(make([]byte, 600)), 1000, 0, 1024, 2, nil)
	if n != 600 || !errors.Is(err, io.EOF) {
		t.Fatal("short read lost", n, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	writer.offsets = nil
	n, err = parallelCopy(ctx, writer, bytes.NewReader(make([]byte, 3072)), 3072, 0, 1024, 3, nil)
	if n != 0 || err != context.Canceled || len(writer.offsets) != 0 {
		t.Fatal("cancelled copy mutated destination", n, err)
	}
}
