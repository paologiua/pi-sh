package commandrun

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"pi-sh/internal/shellbroker"
)

func TestKnownSessionFailureNeverStartsAdapter(t *testing.T) {
	for _, authorization := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		failures := make(chan string, 1)
		if authorization {
			failures <- "synthetic authorization expired"
		} else {
			cancel()
		}
		var started atomic.Int32
		code, err := Run(ctx, failures, func(context.Context) (int, error) {
			started.Add(1)
			return 23, nil
		}, time.Second)
		cancel()
		if code != 0 || err == nil || started.Load() != 0 {
			t.Fatalf("known failure launched adapter: authorization=%v, starts=%d, code=%d, error=%v", authorization, started.Load(), code, err)
		}
		if authorization && !strings.Contains(err.Error(), "synthetic authorization expired") {
			t.Fatal("authorization cause lost", err)
		}
		if errors.Is(err, ErrAuthorization) != authorization {
			t.Fatal("authorization classification lost", err)
		}
		if !authorization && !errors.Is(err, context.Canceled) {
			t.Fatal("cancellation cause lost", err)
		}
	}
}

func TestSessionCheckClassificationDuringAndAtCompletion(t *testing.T) {
	for _, atCompletion := range []bool{false, true} {
		failures := make(chan string, 1)
		started := make(chan struct{})
		if !atCompletion {
			go func() { <-started; failures <- "synthetic check failure" }()
		}
		code, err := Run(context.Background(), failures, func(ctx context.Context) (int, error) {
			close(started)
			if atCompletion {
				failures <- "synthetic check failure"
				return 23, shellbroker.ErrCleanupUnconfirmed
			}
			<-ctx.Done()
			return 0, errors.Join(ctx.Err(), shellbroker.ErrCleanupUnconfirmed)
		}, time.Second)
		if code != 0 || !errors.Is(err, ErrAuthorization) || !errors.Is(err, shellbroker.ErrCleanupUnconfirmed) {
			t.Fatalf("session check lost identity or cleanup evidence: code=%d error=%v", code, err)
		}
	}
}
