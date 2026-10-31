// pi-sftp is an unofficial experimental SFTP client over Raspberry Pi Connect.
package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"

	"pi-sh/internal/cliargs"
	"pi-sh/internal/connectsession"
	"pi-sh/internal/connpolicy"
	"pi-sh/internal/diag"
	"pi-sh/internal/forwardspec"
	"pi-sh/internal/sftpcli"
	"pi-sh/internal/sftpclient"
	"pi-sh/internal/sshconfig"
	"pi-sh/internal/sshdest"
)

type optionList []string

func (o *optionList) String() string     { return "" }
func (o *optionList) Set(s string) error { *o = append(*o, s); return nil }

func main() { os.Exit(app()) }
func app() int {
	f := flag.NewFlagSet("pi-sftp", flag.ContinueOnError)
	var flagOutput bytes.Buffer
	f.SetOutput(&flagOutput)
	var tuningError error
	direct := f.Bool("direct-login", false, "use memory-only direct Raspberry Pi ID login")
	browser := f.Bool("browser", false, "use the authorized Connect browser companion")
	server := f.String("s", "", "existing absolute remote sftp-server path (default: check standard paths)")
	requests, buffer := new(uint), new(uint)
	*requests = 64
	numericFlag := func(name, value string) error {
		normalized := strings.TrimPrefix(strings.TrimLeft(value, " \t\r\n\v\f"), "+")
		number, err := strconv.ParseUint(normalized, 10, strconv.IntSize)
		if err != nil || number == 0 || (name == "R" && number > 256*1024) {
			label := "buffer size"
			if name == "R" {
				label = "number of requests"
			}
			tuningError = fmt.Errorf("Invalid %s %q", label, value)
			return tuningError
		}
		if name == "R" {
			*requests = uint(number)
		} else {
			*buffer = uint(number)
		}
		return nil
	}
	f.Func("R", "maximum concurrent requests per transfer (default 64)", func(value string) error { return numericFlag("R", value) })
	var bandwidth uint64
	f.Func("l", "transfer bandwidth limit in Kbit/s", func(value string) error { var err error; bandwidth, err = sftpclient.ParseBandwidth(value); return err })
	f.Func("B", "transfer buffer size in bytes (default 32768)", func(value string) error { return numericFlag("B", value) })
	f.Func("X", "SFTP buffer= or nrequests= setting", func(value string) error { return sftpclient.ApplyTransferOption(value, buffer, requests) })
	noisy := f.Bool("N", false, "print transfer notices even in batch or quiet mode")
	readOnly := f.Bool("read-only", false, "start the existing server in read-only mode; reject remote writes")
	flush := f.Bool("f", false, "request a flush after each transfer (upload requires server fsync extension)")
	resume := f.Bool("a", false, "resume transfers from the destination's size; the existing prefix is trusted, not compared")
	recursive := f.Bool("r", false, "recursively transfer directories without following contained source symlinks")
	preserve := f.Bool("p", false, "preserve file/directory permission bits and access/modification times")
	batch := f.String("b", "", "batch file, or - for stdin")
	config := f.String("F", "none", "explicit configuration file, or none")
	port := f.String("P", "", "SSH port (always refused: Connect has no SSH port)")
	var verbosity diag.Verbosity
	var quietProgress bool
	f.Var(verbosity.VerboseFlag(), "v", "verbose connection diagnostics; repeat for more (maximum DEBUG3)")
	f.Var(progressQuietFlag{verbosity.QuietFlag(), &quietProgress}, "q", "quiet: suppress connection notices, warnings and diagnostics")
	var overrides optionList
	f.Var(&overrides, "o", "supported configuration directive (repeatable)")
	f.Usage = func() {
		fmt.Fprintln(f.Output(), "usage: pi-sftp (--direct-login|--browser) [-afNpqrv] [-B buffer] [-R requests] [-b file|-] [-F file|none] [-l limit] [-o option] [-s server] [-X setting] [email::]device\n\nUnofficial Raspberry Pi Connect client. Options marked not supported are refused.\nUse -h or --help for this help; see docs/SSH_COMPATIBILITY.md for limits.")
		cliargs.PrintOptions(f)
	}
	cliargs.Refuse(f, map[string]string{
		"4": sshconfig.ReasonFamily, "6": sshconfig.ReasonFamily, "C": sshconfig.ReasonCompress,
		"A": forwardspec.ErrAgentForwarding.Error(),
	}, map[string]string{

		"c": sshconfig.ReasonCrypto, "i": sshconfig.ReasonAuth, "J": forwardspec.ErrProxy.Error(),
		"D": "a local SFTP server program cannot replace the Connect session",
		"S": "pi-sftp always connects through Raspberry Pi Connect; the transport program cannot be replaced",
	})
	if err := cliargs.Parse(f, os.Args[1:]); err != nil {
		if tuningError != nil {
			fmt.Fprintf(os.Stderr, "%v\r\n", tuningError)
			return 1
		}
		_, _ = io.Copy(os.Stderr, &flagOutput)
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if *direct == *browser || f.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: pi-sftp (--direct-login|--browser) [-afpqrv] [-s existing-server] [-b file|-] [-F file|none] [-o option] [email::]device-or-alias")
		return 1
	}
	if _, err := sftpclient.ServerCommand(*server); err != nil {
		fmt.Fprintln(os.Stderr, "pi-sftp:", err)
		return 1
	}
	parsed, err := sshdest.SFTP(f.Arg(0))
	if err == nil {
		err = connpolicy.DestinationUser(parsed)
	}
	if err == nil && parsed.HasPath {
		err = errors.New("an initial remote path in the destination is not implemented; use cd or get after connecting")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "pi-sftp:", err)
		return 1
	}
	// -P and -o values precede the destination's user/port; any is refused.
	operand := map[string]string{"port": *port, "user": parsed.User}
	if operand["port"] == "" {
		operand["port"] = parsed.Port
	}
	target, settings, policy, err := sftpcli.ResolveSession(parsed.Host, *config, overrides, *server, operand)
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
	// As with scp/sftp passing -v/-q/-o LogLevel to ssh, these control only
	// the connection layer; file errors are always reported. The browser
	// companion instruction is never suppressed.
	level, _ := verbosity.Resolve(settings["loglevel"])
	log, _ := diag.Open(diag.Config{Program: "pi-sftp", Level: level, Stderr: os.Stderr})
	ctx, cancel := signal.NotifyContext(context.Background(), terminationSignals()...)
	defer cancel()
	ctx = diag.With(ctx, log)
	source, err := connectsession.AuthenticateWithPolicy(ctx, target, *direct, policy, os.Stderr, "pi-sftp")
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
	session, err := sftpclient.Open(ctx, carrier.Run, sftpclient.Options{Server: *server, ReadOnly: *readOnly, Requests: int(*requests), BufferSize: sftpclient.TransferBuffer(*buffer)})
	if err != nil {
		sessionFailure(log, err)
		return 255
	}
	if *noisy || (!quietProgress && *batch == "") {
		fmt.Fprintf(os.Stderr, "Connected to %s.\n", parsed.Host)
	}
	console := sftpcli.Console{Client: session.Client, Bandwidth: bandwidth, Requests: int(*requests), BufferSize: sftpclient.TransferBuffer(*buffer), RemoteUmask: &session.OriginalUmask, Flush: *flush, Resume: *resume, Preserve: *preserve, Recursive: *recursive, Progress: sftpProgress(os.Stdout, os.Stderr), ProgressDisabled: quietProgress, Input: input, Output: os.Stdout, Errors: os.Stderr, Batch: *batch != "", Noisy: *noisy, Quiet: quietProgress}
	err = console.Run(ctx)
	closeErr := session.Close()
	if err != nil || closeErr != nil {
		if !errors.Is(err, sftpcli.ErrBatchFailed) || closeErr != nil {
			fmt.Fprintln(os.Stderr, "pi-sftp:", errors.Join(err, closeErr))
		}
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

// sessionFailure reports a connection-layer failure (exit 255). Under -q or
// LogLevel QUIET the detail is suppressed like ssh's own messages, but the
// failure is still stated, as scp/sftp print "Connection closed".
func sessionFailure(log *diag.Logger, err error) {
	log.Failure(diag.Fatal, err)
	if !log.Enabled(diag.Fatal) {
		fmt.Fprintln(os.Stderr, "pi-sftp: connection closed")
	}
}
