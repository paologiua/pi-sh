package savedlogin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"testing"
	"time"

	"pi-sh/internal/sessionstore"
	"pi-sh/internal/webauth"
)

// fakeServer stands in for Connect: a token is signed in while active.
type fakeServer struct {
	active   map[string]bool
	signOuts int
}

type fakeSession struct {
	srv      *fakeServer
	Acct     string `json:"account"`
	Token    string `json:"token"`
	signedIn bool
}

func (f *fakeSession) Account() string { return f.Acct }
func (f *fakeSession) Export() ([]byte, error) {
	if !f.signedIn {
		return nil, errors.New("not signed in")
	}
	return json.Marshal(f)
}
func (f *fakeSession) Restore(b []byte) error { return json.Unmarshal(b, f) }
func (f *fakeSession) Verify(context.Context) error {
	if !f.srv.active[f.Token] {
		return webauth.ErrSignedOut
	}
	f.signedIn = true
	return nil
}
func (f *fakeSession) Logout(context.Context) error {
	f.srv.active[f.Token] = false
	f.srv.signOuts++
	return nil
}

type fakeKeyring struct {
	pass []byte
	sets int
}

func (k *fakeKeyring) Get() ([]byte, bool, error) {
	return append([]byte(nil), k.pass...), k.pass != nil, nil
}
func (k *fakeKeyring) Set(p []byte) error { k.pass = append([]byte(nil), p...); k.sets++; return nil }
func (k *fakeKeyring) Delete() error      { k.pass = nil; return nil }

// script answers prompts in order and counts them.
type script struct {
	t        *testing.T
	answers  []string
	choices  []int
	prompted int
}

func (s *script) next() []byte {
	s.prompted++
	if len(s.answers) == 0 {
		s.t.Fatal("unexpected passphrase prompt")
	}
	a := s.answers[0]
	s.answers = s.answers[1:]
	if a == "" {
		return nil
	}
	return []byte(a)
}

func (s *script) prompts() Prompts {
	return Prompts{
		Passphrase:    func(context.Context, string) ([]byte, error) { return s.next(), nil },
		NewPassphrase: func(context.Context, string) ([]byte, error) { return s.next(), nil },
		Choose: func(_ context.Context, _ string, items []string) (int, error) {
			if len(s.choices) == 0 {
				s.t.Fatal("unexpected choice prompt")
			}
			c := s.choices[0]
			s.choices = s.choices[1:]
			return c, nil
		},
	}
}

type env struct {
	t     *testing.T
	store *sessionstore.Store
	srv   *fakeServer
}

func newEnv(t *testing.T) *env {
	return &env{t: t, store: sessionstore.Open(filepath.Join(t.TempDir(), "pi-sh", "sessions")), srv: &fakeServer{active: map[string]bool{}}}
}

func (e *env) manager(s *script) *Manager {
	m := &Manager{store: e.store, prompts: s.prompts(), notices: io.Discard, now: time.Now,
		newClient: func() Session { return &fakeSession{srv: e.srv} },
		// Tests never reach the real keyring.
		systemKeyring: func() (sessionstore.Keyring, error) { return nil, sessionstore.ErrNoKeyring }}
	e.t.Cleanup(m.Close)
	return m
}

// login is a fresh direct login on the fake server.
func (e *env) login(account, token string) *fakeSession {
	e.srv.active[token] = true
	return &fakeSession{srv: e.srv, Acct: account, Token: token, signedIn: true}
}

func TestSaveThenResumeWithPassphrase(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	first := &script{t: t, answers: []string{"secret"}}
	m := e.manager(first)
	if c, account, err := m.Resume(ctx, "me@example.com"); c != nil || account != "me@example.com" || err != nil {
		t.Fatalf("resume without store: %v %q %v", c, account, err)
	}
	if err := m.Save(ctx, e.login("me@example.com", "t1")); err != nil {
		t.Fatal(err)
	}
	second := &script{t: t, answers: []string{"secret"}}
	c, account, err := e.manager(second).Resume(ctx, "")
	if err != nil || c == nil || account != "me@example.com" {
		t.Fatalf("resume: %v %q %v", c, account, err)
	}
	if second.prompted != 1 {
		t.Fatalf("prompts = %d", second.prompted)
	}
}

func TestSkippedOrWrongPassphraseNeverSaves(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	if err := e.manager(&script{t: t, answers: []string{"secret"}}).Save(ctx, e.login("me@example.com", "t1")); err != nil {
		t.Fatal(err)
	}
	for _, answers := range [][]string{{""}, {"bad", "worse", "worst"}} {
		s := &script{t: t, answers: answers}
		m := e.manager(s)
		if c, _, err := m.Resume(ctx, "me@example.com"); c != nil || err != nil {
			t.Fatalf("%q: resumed %v %v", answers, c, err)
		}
		// Skipping must not offer to create or overwrite the store.
		if err := m.Save(ctx, e.login("me@example.com", "t2")); err != nil || s.prompted != len(answers) {
			t.Fatalf("%q: save prompted %d, %v", answers, s.prompted, err)
		}
	}
}

func TestDecliningToCreateSavesNothing(t *testing.T) {
	e := newEnv(t)
	if err := e.manager(&script{t: t, answers: []string{""}}).Save(context.Background(), e.login("me@example.com", "t1")); err != nil {
		t.Fatal(err)
	}
	if exists, _ := e.store.Exists(); exists {
		t.Fatal("store created without a passphrase")
	}
}

func TestExpiredSessionIsRemoved(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	if err := e.manager(&script{t: t, answers: []string{"secret"}}).Save(ctx, e.login("me@example.com", "t1")); err != nil {
		t.Fatal(err)
	}
	e.srv.active["t1"] = false
	m := e.manager(&script{t: t, answers: []string{"secret"}})
	c, account, err := m.Resume(ctx, "")
	if c != nil || account != "me@example.com" || err != nil {
		t.Fatalf("expired: %v %q %v", c, account, err)
	}
	if list, _ := m.List(ctx, false); len(list) != 0 {
		t.Fatalf("expired session kept: %+v", list)
	}
}

func TestSeveralAccountsAreOffered(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	m := e.manager(&script{t: t, answers: []string{"secret"}})
	for i, a := range []string{"a@example.com", "b@example.com"} {
		if err := m.Save(ctx, e.login(a, string(rune('1'+i)))); err != nil {
			t.Fatal(err)
		}
	}
	c, account, err := e.manager(&script{t: t, answers: []string{"secret"}, choices: []int{1}}).Resume(ctx, "")
	if err != nil || c == nil || account != "b@example.com" {
		t.Fatalf("choice: %v %q %v", c, account, err)
	}
	c, account, err = e.manager(&script{t: t, answers: []string{"secret"}, choices: []int{2}}).Resume(ctx, "")
	if err != nil || c != nil || account != "" {
		t.Fatalf("another account: %v %q %v", c, account, err)
	}
}

func TestListLogoutAndChangePassphrase(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	m := e.manager(&script{t: t, answers: []string{"secret"}})
	for _, a := range []string{"a@example.com", "b@example.com"} {
		if err := m.Save(ctx, e.login(a, a)); err != nil {
			t.Fatal(err)
		}
	}
	e.srv.active["b@example.com"] = false
	list, err := e.manager(&script{t: t, answers: []string{"secret"}}).List(ctx, true)
	if err != nil || len(list) != 2 || list[0].State != "active" || list[1].State != "expired" {
		t.Fatalf("list: %+v %v", list, err)
	}
	m = e.manager(&script{t: t, answers: []string{"secret"}})
	if signedOut, err := m.Logout(ctx, "A@example.com"); !signedOut || err != nil || e.srv.signOuts != 1 {
		t.Fatalf("logout active: %v %v", signedOut, err)
	}
	if signedOut, err := m.Logout(ctx, "b@example.com"); signedOut || err != nil || e.srv.signOuts != 1 {
		t.Fatalf("logout expired: %v %v", signedOut, err)
	}
	if _, err := m.Logout(ctx, "b@example.com"); err == nil {
		t.Fatal("logout of a missing session succeeded")
	}
	if exists, _ := e.store.Exists(); exists {
		t.Fatal("sessions file kept after the last logout")
	}
}

func TestChangePassphraseAndFirstSetup(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	created, err := e.manager(&script{t: t, answers: []string{"first"}}).ChangePassphrase(ctx)
	if err != nil || !created {
		t.Fatalf("first setup: %v %v", created, err)
	}
	// A login after the setup is saved under that passphrase, unasked.
	m := e.manager(&script{t: t, answers: []string{"first"}})
	if c, _, err := m.Resume(ctx, "me@example.com"); c != nil || err != nil {
		t.Fatalf("resume from empty store: %v %v", c, err)
	}
	if err := m.Save(ctx, e.login("me@example.com", "t1")); err != nil {
		t.Fatal(err)
	}
	created, err = e.manager(&script{t: t, answers: []string{"first", "renewed"}}).ChangePassphrase(ctx)
	if err != nil || created {
		t.Fatalf("change: %v %v", created, err)
	}
	if list, err := e.manager(&script{t: t, answers: []string{"renewed"}}).List(ctx, false); err != nil || len(list) != 1 {
		t.Fatalf("after change: %+v %v", list, err)
	}
	if _, err := e.manager(&script{t: t, answers: []string{""}}).List(ctx, false); !errors.Is(err, ErrLocked) {
		t.Fatalf("skipped unlock: %v", err)
	}
	if _, err := e.manager(&script{t: t, answers: []string{""}}).ChangePassphrase(ctx); err == nil {
		t.Fatal("skipped unlock changed the passphrase")
	}
}

func TestForgetPassphraseAndLastLogoutCleanup(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	k := &fakeKeyring{}
	withKeyring := func(m *Manager) *Manager {
		m.keyring = k
		m.systemKeyring = func() (sessionstore.Keyring, error) { return k, nil }
		return m
	}
	if err := withKeyring(e.manager(&script{t: t, answers: []string{"secret"}})).Save(ctx, e.login("me@example.com", "t1")); err != nil {
		t.Fatal(err)
	}
	// Forgetting works with UseKeychain off and keeps the sessions.
	m := e.manager(&script{t: t})
	m.systemKeyring = func() (sessionstore.Keyring, error) { return k, nil }
	if removed, err := m.ForgetPassphrase(); !removed || err != nil || k.pass != nil {
		t.Fatalf("forget: %v %v %q", removed, err, k.pass)
	}
	if removed, err := m.ForgetPassphrase(); removed || err != nil {
		t.Fatalf("forget again: %v %v", removed, err)
	}
	if exists, _ := e.store.Exists(); !exists {
		t.Fatal("forgetting the passphrase removed the sessions")
	}
	// The last logout removes the file and the keyring passphrase.
	m = withKeyring(e.manager(&script{t: t, answers: []string{"secret"}}))
	if _, err := m.Logout(ctx, "me@example.com"); err != nil {
		t.Fatal(err)
	}
	if exists, _ := e.store.Exists(); exists || k.pass != nil {
		t.Fatalf("after last logout: file=%v keyring=%q", exists, k.pass)
	}
}

func TestKeyringHoldsOnlyThePassphrase(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	k := &fakeKeyring{}
	m := e.manager(&script{t: t, answers: []string{"secret"}})
	m.keyring = k
	if err := m.Save(ctx, e.login("me@example.com", "t1")); err != nil {
		t.Fatal(err)
	}
	if string(k.pass) != "secret" {
		t.Fatalf("keyring = %q", k.pass)
	}
	s := &script{t: t}
	m = e.manager(s)
	m.keyring = k
	if c, _, err := m.Resume(ctx, ""); c == nil || err != nil || s.prompted != 0 {
		t.Fatalf("keyring unlock: %v %v prompts=%d", c, err, s.prompted)
	}
	k.pass = []byte("stale")
	s = &script{t: t, answers: []string{"secret"}}
	m = e.manager(s)
	m.keyring = k
	if c, _, err := m.Resume(ctx, ""); c == nil || err != nil || s.prompted != 1 || string(k.pass) != "secret" {
		t.Fatalf("stale keyring: %v %v prompts=%d keyring=%q", c, err, s.prompted, k.pass)
	}
}
