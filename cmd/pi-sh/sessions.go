package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"pi-sh/internal/savedlogin"
	"pi-sh/internal/sessionstore"
)

// sessionManager is the saved-session API the management commands use.
type sessionManager interface {
	List(ctx context.Context, check bool) ([]savedlogin.Status, error)
	Logout(ctx context.Context, account string) (bool, error)
	ChangePassphrase(ctx context.Context) (bool, error)
	ForgetPassphrase(ctx context.Context) (bool, error)
	Reset(ctx context.Context) (savedlogin.ResetResult, error)
	Close()
}

var openSessions = func(opts options, notices io.Writer) sessionManager {
	return savedlogin.Open(opts.policy.UseKeychain, notices)
}

// manageSessions runs --sessions, --logout, --passphrase,
// --forget-passphrase or --reset-sessions. These pi-sh extensions only touch sessions saved by
// the pi-sh CLIs.
func manageSessions(ctx context.Context, opts options, stdout, notices io.Writer) error {
	m := openSessions(opts, notices)
	defer m.Close()
	switch opts.manage {
	case "sessions":
		list, err := m.List(ctx, true)
		if err != nil {
			return err
		}
		if len(list) == 0 {
			fmt.Fprintln(stdout, "No saved sessions.")
			return nil
		}
		w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ACCOUNT\tSTATUS\tSAVED\tLAST USED")
		for _, s := range list {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", s.Account, s.State, stamp(s.Saved), stamp(s.Used))
		}
		if err := w.Flush(); err != nil {
			return err
		}
		for _, s := range list {
			if s.Err != nil {
				fmt.Fprintf(notices, "%s: could not check: %v\n", s.Account, s.Err)
			}
		}
		return nil
	case "logout":
		signedOut, err := m.Logout(ctx, opts.logoutAccount)
		switch {
		case signedOut:
			fmt.Fprintf(stdout, "Signed %s out of Raspberry Pi Connect and removed the saved session.\n", opts.logoutAccount)
		case err == nil:
			fmt.Fprintf(stdout, "Removed the saved session for %s (it was no longer signed in).\n", opts.logoutAccount)
		}
		return err
	case "passphrase":
		created, err := m.ChangePassphrase(ctx)
		switch {
		case err != nil:
			return err
		case created:
			fmt.Fprintln(stdout, "Passphrase set. Sessions you sign in to from now on will be saved with it.")
		default:
			fmt.Fprintln(stdout, "Passphrase changed for all saved sessions.")
		}
		return nil
	case "forget-passphrase":
		removed, err := m.ForgetPassphrase(ctx)
		switch {
		case errors.Is(err, sessionstore.ErrNoKeyring):
			fmt.Fprintln(stdout, "This system has no supported keyring, so the passphrase is never stored outside your memory.")
			return nil
		case err != nil:
			return err
		case removed:
			fmt.Fprintln(stdout, "Removed the saved-sessions passphrase from the system keyring. Saved sessions are kept; type the passphrase to use them.")
		default:
			fmt.Fprintln(stdout, "The system keyring holds no pi-sh passphrase.")
		}
		return nil
	case "reset-sessions":
		r, err := m.Reset(ctx)
		if err != nil {
			return err
		}
		if !r.Removed {
			fmt.Fprintln(stdout, "Nothing was reset.")
			return nil
		}
		fmt.Fprintf(stdout, "Deleted all saved sessions and the passphrase (%d signed out of Connect).\n", r.SignedOut)
		if r.Locked {
			fmt.Fprintln(notices, "The sessions were not unlocked, so none was signed out of Connect; they stay valid there until they expire.")
		} else if r.Unconfirmed > 0 {
			fmt.Fprintf(notices, "%d session(s) could not be signed out of Connect and stay valid there until they expire.\n", r.Unconfirmed)
		}
		return nil
	}
	return fmt.Errorf("unknown session command --%s", opts.manage)
}

func stamp(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Local().Format("2006-01-02 15:04")
}
