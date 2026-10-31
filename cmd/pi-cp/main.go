// pi-cp is an unofficial experimental SFTP-backed copy client over Connect.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"pi-sh/internal/cliargs"
	"pi-sh/internal/connectsession"
	"pi-sh/internal/copycli"
	"pi-sh/internal/diag"
	"pi-sh/internal/forwardspec"
	"pi-sh/internal/progress"
	"pi-sh/internal/sftpcli"
	"pi-sh/internal/sftpclient"
	"pi-sh/internal/sshconfig"
)

type optionList []string

func (o *optionList) String() string     { return "" }
func (o *optionList) Set(s string) error { *o = append(*o, s); return nil }
func main()                              { os.Exit(app()) }
func app() int {
	f := flag.NewFlagSet("pi-cp", flag.ContinueOnError)
	direct := f.Bool("direct-login", false, "memory-only direct Raspberry Pi ID login")
	browser := f.Bool("browser", false, "authorized Connect browser companion")
	preserve := f.Bool("p", false, "preserve ordinary permission bits and access/modification times")
	recursive := f.Bool("r", false, "copy directory trees, following source symbolic links")
	var bandwidth uint64
	buffer, requests := uint(0), uint(64)
	f.Func("X", "SFTP buffer= or nrequests= setting", func(value string) error { return sftpclient.ApplyTransferOption(value, &buffer, &requests) })
	f.Func("l", "transfer bandwidth limit in Kbit/s", func(value string) error { var err error; bandwidth, err = sftpclient.ParseBandwidth(value); return err })
	server := f.String("sftp-server", "", "existing absolute remote sftp-server path")
	config := f.String("F", "none", "explicit configuration file, or none")
	port := f.String("P", "", "SSH port (always refused: Connect has no SSH port)")
	var verbosity diag.Verbosity
	var quietProgress bool
	f.Var(verbosity.VerboseFlag(), "v", "verbose connection diagnostics; repeat for more (maximum DEBUG3)")
	f.Var(progressQuietFlag{verbosity.QuietFlag(), &quietProgress}, "q", "quiet: disable progress and suppress connection notices, warnings and diagnostics")
	var overrides optionList
	f.Var(&overrides, "o", "supported configuration directive (repeatable)")
	batch := f.Bool("B", false, "batch mode (BatchMode yes): fails before any prompt, since Connect sign-in always needs a person")
	f.Bool("s", false, "use the SFTP protocol (always the case)")
	f.Bool("T", false, "no effect: strict filename checking applies only to the legacy SCP protocol, as in scp's SFTP mode")
	f.Usage = func() {
		fmt.Fprintln(f.Output(), "usage: pi-cp (--direct-login|--browser) [-Bpqrv] [-F file|none] [-l limit] [-o option] [-X setting] source ... destination\n       remote operand: [email::]device:path\n\nUnofficial Raspberry Pi Connect client. Options marked not supported are refused.\nUse -h or --help for this help; see docs/SSH_COMPATIBILITY.md for limits.")
		cliargs.PrintOptions(f)
	}
	cliargs.Refuse(f, map[string]string{
		"3": "remote-to-remote copies are not implemented",
		"4": sshconfig.ReasonFamily, "6": sshconfig.ReasonFamily, "C": sshconfig.ReasonCompress,
		"A": forwardspec.ErrAgentForwarding.Error(), "O": "the legacy SCP protocol is not implemented; pi-cp always uses SFTP",
		"R": "remote-to-remote copies are not implemented",
	}, map[string]string{
		"c": sshconfig.ReasonCrypto, "i": sshconfig.ReasonAuth, "J": forwardspec.ErrProxy.Error(),
		"D": "a local SFTP server program cannot replace the Connect session",
		"S": "pi-cp always connects through Raspberry Pi Connect; the transport program cannot be replaced",
	})
	if err := cliargs.Parse(f, os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if *direct == *browser {
		fmt.Fprintln(os.Stderr, "usage: pi-cp (--direct-login|--browser) [-Bpqrv] [options] source(s) destination\n       remote operands: [email::]device:path or scp://[email%40domain@]device[/path]")
		return 1
	}
	plan, err := copycli.New(f.Args())
	if err != nil {
		fmt.Fprintln(os.Stderr, "pi-cp:", err)
		return 1
	}
	plan.Preserve, plan.Errors = *preserve, os.Stderr
	plan.Recursive = *recursive
	plan.Bandwidth = bandwidth
	plan.BufferSize, plan.Requests = sftpclient.TransferBuffer(buffer), int(requests)
	plan.Progress = progress.NewTerminal(os.Stdout, quietProgress)
	if _, err = sftpclient.ServerCommand(*server); err != nil {
		fmt.Fprintln(os.Stderr, "pi-cp:", err)
		return 1
	}
	if *batch {
		// scp -B passes -oBatchMode=yes to ssh; it is placed first here.
		overrides = append(optionList{"BatchMode=yes"}, overrides...)
	}
	target, settings, policy, err := sftpcli.ResolveSession(plan.Device, *config, overrides, *server, map[string]string{"port": *port, "user": plan.Account})
	if err != nil {
		fmt.Fprintln(os.Stderr, "pi-cp:", err)
		return 1
	}
	// As with scp/sftp passing -v/-q/-o LogLevel to ssh, these control only
	// the connection layer; -q also separately disables progress, even after -v.
	// File errors are always reported. The browser
	// companion instruction is never suppressed.
	level, _ := verbosity.Resolve(settings["loglevel"])
	log, _ := diag.Open(diag.Config{Program: "pi-cp", Level: level, Stderr: os.Stderr})
	ctx, cancel := signal.NotifyContext(context.Background(), terminationSignals()...)
	defer cancel()
	ctx = diag.With(ctx, log)
	source, err := connectsession.AuthenticateWithPolicy(ctx, target, *direct, policy, os.Stderr, "pi-cp")
	if err != nil {
		sessionFailure(log, err)
		return 255
	}
	defer source.Close()
	carrier, err := connectsession.DialWithPolicy(ctx, source, policy)
	if err != nil {
		sessionFailure(log, err)
		return 255
	}
	defer carrier.Close()
	session, err := sftpclient.Open(ctx, carrier.Run, sftpclient.Options{Server: *server, BufferSize: plan.BufferSize, Requests: plan.Requests})
	if err != nil {
		sessionFailure(log, err)
		return 255
	}
	err = plan.Run(ctx, session.Client, &session.OriginalUmask)
	err = errors.Join(err, session.Close())
	if err != nil {
		fmt.Fprintln(os.Stderr, "pi-cp:", err)
		if ctx.Err() != nil || errors.Is(err, copycli.ErrLocalSource) {
			return 255
		}
		return 1
	}
	return 0
}

// sessionFailure reports a connection-layer failure (exit 255). Under -q or
// LogLevel QUIET the detail is suppressed like ssh's own messages, but the
// failure is still stated, as scp/sftp print "Connection closed".
func sessionFailure(log *diag.Logger, err error) {
	log.Failure(diag.Fatal, err)
	if !log.Enabled(diag.Fatal) {
		fmt.Fprintln(os.Stderr, "pi-cp: connection closed")
	}
}
