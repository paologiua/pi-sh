package shellbroker

import (
	"context"
	"errors"
	"testing"
	"time"
)

type silentStream struct{}

func (silentStream) Send(ctx context.Context, b []byte) error { return ctx.Err() }
func (silentStream) Receive(ctx context.Context) ([]byte, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// A device that never answers the bootstrap ends staging after stageTimeout.
// Before, staging waited on the caller's context alone, so a TTY session sat
// with the local terminal raw (Ctrl-C and ~. not yet read) indefinitely.
func TestStagingIsBounded(t *testing.T) {
	old := stageTimeout
	stageTimeout = 150 * time.Millisecond
	defer func() { stageTimeout = old }()
	adapter, err := NewAdapter("true\n", []string{"cat"}, "needs cat")
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if _, err := adapter.Start(context.Background(), silentStream{}); !errors.Is(err, ErrStageTimeout) {
		t.Fatal(err)
	}
	if time.Since(started) > 5*time.Second {
		t.Fatal("staging was not bounded")
	}
}
