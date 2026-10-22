// pi-sh is an experimental native Connect shell client with direct or browser login.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	"pi-sh/internal/command"
	"pi-sh/internal/connectsession"
	"pi-sh/internal/connpolicy"
	"pi-sh/internal/shellbroker"
	"pi-sh/internal/terminal"
	"pi-sh/internal/transport"
)

func main() { os.Exit(app()) }

func app() int {
	opts, err := parseOptions(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "pi-sh:", err)
		fmt.Fprintln(os.Stderr, "usage: pi-sh (--browser|--direct-login) [options] [device-or-alias]\n       pi-sh (--browser|--direct-login) --experimental-command [options] device-or-alias command [arguments...]\n       pi-sh -G [-F path|none] [-o option] [device-or-alias]\n       pi-sh -V\n       pi-sh -Q query")
		return 255
	}
	if opts.inspection != "" {
		if err := writeInspection(opts, os.Stdout, os.Stderr); err != nil {
			fmt.Fprintln(os.Stderr, "pi-sh:", err)
			return 255
		}
		return 0
	}
	if opts.dump {
		if err := opts.writeConfig(os.Stdout); err != nil {
			return 255
		}
		return 0
	}
	escape, _ := parseEscape(opts.escape)
	destination := opts.destination()
	var plan *command.Plan
	var brokerPlan *shellbroker.Plan
	var ttyCommand *ttyCommandRequest
	if opts.legacy {
		plan, err = commandArguments(opts.args)
	} else if opts.broker {
		useTTY, warning := opts.requestTTY(terminal.IsTerminal(os.Stdin))
		if warning {
			fmt.Fprintln(os.Stderr, "pi-sh: pseudo-terminal will not be allocated because stdin is not a terminal or is disabled")
		}
		text := strings.Join(opts.args[1:], " ")
		if useTTY {
			ttyCommand = &ttyCommandRequest{text: text, term: os.Getenv("TERM"), stdinNull: opts.stdinNull}
			// Validate before authentication; current terminal size is captured
			// later, after potentially lengthy interactive login.
			_, err = shellbroker.NewTTYConfigured(text, shellbroker.TTYSettings{TERM: &ttyCommand.term})
		} else {
			brokerPlan, err = shellbroker.New(text)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "pi-sh:", err)
		return 255
	}
	ctx, cancel := signal.NotifyContext(context.Background(), terminationSignals()...)
	defer cancel()
	source, err := authentication(ctx, destination, opts.direct, opts.policy)
	if err == nil {
		defer source.Close()
		stdin := opts.stdin(os.Stdin)
		if opts.stdinNull {
			stdin = nil
		}
		err = connectWithPolicy(ctx, opts.policy, source, escape, plan, brokerPlan, ttyCommand, stdin)
	}
	if err != nil {
		if errors.Is(err, errEscape) {
			fmt.Fprintln(os.Stderr, "pi-sh: disconnected locally; remote TTY cleanup was not confirmed")
			return 0
		}
		var status *commandStatus
		if errors.As(err, &status) {
			return status.code
		}
		if errors.Is(err, transport.ErrClosed) || errors.Is(err, io.EOF) {
			unconfirmed := errors.Is(err, shellbroker.ErrCleanupUnconfirmed)
			err = errors.New("remote connection closed; Connect has not supplied a remote exit status")
			if unconfirmed {
				err = errors.Join(err, shellbroker.ErrCleanupUnconfirmed)
			}
		}
		fmt.Fprintln(os.Stderr, "pi-sh:", err)
		return 255
	}
	return 0
}

func connect(ctx context.Context, bridge sessionSource, escape rune, plan *command.Plan, brokerPlan *shellbroker.Plan, ttyCommand *ttyCommandRequest, stdin io.Reader) error {
	return connectWithPolicy(ctx, connpolicy.Default(), bridge, escape, plan, brokerPlan, ttyCommand, stdin)
}

func connectWithPolicy(ctx context.Context, policy connpolicy.Policy, bridge sessionSource, escape rune, plan *command.Plan, brokerPlan *shellbroker.Plan, ttyCommand *ttyCommandRequest, stdin io.Reader) error {
	peerContext := ctx
	if brokerPlan != nil || ttyCommand != nil {
		// The coordinator owns Close, allowing Run to report cleanup evidence
		// before transport teardown. Only the no-TTY broker has an abort handshake.
		peerContext = context.WithoutCancel(ctx)
	}
	// Establishment (with ConnectTimeout/ConnectionAttempts) ends at channel
	// readiness; no command byte has been sent before it returns.
	p, ch, err := connectsession.Establish(ctx, peerContext, bridge, policy)
	if err != nil {
		return err
	}
	defer p.Close()
	if plan != nil {
		fmt.Fprintln(os.Stderr, "pi-sh: experimental command adapter; remote PTY, merged stdout/stderr, stdin /dev/null")
		return runLegacyFileCommand(ctx, bridge.Failures(), ch, plan, os.Stdout)
	}
	if brokerPlan != nil {
		fmt.Fprintln(os.Stderr, "pi-sh: experimental shell-only broker; binary stdin, separate stdout/stderr; no Python")
		return runShellFileCommand(ctx, bridge.Failures(), ch, brokerPlan, stdin, os.Stdout, os.Stderr)
	}
	if ttyCommand != nil {
		fmt.Fprintln(os.Stderr, "pi-sh: experimental TTY command; merged stdout/stderr; abnormal remote cleanup remains unverified")
		return runTTYSession(ctx, bridge.Failures(), p, ch, *ttyCommand, stdin, os.Stdin, os.Stdout, os.Stderr, escape)
	}
	return runInteractiveSession(ctx, bridge.Failures(), p, ch, os.Stdin, os.Stdout, os.Stderr, escape)
}
