package main

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"pi-sh/internal/savedlogin"
)

type fakeManager struct {
	list      []savedlogin.Status
	signedOut bool
	err       error
	logouts   []string
	changed   int
	created   bool
	removed   bool
}

func (f *fakeManager) List(context.Context, bool) ([]savedlogin.Status, error) { return f.list, f.err }
func (f *fakeManager) Logout(_ context.Context, account string) (bool, error) {
	f.logouts = append(f.logouts, account)
	return f.signedOut, f.err
}
func (f *fakeManager) ChangePassphrase(context.Context) (bool, error) {
	f.changed++
	return f.created, f.err
}
func (f *fakeManager) ForgetPassphrase() (bool, error) { return f.removed, f.err }
func (f *fakeManager) Close()                          {}

func runManage(t *testing.T, f *fakeManager, args ...string) (string, error) {
	t.Helper()
	opts, err := parseOptions(args)
	if err != nil {
		t.Fatal(args, err)
	}
	old := openSessions
	openSessions = func(options, io.Writer) sessionManager { return f }
	defer func() { openSessions = old }()
	var out strings.Builder
	err = manageSessions(context.Background(), opts, &out, io.Discard)
	return out.String(), err
}

func TestSessionCommands(t *testing.T) {
	when := time.Date(2026, 11, 2, 18, 0, 0, 0, time.Local)
	f := &fakeManager{list: []savedlogin.Status{{Account: "a@example.com", Saved: when, Used: when, State: "active"}, {Account: "b@example.com", Saved: when, State: "expired"}}}
	out, err := runManage(t, f, "--sessions")
	if err != nil || !strings.Contains(out, "a@example.com  active   2026-11-02 18:00") || !strings.Contains(out, "b@example.com  expired") {
		t.Fatalf("--sessions:\n%s%v", out, err)
	}
	if out, _ := runManage(t, &fakeManager{}, "--sessions"); out != "No saved sessions.\n" {
		t.Fatalf("empty list: %q", out)
	}
	f = &fakeManager{signedOut: true}
	if out, err := runManage(t, f, "--logout", "a@example.com"); err != nil || !strings.Contains(out, "Signed a@example.com out") || f.logouts[0] != "a@example.com" {
		t.Fatalf("--logout: %q %v", out, err)
	}
	if out, err := runManage(t, &fakeManager{}, "--logout", "a@example.com"); err != nil || !strings.Contains(out, "no longer signed in") {
		t.Fatalf("--logout expired: %q %v", out, err)
	}
	f = &fakeManager{err: errors.New("sign-out failed")}
	if out, err := runManage(t, f, "--logout", "a@example.com"); err == nil || out != "" {
		t.Fatalf("--logout failure: %q %v", out, err)
	}
	f = &fakeManager{}
	if out, err := runManage(t, f, "-oUseKeychain=yes", "--passphrase"); err != nil || f.changed != 1 || !strings.Contains(out, "Passphrase changed") {
		t.Fatalf("--passphrase: %q %v", out, err)
	}
	if out, err := runManage(t, &fakeManager{created: true}, "--passphrase"); err != nil || !strings.Contains(out, "Passphrase set") {
		t.Fatalf("first --passphrase: %q %v", out, err)
	}
	if out, err := runManage(t, &fakeManager{removed: true}, "--forget-passphrase"); err != nil || !strings.Contains(out, "Removed the saved-sessions passphrase") {
		t.Fatalf("--forget-passphrase: %q %v", out, err)
	}
	if out, err := runManage(t, &fakeManager{}, "--forget-passphrase"); err != nil || !strings.Contains(out, "holds no pi-sh passphrase") {
		t.Fatalf("--forget-passphrase without entry: %q %v", out, err)
	}
}

func TestSessionCommandOptions(t *testing.T) {
	o, err := parseOptions([]string{"-o", "UseKeychain=yes", "--sessions"})
	if err != nil || o.manage != "sessions" || !o.policy.UseKeychain {
		t.Fatalf("%+v %v", o.policy, err)
	}
	for _, args := range [][]string{
		{"--logout"}, {"--logout", "pi@raspberrypi"}, {"--sessions", "device"}, {"--sessions", "--passphrase"}, {"--forget-passphrase", "device"},
		{"--browser", "--sessions"}, {"-G", "--sessions"}, {"-oUseKeychain=maybe", "--sessions"},
	} {
		if _, err := parseOptions(args); err == nil {
			t.Errorf("accepted %q", args)
		}
	}
}
