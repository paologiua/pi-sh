package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"pi-sh/internal/shellbroker"
)

func TestCoordinatorWaitsForBrokerCleanup(t *testing.T) {
	for _, authorization := range []bool{false, true} {
		for _, confirmed := range []bool{false, true} {
			ctx, cancel := context.WithCancel(context.Background())
			failures := make(chan string, 1)
			started, cleaning, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
			result := make(chan error, 1)
			go func() {
				result <- coordinateCommand(ctx, failures, func(ctx context.Context) (int, error) {
					close(started)
					<-ctx.Done()
					close(cleaning)
					<-release
					if !confirmed {
						return 0, errors.Join(ctx.Err(), shellbroker.ErrCleanupUnconfirmed)
					}
					return 0, ctx.Err()
				}, true)
			}()
			<-started
			if authorization {
				failures <- "synthetic revoked"
			} else {
				cancel()
			}
			select {
			case <-cleaning:
			case <-time.After(time.Second):
				t.Fatal("plan was not cancelled")
			}
			select {
			case err := <-result:
				t.Fatalf("coordinator returned before cleanup: %v", err)
			default:
			}
			close(release)
			select {
			case err := <-result:
				var status *commandStatus
				if err == nil || errors.As(err, &status) {
					t.Fatalf("cancellation became remote status: %v", err)
				}
				if errors.Is(err, shellbroker.ErrCleanupUnconfirmed) == confirmed {
					t.Fatalf("cleanup evidence lost: %v", err)
				}
				if authorization && !strings.Contains(err.Error(), "synthetic revoked") {
					t.Fatal("authorization failure lost")
				}
				if !authorization && !errors.Is(err, context.Canceled) {
					t.Fatal("cancellation lost")
				}
			case <-time.After(time.Second):
				t.Fatal("coordinator did not finish")
			}
			cancel()
		}
	}
}

func TestCoordinatorBoundsUnresponsivePlan(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	result := make(chan error, 1)
	go func() {
		result <- coordinateCommand(ctx, nil, func(ctx context.Context) (int, error) {
			close(started)
			<-release // Model a caller-owned blocked writer that ignores context.
			return 0, ctx.Err()
		}, true)
	}()
	<-started
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) || !errors.Is(err, shellbroker.ErrCleanupUnconfirmed) {
			t.Fatalf("unresponsive plan reported clean shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("coordinator waited indefinitely for caller I/O")
	}
}
