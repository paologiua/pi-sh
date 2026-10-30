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
