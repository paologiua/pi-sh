package sftpcli

import (
	"bytes"
	"context"
	"errors"
	"github.com/pkg/sftp"
	"io"
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

func TestRecursiveErrorClassificationAndRedaction(t *testing.T) {
	for _, err := range []error{os.ErrInvalid, os.ErrNotExist, &sftp.StatusError{Code: 3}, errors.Join(os.ErrPermission, os.ErrInvalid)} {
		if !treeRecoverable(err) {
			t.Fatalf("ordinary error not recoverable: %v", err)
		}
	}
	for _, err := range []error{context.Canceled, io.EOF, matchError(true, "path", errors.Join(&sftp.StatusError{Code: 3}, io.EOF)), &sftp.StatusError{Code: 6}, &sftp.StatusError{Code: 7}, errors.Join(os.ErrPermission, io.EOF)} {
		if treeRecoverable(err) {
			t.Fatalf("fatal error recoverable: %v", err)
		}
	}
	raw := errors.New("synthetic-secret-server-text")
	err := safeError(errors.Join(shown(os.ErrInvalid, "safe path: invalid"), raw))
	if strings.Contains(err.Error(), raw.Error()) || !strings.Contains(err.Error(), "safe path") {
		t.Fatal("joined error redaction", err)
	}
	var diagnostic bytes.Buffer
	c := Console{Errors: &diagnostic}
	c.report(errors.Join(shown(os.ErrInvalid, "safe path: invalid"), raw))
	if strings.Contains(diagnostic.String(), raw.Error()) {
		t.Fatal("reported joined error leaked server text")
	}
	wrapped := matchError(true, "safe path", os.ErrPermission)
	if !treeRecoverable(errors.Join(wrapped)) {
		t.Fatal("label lost error classification")
	}
}

func TestRecursiveCommandFlagsAndCancellation(t *testing.T) {
	for _, flag := range []string{"-r", "-R", "-arRpf"} {
		args, options, err := transferFlags([]string{flag, "source"}, transferOptions{})
		if err != nil || !options.recursive || len(args) != 1 {
			t.Fatal(args, options, err)
		}
	}
	c := &Console{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, op := range []string{"get", "put", "mget", "mput", "reget", "reput"} {
		if _, err := c.Execute(ctx, []string{op, "-r", "unused"}); err != context.Canceled {
			t.Fatal(op, err)
		}
	}
}
