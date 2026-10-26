//go:build darwin || linux

package copycli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pi-sh/internal/progress"
)

func TestRecursiveProgressUploadsAndDownloadsEachFile(t *testing.T) {
	client, _ := recursiveClient(t)
	dir := t.TempDir()
	root := filepath.Join(dir, "tree")
	if err := os.MkdirAll(filepath.Join(root, "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{"empty": {}, "one": bytes.Repeat([]byte{0, 255}, 32769), filepath.Join("nested", "two"): []byte("two")}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(root, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	destination := filepath.Join(dir, "destination")
	if err := os.Mkdir(destination, 0700); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	display := progress.New(&output, func() (int, bool) { return 80, true })
	for _, args := range [][]string{{root, "device:" + destination}, {"device:" + filepath.Join(destination, "tree"), filepath.Join(dir, "download")}} {
		plan, err := New(args)
		if err != nil {
			t.Fatal(err)
		}
		plan.Recursive, plan.Progress = true, display
		if err := plan.Run(context.Background(), client, nil); err != nil {
			t.Fatal(err)
		}
	}
	if strings.Count(output.String(), "100%") != 6 || strings.Count(output.String(), "\n") != 6 {
		t.Fatalf("expected one completed line per file per direction: %q", output.String())
	}
	for name, want := range files {
		got, err := os.ReadFile(filepath.Join(dir, "download", name))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatal("progress changed file data", name, err)
		}
	}
}
