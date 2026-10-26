package main

import (
	"io"
	"os"
	"strings"
	"testing"

	"pi-sh/internal/account"
	"pi-sh/internal/copycli"
)

// TestMain isolates tests from the developer's own default account.
func TestMain(m *testing.M) {
	os.Setenv(account.EnvName, "")
	os.Exit(m.Run())
}

func TestAccountOperands(t *testing.T) {
	plan, err := copycli.New([]string{"me@example.com::pi:a", "me@example.com::pi:b", "target"})
	if err != nil || plan.Account != "me@example.com" || plan.Device != "pi" {
		t.Fatalf("%+v %v", plan, err)
	}
	plan, err = copycli.New([]string{"source", "scp://me%40example.com@pi/x"})
	if err != nil || plan.Account != "me@example.com" || plan.Target.Path != "x" {
		t.Fatalf("%+v %v", plan, err)
	}
	if _, err := copycli.New([]string{"me@example.com::pi:a", "other@example.com::pi:b", "target"}); err == nil {
		t.Fatal("download sources with different accounts accepted")
	}
	for _, tc := range []struct {
		args       []string
		code       int
		diagnostic string
	}{
		{[]string{"--direct-login", "source", "pi@raspberrypi:x"}, 1, "email::device"},
		{[]string{"--direct-login", "source", "scp://pi@raspberrypi/x"}, 1, "email::device"},
		{[]string{"--direct-login", "source", "me@example.com::raspberrypi"}, 1, "email::device:path"},
		{[]string{"--browser", "source", "me@example.com::raspberrypi:x"}, 255, "cannot select or verify"},
		{[]string{"--browser", "-oUser=me@example.com", "source", "raspberrypi:x"}, 255, "cannot select or verify"},
	} {
		oldArgs, oldErrors := os.Args, os.Stderr
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		os.Args = append([]string{"pi-cp"}, tc.args...)
		os.Stderr = w
		code := app()
		os.Args, os.Stderr = oldArgs, oldErrors
		w.Close()
		output, err := io.ReadAll(r)
		r.Close()
		if err != nil || code != tc.code || !strings.Contains(string(output), tc.diagnostic) || strings.Contains(string(output), "fetch(") {
			t.Errorf("%q: %d %q", tc.args, code, output)
		}
	}
}
