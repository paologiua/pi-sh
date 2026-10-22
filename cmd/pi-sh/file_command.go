package main

import (
	"context"
	"io"
	"os"

	"pi-sh/internal/command"
	"pi-sh/internal/localio"
	"pi-sh/internal/shellbroker"
	"pi-sh/internal/transport"
)

func runLegacyFileCommand(ctx context.Context, failures <-chan string, ch *transport.Channel, plan *command.Plan,
	stdout *os.File) (result error) {
	streams, err := localio.Open(ctx, localio.Files{Stdout: stdout})
	if err != nil {
		return err
	}
	defer func() { result = localCleanupResult(result, streams.Close()) }()
	return awaitCommand(ctx, failures, func(runCtx context.Context) (int, error) {
		stop := context.AfterFunc(runCtx, streams.Cancel)
		defer stop()
		return plan.Run(runCtx, commandStream{ch}, streams.Stdout)
	})
}

func runShellFileCommand(ctx context.Context, failures <-chan string, ch *transport.Channel, plan *shellbroker.Plan,
	stdin io.Reader, stdout, stderr *os.File) (result error) {
	var inputFile *os.File
	if file, ok := stdin.(*os.File); ok {
		inputFile = file
	}
	streams, err := localio.Open(ctx, localio.Files{Stdin: inputFile, Stdout: stdout, Stderr: stderr})
	if err != nil {
		return err
	}
	defer func() { result = localCleanupResult(result, streams.Close()) }()
	if streams.Stdin != nil {
		stdin = streams.Stdin
	}
	var stopIO func()
	if streams.Interruptible && (inputFile != nil || stdin == nil) {
		stopIO = streams.Cancel
	}
	return coordinateCommand(ctx, failures, func(runCtx context.Context) (int, error) {
		// Binary broker output writes are synchronous in its record loop. Wake
		// them as soon as the coordinator cancels, before waiting for C/D cleanup.
		stop := context.AfterFunc(runCtx, streams.Cancel)
		defer stop()
		return plan.Run(runCtx, commandStream{ch}, shellbroker.IO{Stdin: stdin, Stdout: streams.Stdout, Stderr: streams.Stderr, StopIO: stopIO})
	}, true)
}
