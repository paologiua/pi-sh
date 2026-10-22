package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestDecodeCanCancelBlockedInput(t *testing.T) {
	r, w := io.Pipe()
	defer r.Close()
	defer w.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var value any
	if err := decode(ctx, json.NewDecoder(r), &value); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestInvalidInputIsRedacted(t *testing.T) {
	err := run(context.Background(), strings.NewReader("PRIVATE-TEST-SENTINEL"), io.Discard)
	if err == nil || strings.Contains(err.Error(), "PRIVATE-TEST-SENTINEL") {
		t.Fatal("invalid signaling input was accepted or exposed")
	}
}
