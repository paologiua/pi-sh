package main

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"

	"pi-sh/internal/command"
	"pi-sh/internal/commandrun"
	"pi-sh/internal/shellbroker"
	"pi-sh/internal/transport"
)

// A commandStatus is produced only by an explicit experimental adapter result,
// never inferred from a shell/peer close or an interactive prompt.
type commandStatus struct{ code int }

func (*commandStatus) Error() string { return "experimental command adapter completed" }

// OpenSSH composes a remote shell command by joining the remaining arguments
// with spaces. Local quoting has already been removed by the invoking shell.
func commandArguments(args []string) (*command.Plan, error) {
	if len(args) < 2 {
		return nil, errors.New("experimental command requires an explicit device and command")
	}
	return command.New(strings.Join(args[1:], " "))
}

type commandStream struct{ channel *transport.Channel }

func (s commandStream) Send(ctx context.Context, b []byte) error {
	return s.channel.Send(ctx, transport.Message{Text: true, Data: b})
}
func (s commandStream) Receive(ctx context.Context) ([]byte, error) {
	m, err := s.channel.Receive(ctx)
	return m.Data, err
}

func runCommand(ctx context.Context, failures <-chan string, ch *transport.Channel, plan *command.Plan, output io.Writer) error {
	return awaitCommand(ctx, failures, func(ctx context.Context) (int, error) { return plan.Run(ctx, commandStream{ch}, output) })
}

func runShellCommand(ctx context.Context, failures <-chan string, ch *transport.Channel, plan *shellbroker.Plan, stdio shellbroker.IO) error {
	return coordinateCommand(ctx, failures, func(ctx context.Context) (int, error) { return plan.Run(ctx, commandStream{ch}, stdio) }, true)
}

func awaitCommand(ctx context.Context, failures <-chan string, run func(context.Context) (int, error)) error {
	return coordinateCommand(ctx, failures, run, false)
}

func coordinateCommand(ctx context.Context, failures <-chan string, run func(context.Context) (int, error), cleanup bool) error {
	var grace time.Duration
	if cleanup {
		grace = 3 * time.Second
	}
	code, err := commandrun.Run(ctx, failures, run, grace)
	if err != nil {
		return err
	}
	return &commandStatus{code: code}
}
