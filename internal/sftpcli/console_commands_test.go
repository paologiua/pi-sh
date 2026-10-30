package sftpcli

import (
	"bytes"
	"context"
	"github.com/pkg/sftp"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestVersionConsoleOutput(t *testing.T) {
	var out bytes.Buffer
	c := Console{Output: &out}
	if _, err := c.Execute(context.Background(), []string{"version"}); err != nil {
		t.Fatal(err)
	}
	if out.String() != "SFTP protocol version 3\n" {
		t.Fatalf("unexpected version output %q", out.String())
	}
	if _, err := c.Execute(context.Background(), []string{"version", "extra"}); err == nil {
		t.Fatal("extra operand accepted")
	}
}

func TestCommandsIgnoreCase(t *testing.T) {
	var out bytes.Buffer
	c := Console{Output: &out}
	if _, err := c.Execute(context.Background(), []string{"VeRsIoN"}); err != nil {
		t.Fatal(err)
	}
	if out.String() != "SFTP protocol version 3\n" {
		t.Fatal(out.String())
	}
}

func TestChdirAlias(t *testing.T) {
	c := Console{Client: memoryClient(t), RemoteDir: "/"}
	if err := c.Client.Mkdir("/sub"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Execute(context.Background(), []string{"chdir", "sub"}); err != nil {
		t.Fatal(err)
	}
	if c.RemoteDir != "/sub" {
		t.Fatal(c.RemoteDir)
	}
}

func TestLchdirAlias(t *testing.T) {
	c := Console{LocalDir: t.TempDir()}
	target := t.TempDir()
	if _, err := c.Execute(context.Background(), []string{"lchdir", target}); err != nil {
		t.Fatal(err)
	}
	if c.LocalDir != target {
		t.Fatal(c.LocalDir)
	}
}

func TestDirAlias(t *testing.T) {
	var a, b bytes.Buffer
	c := Console{Client: memoryClient(t), RemoteDir: "/", Output: &a}
	if err := c.Client.Mkdir("/sub"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Execute(context.Background(), []string{"dir"}); err != nil {
		t.Fatal(err)
	}
	c.Output = &b
	if _, err := c.Execute(context.Background(), []string{"ls"}); err != nil {
		t.Fatal(err)
	}
	if a.String() != b.String() {
		t.Fatal(a.String(), b.String())
	}
}

func TestCdReturnsToInitialDirectory(t *testing.T) {
	c := Console{Client: memoryClient(t), RemoteDir: "/"}
	if err := c.Client.Mkdir("/sub"); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"cd", "sub"}, {"cd"}} {
		if _, err := c.Execute(context.Background(), args); err != nil {
			t.Fatal(err)
		}
	}
	if c.RemoteDir != "/" {
		t.Fatal(c.RemoteDir)
	}
}

func TestLcdReturnsHome(t *testing.T) {
	home := t.TempDir()
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
	} else {
		t.Setenv("HOME", home)
	}
	c := Console{LocalDir: t.TempDir()}
	if _, err := c.Execute(context.Background(), []string{"lcd"}); err != nil {
		t.Fatal(err)
	}
	if c.LocalDir != home {
		t.Fatal(c.LocalDir)
	}
}

func TestLmkdirLiteral(t *testing.T) {
	root := t.TempDir()
	c := Console{LocalDir: root}
	if _, err := c.Execute(context.Background(), []string{"lmkdir", "name with space"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(root, "name with space"))
	if err != nil || !info.IsDir() {
		t.Fatal(err)
	}
	if _, err := c.Execute(context.Background(), []string{"lmkdir", "name with space"}); err == nil {
		t.Fatal("existing directory accepted")
	}
}

func TestLumaskCreationAndOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix mode comparison")
	}
	var out bytes.Buffer
	root := t.TempDir()
	c := Console{LocalDir: root, Output: &out}
	if _, err := c.Execute(context.Background(), []string{"lumask", "077"}); err != nil {
		t.Fatal(err)
	}
	if out.String() != "Local umask: 077\n" {
		t.Fatal(out.String())
	}
	if _, err := c.Execute(context.Background(), []string{"lmkdir", "private"}); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(filepath.Join(root, "private"))
	if info.Mode().Perm() != 0700 {
		t.Fatal(info.Mode())
	}
	file, err := c.openLocalFile(filepath.Join(root, "data"), os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
	info, _ = os.Stat(filepath.Join(root, "data"))
	if info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
	if _, err := c.Execute(context.Background(), []string{"lumask", "000"}); err != nil {
		t.Fatal(err)
	}
	file, err = c.openLocalFile(filepath.Join(root, "data"), os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
	info, _ = os.Stat(filepath.Join(root, "data"))
	if info.Mode().Perm() != 0600 {
		t.Fatal("existing file mode changed")
	}
	file, err = c.openLocalFile(filepath.Join(root, "public"), os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
	info, _ = os.Stat(filepath.Join(root, "public"))
	if info.Mode().Perm() != 0666 {
		t.Fatal("mask failed to replace inherited mask", info.Mode())
	}
}

func TestNoisyOverridesBatchNotices(t *testing.T) {
	client := memoryClient(t)
	file, err := client.Create("/dir")
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
	var out bytes.Buffer
	c := Console{Client: client, RemoteDir: "/", Output: &out, Batch: true, Noisy: true}
	if _, err := c.Execute(context.Background(), []string{"chmod", "700", "dir"}); err != nil {
		t.Fatal(err)
	}
	if out.String() != "Changing mode on /dir\n" {
		t.Fatal(out.String())
	}
	if !c.ProgressDisabled {
		t.Fatal("noisy incorrectly enabled the meter")
	}
}

type chunkWriter struct{ max, count int }

func (w *chunkWriter) Write(p []byte) (int, error) {
	w.max = max(w.max, len(p))
	w.count += len(p)
	return len(p), nil
}
func TestTransferBufferSize(t *testing.T) {
	w := &chunkWriter{}
	c := Console{BufferSize: 1024}
	data := bytes.Repeat([]byte{1}, 4097)
	n, err := c.copyData(w, bytes.NewReader(data), "file", int64(len(data)), 0)
	if err != nil || n != 4097 || w.count != 4097 || w.max != 1024 {
		t.Fatal(n, w, err)
	}
}

func TestBandwidthLimitAndCancellation(t *testing.T) {
	c := Console{Bandwidth: 128}
	start := time.Now()
	var dst bytes.Buffer
	data := bytes.Repeat([]byte{1}, 1024)
	n, err := c.copyData(&dst, bytes.NewReader(data), "file", 1024, 0)
	if err != nil || n != 1024 || !bytes.Equal(dst.Bytes(), data) {
		t.Fatal(n, err)
	}
	if time.Since(start) < 50*time.Millisecond {
		t.Fatal("bandwidth limit was not applied")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c.Context = ctx
	dst.Reset()
	if _, err := c.copyData(&dst, bytes.NewReader(data), "file", 1024, 0); err != context.Canceled || dst.Len() != 0 {
		t.Fatal("cancellation", err, dst.Len())
	}
}

func TestPwdOutput(t *testing.T) {
	var out bytes.Buffer
	c := Console{RemoteDir: "/safe directory", Output: &out}
	if _, err := c.Execute(context.Background(), []string{"pwd"}); err != nil {
		t.Fatal(err)
	}
	if out.String() != "Remote working directory: /safe directory\n" {
		t.Fatal(out.String())
	}
}

func TestLpwdOutput(t *testing.T) {
	var out bytes.Buffer
	c := Console{LocalDir: "/safe directory", Output: &out}
	if _, err := c.Execute(context.Background(), []string{"lpwd"}); err != nil {
		t.Fatal(err)
	}
	if out.String() != "Local working directory: /safe directory\n" {
		t.Fatal(out.String())
	}
}

func TestStatusTextUnknownUint32IsPortable(t *testing.T) {
	if got := statusText(&sftp.StatusError{Code: ^uint32(0)}); got != "Unknown status" {
		t.Fatal(got)
	}
}
