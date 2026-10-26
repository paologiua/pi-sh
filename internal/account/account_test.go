package account

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	for _, ok := range []string{"a@b.co", "name.surname+tag@example.com", "Owner_1@Sub.Example-Domain.org", "x@y.invalid"} {
		if err := Validate(ok); err != nil {
			t.Errorf("%q rejected: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "pi", "@example.com", "a@", "a@b", "a@@b.com", "a@b@c.com", "a b@c.com", "a@b..com",
		"a@-b.com", "a@b-.com", "a@.b.com", "a@b.com.", ".a@b.com", "a.@b.com", "a..b@c.com", "a@b_c.com", "a\"b@c.com",
		"a:b@c.com", "a@b.com\n", "\ta@b.com", "é@b.com", "a@bé.com", strings.Repeat("a", 65) + "@b.com",
		"a@" + strings.Repeat("b", 64) + ".com", "a@" + strings.Repeat("b.", 127) + "com"} {
		if Validate(bad) == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestDefaultSources(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	xdg := filepath.Join(dir, "xdg")
	write := func(path, text string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	homeFile := filepath.Join(home, ".config", "pi-sh", "account")
	xdgFile := filepath.Join(xdg, "pi-sh", "account")
	run := func(env map[string]string) (string, error) {
		return defaultFrom(func(k string) (string, bool) { v, ok := env[k]; return v, ok },
			func() (string, error) { return home, nil })
	}
	if got, err := run(nil); got != "" || err != nil {
		t.Fatalf("missing file: %q %v", got, err)
	}
	write(homeFile, "# default Raspberry Pi ID account\n\n  home@example.com  \r\n")
	if got, err := run(nil); got != "home@example.com" || err != nil {
		t.Fatalf("home file: %q %v", got, err)
	}
	// A relative XDG_CONFIG_HOME is ignored, as the XDG specification says.
	if got, err := run(map[string]string{"XDG_CONFIG_HOME": "relative"}); got != "home@example.com" || err != nil {
		t.Fatalf("relative XDG: %q %v", got, err)
	}
	if got, err := run(map[string]string{"XDG_CONFIG_HOME": xdg}); got != "" || err != nil {
		t.Fatalf("XDG without file must not fall back to ~/.config: %q %v", got, err)
	}
	write(xdgFile, "xdg@example.com\n")
	if got, err := run(map[string]string{"XDG_CONFIG_HOME": xdg}); got != "xdg@example.com" || err != nil {
		t.Fatalf("XDG file: %q %v", got, err)
	}
	if got, err := run(map[string]string{EnvName: "env@example.com", "XDG_CONFIG_HOME": xdg}); got != "env@example.com" || err != nil {
		t.Fatalf("environment precedence: %q %v", got, err)
	}
	if got, err := run(map[string]string{EnvName: ""}); got != "" || err != nil {
		t.Fatalf("empty environment disables the default: %q %v", got, err)
	}
	if _, err := run(map[string]string{EnvName: "pi"}); err == nil || !strings.Contains(err.Error(), EnvName) {
		t.Fatalf("invalid environment value: %v", err)
	}
	for _, text := range []string{"", "# only a comment\n", "a@example.com\nb@example.com\n", "pi\n", strings.Repeat("#", maxFileBytes+1)} {
		write(homeFile, text)
		if got, err := run(nil); err == nil {
			t.Errorf("file %q accepted as %q", text, got)
		}
	}
	if err := os.Remove(homeFile); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(homeFile, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := run(nil); !errors.Is(err, errFile) {
		t.Fatalf("directory accepted: %v", err)
	}
}

// The default file is only read: loading it must not create anything.
func TestDefaultNeverWrites(t *testing.T) {
	home := t.TempDir()
	if _, err := defaultFrom(func(string) (string, bool) { return "", false }, func() (string, error) { return home, nil }); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(home)
	if err != nil || len(entries) != 0 {
		t.Fatalf("default lookup created %v (%v)", entries, err)
	}
}
