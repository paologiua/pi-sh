package sftpcli

import (
	"context"
	"io"
	"time"
)

// throttledReader charges file payload bytes, not signaling or metadata. It
// measures elapsed I/O time too, so a slow connection incurs no extra delay.
type throttledReader struct {
	io.Reader
	ctx   context.Context
	kbps  uint64
	start time.Time
	count uint64
}

func (r *throttledReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.Reader.Read(p)
	if n <= 0 {
		return n, err
	}
	r.count += uint64(n)
	// Split into seconds/remainder to avoid multiplying a whole large file by ns.
	bytesPerSecond := r.kbps * 128
	seconds, rest := r.count/bytesPerSecond, r.count%bytesPerSecond
	deadline := r.start.Add(time.Duration(seconds)*time.Second + time.Duration(rest*uint64(time.Second)/bytesPerSecond))
	if delay := time.Until(deadline); delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-r.ctx.Done():
			return 0, r.ctx.Err()
		}
	}
	return n, err
}
