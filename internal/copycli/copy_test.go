package copycli

import (
	"bytes"
	"context"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/pkg/sftp"
)

func TestCopyOperandSyntax(t *testing.T) {
	for _, tc := range []struct{ input, device, path string }{
		{"pi:file", "pi", "file"}, {"pi:", "pi", "."}, {"./file:colon", "", "./file:colon"}, {"/dir/file:colon", "", "/dir/file:colon"}, {"C:\\dir\\file", "", "C:\\dir\\file"}, {"local", "", "local"},
	} {
		got, err := Parse(tc.input)
		if err != nil || got.Device != tc.device || got.Path != tc.path {
			t.Fatal(tc, got, err)
		}
	}
	for _, args := range [][]string{{"local"}, {"a", "b"}, {"one:a", "two:b"}, {"a:x", "b:y", "local"}, {"scp://pi/path", "local"}, {"user@pi:file", "local"}} {
		if _, err := New(args); err == nil {
			t.Fatal("unsupported operands accepted", args)
		}
	}
}

func TestCopyMultipleBinaryFilesAndDirectoryPreflight(t *testing.T) {
	local, remote := net.Pipe()
	server := sftp.NewRequestServer(remote, sftp.InMemHandler())
	go func() { server.Serve(); server.Close() }()
	client, err := sftp.NewClientPipe(local, local)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	defer server.Close()
	dir := t.TempDir()
	source1 := filepath.Join(dir, "one ' 世界")
	second := "two:colon"
	if runtime.GOOS == "windows" {
		second = "two.bin"
	}
	source2 := filepath.Join(dir, second)
	data := make([]byte, 256*64)
	for i := range data {
		data[i] = byte(i)
	}
	for _, file := range []string{source1, source2} {
		if err := os.WriteFile(file, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := client.Mkdir("/dest"); err != nil {
		t.Fatal(err)
	}
	plan, err := New([]string{source1, source2, "pi:/dest"})
	if err != nil {
		t.Fatal(err)
	}
	if err = plan.Run(context.Background(), client, nil); err != nil {
		t.Fatal(err)
	}
	downloads := filepath.Join(dir, "downloads")
	if err = os.Mkdir(downloads, 0700); err != nil {
		t.Fatal(err)
	}
	plan, err = New([]string{"pi:/dest/one ' 世界", "pi:/dest/" + second, downloads})
	if err != nil {
		t.Fatal(err)
	}
	if err = plan.Run(context.Background(), client, nil); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"one ' 世界", second} {
		got, err := os.ReadFile(filepath.Join(downloads, name))
		if err != nil || !bytes.Equal(got, data) {
			t.Fatal("copy binary mismatch", err)
		}
	}
	plan, err = New([]string{source1, source2, "pi:/missing"})
	if err != nil {
		t.Fatal(err)
	}
	if err = plan.Run(context.Background(), client, nil); err == nil {
		t.Fatal("multiple sources overwrote a missing destination")
	}
	if _, err = client.Stat("/missing"); err == nil {
		t.Fatal("failed preflight mutated destination")
	}
}
