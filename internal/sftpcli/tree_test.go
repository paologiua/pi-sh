package sftpcli

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestTreePathsRemainLiteralAndRejectTraversalNames(t *testing.T) {
	for _, name := range []string{"", ".", "..", "../escape", "dir/child", "nul\x00byte"} {
		if treeName(name, false) || treeName(name, true) {
			t.Fatalf("unsafe entry accepted: %q", name)
		}
	}
	for _, name := range []string{"世界", " space ", "quote'", "line\nbreak", "-leading"} {
		if !treeName(name, false) || !treeName(name, true) {
			t.Fatalf("literal name rejected: %q", name)
		}
	}
	if got := treeAppend("link/../directory", "child", false); got != "link/../directory/child" {
		t.Fatal("remote path was cleaned", got)
	}
	root := "link" + string(os.PathSeparator) + ".." + string(os.PathSeparator) + "directory"
	if got := treeAppend(root, "child", true); got != root+string(os.PathSeparator)+"child" {
		t.Fatal("local path was cleaned", got)
	}
	if treeBase("directory/.", false) != "." || treeBase("directory/", false) != "directory" {
		t.Fatal("directory suffix behavior changed")
	}
}

func TestTreeDepthAndCancellationBeforeMutation(t *testing.T) {
	console := &Console{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := console.CopyTree(ctx, true, "unused", "unused"); err != context.Canceled {
		t.Fatal("cancellation did not precede filesystem access", err)
	}
	if err := console.treeDir(context.Background(), true, "unused", "unused", nil, make([]os.FileInfo, 64), nil); err == nil || !strings.Contains(err.Error(), "depth") {
		t.Fatal("depth limit did not precede filesystem access", err)
	}
	if err := console.treeDir(context.Background(), false, "unused", "unused", nil, nil, make([]string, 64)); err == nil || !strings.Contains(err.Error(), "depth") {
		t.Fatal("remote depth limit did not precede filesystem access", err)
	}
}
