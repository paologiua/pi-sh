// pi-sftp is an unofficial experimental SFTP client over Raspberry Pi Connect.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"

	"pi-sh/internal/cliargs"
	"pi-sh/internal/connectsession"
	"pi-sh/internal/sftpcli"
	"pi-sh/internal/sftpclient"
)

type optionList []string

func (o *optionList) String() string     { return "" }
func (o *optionList) Set(s string) error { *o = append(*o, s); return nil }

func main() { os.Exit(app()) }
func app() int {
	f := flag.NewFlagSet("pi-sftp", flag.ContinueOnError)
	direct := f.Bool("direct-login", false, "use memory-only direct Raspberry Pi ID login")
	browser := f.Bool("browser", false, "use the authorized Connect browser companion")
	server := f.String("s", "", "existing absolute remote sftp-server path (default: check standard paths)")
	readOnly := f.Bool("read-only", false, "start the existing server in read-only mode; reject remote writes")
	flush := f.Bool("f", false, "request a flush after each transfer (upload requires server fsync extension)")
	resume := f.Bool("a", false, "resume transfers from destination size; existing prefixes must match")
	preserve := f.Bool("p", false, "preserve regular-file permission bits and access/modification times")
	batch := f.String("b", "", "batch file, or - for stdin")
	config := f.String("F", "none", "explicit configuration file, or none")
	var overrides optionList
	f.Var(&overrides, "o", "supported configuration directive (repeatable)")
	if err := cliargs.Parse(f, os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if *direct == *browser || f.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: pi-sftp (--direct-login|--browser) [-afp] [-s existing-server] [-b file|-] [-F file|none] [-o option] device-or-alias")
		return 1
	}
	if _, err := sftpclient.ServerCommand(*server); err != nil {
		fmt.Fprintln(os.Stderr, "pi-sftp:", err)
		return 1
	}
	target, err := sftpcli.DestinationWithServer(f.Arg(0), *config, overrides, *server)
	if err != nil {
		fmt.Fprintln(os.Stderr, "pi-sftp:", err)
		return 1
	}
	input := io.Reader(os.Stdin)
	if *batch != "" && *batch != "-" {
		file, err := os.Open(*batch)
		if err != nil {
			fmt.Fprintln(os.Stderr, "pi-sftp: cannot open batch file")
			return 1
		}
		defer file.Close()
		input = file
	}
	ctx, cancel := signal.NotifyContext(context.Background(), terminationSignals()...)
	defer cancel()
	source, err := connectsession.Authenticate(ctx, target, *direct, os.Stderr, "pi-sftp")
	if err != nil {
		fmt.Fprintln(os.Stderr, "pi-sftp:", err)
		return 255
	}
	defer source.Close()
	carrier, err := connectsession.Dial(ctx, source)
	if err != nil {
		fmt.Fprintln(os.Stderr, "pi-sftp:", err)
		return 255
	}
	defer carrier.Close()
	session, err := sftpclient.Open(ctx, carrier.Run, sftpclient.Options{Server: *server, ReadOnly: *readOnly})
	if err != nil {
		fmt.Fprintln(os.Stderr, "pi-sftp:", err)
		return 255
	}
	console := sftpcli.Console{Client: session.Client, RemoteUmask: &session.OriginalUmask, Flush: *flush, Resume: *resume, Preserve: *preserve, Input: input, Output: os.Stdout, Errors: os.Stderr, Batch: *batch != ""}
	err = console.Run(ctx)
	closeErr := session.Close()
	if err != nil || closeErr != nil {
		fmt.Fprintln(os.Stderr, "pi-sftp:", errors.Join(err, closeErr))
		if ctx.Err() != nil {
			return 255
		}
		return 1
	}
	return 0
}

func destination(original, config string, overrides []string) (string, error) {
	return sftpcli.Destination(original, config, overrides)
}
