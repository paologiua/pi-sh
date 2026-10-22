// transfer-probe is a developer-only, opt-in synthetic SFTP verification tool.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"time"

	"pi-sh/internal/connectsession"
	"pi-sh/internal/sftpcli"
	"pi-sh/internal/sftpclient"
	"pi-sh/internal/shellbroker"
	"pi-sh/internal/transferprobe"
)

func main() { os.Exit(app()) }
func app() int {
	f := flag.NewFlagSet("transfer-probe", flag.ContinueOnError)
	root := f.String("root", "", "exact scratch root from an approved plan")
	execute := f.Bool("execute-approved", false, "execute only after explicit owner approval of this exact root")
	cleanup := f.Bool("cleanup-only", false, "remove only known probe entries, after approval; never replay transfers")
	direct := f.Bool("direct-login", false, "owner enters credentials in local prompts")
	browser := f.Bool("browser", false, "use authorized browser companion")
	server := f.String("sftp-server", "", "existing remote sftp-server path")
	if err := f.Parse(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if *root == "" {
		if *execute {
			fmt.Fprintln(os.Stderr, "transfer-probe: execution requires the exact approved root")
			return 1
		}
		var err error
		*root, err = transferprobe.NewRoot()
		if err != nil {
			fmt.Fprintln(os.Stderr, "transfer-probe: scratch-name generation failed")
			return 1
		}
	}
	plan, err := transferprobe.Plan(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "transfer-probe:", err)
		return 1
	}
	if !*execute {
		if f.NArg() != 0 || *direct || *browser || *cleanup || *server != "" {
			fmt.Fprintln(os.Stderr, "transfer-probe: plan mode does not authenticate or execute; omit connection/cleanup arguments")
			return 1
		}
		if _, err := fmt.Fprint(os.Stdout, plan); err != nil {
			fmt.Fprintln(os.Stderr, "transfer-probe: cannot write plan")
			return 1
		}
		return 0
	}
	if *direct == *browser || f.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "transfer-probe: choose one authentication mode and one exact device")
		return 1
	}
	target, err := sftpcli.Destination(f.Arg(0), "none", nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "transfer-probe:", err)
		return 1
	}
	if _, err := sftpclient.ServerCommand(*server); err != nil {
		fmt.Fprintln(os.Stderr, "transfer-probe:", err)
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), terminationSignals()...)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	source, err := connectsession.Authenticate(ctx, target, *direct, os.Stderr, "transfer-probe")
	if err != nil {
		fmt.Fprintln(os.Stderr, "transfer-probe:", err)
		return 255
	}
	defer source.Close()
	carrier, err := connectsession.Dial(ctx, source)
	if err != nil {
		fmt.Fprintln(os.Stderr, "transfer-probe:", err)
		return 255
	}
	defer carrier.Close()
	started := time.Now()
	run := func(ctx context.Context, command string, streams shellbroker.IO) (int, error) {
		code, err := carrier.Run(ctx, command, streams)
		return code, reportBroker(os.Stdout, code, err, time.Since(started))
	}
	session, err := sftpclient.Open(ctx, run, sftpclient.Options{Server: *server})
	if err != nil {
		fmt.Fprintln(os.Stderr, "transfer-probe:", err)
		return 255
	}
	if *cleanup {
		err = transferprobe.Cleanup(session.Client, *root)
		if err == nil {
			_, err = fmt.Fprintln(os.Stdout, "cleanup: passed")
		}
	} else {
		var reportError error
		err = transferprobe.Run(ctx, session.Client, *root, session.OriginalUmask, func(event transferprobe.Event) {
			status := "failed"
			if event.Passed {
				status = "passed"
			}
			if event.Skipped {
				status = "skipped"
			}
			if _, err := fmt.Fprintf(os.Stdout, "%s: %s elapsed-ms=%d\n", event.Check, status, time.Since(started).Milliseconds()); err != nil {
				reportError = errors.New("cannot write verification report")
			}
		})
		err = errors.Join(err, reportError)
	}
	err = errors.Join(err, session.Close())
	if err != nil {
		fmt.Fprintln(os.Stderr, "transfer-probe: verification or cleanup failed; exact scratch root:", *root)
		fmt.Fprintln(os.Stderr, "transfer-probe:", sftpclient.SafeError(err))
		return 1
	}
	return 0
}
