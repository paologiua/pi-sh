package platform

import (
	"context"
	"testing"
)

func TestLocalRuntime(t *testing.T) {
	r := Check(context.Background())
	if len(r.Results) != 3 {
		t.Fatalf("missing checks: %+v", r)
	}
	for _, v := range r.Results {
		if !v.Passed {
			t.Errorf("%s: %s", v.Name, v.Error)
		}
	}
}

func TestCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := Check(ctx)
	for _, v := range r.Results {
		if v.Passed {
			t.Errorf("canceled check passed: %s", v.Name)
		}
	}
}
