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
		if streams.Stdin != nil {
			// Only the session's own stdin endpoint observes this interrupt.
			actions.interrupt = streams.InterruptRead
		}
	}
	return actions
}
