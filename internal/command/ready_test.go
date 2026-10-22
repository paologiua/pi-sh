package command

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"
)

type promptStream struct {
	plan   *Plan
	output *bytes.Buffer
	calls  int
}

func (s *promptStream) Send(context.Context, []byte) error { return nil }
func (s *promptStream) Receive(context.Context) ([]byte, error) {
	s.calls++
	switch s.calls {
	case 1:
		return append(bytes.Clone(s.plan.ready), []byte("prompt")...), nil
	case 2:
		if s.output.String() != "prompt" {
			return nil, errors.New("interactive prompt was withheld")
		}
		return append(bytes.Clone(s.plan.exit), []byte("23\x1f")...), nil
	default:
		return nil, io.EOF
	}
}

func TestReadResultFlushesInteractivePromptBeforeNextReceive(t *testing.T) {
	p, _ := New("true")
	var output bytes.Buffer
	ready := 0
	code, err := ReadResult(context.Background(), &promptStream{plan: p, output: &output}, &output, p.ready, p.exit, time.Second, func() error { ready++; return nil })
	if err != nil || code != 23 || ready != 1 || output.String() != "prompt" {
		t.Fatalf("prompt/code/readiness: %d %d %q %v", code, ready, output.String(), err)
	}
}

func TestReadResultReadyCallbackFailureAndFraming(t *testing.T) {
	p, _ := New("true")
	want := errors.New("readiness setup failed")
	_, err := ReadResult(context.Background(), &scriptedStream{chunks: [][]byte{append(bytes.Clone(p.ready), []byte("must not reach output")...)}}, io.Discard, p.ready, p.exit, time.Second, func() error { return want })
	if !errors.Is(err, want) {
		t.Fatal("readiness failure ignored", err)
	}
	for _, frames := range [][2][]byte{{nil, p.exit}, {p.ready, nil}, {p.ready, p.ready}, {bytes.Repeat([]byte{'x'}, 257), p.exit}} {
		if _, err := ReadResult(context.Background(), nil, nil, frames[0], frames[1], time.Second, nil); err == nil {
			t.Fatal("invalid framing accepted")
		}
	}
}
