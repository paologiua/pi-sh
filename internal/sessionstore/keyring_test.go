package sessionstore

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

type call struct {
	name  string
	args  []string
	stdin []byte
}

type fakeRunner struct {
	calls []call
	reply func(c call) runResult
}

func (f *fakeRunner) run(_ context.Context, name string, args []string, stdin []byte) (runResult, error) {
	c := call{name, append([]string(nil), args...), bytes.Clone(stdin)}
	f.calls = append(f.calls, c)
	if f.reply == nil {
		return runResult{}, nil
	}
	return f.reply(c), nil
}

const secret = "s3cr3t pass 'quoted\" \\ é"

func secretInArgs(t *testing.T, calls []call) {
	t.Helper()
	hexed := string(encodeSecret([]byte(secret)))
	for _, c := range calls {
		for _, a := range c.args {
			if strings.Contains(a, secret) || strings.Contains(a, hexed) || strings.Contains(a, "s3cr3t") {
				t.Fatalf("secret in argv: %q %q", c.name, c.args)
			}
		}
	}
}

func TestMacKeyring(t *testing.T) {
	f := &fakeRunner{}
	k, err := systemKeyring("darwin", func(p string) (string, error) { return p, nil }, f.run)
	if err != nil {
		t.Fatal(err)
	}
	if err := k.Set([]byte(secret)); err != nil {
		t.Fatal(err)
	}
	c := f.calls[0]
	if c.name != "/usr/bin/security" || strings.Join(c.args, " ") != "-i" {
		t.Fatalf("Set ran %q %q", c.name, c.args)
	}
	want := "add-generic-password -U -s pi-sh -a sessions -w " + string(encodeSecret([]byte(secret))) + "\n"
	if string(c.stdin) != want {
		t.Fatalf("Set stdin = %q", c.stdin)
	}

	f.reply = func(call) runResult { return runResult{Stderr: []byte("add-generic-password: returned -1")} }
	if err := k.Set([]byte(secret)); err == nil {
		t.Fatal("stderr failure ignored")
	}

	f.reply = func(call) runResult { return runResult{Stdout: append(encodeSecret([]byte(secret)), '\n')} }
	got, ok, err := k.Get()
	if err != nil || !ok || string(got) != secret {
		t.Fatalf("Get = %q %v %v", got, ok, err)
	}
	if c := f.calls[len(f.calls)-1]; strings.Join(c.args, " ") != "find-generic-password -s pi-sh -a sessions -w" {
		t.Fatalf("Get args %q", c.args)
	}

	f.reply = func(call) runResult { return runResult{ExitCode: 44} }
	if got, ok, err := k.Get(); got != nil || ok || err != nil {
		t.Fatalf("Get missing = %q %v %v", got, ok, err)
	}
	if err := k.Delete(); err != nil {
		t.Fatalf("Delete missing = %v", err)
	}
	if c := f.calls[len(f.calls)-1]; strings.Join(c.args, " ") != "delete-generic-password -s pi-sh -a sessions" {
		t.Fatalf("Delete args %q", c.args)
	}

	f.reply = func(call) runResult { return runResult{Stdout: []byte("not hex\n")} }
	if _, _, err := k.Get(); err == nil || strings.Contains(err.Error(), "not hex") {
		t.Fatalf("bad item = %v", err)
	}
	secretInArgs(t, f.calls)
}

func TestSecretToolKeyring(t *testing.T) {
	f := &fakeRunner{}
	k, err := systemKeyring("linux", func(string) (string, error) { return "/usr/bin/secret-tool", nil }, f.run)
	if err != nil {
		t.Fatal(err)
	}
	if err := k.Set([]byte(secret)); err != nil {
		t.Fatal(err)
	}
	c := f.calls[0]
	if c.name != "/usr/bin/secret-tool" || strings.Join(c.args, "|") != "store|--label=pi-sh sessions|service|pi-sh|account|sessions" {
		t.Fatalf("Set ran %q %q", c.name, c.args)
	}
	if !bytes.Equal(c.stdin, encodeSecret([]byte(secret))) {
		t.Fatalf("Set stdin = %q", c.stdin)
	}

	f.reply = func(call) runResult { return runResult{Stdout: encodeSecret([]byte(secret))} }
	got, ok, err := k.Get()
	if err != nil || !ok || string(got) != secret {
		t.Fatalf("Get = %q %v %v", got, ok, err)
	}
	if c := f.calls[len(f.calls)-1]; strings.Join(c.args, " ") != "lookup service pi-sh account sessions" {
		t.Fatalf("Get args %q", c.args)
	}
	f.reply = func(call) runResult { return runResult{ExitCode: 1} }
	if got, ok, err := k.Get(); got != nil || ok || err != nil {
		t.Fatalf("Get missing = %q %v %v", got, ok, err)
	}
	f.reply = nil
	if err := k.Delete(); err != nil {
		t.Fatal(err)
	}
	if c := f.calls[len(f.calls)-1]; strings.Join(c.args, " ") != "clear service pi-sh account sessions" {
		t.Fatalf("Delete args %q", c.args)
	}
	secretInArgs(t, f.calls)
}

func TestNoKeyring(t *testing.T) {
	missing := func(string) (string, error) { return "", errors.New("not found") }
	_, err := systemKeyring("linux", missing, nil)
	if !errors.Is(err, ErrNoKeyring) || !strings.Contains(err.Error(), "secret-tool") {
		t.Fatalf("linux without secret-tool = %v", err)
	}
	if err == nil || !strings.Contains(err.Error(), "libsecret") {
		t.Fatalf("missing libsecret hint: %v", err)
	}
	if _, err := systemKeyring("windows", missing, nil); !errors.Is(err, ErrNoKeyring) {
		t.Fatalf("windows = %v", err)
	}
	if _, err := systemKeyring("darwin", missing, nil); err == nil {
		t.Fatal("darwin without security accepted")
	}
}
