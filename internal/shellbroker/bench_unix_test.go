//go:build darwin || linux

package shellbroker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// delayedStream adds a fixed one-way delay in each direction to the local PTY
// carrier, preserving order. Sends return after local queuing, like WebRTC.
type delayedStream struct {
	out  chan timedFrame
	in   chan timedFrame
	done <-chan struct{}
	// receiveDelay, when nonzero, overrides oneWay for received frames.
	receiveDelay atomic.Int64
	// delayAfterCommit starts receiveDelay only once G was sent.
	delayAfterCommit time.Duration
}

type timedFrame struct {
	due  time.Time
	data []byte
	err  error
}

func newDelayedStream(ctx context.Context, file *os.File, oneWay time.Duration) *delayedStream {
	inner := newTestStream(ctx, file)
	s := &delayedStream{out: make(chan timedFrame, 1<<16), in: make(chan timedFrame, 1<<16), done: ctx.Done()}
	go func() {
		for {
			select {
			case f := <-s.out:
				time.Sleep(time.Until(f.due))
				if _, err := file.Write(f.data); err != nil {
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		for {
			select {
			case r := <-inner.frames:
				delay := oneWay
				if d := s.receiveDelay.Load(); d > 0 {
					delay = time.Duration(d)
				}
				select {
				case s.in <- timedFrame{time.Now().Add(delay), r.data, r.err}:
				case <-ctx.Done():
					return
				}
				if r.err != nil {
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return s
}

func (s *delayedStream) Send(ctx context.Context, b []byte) error {
	if s.delayAfterCommit > 0 && bytes.Contains(b, []byte(":G ")) {
		s.receiveDelay.Store(int64(s.delayAfterCommit))
	}
	select {
	case s.out <- timedFrame{due: time.Now().Add(0), data: append([]byte(nil), b...)}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *delayedStream) Receive(ctx context.Context) ([]byte, error) {
	select {
	case f := <-s.in:
		time.Sleep(time.Until(f.due))
		return f.data, f.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// TestShellBrokerThroughputBenchmark measures local broker throughput with a
// simulated round trip. It is opt-in: PI_SH_BROKER_BENCH=1, optional
// PI_SH_BROKER_BENCH_RTT (comma-separated milliseconds) and _BYTES.
func TestShellBrokerThroughputBenchmark(t *testing.T) {
	if os.Getenv("PI_SH_BROKER_BENCH") == "" {
		t.Skip("set PI_SH_BROKER_BENCH=1 to run the throughput benchmark")
	}
	size := 1 << 20
	if v := os.Getenv("PI_SH_BROKER_BENCH_BYTES"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			t.Fatal("invalid PI_SH_BROKER_BENCH_BYTES")
		}
		size = n
	}
	rtts := []int{0, 10, 40}
	if v := os.Getenv("PI_SH_BROKER_BENCH_RTT"); v != "" {
		rtts = nil
		for _, field := range strings.Split(v, ",") {
			n, err := strconv.Atoi(field)
			if err != nil || n < 0 {
				t.Fatal("invalid PI_SH_BROKER_BENCH_RTT")
			}
			rtts = append(rtts, n)
		}
	}
	input := make([]byte, size)
	for i := range input {
		input[i] = byte(i * 7)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(input))
	generated := "LC_ALL=C head -c " + strconv.Itoa(size) + " /dev/zero; LC_ALL=C head -c " + strconv.Itoa(size) + " /dev/urandom"
	for _, rtt := range rtts {
		oneWay := time.Duration(rtt) * time.Millisecond / 2
		makeStream := func(ctx context.Context, file *os.File) Stream { return newDelayedStream(ctx, file, oneWay) }
		for _, tc := range []struct {
			name, text string
			in         []byte
			moved      int
			check      func(out []byte) bool
		}{
			{"upload", "LC_ALL=C wc -c", input, size, func(out []byte) bool { return strings.TrimSpace(string(out)) == strconv.Itoa(size) }},
			{"download", generated, nil, 2 * size, func(out []byte) bool { return len(out) == 2*size && bytes.Count(out[:size], []byte{0}) == size }},
			{"roundtrip", "cat", input, 2 * size, func(out []byte) bool { return fmt.Sprintf("%x", sha256.Sum256(out)) == digest }},
		} {
			var out, stderr bytes.Buffer
			start := time.Now()
			code, err := runLocalWithStream(t, tc.text, func(context.CancelFunc) IO {
				stdio := IO{Stdout: &out, Stderr: &stderr}
				if tc.in != nil {
					stdio.Stdin = bytes.NewReader(tc.in)
				}
				return stdio
			}, nil, 900*time.Second, makeStream)
			elapsed := time.Since(start)
			if err != nil || code != 0 || !tc.check(out.Bytes()) || stderr.Len() != 0 {
				t.Fatalf("%s rtt=%dms: code=%d err=%v out=%d stderr=%q", tc.name, rtt, code, err, out.Len(), stderr.String())
			}
			t.Logf("%-9s rtt=%2dms moved=%d elapsed=%v rate=%.1fKiB/s", tc.name, rtt, tc.moved, elapsed.Round(time.Millisecond), float64(tc.moved)/1024/elapsed.Seconds())
		}
	}
}

// Real-Bash form of the native 2026-10-22 defect: stdin fails locally while
// `exit 0` completes remotely. The broker sends X and exits before C arrives,
// so D can never come; the in-flight X must still confirm cleanup.
func TestShellBrokerAbortAfterRemoteCompletion(t *testing.T) {
	for i := 0; i < 3; i++ {
		code, err := runLocalWithStream(t, "exit 0", func(context.CancelFunc) IO {
			return IO{Stdin: failingInput{}}
		}, nil, 20*time.Second, func(ctx context.Context, file *os.File) Stream {
			s := newDelayedStream(ctx, file, 0)
			s.delayAfterCommit = 500 * time.Millisecond
			return s
		})
		if code != 0 || err == nil || !strings.Contains(err.Error(), "local stdin read failed") || errors.Is(err, ErrCleanupUnconfirmed) {
			t.Fatalf("run %d: %d %v", i, code, err)
		}
	}
}
