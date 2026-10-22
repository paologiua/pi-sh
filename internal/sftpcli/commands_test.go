package sftpcli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/pkg/sftp"
)

func memoryClient(t *testing.T) *sftp.Client {
	return memoryClientWith(t, nil)
}

func memoryClientWith(t *testing.T, configure func(*sftp.Handlers)) *sftp.Client {
	t.Helper()
	client, remote := net.Pipe()
	handlers := sftp.InMemHandler()
	if configure != nil {
		configure(&handlers)
	}
	server := sftp.NewRequestServer(remote, handlers)
	go func() { server.Serve(); server.Close() }()
	c, err := sftp.NewClientPipe(client, client)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close(); server.Close() })
	return c
}

type denySetstat struct{ sftp.FileCmder }

func (d denySetstat) Filecmd(r *sftp.Request) error {
	if r.Method == "Setstat" {
		return os.ErrPermission
	}
	return d.FileCmder.Filecmd(r)
}

func TestUploadPermissionFailurePrecedesContents(t *testing.T) {
	client := memoryClientWith(t, func(h *sftp.Handlers) { h.FileCmd = denySetstat{h.FileCmd} })
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "secret"), []byte("must not be sent"), 0600); err != nil {
		t.Fatal(err)
	}
	c := Console{Client: client, LocalDir: dir, RemoteDir: "/"}
	if _, err := c.Execute(context.Background(), []string{"put", "secret", "target"}); err == nil {
		t.Fatal("permission failure ignored")
	}
	info, err := client.Stat("/target")
	if err != nil || info.Size() != 0 {
		t.Fatal("contents sent before permissions were restricted", err)
	}
}

func TestConsoleBinaryTransfersAndCommands(t *testing.T) {
	client := memoryClient(t)
	dir := t.TempDir()
	data := make([]byte, 256*32)
	for i := range data {
		data[i] = byte(i)
	}
	if err := os.WriteFile(filepath.Join(dir, "input ' 世界"), data, 0600); err != nil {
		t.Fatal(err)
	}
	var output, diagnostics bytes.Buffer
	c := Console{Client: client, LocalDir: dir, RemoteDir: "/", Batch: true, Output: &output, Errors: &diagnostics, Input: strings.NewReader(`mkdir sub
cd sub
put "input ' 世界" "remote ' 世界"
ls
chmod 600 "remote ' 世界"
rename "remote ' 世界" renamed
symlink renamed linked
readlink linked
get linked downloaded
rm linked
rm renamed
cd /
rmdir sub
pwd
@lpwd
quit
`)}
	if err := c.Run(context.Background()); err != nil {
		t.Fatalf("console: %v %s", err, diagnostics.String())
	}
	got, err := os.ReadFile(filepath.Join(dir, "downloaded"))
	if err != nil || !bytes.Equal(got, data) {
		t.Fatal("binary transfer round trip changed bytes", err)
	}
	if _, err = client.Stat("/sub"); err == nil {
		t.Fatal("explicit cleanup commands did not run")
	}
	if !strings.Contains(output.String(), "remote ' 世界") || !strings.Contains(output.String(), "renamed") {
		t.Fatal("listing/link result missing")
	}
}

func TestBatchStopIgnoreAndQuiet(t *testing.T) {
	for _, ignore := range []bool{false, true} {
		client := memoryClient(t)
		var out, diagnostics bytes.Buffer
		prefix := ""
		if ignore {
			prefix = "-"
		}
		c := Console{Client: client, LocalDir: t.TempDir(), RemoteDir: "/", Batch: true, Output: &out, Errors: &diagnostics, Input: strings.NewReader(prefix + "rm missing\n@mkdir after\nquit\n")}
		err := c.Run(context.Background())
		if (err == nil) != ignore {
			t.Fatalf("batch failure policy: %v", err)
		}
		_, exists := client.Stat("/after")
		if (exists == nil) != ignore {
			t.Fatal("batch executed wrong suffix")
		}
		if strings.Contains(out.String(), "mkdir after") {
			t.Fatal("quiet prefix echoed command")
		}
	}
}

func TestCommandQuotingAndUnsupportedFlags(t *testing.T) {
	args, err := Split(`put 'one two' "three\"four" five\ six # comment`)
	if err != nil || !reflect.DeepEqual(args, []string{"put", "one two", "three\"four", "five six"}) {
		t.Fatal(args, err)
	}
	for _, line := range []string{"get 'unterminated", "get dangling\\", "get x\x00y"} {
		if _, err := Split(line); err == nil {
			t.Fatal("malformed command accepted")
		}
	}
	c := Console{Client: memoryClient(t), RemoteDir: "/", LocalDir: t.TempDir(), Output: io.Discard}
	for _, args := range [][]string{{"put", "-r", "x"}, {"!", "local-command"}, {"chmod", "4777", "x"}, {"quit", "unexpected"}} {
		done, err := c.Execute(context.Background(), args)
		if err == nil || done {
			t.Fatalf("unsupported command accepted: %q", args)
		}
	}
	if _, err := c.Execute(context.Background(), []string{"get", "missing", "local"}); err == nil {
		t.Fatal("get lost a remote failure")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c.Input = strings.NewReader("")
	if err := c.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled console reported success")
	}
}

func TestPathComponentsAreNotCleanedAndUnsafeBasenamesRejected(t *testing.T) {
	c := Console{RemoteDir: "/remote", LocalDir: "local"}
	if c.remote("link/../file") != "/remote/link/../file" {
		t.Fatal("remote symlink traversal changed")
	}
	if !strings.Contains(c.local("link/../file"), "link/../file") {
		t.Fatal("local symlink traversal changed")
	}
	for _, name := range []string{"/", ".", ".."} {
		if _, err := localBase(name); err == nil {
			t.Fatal("unsafe automatic basename accepted")
		}
	}
}

func TestDestinationIncludeUsesCLIHostnameBeforeLoginPolicy(t *testing.T) {
	dir := t.TempDir()
	for name, text := range map[string]string{
		"root":     "HostName fallback\nInclude \"" + filepath.ToSlash(dir) + "/%h\"\n",
		"fallback": "RequestTTY no\n",
		"override": "StdinNull yes\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	target, err := Destination("alias", filepath.Join(dir, "root"), nil)
	if err != nil || target != "fallback" {
		t.Fatal("included alias configuration", target, err)
	}
	if _, err := Destination("alias", filepath.Join(dir, "root"), []string{"HostName=override"}); err == nil || !strings.Contains(err.Error(), "private protocol stdin") {
		t.Fatal("Include did not use CLI HostName before policy validation", err)
	}
}
