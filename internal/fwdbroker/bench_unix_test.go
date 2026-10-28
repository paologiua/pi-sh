//go:build darwin || linux

package fwdbroker

import (
	"bytes"
	"context"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// delayed adds a fixed one-way delay in each direction to a carrier,
// preserving order. Sends return after local queuing, like WebRTC.
type delayed struct {
	inner  ptyStream
	oneWay time.Duration
	out    chan timedFrame
	in     chan timedFrame
	done   <-chan struct{}
}

type timedFrame struct {
	due  time.Time
	data []byte
	err  error
}

func newDelayed(ctx context.Context, inner ptyStream, oneWay time.Duration) *delayed {
	d := &delayed{inner: inner, oneWay: oneWay, out: make(chan timedFrame, 1<<16), in: make(chan timedFrame, 1<<16), done: ctx.Done()}
	go func() {
		for {
			select {
			case f := <-d.out:
				time.Sleep(time.Until(f.due))
				if _, err := inner.file.Write(f.data); err != nil {
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
				select {
				case d.in <- timedFrame{time.Now().Add(oneWay), r.data, r.err}:
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
	return d
}

func (d *delayed) Send(ctx context.Context, b []byte) error {
	select {
	case d.out <- timedFrame{due: time.Now().Add(d.oneWay), data: append([]byte(nil), b...)}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (d *delayed) Receive(ctx context.Context) ([]byte, error) {
	select {
	case f := <-d.in:
		time.Sleep(time.Until(f.due))
		return f.data, f.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// TestForwardingThroughputBenchmark measures one forwarded connection in
// each direction (docs/FORWARDING.md "Throughput"). It is opt-in:
// PI_SH_FWD_BENCH=1, optional PI_SH_FWD_BENCH_BYTES and PI_SH_FWD_BENCH_RTT
// (comma-separated milliseconds).
func TestForwardingThroughputBenchmark(t *testing.T) {
	if os.Getenv("PI_SH_FWD_BENCH") == "" {
		t.Skip("set PI_SH_FWD_BENCH=1 to run the forwarding throughput benchmark")
	}
	size := 1 << 20
	if v := os.Getenv("PI_SH_FWD_BENCH_BYTES"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			t.Fatal("invalid PI_SH_FWD_BENCH_BYTES")
		}
		size = n
	}
	rtts := []int{0, 40}
	if v := os.Getenv("PI_SH_FWD_BENCH_RTT"); v != "" {
		rtts = nil
		for _, f := range strings.Split(v, ",") {
			n, err := strconv.Atoi(f)
			if err != nil || n < 0 {
				t.Fatal("invalid PI_SH_FWD_BENCH_RTT")
			}
			rtts = append(rtts, n)
		}
	}
	payload := make([]byte, size)
	for i := range payload {
		payload[i] = byte(i * 7)
	}
	// One server sends the payload and closes; another counts what it gets.
	source, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	go func() {
		for {
			c, err := source.Accept()
			if err != nil {
				return
			}
			go func() { c.Write(payload); c.Close() }()
		}
	}()
	sink, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer sink.Close()
	go func() {
		for {
			c, err := sink.Accept()
			if err != nil {
				return
			}
			go func() {
				n, _ := io.Copy(io.Discard, c)
				c.Write([]byte(strconv.FormatInt(n, 10)))
				c.Close()
			}()
		}
	}()
	for _, connector := range []string{"nc", "perl"} {
		for _, rtt := range rtts {
			m, err := startMuxDelayed(t, connector, time.Duration(rtt)*time.Millisecond/2)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			start := time.Now()
			s, err := m.Dial(ctx, tcpTarget(source))
			if err != nil {
				t.Fatal(err)
			}
			s.CloseWrite()
			got, err := io.ReadAll(s)
			down := time.Since(start)
			s.Close()
			if err != nil || len(got) != size {
				t.Fatalf("%s download: %d bytes, %v", connector, len(got), err)
			}
			start = time.Now()
			s, err = m.Dial(ctx, tcpTarget(sink))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.Write(payload); err != nil {
				t.Fatal(err)
			}
			s.CloseWrite()
			reply, err := io.ReadAll(s)
			up := time.Since(start)
			s.Close()
			cancel()
			if err != nil || string(reply) != strconv.Itoa(size) {
				t.Fatalf("%s upload: reply %q, %v", connector, reply, err)
			}
			kib := float64(size) / 1024
			t.Logf("connector=%s rtt=%dms download=%.1f KiB/s upload=%.1f KiB/s", connector, rtt, kib/down.Seconds(), kib/up.Seconds())
		}
	}
}

// TestForwardingConcurrencyBenchmark measures the total rate of 1, 16 and 64
// concurrent 256 KiB downloads through the Perl connector with a 40 ms round
// trip, and checks every byte. Opt-in like the benchmark above
// (PI_SH_FWD_BENCH=1). On 2026-10-28 handling one carrier record per adapter
// round gave about 1.9 MiB/s for 16 streams and 0.6 MiB/s for 64; draining up
// to 64 records per round gave about 4 MiB/s for both.
func TestForwardingConcurrencyBenchmark(t *testing.T) {
	if os.Getenv("PI_SH_FWD_BENCH") == "" {
		t.Skip("set PI_SH_FWD_BENCH=1 to run the forwarding concurrency benchmark")
	}
	payload := make([]byte, 256<<10)
	for i := range payload {
		payload[i] = byte(i*7 + i>>8)
	}
	source, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	go func() {
		for {
			c, err := source.Accept()
			if err != nil {
				return
			}
			go func() { c.Write(payload); c.Close() }()
		}
	}()
	for _, k := range []int{1, 16, 64} {
		m, err := startMuxDelayed(t, "perl", 20*time.Millisecond)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		var wg sync.WaitGroup
		var bad atomic.Int32
		start := time.Now()
		for range k {
			wg.Add(1)
			go func() {
				defer wg.Done()
				s, err := m.Dial(ctx, tcpTarget(source))
				if err != nil {
					bad.Add(1)
					return
				}
				defer s.Close()
				if got, err := io.ReadAll(s); err != nil || !bytes.Equal(got, payload) {
					bad.Add(1)
				}
			}()
		}
		wg.Wait()
		cancel()
		if bad.Load() > 0 {
			t.Fatalf("%d streams: %d incomplete", k, bad.Load())
		}
		t.Logf("streams=%d total=%.0f KiB/s", k, float64(k*256)/time.Since(start).Seconds())
	}
}
