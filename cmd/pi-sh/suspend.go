package main

import (
	"context"

	"pi-sh/internal/localio"
	"pi-sh/internal/terminal"
)

func sessionInputActions(tty *terminal.Session, streams *localio.Session) inputActions {
	actions := inputActions{diagnostic: streams.Stderr}
	if terminal.CanSuspend() && streams.Interruptible {
		actions.suspend = func(ctx context.Context) error {
			return streams.WithSuspended(func() error {
				if tty != nil {
					return tty.Suspend(ctx)
				}
				return terminal.SuspendProcess(ctx)
			})
		}
	}
	return actions
}
