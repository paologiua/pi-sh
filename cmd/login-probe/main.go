// login-probe investigates a fresh web session. It is not a reusable pi-sh login
// command: cookies live only for this process and are never exported.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

	"golang.org/x/term"
	"pi-sh/internal/webauth"
)

func main() { os.Exit(run()) }

func run() int {
	login := flag.Bool("login", false, "prompt locally for credentials; default only checks credential-free navigation")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: login-probe [--login]")
		return 255
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if *login {
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			fmt.Fprintln(os.Stderr, "login-probe: --login requires a local interactive terminal")
			return 255
		}
		state, err := term.GetState(int(os.Stdin.Fd()))
		if err != nil {
			return 255
		}
		defer term.Restore(int(os.Stdin.Fd()), state)
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, terminationSignals()...)
		defer signal.Stop(signals)
		done := make(chan struct{})
		defer close(done)
		go func() {
			select {
			case <-signals:
			case <-ctx.Done():
			case <-done:
				return
			}
			_ = term.Restore(int(os.Stdin.Fd()), state)
			fmt.Fprintln(os.Stderr, "\nlogin-probe: cancelled")
			os.Exit(255)
		}()
	}
	c := webauth.New()
	stage, err := c.Begin(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "login-probe:", err)
		return 255
	}
	fmt.Println("Preflight:", stage)
	if !*login {
		fmt.Println("No credentials submitted. Run with --login in your local terminal to test the next steps.")
		return 0
	}
	if stage != webauth.PasswordRequired {
		fmt.Fprintln(os.Stderr, "login-probe: unexpected initial authentication step")
		return 255
	}
	fmt.Fprintln(os.Stderr, "Experimental direct HTTPS login. Credentials and cookies are not saved; this probe does not open a remote shell.")
	fmt.Fprintln(os.Stderr, "Raspberry Pi ID email (type it, then press Enter):")
	email, err := readLine()
	if err != nil {
		return 255
	}
	fmt.Fprintln(os.Stderr, "Password (hidden; type it, then press Enter):")
	password, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return 255
	}
	stage, err = c.Password(ctx, email, password)
	clear(password)
	if err != nil {
		fmt.Fprintln(os.Stderr, "login-probe:", err)
		return 255
	}
	if stage == webauth.CodeRequired {
		fmt.Fprintln(os.Stderr, "One-time code (hidden; type it, then press Enter):")
		code, e := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if e != nil {
			return 255
		}
		stage, err = c.Code(ctx, code)
		clear(code)
		if err != nil {
			fmt.Fprintln(os.Stderr, "login-probe:", err)
			return 255
		}
	}
	if stage != webauth.Authenticated {
		fmt.Fprintln(os.Stderr, "login-probe: additional unrecognized authentication step")
		return 255
	}
	fmt.Println("Connect devices page reached. Remote-shell authorization and native transport were not tested by this probe.")
	fmt.Println("The in-memory session ends with this process; no reusable login has been saved.")
	return 0
}

// Read one byte at a time so email input cannot prefetch a pasted password into
// a buffered reader before terminal echo is disabled.
func readLine() (string, error) {
	var b [1]byte
	var line []byte
	for len(line) < 320 {
		if _, err := os.Stdin.Read(b[:]); err != nil {
			return "", err
		}
		if b[0] == '\n' {
			return strings.TrimSpace(string(line)), nil
		}
		if b[0] != '\r' {
			line = append(line, b[0])
		}
	}
	return "", fmt.Errorf("email exceeds local length limit")
}
