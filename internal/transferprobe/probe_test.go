package transferprobe

import (
	"context"
	"strings"
	"testing"
)

func TestScratchPlanRejectsGeneralPathsBeforeAnyClientOperation(t *testing.T) {
	for _, root := range []string{"/tmp", "/", "relative", "/tmp/pi-sh-probe-1", "/tmp/pi-sh-probe-" + strings.Repeat("a", 32) + "/..", "/tmp/pi-sh-probe-" + strings.Repeat("a", 32) + "/", "/tmp/pi-sh-probe-" + strings.Repeat("A", 32), "/tmp/pi-sh-probe-" + strings.Repeat("a", 32) + "\n"} {
		if _, err := Plan(root); err == nil {
			t.Fatal("unsafe root accepted", root)
		}
		if err := Run(context.Background(), nil, root, 0022, nil); err == nil {
			t.Fatal("invalid root reached run", root)
		}
		if err := Cleanup(nil, root); err == nil {
			t.Fatal("invalid root reached cleanup", root)
		}
	}
	root, err := NewRoot()
	if err != nil {
		t.Fatal(err)
	}
	plan, err := Plan(root)
	if err != nil || !strings.Contains(plan, root) || !strings.Contains(plan, "1,314,816") || !strings.Contains(plan, "tree/nested/leaf ' 世界.bin") {
		t.Fatal("plan not concrete", plan, err)
	}
	next, err := NewRoot()
	if err != nil || next == root {
		t.Fatal("scratch nonce reused", err)
	}
	names := KnownChildren()
	names[0] = "changed"
	if KnownChildren()[0] == "changed" {
		t.Fatal("cleanup allowlist mutable")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Run(ctx, nil, root, 0022, nil); err != context.Canceled {
		t.Fatal("cancelled run proceeded", err)
	}
}
