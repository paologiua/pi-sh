package shellbroker

import (
	"context"
	"errors"
	"pi-sh/internal/command"
	"strings"
	"testing"
	"time"
)

func TestTTYSettingsValidation(t *testing.T) {
	for _, value := range []string{"xterm\x00secret", "bad\nTERM", "bad\x7f", string([]byte{255}), strings.Repeat("x", 257)} {
		if _, err := NewTTYConfigured("true", TTYSettings{TERM: &value}); err == nil {
			t.Fatal("invalid TERM accepted")
		}
	}
	for _, size := range []TTYSize{{-1, 0}, {0, -1}, {65536, 1}, {1, 65536}} {
		if _, err := NewTTYConfigured("true", TTYSettings{Size: &size}); err == nil {
			t.Fatal("invalid size accepted")
		}
	}
}

func TestTTYInputDrainRequiresCompletedResultAndPreservesFailure(t *testing.T) {
	inputErr := errors.New("fixture carrier send failed")
	outputErr := errors.New("fixture local output failed")
	for _, tc := range []struct {
		name     string
		result   *ttyResult
		cancel   bool
		wantCode int
		wantErr  error
	}{
		{"verified-nonzero", &ttyResult{23, nil}, false, 23, nil},
		{"verified-zero", &ttyResult{0, nil}, false, 0, nil},
		{"missing-or-malformed", &ttyResult{0, command.ErrStatus}, false, 0, command.ErrStatus},
		{"output-failure", &ttyResult{0, outputErr}, false, 0, outputErr},
		{"silent-receiver", nil, false, 0, inputErr},
		{"cancelled-with-queued-success", &ttyResult{23, nil}, true, 0, context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			results := make(chan ttyResult, 1)
			if tc.result != nil {
				results <- *tc.result
			}
			if tc.cancel {
				cancel()
			}
			code, err := drainTTYResult(ctx, results, inputErr, 10*time.Millisecond)
			if code != tc.wantCode || (tc.wantErr == nil && err != nil) || (tc.wantErr != nil && !errors.Is(err, tc.wantErr)) {
				t.Fatalf("code%d err%v want code%d err%v", code, err, tc.wantCode, tc.wantErr)
			}
			if err != nil && !errors.Is(err, ErrCleanupUnconfirmed) {
				t.Fatal("failure lost cleanup boundary", err)
			}
			if tc.result != nil && tc.result.err != nil && !errors.Is(err, inputErr) {
				t.Fatal("reader failure lost input evidence", err)
			}
		})
	}
}
