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
	"pi-sh/internal/diag"
	"pi-sh/internal/copycli"
	"pi-sh/internal/sftpcli"
	"pi-sh/internal/sftpclient"
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
	server := f.String("sftp-server", "", "existing absolute remote sftp-server path")
	config := f.String("F", "none", "explicit configuration file, or none")
	var verbosity diag.Verbosity
	f.Var(verbosity.VerboseFlag(), "v", "verbose connection diagnostics; repeat for more (maximum DEBUG3)")
	f.Var(verbosity.QuietFlag(), "q", "quiet: suppress connection notices, warnings and diagnostics")
	var overrides optionList
	f.Var(&overrides, "o", "supported configuration directive (repeatable)")
	if err := cliargs.Parse(f, os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if *direct == *browser {
		fmt.Fprintln(os.Stderr, "usage: pi-cp (--direct-login|--browser) [-pqrv] [options] source(s) destination")
		return 1
	}
	plan, err := copycli.New(f.Args())
	if err != nil {
		fmt.Fprintln(os.Stderr, "pi-cp:", err)
		return 1
	}
	plan.Preserve, plan.Errors = *preserve, os.Stderr
	plan.Recursive = *recursive
	if _, err = sftpclient.ServerCommand(*server); err != nil {
		fmt.Fprintln(os.Stderr, "pi-cp:", err)
		return 1
	}
	target, settings, err := sftpcli.Settings(plan.Device, *config, overrides)
	if err != nil {
		fmt.Fprintln(os.Stderr, "pi-cp:", err)
		return 1
	}
	// As with scp/sftp passing -v/-q/-o LogLevel to ssh, these control only
	// the connection layer; file errors are always reported. The browser
	// companion instruction is never suppressed.
	level, _ := verbosity.Resolve(settings["loglevel"])
	log, _ := diag.Open(diag.Config{Program: "pi-cp", Level: level, Stderr: os.Stderr})
	ctx, cancel := signal.NotifyContext(context.Background(), terminationSignals()...)
	defer cancel()
	ctx = diag.With(ctx, log)
	source, err := connectsession.Authenticate(ctx, target, *direct, os.Stderr, "pi-cp")
	if err != nil {
		sessionFailure(log, err)
		return 255
	}
	defer source.Close()
	carrier, err := connectsession.Dial(ctx, source)
	if err != nil {
		sessionFailure(log, err)
		return 255
	}
	defer carrier.Close()
	session, err := sftpclient.Open(ctx, carrier.Run, sftpclient.Options{Server: *server})
	if err != nil {
		sessionFailure(log, err)
		return 255
	}
	err = plan.Run(ctx, session.Client, &session.OriginalUmask)
	err = errors.Join(err, session.Close())
	if err != nil {
		fmt.Fprintln(os.Stderr, "pi-cp:", err)
		if ctx.Err() != nil {
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
