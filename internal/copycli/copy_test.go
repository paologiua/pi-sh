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
		// OpenSSH scp grammar: URIs, bracketed hosts, ignored empty user, leading colon is local.
		{"scp://pi/path", "pi", "path"}, {"scp://pi//abs", "pi", "/abs"}, {"scp://pi", "pi", "."}, {"scp://pi/p%20q", "pi", "p q"},
		{"[pi]:x", "pi", "x"}, {"@pi:x", "pi", "x"}, {":x", "", ":x"}, {"Pi #1:x", "Pi #1", "x"},
	} {
		got, err := Parse(tc.input)
		if err != nil || got.Device != tc.device || got.Path != tc.path {
			t.Fatal(tc, got, err)
		}
	}
	for _, args := range [][]string{{"local"}, {"a", "b"}, {"one:a", "two:b"}, {"a:x", "b:y", "local"}, {"scp://pi/path", "scp://other/x"}, {"user@pi:file", "local"},
		{"scp://user@pi/x", "local"}, {"scp://pi:22/x", "local"}, {"[::1]:x", "local"}, {"alice@[::1]:x", "local"},
		{"scp://[::1]/x", "local"}, {"scp://a@b@pi/x", "local"}, {"scp://pi:0/x", "local"}} {
		if _, err := New(args); err == nil {
			t.Fatal("unsupported operands accepted", args)
		}
	}
}

func TestCopyAccountOnlyOperandChoosesDevice(t *testing.T) {
	p, err := New([]string{"local", "me@example.com:notes.txt"})
	if err != nil || !p.Upload || !p.Choose || p.Device != "" || p.Account != "me@example.com" || p.Target.Path != "notes.txt" {
		t.Fatalf("upload: %+v %v", p, err)
	}
	p, err = New([]string{"me@example.com:a", "me@example.com:", "local"})
	if err != nil || p.Upload || !p.Choose || p.Sources[1].Path != "." {
		t.Fatalf("download: %+v %v", p, err)
	}
	for _, args := range [][]string{
		{"pi@raspberrypi:x", "local"}, {"me@example.com:a", "other@example.com:b", "local"},
		{"me@example.com:a", "me@example.com::pi:b", "local"}, {"local", "me@example.com:a", "local2"},
	} {
		if _, err := New(args); err == nil {
			t.Errorf("accepted %q", args)
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
