package main

import (
	"context"
	"os"

	"pi-sh/internal/connectsession"
)

type sessionSource = connectsession.Source

func authentication(ctx context.Context, destination string, direct bool) (sessionSource, error) {
	return connectsession.Authenticate(ctx, destination, direct, os.Stderr, "pi-sh")
}
