package sshconfig

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestHostSelectionAndFirstValue(t *testing.T) {
	text := `# a comment with a backslash \ and "quotes"
HostName initial-%h
Host pi-* ?ox !pi-excluded
  EscapeChar = "!" # keep the first value
  StdinNull yes
Host pi-excluded
  EscapeChar "#"
Host *
  EscapeChar none
  RequestTTY no
Host initial-pi-one
  RequestTTY force
`
	for _, tc := range []struct{ host, escape, stdin string }{
		{"pi-one", "!", "yes"}, {"box", "!", "yes"}, {"pi-excluded", "#", ""}, {"unmatched", "none", ""}, {"PI-ONE", "!", "yes"},
	} {
		got, err := Parse(strings.NewReader(text), tc.host)
		if err != nil {
			t.Fatal(err)
		}
		want := map[string]string{"hostname": "initial-%h", "escapechar": tc.escape, "requesttty": "no"}
		if tc.stdin != "" {
			want["stdinnull"] = tc.stdin
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: got %#v, want %#v", tc.host, got, want)
		}
	}
	got, err := Parse(strings.NewReader("Host !blocked\nEscapeChar !\nHost *\nEscapeChar none\n"), "other")
	if err != nil || got["escapechar"] != "none" {
		t.Fatal("negative-only Host must not match")
	}
}

func TestConfigGrammarAndErrors(t *testing.T) {
	for _, text := range []string{
		"Host", "Host !", "Host *\nStdinNull maybe", "Host *\nRequestTTY maybe", "Host *\nEscapeChar ab",
		"Host *\nHostName %n", "Host *\nHostName value%", "Host *\nHostName \"unterminated", "HostName \" \"", "HostName \"a\x1bb\"", "HostName \"a\tb\"",
		"Host *\nHostName two words", "Host *\nEscapeChar = = none", "Host *\nHostName x\\y",
		"Host other\nProxyCommand echo hidden", "Host other\nLocalCommand echo hidden", "Include another",
		"Match exec true", "IgnoreUnknown *", "EscapeChar !\nEscapeChar invalid", "HostName x\x00y",
		strings.Repeat("a", 64<<10), strings.Repeat("# comment\n", MaxBytes/10+1),
	} {
		if _, err := Parse(strings.NewReader(text), "pi"); err == nil {
			t.Errorf("invalid config accepted: %.100q", text)
		}
	}
	got, err := Parse(strings.NewReader("\tEsCaPeChAr\t= \" \" # space\r\nHostName \"Pi #1\"\r\n"), "pi")
	if err != nil || got["escapechar"] != " " || got["hostname"] != "Pi #1" {
		t.Fatalf("quotes/comments/CRLF: %#v, %v", got, err)
	}
	_, err = Parse(io.MultiReader(strings.NewReader("HostName pi\n"), errorReader{}), "alias")
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("read failure not sanitized: %v", err)
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("secret OS error") }

func TestConfigLoadAndHostnameTokens(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	if err := os.WriteFile(path, []byte("HostName device\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path, "alias")
	if err != nil || got["hostname"] != "device" {
		t.Fatal(got, err)
	}
	for _, path := range []string{dir, filepath.Join(dir, "secret-missing-path")} {
		if _, err := Load(path, "alias"); err == nil || strings.Contains(err.Error(), path) {
			t.Fatal("invalid path accepted or disclosed")
		}
	}
	name, err := Hostname("%h-%%", "Pi %h")
	if err != nil || name != "Pi %h-%" {
		t.Fatalf("tokens expanded recursively: %q %v", name, err)
	}
	for _, values := range [][2]string{{strings.Repeat("%h", MaxTargetBytes), "ab"}, {"%h", strings.Repeat("x", MaxTargetBytes+1)}, {strings.Repeat("a", MaxTargetBytes+1), "x"}} {
		if _, err := Hostname(values[0], values[1]); err == nil {
			t.Fatal("expanded target byte limit ignored")
		}
	}
}

func TestHostWildcardByteSemantics(t *testing.T) {
	for _, tc := range []struct {
		pattern, value string
		want           bool
	}{
		{"pi?", "pi1", true}, {"pi?", "pi12", false}, {"*pi*", "api12", true}, {"*pi", "api12", false},
		{"[ab]", "a", false}, {"[ab]", "[ab]", true}, {"*", "a/b", true}, {"*?*?", "a", false},
		{"?", "é", false}, {"??", "é", true}, {"*a*b", "aabaaab", true}, {"*a*b", "aabaaac", false},
	} {
		if got := match(tc.pattern, tc.value); got != tc.want {
			t.Errorf("%q %q: %v", tc.pattern, tc.value, got)
		}
	}
}
