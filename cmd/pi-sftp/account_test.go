package main

import (
	"os"
	"strings"
	"testing"

	"pi-sh/internal/account"
	"pi-sh/internal/sftpcli"
)

// TestMain isolates tests from the developer's own default account.
func TestMain(m *testing.M) {
	os.Setenv(account.EnvName, "")
	os.Exit(m.Run())
}

// email::device and sftp://email%40domain@device select the account; a
// Unix-style user is refused before login, and --browser refuses an explicit
// account it cannot select or verify (status 255, no companion instruction).
func TestAccountSelection(t *testing.T) {
	for _, tc := range []struct {
		args       []string
		code       int
		diagnostic string
	}{
		{[]string{"--direct-login", "pi@raspberrypi"}, 1, "email::device"},
		{[]string{"--direct-login", "sftp://pi@raspberrypi"}, 1, "email::device"},
		{[]string{"--direct-login", "-oUser=pi", "raspberrypi"}, 1, "email::device"},
		{[]string{"--direct-login", "bad@local::raspberrypi"}, 1, "invalid Raspberry Pi ID account"},
		{[]string{"--direct-login", "me@example.com::raspberrypi:dir"}, 1, "initial remote path"},
		{[]string{"--browser", "me@example.com::raspberrypi"}, 255, "cannot select or verify"},
		{[]string{"--browser", "sftp://me%40example.com@raspberrypi"}, 255, "cannot select or verify"},
		{[]string{"--browser", "-oUser=me@example.com", "raspberrypi"}, 255, "cannot select or verify"},
	} {
		code, output := runApp(t, tc.args...)
		if code != tc.code || !strings.Contains(output, tc.diagnostic) || strings.Contains(output, "Password (hidden") || strings.Contains(output, "fetch(") {
			t.Errorf("%q: %d %q", tc.args, code, output)
		}
	}
	_, _, policy, err := sftpcli.ResolveSession("raspberrypi", "none", nil, "", map[string]string{"user": "me@example.com"})
	if err != nil || policy.LoginAccount() != "me@example.com" {
		t.Fatalf("%+v %v", policy, err)
	}
	t.Setenv(account.EnvName, "default@example.com")
	_, _, policy, err = sftpcli.ResolveSession("raspberrypi", "none", nil, "", nil)
	if err != nil || policy.Account != "" || policy.LoginAccount() != "default@example.com" {
		t.Fatalf("default account: %+v %v", policy, err)
	}
}
