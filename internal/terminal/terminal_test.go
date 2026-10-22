package terminal

import (
	"context"
	"errors"
	"os"
	"testing"
)

func TestRejectNonTerminal(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	if _, err := Open(context.Background(), r, w); !errors.Is(err, ErrNotTerminal) {
		t.Fatal(err)
	}
	if _, err := Open(context.Background(), nil, nil); !errors.Is(err, ErrNotTerminal) {
		t.Fatal(err)
	}
}

func TestCanceledBeforeOpen(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Open(ctx, nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
