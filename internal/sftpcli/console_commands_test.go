package sftpcli

import (
	"bytes"
	"context"
	"testing"
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
