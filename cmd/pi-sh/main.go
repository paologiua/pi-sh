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
	"pi-sh/internal/diag"
	"pi-sh/internal/shellbroker"
	"pi-sh/internal/terminal"
	"pi-sh/internal/transport"
)

func main() { os.Exit(app()) }

func app() int {
	opts, err := parseOptions(os.Args[1:])
	var fromConfig configError
	if err != nil && !errors.As(err, &fromConfig) {
		fmt.Fprintln(os.Stderr, "pi-sh:", err)
		fmt.Fprintln(os.Stderr, "usage: pi-sh (--browser|--direct-login) [options] [-l email] [email::]device-or-alias\n       pi-sh (--browser|--direct-login) --experimental-command [options] [email::]device-or-alias command [arguments...]\n       pi-sh -G [-F path|none] [-o option] [-l email] [[email::]device-or-alias]\n       pi-sh -V\n       pi-sh -Q query")
		return 255
	}
	if err == nil && opts.inspection != "" {
		if err := writeInspection(opts, os.Stdout, os.Stderr); err != nil {
			fmt.Fprintln(os.Stderr, "pi-sh:", err)
			return 255
		}
		return 0
	}
	// As in ssh(1), -E/-y take effect after option parsing (-V/-Q exit first);
	// an unopenable log file exits 1. Configuration-file errors and all later
	// diagnostics follow the selected level and destination.
	log, logErr := opts.logging.open(os.Stderr)
	if logErr != nil {
		fmt.Fprintln(os.Stderr, "pi-sh:", logErr)
		return 1
	}
	defer log.Close()
	if err != nil {
		log.Failure(diag.Error, err)
		return 255
	}
	logStartup(log, opts)
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
	if opts.noSession() {
		// SessionType none (-N, -W): no shell, command or TTY.
	} else if opts.legacy {
		plan, err = commandArguments(opts.args)
	} else if opts.broker || !opts.legacyShell {
		useTTY, warning := opts.requestTTY(terminal.IsTerminal(os.Stdin))
		if warning {
			log.Info(msgNoTTY)
		}
		// Without a command, run the login shell through the same adapters, as
		// sshd does: a TTY session, or a no-TTY session for -T/-n/piped stdin.
		text, shell := shellbroker.LoginShell, true
		if opts.broker {
			text, shell = strings.Join(opts.args[1:], " "), false
		}
		if useTTY {
			ttyCommand = &ttyCommandRequest{text: text, term: os.Getenv("TERM"), stdinNull: opts.stdinNull, shell: shell}
			// Validate before authentication; current terminal size is captured
			// later, after potentially lengthy interactive login.
			_, err = shellbroker.NewTTYConfigured(text, shellbroker.TTYSettings{TERM: &ttyCommand.term})
		} else {
			brokerPlan, err = shellbroker.New(text)
		}
	}
	if err != nil {
		log.Failure(diag.Fatal, err)
		return 255
	}
	ctx, cancel := signal.NotifyContext(context.Background(), terminationSignals()...)
	defer cancel()
	ctx = diag.With(ctx, log)
	if opts.direct {
		log.Debug(msgAuthDirect)
	} else {
		log.Debug(msgAuthBrowser)
	}
	source, err := authentication(ctx, destination, opts.direct, opts.policy)
	if err == nil && opts.noSession() {
		defer source.Close()
		var stdin io.Reader = os.Stdin
		if opts.stdinNull {
			stdin = nil
		}
		code, err := runForwarding(ctx, opts.policy, source, opts, stdin, os.Stdout)
		if err != nil {
			log.Failure(diag.Fatal, err)
		}
		return code
	}
	if err == nil {
		defer source.Close()
		if opts.forwarding() {
			// Like ssh, forwardings are set up before the session starts.
			f, listeners, ferr := startForwarding(ctx, opts.policy, source, opts)
			if ferr != nil {
				log.Failure(diag.Fatal, ferr)
				return 255
			}
			if listeners != nil {
				f.serve(ctx, listeners)
			}
			defer f.stop(ctx)
		}
		stdin := opts.stdin(os.Stdin)
		if opts.stdinNull {
			stdin = nil
		}
		err = connectWithPolicy(ctx, opts.policy, source, escape, plan, brokerPlan, ttyCommand, stdin, !opts.broker && !opts.legacy)
	}
	if err != nil {
		if errors.Is(err, errEscape) {
			// Like ssh after ~., no remote exit status exists: exit 255.
			log.Info(msgEscape)
			return 255
		}
		var status *commandStatus
		if errors.As(err, &status) {
			log.Debug(msgExitStatus, diag.Count("exit_status", status.code))
			return status.code
		}
		if errors.Is(err, transport.ErrClosed) || errors.Is(err, io.EOF) {
			unconfirmed := errors.Is(err, shellbroker.ErrCleanupUnconfirmed)
			err = errors.New("remote connection closed; Connect has not supplied a remote exit status")
			if unconfirmed {
				err = errors.Join(err, shellbroker.ErrCleanupUnconfirmed)
			}
		}
		log.Failure(diag.Fatal, err)
		return 255
	}
	return 0
}

func connect(ctx context.Context, bridge sessionSource, escape rune, plan *command.Plan, brokerPlan *shellbroker.Plan, ttyCommand *ttyCommandRequest, stdin io.Reader) error {
	return connectWithPolicy(ctx, connpolicy.Default(), bridge, escape, plan, brokerPlan, ttyCommand, stdin, false)
}

// loginShell marks adapter sessions running shellbroker.LoginShell. Like ssh(1)
// starting a shell, they print no adapter notice at the default log level.
func connectWithPolicy(ctx context.Context, policy connpolicy.Policy, bridge sessionSource, escape rune, plan *command.Plan, brokerPlan *shellbroker.Plan, ttyCommand *ttyCommandRequest, stdin io.Reader, loginShell bool) error {
	log := diag.From(ctx)
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
		log.Info(msgLegacy)
		log.Debug(msgSessionMode, diag.State("adapter", "legacy"))
		return runLegacyFileCommand(ctx, bridge.Failures(), ch, plan, os.Stdout)
	}
	if brokerPlan != nil {
		if loginShell {
			log.Debug(msgSessionLogin, diag.State("adapter", "broker-no-tty"))
		} else {
			log.Info(msgBroker)
			log.Debug(msgSessionMode, diag.State("adapter", "broker-no-tty"))
		}
		return runShellFileCommand(ctx, bridge.Failures(), ch, brokerPlan, stdin, os.Stdout, os.Stderr)
	}
	if ttyCommand != nil {
		if loginShell {
			log.Debug(msgSessionLogin, diag.State("adapter", "broker-tty"))
		} else {
			log.Info(msgTTYCommand)
			log.Debug(msgSessionMode, diag.State("adapter", "broker-tty"))
		}
		return runTTYSession(ctx, bridge.Failures(), p, ch, *ttyCommand, stdin, os.Stdin, os.Stdout, os.Stderr, escape)
	}
	log.Debug(msgSessionShell, diag.State("adapter", "legacy-shell"))
	return runInteractiveSession(ctx, bridge.Failures(), p, ch, os.Stdin, os.Stdout, os.Stderr, escape)
}
