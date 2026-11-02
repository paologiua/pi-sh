// Package loginprompt reads direct-login credentials from the controlling
// terminal, keeping command stdin and output streams available for the session.
package loginprompt

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"golang.org/x/term"
	"pi-sh/internal/webauth"
)

type Authenticator interface {
	Begin(context.Context) (webauth.Stage, error)
	Password(context.Context, string, []byte) (webauth.Stage, error)
	Code(context.Context, []byte) (webauth.Stage, error)
}

func Authenticate(ctx context.Context, c Authenticator) error {
	return AuthenticateAs(ctx, c, "")
}

// AuthenticateAs is Authenticate with a preselected Raspberry Pi ID account
// (from email::device, -l, User or the local default). A nonempty account
// replaces the email prompt; the password and one-time code are still read
// from the controlling terminal and never stored.
func AuthenticateAs(ctx context.Context, c Authenticator, account string) error {
	in, out, err := openTerminal()
	if err != nil {
		return errors.New("direct login requires a controlling terminal")
	}
	defer in.Close()
	if out != in {
		defer out.Close()
	}
	return authenticate(ctx, c, in, out, account)
}

func authenticate(ctx context.Context, c Authenticator, in, out *os.File, account string) error {
	return onTerminal(ctx, in, out, func(guard *terminalGuard) error { return run(ctx, c, in, out, guard, account) })
}

// onTerminal runs f with in's state saved, restores it afterwards and returns
// on cancellation even while f is blocked in a terminal read.
func onTerminal(ctx context.Context, in, out *os.File, f func(*terminalGuard) error) (resultErr error) {
	fd := int(in.Fd())
	state, err := term.GetState(fd)
	if err != nil {
		return errors.New("cannot read login terminal state")
	}
	guard := &terminalGuard{fd: fd, state: state}
	defer func() {
		if guard.restore(true) != nil {
			resultErr = errors.Join(resultErr, errors.New("could not restore login terminal"))
		}
	}()
	result := make(chan error, 1)
	go func() { result <- f(guard) }()
	select {
	case err := <-result:
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Choose shows items numbered from 1 on the controlling terminal and returns
// the index of the one the person selects. An empty answer cancels.
func Choose(ctx context.Context, heading string, items []string) (int, error) {
	in, out, err := openTerminal()
	if err != nil {
		return 0, errors.New("choosing requires a controlling terminal")
	}
	defer in.Close()
	if out != in {
		defer out.Close()
	}
	return choose(ctx, in, out, heading, items)
}

func choose(ctx context.Context, in, out *os.File, heading string, items []string) (choice int, err error) {
	err = onTerminal(ctx, in, out, func(*terminalGuard) error {
		fmt.Fprintln(out, heading)
		for i, item := range items {
			fmt.Fprintf(out, "%3d) %s\n", i+1, item)
		}
		for {
			fmt.Fprintf(out, "Select 1-%d (Enter to cancel): ", len(items))
			line, err := readLine(in, 32)
			if err != nil {
				return errors.New("could not read selection")
			}
			line = strings.TrimSpace(line)
			if line == "" {
				return context.Canceled
			}
			if n, err := strconv.Atoi(line); err == nil && n >= 1 && n <= len(items) {
				choice = n - 1
				return nil
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
		}
	})
	return choice, err
}

// Passphrase reads one hidden line after label. An empty answer returns nil.
func Passphrase(ctx context.Context, label string) ([]byte, error) {
	in, out, err := openTerminal()
	if err != nil {
		return nil, errors.New("passphrase entry requires a controlling terminal")
	}
	defer in.Close()
	if out != in {
		defer out.Close()
	}
	return passphrase(ctx, in, out, label)
}

func passphrase(ctx context.Context, in, out *os.File, label string) (secret []byte, err error) {
	err = onTerminal(ctx, in, out, func(guard *terminalGuard) error {
		fmt.Fprintf(out, "%s ", label)
		secret, err = readSecret(ctx, in, guard)
		fmt.Fprintln(out)
		return err
	})
	if err != nil {
		clear(secret)
		return nil, err
	}
	if len(secret) == 0 {
		return nil, nil
	}
	return secret, nil
}

// NewPassphrase asks for a new passphrase and its confirmation, like
// ssh-keygen, up to three times. An empty first answer returns nil (skip).
func NewPassphrase(ctx context.Context, label string) ([]byte, error) {
	in, out, err := openTerminal()
	if err != nil {
		return nil, errors.New("passphrase entry requires a controlling terminal")
	}
	defer in.Close()
	if out != in {
		defer out.Close()
	}
	return newPassphrase(ctx, in, out, label)
}

func newPassphrase(ctx context.Context, in, out *os.File, label string) ([]byte, error) {
	for range 3 {
		first, err := passphrase(ctx, in, out, label)
		if err != nil || first == nil {
			return nil, err
		}
		again, err := passphrase(ctx, in, out, "Enter the same passphrase again:")
		if err != nil {
			clear(first)
			return nil, err
		}
		same := bytes.Equal(first, again)
		clear(again)
		if same {
			return first, nil
		}
		clear(first)
		fmt.Fprintln(out, "Passphrases do not match. Try again.")
	}
	return nil, errors.New("passphrases did not match")
}

// Confirm asks question on the controlling terminal and reports whether the
// person typed "yes" (any other answer, including Enter, declines).
func Confirm(ctx context.Context, question string) (bool, error) {
	in, out, err := openTerminal()
	if err != nil {
		return false, errors.New("confirmation requires a controlling terminal")
	}
	defer in.Close()
	if out != in {
		defer out.Close()
	}
	return confirm(ctx, in, out, question)
}

func confirm(ctx context.Context, in, out *os.File, question string) (yes bool, err error) {
	err = onTerminal(ctx, in, out, func(*terminalGuard) error {
		fmt.Fprintf(out, "%s Type yes to continue: ", question)
		line, err := readLine(in, 32)
		if err != nil {
			return errors.New("could not read confirmation")
		}
		yes = strings.EqualFold(strings.TrimSpace(line), "yes")
		return nil
	})
	return yes, err
}

// readLine reads one visible line, without its terminator, up to limit bytes.
func readLine(in *os.File, limit int) (string, error) {
	var line []byte
	for len(line) < limit {
		var b [1]byte
		if _, err := in.Read(b[:]); err != nil {
			return "", err
		}
		if b[0] == '\n' {
			return string(line), nil
		}
		if b[0] != '\r' {
			line = append(line, b[0])
		}
	}
	return "", errors.New("input line exceeds local limit")
}

// A cancelled blocking terminal read may outlive this call until the dedicated
// CLI process exits. Context is checked before any subsequent credential POST.
func run(ctx context.Context, c Authenticator, in, out *os.File, guard *terminalGuard, account string) error {
	stage, err := c.Begin(ctx)
	if err != nil {
		return err
	}
	if stage != webauth.PasswordRequired {
		return errors.New("unexpected initial login step")
	}
	var email []byte
	if account != "" {
		// The account is not secret; naming it mirrors ssh's "user@host's
		// password:" so the person knows which account the password is for.
		fmt.Fprintf(out, "Raspberry Pi ID account: %s\n", account)
		email = []byte(account)
	} else {
		fmt.Fprintln(out, "Raspberry Pi ID email (type it, then press Enter):")
	}
	for account == "" && len(email) < 320 {
		var b [1]byte
		if _, err = in.Read(b[:]); err != nil {
			return errors.New("could not read login email")
		}
		if b[0] == '\n' {
			break
		}
		if b[0] != '\r' {
			email = append(email, b[0])
		}
	}
	if len(email) >= 320 {
		return errors.New("login email exceeds local limit")
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	fmt.Fprintln(out, "Password (hidden; type it, then press Enter):")
	password, err := readSecret(ctx, in, guard)
	fmt.Fprintln(out)
	if err != nil {
		return errors.New("could not read hidden password")
	}
	if ctx.Err() != nil {
		clear(password)
		return ctx.Err()
	}
	stage, err = c.Password(ctx, strings.TrimSpace(string(email)), password)
	clear(password)
	clear(email)
	if err != nil {
		return err
	}
	if stage == webauth.CodeRequired {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		fmt.Fprintln(out, "One-time code (hidden; type it, then press Enter):")
		code, e := readSecret(ctx, in, guard)
		fmt.Fprintln(out)
		if e != nil {
			return errors.New("could not read hidden one-time code")
		}
		if ctx.Err() != nil {
			clear(code)
			return ctx.Err()
		}
		stage, err = c.Code(ctx, code)
		clear(code)
		if err != nil {
			return err
		}
	}
	if stage != webauth.Authenticated {
		return errors.New("Connect login did not reach devices page")
	}
	return nil
}

// Serialize mode changes against cancellation so a late reader cannot disable
// echo after the cancellation path has restored and closed the prompt.
type terminalGuard struct {
	mu     sync.Mutex
	fd     int
	state  *term.State
	closed bool
}

func (g *terminalGuard) raw(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed || ctx.Err() != nil {
		return context.Canceled
	}
	_, err := term.MakeRaw(g.fd)
	return err
}
func (g *terminalGuard) restore(closePrompt bool) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return nil
	}
	g.closed = closePrompt
	return term.Restore(g.fd, g.state)
}
func readSecret(ctx context.Context, in *os.File, g *terminalGuard) (secret []byte, err error) {
	if err = g.raw(ctx); err != nil {
		return nil, err
	}
	defer func() {
		if e := g.restore(false); e != nil {
			err = errors.Join(err, errors.New("could not restore secret input terminal"))
		}
		if err != nil {
			clear(secret)
			secret = nil
		}
	}()
	for len(secret) < 4096 {
		var b [1]byte
		if _, err = in.Read(b[:]); err != nil {
			return secret, err
		}
		if ctx.Err() != nil {
			return secret, ctx.Err()
		}
		switch b[0] {
		case '\r', '\n':
			return secret, nil
		case 3, 4:
			return secret, context.Canceled
		case 8, 127:
			if len(secret) > 0 {
				_, n := utf8.DecodeLastRune(secret)
				clear(secret[len(secret)-n:])
				secret = secret[:len(secret)-n]
			}
		case 21:
			clear(secret)
			secret = secret[:0]
		default:
			secret = append(secret, b[0])
		}
	}
	return secret, errors.New("secret input exceeds local limit")
}
