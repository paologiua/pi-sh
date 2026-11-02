// Package savedlogin resumes and saves direct-login sessions in the encrypted
// session store, OpenSSH-like: one passphrase protects every saved session,
// as a passphrase protects a private key, and the system keyring holds that
// passphrase only when UseKeychain is enabled. Only sessions created by the
// pi-sh CLIs are saved; passwords and one-time codes never are.
package savedlogin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"pi-sh/internal/loginprompt"
	"pi-sh/internal/sessionstore"
	"pi-sh/internal/webauth"
)

// Prompts reads from the person; tests replace the terminal prompts.
type Prompts struct {
	// Passphrase and NewPassphrase return nil when the person skips.
	Passphrase    func(ctx context.Context, label string) ([]byte, error)
	NewPassphrase func(ctx context.Context, label string) ([]byte, error)
	Choose        func(ctx context.Context, heading string, items []string) (int, error)
}

var terminal = Prompts{loginprompt.Passphrase, loginprompt.NewPassphrase, loginprompt.Choose}

// unlockAttempts matches ssh's three passphrase prompts per key.
const unlockAttempts = 3

const (
	labelUnlock = "Passphrase for saved pi-sh sessions (Enter to skip):"
	labelCreate = "Save this session for next time? Choose a passphrase for saved pi-sh sessions (Enter to skip):"
	labelChange = "New passphrase for saved pi-sh sessions:"
)

// ErrLocked means the saved sessions exist but were not unlocked.
var ErrLocked = errors.New("saved sessions are locked: no passphrase was entered")

// Session is the part of *webauth.Client a saved session needs.
type Session interface {
	Account() string
	Export() ([]byte, error)
	Restore([]byte) error
	Verify(context.Context) error
	Logout(context.Context) error
}

// Manager owns at most one unlocked vault for the life of a process.
type Manager struct {
	store     *sessionstore.Store // nil when no configuration directory exists
	keyring   sessionstore.Keyring
	vault     *sessionstore.Vault
	skipped   bool
	prompts   Prompts
	notices   io.Writer
	newClient func() Session
	now       func() time.Time
}

// Open prepares the default store. useKeychain selects the system keyring
// for the passphrase; an unavailable keyring is reported and not used.
// Nothing is read or prompted until a session is needed.
func Open(useKeychain bool, notices io.Writer) *Manager {
	m := &Manager{prompts: terminal, notices: notices, newClient: func() Session { return webauth.New() }, now: time.Now}
	if store, err := sessionstore.Default(); err == nil {
		m.store = store
	}
	if useKeychain && m.store != nil {
		if k, err := sessionstore.SystemKeyring(); err == nil {
			m.keyring = k
		} else {
			fmt.Fprintf(notices, "UseKeychain: %v; the passphrase will be asked for\n", err)
		}
	}
	return m
}

// Close forgets the unlocked key.
func (m *Manager) Close() {
	if m.vault != nil {
		m.vault.Close()
		m.vault = nil
	}
}

// unlock opens the existing store, from the keyring or by asking up to three
// times. An empty answer or three wrong ones skip saved sessions for this
// process, as ssh moves on after a key's passphrase prompts.
func (m *Manager) unlock(ctx context.Context) error {
	if m.store == nil || m.vault != nil || m.skipped {
		return nil
	}
	if exists, err := m.store.Exists(); err != nil || !exists {
		return err
	}
	if m.keyring != nil {
		pass, found, err := m.keyring.Get()
		if err != nil {
			fmt.Fprintf(m.notices, "UseKeychain: %v\n", err)
		} else if found {
			v, err := m.store.Unlock(pass)
			clear(pass)
			if err == nil {
				m.vault = v
				return nil
			}
			if !errors.Is(err, sessionstore.ErrPassphrase) {
				return err
			}
			fmt.Fprintln(m.notices, "The keyring passphrase does not unlock the saved sessions.")
		}
	}
	for range unlockAttempts {
		pass, err := m.prompts.Passphrase(ctx, labelUnlock)
		if err != nil {
			return err
		}
		if pass == nil {
			m.skipped = true
			return nil
		}
		v, err := m.store.Unlock(pass)
		if err == nil {
			m.vault = v
			m.remember(pass)
			clear(pass)
			return nil
		}
		clear(pass)
		if !errors.Is(err, sessionstore.ErrPassphrase) {
			return err
		}
		fmt.Fprintln(m.notices, "Incorrect passphrase.")
	}
	m.skipped = true
	return nil
}

// remember stores the passphrase in the keyring, when enabled.
func (m *Manager) remember(pass []byte) {
	if m.keyring != nil {
		if err := m.keyring.Set(pass); err != nil {
			fmt.Fprintf(m.notices, "UseKeychain: %v\n", err)
		}
	}
}

// Resume returns a signed-in session (a *webauth.Client) from a saved one, or
// nil when there is none to use, together with the account to sign in as.
// With no account given, a single saved account is used and several are
// offered for choice. An expired session is removed.
func (m *Manager) Resume(ctx context.Context, account string) (Session, string, error) {
	if err := m.unlock(ctx); err != nil || m.vault == nil {
		return nil, account, err
	}
	entries, err := m.vault.List()
	if err != nil {
		return nil, account, err
	}
	if account == "" && len(entries) == 1 {
		account = entries[0].Account
	} else if account == "" && len(entries) > 1 {
		items := make([]string, 0, len(entries)+1)
		for _, e := range entries {
			items = append(items, e.Account)
		}
		i, err := m.prompts.Choose(ctx, "Saved Raspberry Pi ID accounts:", append(items, "Sign in with another account"))
		if err != nil {
			return nil, "", err
		}
		if i == len(entries) {
			return nil, "", nil
		}
		account = entries[i].Account
	}
	entry, found, err := m.vault.Get(account)
	if err != nil || !found {
		return nil, account, err
	}
	c := m.newClient()
	if err := c.Restore(entry.Data); err != nil {
		return nil, account, err
	}
	if err := c.Verify(ctx); err != nil {
		if !errors.Is(err, webauth.ErrSignedOut) {
			return nil, account, err
		}
		if _, err := m.vault.Delete(entry.Account); err != nil {
			return nil, account, err
		}
		fmt.Fprintf(m.notices, "The saved session for %s has expired; sign in again.\n", entry.Account)
		return nil, entry.Account, nil
	}
	return c, entry.Account, m.put(c, entry.Saved)
}

// Save stores a freshly signed-in session. Without a sessions file it offers
// to create one with a new passphrase; if the person skipped unlocking the
// existing file, nothing is saved.
func (m *Manager) Save(ctx context.Context, c Session) error {
	if m.store == nil || m.skipped {
		return nil
	}
	if m.vault == nil {
		if exists, err := m.store.Exists(); err != nil || exists {
			return err
		}
		pass, err := m.prompts.NewPassphrase(ctx, labelCreate)
		if err != nil || pass == nil {
			return err
		}
		v, err := m.store.Create(pass)
		if err != nil {
			clear(pass)
			return err
		}
		m.vault = v
		m.remember(pass)
		clear(pass)
	}
	if err := m.put(c, m.now()); err != nil {
		return err
	}
	fmt.Fprintf(m.notices, "Session saved for %s.\n", c.Account())
	return nil
}

// Refresh stores the session's current cookies (the service may rotate them)
// at the end of a process that used a saved or newly saved session.
func (m *Manager) Refresh(c Session) error {
	if m.vault == nil {
		return nil
	}
	entry, found, err := m.vault.Get(c.Account())
	if err != nil || !found {
		return err
	}
	return m.put(c, entry.Saved)
}

func (m *Manager) put(c Session, saved time.Time) error {
	data, err := c.Export()
	if err != nil {
		return err
	}
	defer clear(data)
	return m.vault.Put(sessionstore.Entry{Account: c.Account(), Saved: saved, Used: m.now(), Data: data})
}

// Status describes one saved session for --sessions.
type Status struct {
	Account     string
	Saved, Used time.Time
	// State is "active", "expired", or "unknown" with Err set.
	State string
	Err   error
}

// List reports the saved sessions; check asks Connect whether each is still
// signed in (one read-only request per session).
func (m *Manager) List(ctx context.Context, check bool) ([]Status, error) {
	if err := m.unlock(ctx); err != nil {
		return nil, err
	}
	if m.vault == nil {
		if m.skipped {
			return nil, ErrLocked
		}
		return nil, nil
	}
	entries, err := m.vault.List()
	if err != nil {
		return nil, err
	}
	out := make([]Status, len(entries))
	for i, e := range entries {
		out[i] = Status{Account: e.Account, Saved: e.Saved, Used: e.Used}
		if !check {
			continue
		}
		c := m.newClient()
		err := c.Restore(e.Data)
		if err == nil {
			err = c.Verify(ctx)
		}
		switch {
		case err == nil:
			out[i].State = "active"
		case errors.Is(err, webauth.ErrSignedOut):
			out[i].State = "expired"
		default:
			out[i].State, out[i].Err = "unknown", err
		}
	}
	return out, nil
}

// Logout signs the saved session out of Connect, when it is still signed in,
// and deletes it locally in every case. serverSignedOut reports whether
// Connect accepted the sign-out; err carries a sign-out failure after the
// local copy was deleted.
func (m *Manager) Logout(ctx context.Context, account string) (serverSignedOut bool, err error) {
	if err := m.unlock(ctx); err != nil {
		return false, err
	}
	if m.vault == nil {
		if m.skipped {
			return false, ErrLocked
		}
		return false, fmt.Errorf("no saved session for %s", account)
	}
	entry, found, err := m.vault.Get(account)
	if err != nil {
		return false, err
	}
	if !found {
		return false, fmt.Errorf("no saved session for %s", account)
	}
	c := m.newClient()
	signOut := c.Restore(entry.Data)
	if signOut == nil {
		signOut = c.Verify(ctx)
	}
	if signOut == nil {
		signOut = c.Logout(ctx)
	}
	if _, err := m.vault.Delete(entry.Account); err != nil {
		return false, err
	}
	if errors.Is(signOut, webauth.ErrSignedOut) {
		return false, nil
	}
	return signOut == nil, signOut
}

// ChangePassphrase re-encrypts every saved session under a new passphrase.
func (m *Manager) ChangePassphrase(ctx context.Context) error {
	if err := m.unlock(ctx); err != nil {
		return err
	}
	if m.vault == nil {
		if m.skipped {
			return ErrLocked
		}
		return errors.New("there are no saved sessions")
	}
	pass, err := m.prompts.NewPassphrase(ctx, labelChange)
	if err != nil {
		return err
	}
	if pass == nil {
		return errors.New("passphrase unchanged")
	}
	defer clear(pass)
	if err := m.vault.ChangePassphrase(pass); err != nil {
		return err
	}
	m.remember(pass)
	return nil
}
