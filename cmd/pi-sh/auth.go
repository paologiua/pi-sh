package main

import (
	"context"
	"os"

	"pi-sh/internal/connectsession"
	"pi-sh/internal/connpolicy"
)

type sessionSource = connectsession.Source

func authentication(ctx context.Context, destination string, direct bool, policy connpolicy.Policy) (sessionSource, error) {
	return connectsession.AuthenticateWithPolicy(ctx, destination, direct, policy, os.Stderr, "pi-sh")
}
