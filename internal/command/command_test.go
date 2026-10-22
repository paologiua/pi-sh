package command

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

type scriptedStream struct {
	chunks [][]byte
	err    error
	sent   []byte
}

func (s *scriptedStream) Send(_ context.Context, b []byte) error {
	s.sent = append([]byte(nil), b...)
	return nil
}
func (s *scriptedStream) Receive(context.Context) ([]byte, error) {
	if len(s.chunks) == 0 {
		if s.err != nil {
			return nil, s.err
		}
		return nil, io.EOF
	}
	b := s.chunks[0]
	s.chunks = s.chunks[1:]
	return b, nil
}

func TestFragmentedCommandFrames(t *testing.T) {
	p, err := New("printf '%s' 'quoted text'; exit 7")
	if err != nil {
		t.Fatal(err)
	}
	want := []byte("first\nsecond\r\n\x00\xffé世界")
	data := append([]byte("echo and login prompt\r\n"), p.ready...)
	data = append(data, want...)
	data = append(data, p.exit...)
	data = append(data, []byte("7\x1f")...)
	for size := 1; size <= len(data); size++ {
		s := &scriptedStream{}
		for start := 0; start < len(data); start += size {
			s.chunks = append(s.chunks, data[start:min(start+size, len(data))])
		}
		var out bytes.Buffer
		code, err := p.Run(context.Background(), s, &out)
		if err != nil || code != 7 || !bytes.Equal(out.Bytes(), want) {
			t.Fatalf("chunk=%d: code=%d err=%v output=%q", size, code, err, out.Bytes())
		}
		if string(s.sent) != p.bootstrap {
			t.Fatal("wrong bootstrap")
		}
	}
}

func TestMissingMalformedAndAmbiguousStatus(t *testing.T) {
	p, _ := New("false")
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"no-ready", []byte("exit 0")},
		{"no-status", append(append([]byte(nil), p.ready...), []byte("data")...)},
		{"truncated", append(append([]byte(nil), p.ready...), p.exit...)},
		{"out-of-range", []byte(string(p.ready) + string(p.exit) + "256\x1f")},
		{"leading-zero", []byte(string(p.ready) + string(p.exit) + "00\x1f")},
		{"invalid-digit", []byte(string(p.ready) + string(p.exit) + "x\x1f")},
		{"trailing-data", []byte(string(p.ready) + string(p.exit) + "0\x1fextra")},
		{"huge-preamble", bytes.Repeat([]byte("x"), maxPreamble+len(p.ready)+1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := p.Run(context.Background(), &scriptedStream{chunks: [][]byte{tc.data}}, io.Discard); err == nil {
				t.Fatal("unverified status accepted")
			}
		})
	}
	s := &scriptedStream{chunks: [][]byte{[]byte(string(p.ready) + string(p.exit) + "0\x1f"), []byte("late output")}}
	if _, err := p.Run(context.Background(), s, io.Discard); err == nil {
		t.Fatal("late output accepted")
	}
}

func TestCancellationAfterStatusIsFailure(t *testing.T) {
	p, _ := New("true")
	s := &scriptedStream{chunks: [][]byte{[]byte(string(p.ready) + string(p.exit) + "0\x1f")}, err: context.Canceled}
	if _, err := p.Run(context.Background(), s, io.Discard); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestBootstrapBoundsAndQuoting(t *testing.T) {
	for _, text := range []string{"literal\nnewline", "control\x03", string([]byte{255}), strings.Repeat("x", maxBootstrap)} {
		if _, err := New(text); err == nil {
			t.Fatal("unsupported input accepted")
		}
	}
	a, _ := New("printf '%s' \"\u00e9\"; exit 23")
	b, _ := New("true")
	if bytes.Equal(a.ready, b.ready) || strings.Contains(a.bootstrap, "\x1e") {
		t.Fatal("marker reused or echoed literally")
	}
	if !strings.HasPrefix(a.bootstrap, " unset HISTFILE; exec /bin/sh -c '") || len(a.bootstrap) > maxBootstrap {
		t.Fatal("unsafe bootstrap")
	}
}

type unclosedStream struct{ scriptedStream }

func (s *unclosedStream) Receive(ctx context.Context) ([]byte, error) {
	if len(s.chunks) > 0 {
		return s.scriptedStream.Receive(ctx)
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestCompletionRequiresBoundedChannelClose(t *testing.T) {
	p, _ := New("true")
	p.closeWait = 10 * time.Millisecond
	s := &unclosedStream{scriptedStream{chunks: [][]byte{[]byte(string(p.ready) + string(p.exit) + "0\x1f")}}}
	if _, err := p.Run(context.Background(), s, io.Discard); !errors.Is(err, ErrStatus) {
		t.Fatalf("open channel after completion produced %v", err)
	}
}
