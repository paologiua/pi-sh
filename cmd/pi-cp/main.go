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
	var overrides optionList
	f.Var(&overrides, "o", "supported configuration directive (repeatable)")
	if err := cliargs.Parse(f, os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if *direct == *browser {
		fmt.Fprintln(os.Stderr, "usage: pi-cp (--direct-login|--browser) [options] source(s) destination")
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
	target, err := sftpcli.DestinationWithServer(plan.Device, *config, overrides, *server)
	if err != nil {
		fmt.Fprintln(os.Stderr, "pi-cp:", err)
		return 1
	}
	ctx, cancel := signal.NotifyContext(context.Background(), terminationSignals()...)
	defer cancel()
	source, err := connectsession.Authenticate(ctx, target, *direct, os.Stderr, "pi-cp")
	if err != nil {
		fmt.Fprintln(os.Stderr, "pi-cp:", err)
		return 255
	}
	defer source.Close()
	carrier, err := connectsession.Dial(ctx, source)
	if err != nil {
		fmt.Fprintln(os.Stderr, "pi-cp:", err)
		return 255
	}
	defer carrier.Close()
	session, err := sftpclient.Open(ctx, carrier.Run, sftpclient.Options{Server: *server})
	if err != nil {
		fmt.Fprintln(os.Stderr, "pi-cp:", err)
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
