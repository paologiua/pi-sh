//go:build darwin || linux

package main

import (
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
)

func matchFixture(t *testing.T, text string) (string, string) {
	t.Helper()
	me, err := user.Current()
	if err != nil || me.Username == "" || strings.ContainsAny(me.Username, " \t\"'\\#") {
		t.Skip("local user name unsuitable for fixtures")
	}
	text = strings.ReplaceAll(text, "{me}", me.Username)
	text = strings.ReplaceAll(text, "{ME}", strings.ToUpper(me.Username))
	dir := t.TempDir()
	text = strings.ReplaceAll(text, "{dir}", filepath.ToSlash(dir))
	path := filepath.Join(dir, "config")
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	return dir, path
}

func TestMatchOpenSSHDifferential(t *testing.T) {
	ssh, err := exec.LookPath("ssh")
	if err != nil {
		t.Skip("local OpenSSH unavailable")
	}
	// Both implementations run Match exec through $SHELL -c.
	t.Setenv("SHELL", "/bin/sh")
	const hit, rest = "EscapeChar !\n", "Host *\nEscapeChar none\n"
	for _, tc := range []struct {
		name, text string
		args       []string
		child      string
	}{
		{"host-case-sensitive", "Host pi-*\n" + hit + rest, []string{"PI-ONE"}, ""},
		{"host-exact-case", "Host PI-*\n" + hit + rest, []string{"PI-ONE"}, ""},
		{"match-host-folds-pattern", "Match host PI\n" + hit + rest, []string{"pi"}, ""},
		{"match-host-folds-target", "Match host pi\n" + hit + rest, []string{"PI"}, ""},
		{"match-host-hostname", "HostName Dev-%h\nMatch host dev-pi\n" + hit + rest, []string{"PI"}, ""},
		{"match-host-not-original", "HostName device\nMatch host pi\n" + hit + rest, []string{"pi"}, ""},
		{"originalhost", "HostName device\nMatch originalhost PI\n" + hit + rest, []string{"pi"}, ""},
		{"all", "Match all\n" + hit, []string{"pi"}, ""},
		{"not-all", "Match !all\n" + hit + rest, []string{"pi"}, ""},
		{"all-after-match", "Match host pi all\n" + hit + rest, []string{"pi"}, ""},
		{"all-after-failure", "Match host x all\n" + hit + rest, []string{"pi"}, ""},
		{"all-comment", "Match all #c\n" + hit + rest, []string{"pi"}, ""},
		{"all-empty-argument", "Match all \"\"\n" + hit + rest, []string{"pi"}, ""},
		{"case-insensitive-criteria", "Match HOST pi Final EXEC true\n" + hit + rest, []string{"pi"}, ""},
		{"equals-syntax", "Match host=pi\n" + hit + rest, []string{"pi"}, ""},
		{"equals-syntax-case", "Match HOST=pi\n" + hit + rest, []string{"pi"}, ""},
		{"equals-in-pattern", "Match host=pi=x\n" + hit + rest, []string{"pi"}, ""},
		{"keyword-equals", "Match = host pi\n" + hit + rest, []string{"pi"}, ""},
		{"comment-ends-criteria", "Match host pi # host x\n" + hit + rest, []string{"pi"}, ""},
		{"quoted-comment", "Match host pi \"#x\" host x\n" + hit + rest, []string{"pi"}, ""},
		{"list", "Match host a,pi\n" + hit + rest, []string{"pi"}, ""},
		{"list-negation-wins", "Match host *,!pi\n" + hit + rest, []string{"pi"}, ""},
		{"only-negated", "Match host !x\n" + hit + rest, []string{"pi"}, ""},
		{"empty-subpatterns", "Match host ,pi,,\n" + hit + rest, []string{"pi"}, ""},
		{"only-empty-subpatterns", "Match host ,\n" + hit + rest, []string{"pi"}, ""},
		{"double-negation", "Match host !!pi\n" + hit + rest, []string{"pi"}, ""},
		{"no-brackets", "Match host [p]i\n" + hit + rest, []string{"pi"}, ""},
		{"wildcards", "Match host p?,x*\n" + hit + rest, []string{"pi"}, ""},
		{"quoted-space", "Match host \"a pi\"\n" + hit + rest, []string{"pi"}, ""},
		{"negated-criterion", "Match !host x\n" + hit + rest, []string{"pi"}, ""},
		{"negated-criterion-fails", "Match !host pi\n" + hit + rest, []string{"pi"}, ""},
		{"and", "Match host a host pi\n" + hit + rest, []string{"pi"}, ""},
		{"and-negated", "Match host pi !host b originalhost pi\n" + hit + rest, []string{"pi"}, ""},
		{"later-block-resets", "Match host pi\nMatch host x\n" + hit + rest, []string{"pi"}, ""},
		{"host-after-match", "Match host x\nHost pi\n" + hit + rest, []string{"pi"}, ""},
		{"first-value", "Match all\nEscapeChar !\nMatch host pi\nEscapeChar none\nStdinNull yes\n", []string{"pi"}, ""},
		{"tagged-P", "Match tagged foo\n" + hit + rest, []string{"-P", "foo", "pi"}, ""},
		{"tagged-P-attached", "Match tagged f*\n" + hit + rest, []string{"-Pfoo", "pi"}, ""},
		{"tagged-case", "Match tagged FOO\n" + hit + rest, []string{"-P", "foo", "pi"}, ""},
		{"tagged-list", "Match tagged bar,foo\n" + hit + rest, []string{"-P", "foo", "pi"}, ""},
		{"tagged-negated-list", "Match tagged !foo,*\n" + hit + rest, []string{"-P", "foo", "pi"}, ""},
		{"tagged-empty-unset", "Match tagged \"\"\n" + hit + rest, []string{"pi"}, ""},
		{"tagged-equals-empty", "Match tagged=\n" + hit + rest, []string{"pi"}, ""},
		{"tagged-star-unset", "Match tagged *\n" + hit + rest, []string{"pi"}, ""},
		{"tagged-list-unset", "Match tagged \",x\"\n" + hit + rest, []string{"pi"}, ""},
		{"not-tagged-empty", "Match !tagged \"\"\n" + hit + rest, []string{"-P", "x", "pi"}, ""},
		{"P-empty", "Match tagged \"\"\n" + hit + rest, []string{"-P", "", "pi"}, ""},
		{"P-empty-star", "Match tagged *\n" + hit + rest, []string{"-P", "", "pi"}, ""},
		{"P-first", "Match tagged b\n" + hit + rest, []string{"-P", "a", "-P", "b", "pi"}, ""},
		{"P-comma-literal", "Match tagged a,b\n" + hit + rest, []string{"-P", "a,b", "pi"}, ""},
		{"Tag-keyword", "Tag foo\nMatch tagged foo\n" + hit + rest, []string{"pi"}, ""},
		{"Tag-first", "Tag foo\nTag bar\nMatch tagged bar\n" + hit + rest, []string{"pi"}, ""},
		{"Tag-none-literal", "Tag none\nMatch tagged none\n" + hit + rest, []string{"pi"}, ""},
		{"Tag-quoted", "Tag \"a b\"\nMatch tagged \"a b\"\n" + hit + rest, []string{"pi"}, ""},
		{"P-overrides-Tag", "Tag foo\nMatch tagged bar\n" + hit + rest, []string{"-P", "bar", "pi"}, ""},
		{"o-Tag-before-P", "Tag foo\n", []string{"-o", "Tag=baz", "-P", "bar", "pi"}, ""},
		{"P-before-o-Tag", "Tag foo\n", []string{"-P", "bar", "-o", "Tag=baz", "pi"}, ""},
		{"Tag-in-inactive-block", "Match tagged x\nTag x\nMatch tagged x\n" + hit + rest, []string{"pi"}, ""},
		{"user-local-default", "Match user {me}\n" + hit + rest, []string{"pi"}, ""},
		{"user-case-sensitive", "Match user {ME}\n" + hit + rest, []string{"pi"}, ""},
		{"localuser", "Match localuser {me},!root\n" + hit + rest, []string{"pi"}, ""},
		{"localuser-equals", "Match LocalUser={me}\n" + hit + rest, []string{"pi"}, ""},
		{"version-any", "Match version *\n" + hit + rest, []string{"pi"}, ""},
		{"sessiontype-shell", "Match sessiontype shell\n" + hit + rest, []string{"pi"}, ""},
		{"sessiontype-exec", "Match sessiontype exec\n" + hit + rest, []string{"pi", "echo", "hi"}, ""},
		{"sessiontype-empty-command", "Match sessiontype shell\n" + hit + rest, []string{"pi", ""}, ""},
		{"sessiontype-space-command", "Match sessiontype exec\n" + hit + rest, []string{"pi", "", ""}, ""},
		{"sessiontype-case", "Match sessiontype SHELL\n" + hit + rest, []string{"pi"}, ""},
		{"sessiontype-list", "Match sessiontype subsystem,exec\n" + hit + rest, []string{"pi", "true"}, ""},
		{"command-exact", "Match command \"echo hi\"\n" + hit + rest, []string{"pi", "echo", "hi"}, ""},
		{"command-wildcard", "Match command echo*\n" + hit + rest, []string{"pi", "echo", "hi"}, ""},
		{"command-word", "Match command echo\n" + hit + rest, []string{"pi", "echo", "hi"}, ""},
		{"command-list", "Match command echo,*\n" + hit + rest, []string{"pi", "echo", "hi"}, ""},
		{"command-comma-split", "Match command \"a,b\"\n" + hit + rest, []string{"pi", "a,b"}, ""},
		{"command-empty", "Match command \"\"\n" + hit + rest, []string{"pi"}, ""},
		{"command-star-none", "Match command *\n" + hit + rest, []string{"pi"}, ""},
		{"command-star", "Match command *\n" + hit + rest, []string{"pi", "true"}, ""},
		{"command-empty-with-command", "Match command \"\"\n" + hit + rest, []string{"pi", "true"}, ""},
		{"command-joined-spaces", "Match command \"a  b\"\n" + hit + rest, []string{"pi", "a", "", "b"}, ""},
		{"command-after-double-dash", "Match command x\n" + hit + rest, []string{"pi", "--", "x"}, ""},
		{"command-case", "Match command ECHO\n" + hit + rest, []string{"pi", "echo"}, ""},
		{"command-negated-only", "Match command !x\n" + hit + rest, []string{"pi", "echo"}, ""},
		{"canonical-first-pass", "Match canonical\n" + hit, []string{"pi"}, ""},
		{"not-canonical", "Match !canonical\n" + hit, []string{"pi"}, ""},
		{"final", "Match final\n" + hit, []string{"pi"}, ""},
		{"not-final", "Match !final\n" + hit, []string{"pi"}, ""},
		{"not-final-first", "Match !final\nEscapeChar !\nMatch final\nEscapeChar none\n", []string{"pi"}, ""},
		{"final-after-not-final", "Match final\nEscapeChar !\nMatch !final\nEscapeChar none\n", []string{"pi"}, ""},
		{"canonical-in-final-pass", "Match canonical\nEscapeChar !\nMatch final\nStdinNull yes\n", []string{"pi"}, ""},
		{"final-requested-by-failed-line", "Match host x final\nMatch canonical\n" + hit, []string{"pi"}, ""},
		{"negated-final-no-pass", "Match host x !final\nMatch canonical\n" + hit, []string{"pi"}, ""},
		{"final-all", "Match final all\n" + hit, []string{"pi"}, ""},
		{"canonical-all", "Match canonical all\n" + hit, []string{"pi"}, ""},
		{"canonical-final", "Match canonical final\n" + hit, []string{"pi"}, ""},
		{"final-hostname-fixed", "Match final\nHostName dev\nMatch host dev\n" + hit, []string{"pi"}, ""},
		{"final-hostname-not-final", "Match !final\nHostName dev\n", []string{"pi"}, ""},
		{"final-host-uses-target", "HostName Dev-%h\nMatch final host dev-pi\n" + hit, []string{"PI"}, ""},
		{"final-percent-literal", "HostName a%%h\nMatch final\n", []string{"pi"}, ""},
		{"final-tag", "Match final\nTag x\nMatch tagged x\n" + hit, []string{"pi"}, ""},
		{"final-host-original", "Host PI\nMatch final\nHost PI\n" + hit, []string{"PI"}, ""},
		{"final-later-values", "HostName Dev\nMatch final\nMatch host Dev\nStdinNull yes\n", []string{"PI"}, ""},
		{"final-from-inactive-include", "Host other\nInclude {dir}/child\nHost *\nMatch canonical\n" + hit, []string{"pi"}, "Match final\n"},
		{"include-in-match", "Match host pi\nInclude {dir}/child\n" + rest, []string{"pi"}, hit},
		{"include-in-failed-match", "Match host x\nInclude {dir}/child\n" + rest, []string{"pi"}, "Host *\n" + hit},
		{"match-in-include", "Include {dir}/child\n" + rest, []string{"pi"}, "Match originalhost pi\n" + hit},
		{"exec-true", "Match exec true\n" + hit + rest, []string{"pi"}, ""},
		{"exec-false", "Match exec false\n" + hit + rest, []string{"pi"}, ""},
		{"exec-status", "Match exec \"exit 3\"\n" + hit + rest, []string{"pi"}, ""},
		{"exec-negated", "Match !exec false\n" + hit + rest, []string{"pi"}, ""},
		{"exec-equals", "Match exec=\"test 1 = 1\"\n" + hit + rest, []string{"pi"}, ""},
		{"exec-stdin-null", "Match exec \"read x\"\n" + hit + rest, []string{"pi"}, ""},
		{"exec-tokens", "Match exec \"test %n = PI && test %h = PI && test $(printf %%s x%%y | wc -c) -eq 3 && test %r = %u\"\n" + hit + rest, []string{"PI"}, ""},
		{"exec-hostname-token", "HostName Dev-%h\nMatch exec \"test %h = Dev-PI && test %n = PI\"\n" + hit + rest, []string{"PI"}, ""},
		{"exec-local-tokens", "Match exec \"test %u = $(id -un) && test %i = $(id -u) && test %d = ~ && test %l = $(hostname) && test %L = $(hostname | cut -d. -f1)\"\n" + hit + rest, []string{"pi"}, ""},
		{"exec-final-pass", "Match final exec \"test %h = pi\"\n" + hit, []string{"pi"}, ""},
		{"exec-skipped", "Match host x exec \"touch {dir}/ran\"\nMatch exec \"test ! -e {dir}/ran\"\n" + hit + rest, []string{"pi"}, ""},
		{"exec-before-failure-runs", "Match exec \"touch {dir}/ran\" host x\nMatch exec \"test -e {dir}/ran\"\n" + hit + rest, []string{"pi"}, ""},
		{"exec-in-inactive-include", "Host other\nInclude {dir}/child\nHost *\nMatch exec \"test -e {dir}/ran\"\n" + hit + rest, []string{"pi"}, "Match exec \"touch {dir}/ran\"\n"},
		{"localnetwork-loopback", "Match localnetwork 127.0.0.0/8\n" + hit + rest, []string{"pi"}, ""},
		{"localnetwork-address", "Match localnetwork 127.0.0.1\n" + hit + rest, []string{"pi"}, ""},
		{"localnetwork-none", "Match localnetwork 10.255.0.0/16\n" + hit + rest, []string{"pi"}, ""},
		{"localnetwork-list", "Match localnetwork 10.255.0.0/16,::1/128\n" + hit + rest, []string{"pi"}, ""},
		{"localnetwork-v6", "Match localnetwork ::1\n" + hit + rest, []string{"pi"}, ""},
		{"localnetwork-link-local", "Match localnetwork fe80::/10\n" + hit + rest, []string{"pi"}, ""},
		{"localnetwork-default", "Match localnetwork 0.0.0.0/0\n" + hit + rest, []string{"pi"}, ""},
		{"localnetwork-negated", "Match !localnetwork 10.255.0.0/16\n" + hit + rest, []string{"pi"}, ""},
		{"localnetwork-zero-mask", "Match localnetwork 127.0.0.0/08\n" + hit + rest, []string{"pi"}, ""},
		{"localnetwork-mapped", "Match localnetwork ::ffff:127.0.0.1/128\n" + hit + rest, []string{"pi"}, ""},
		{"localnetwork-equals", "Match localnetwork=127.0.0.0/8\n" + hit + rest, []string{"pi"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, path := matchFixture(t, tc.text)
			args := append([]string{"-G", "-F", path}, tc.args...)
			if tc.child != "" {
				if err := os.WriteFile(filepath.Join(dir, "child"), []byte(strings.ReplaceAll(tc.child, "{dir}", filepath.ToSlash(dir))), 0600); err != nil {
					t.Fatal(err)
				}
			}
			want, err := openSSHDump(t, ssh, args)
			if err != nil {
				t.Fatalf("OpenSSH reference: %v", err)
			}
			// Side-effect fixtures must observe the same state in both runs.
			os.Remove(filepath.Join(dir, "ran"))
			compareDump(t, args, want)
		})
	}
}

func TestMatchRejectsLikeOpenSSH(t *testing.T) {
	ssh, err := exec.LookPath("ssh")
	if err != nil {
		t.Skip("local OpenSSH unavailable")
	}
	t.Setenv("SHELL", "/bin/sh")
	for _, tc := range []struct {
		text string
		args []string
	}{
		{"Match\n", nil}, {"Match host\n", nil}, {"Match host \"\"\n", nil}, {"Match host=\n", nil},
		{"Match host= pi\n", nil}, {"Match all host x\n", nil}, {"Match all all\n", nil},
		{"Match host a host b all\n", nil}, {"Match host #c\n", nil}, {"Match # c\n", nil},
		{"Match host a b\n", nil}, {"Match bogus x\n", nil}, {"Match bogus=x\n", nil},
		{"Match final=x pi\n", nil}, {"Match all=x\n", nil}, {"Match hostx pi\n", nil}, {"Match !\n", nil},
		{"Match !!host pi\n", nil}, {"Match \"host pi\"\n", nil}, {"Match all \"\" host x\n", nil},
		{"Match =pi\n", nil}, {"Match !=pi\n", nil}, {"Match final foo\n", nil},
		{"Match tagged\n", nil}, {"Match command\n", nil}, {"Match user \"\"\n", nil},
		{"Match localuser \"\"\n", nil}, {"Match originalhost \"\"\n", nil}, {"Match version \"\"\n", nil},
		{"Match sessiontype \"\"\n", nil}, {"Match exec \"\"\n", nil}, {"Match localnetwork \"\"\n", nil},
		{"Match localnetwork bogus\n", nil}, {"Match localnetwork 127.0.0.1/8\n", nil},
		{"Match localnetwork !127.0.0.0/8\n", nil}, {"Match localnetwork 127.0.0.0/33\n", nil},
		{"Match localnetwork 127.0.0.0/8,\n", nil}, {"Match localnetwork ,127.0.0.0/8\n", nil},
		{"Match localnetwork 127.0.0.0/\n", nil}, {"Match localnetwork 127.0.0.0/8/8\n", nil},
		{"Match localnetwork 127.1/8\n", nil}, {"Match host x localnetwork bogus\n", nil},
		{"Match exec \"echo %z\"\n", nil}, {"Match exec \"echo %\"\n", nil}, {"Match exec \"kill -9 $$\"\n", nil},
		{"Tag\n", nil}, {"Tag a b\n", nil}, {"Tag \"\"\n", nil}, {"EscapeChar !\n", []string{"-o", "Tag="}},
		{"EscapeChar !\n", []string{"-o", "Tag=x y"}}, {"EscapeChar !\n", []string{"-o", "Match=all"}},
	} {
		_, path := matchFixture(t, tc.text)
		args := append(append([]string{"-G", "-F", path}, tc.args...), "pi")
		if _, err := openSSHDump(t, ssh, args); err == nil {
			t.Fatalf("reference accepted %q %q", tc.text, tc.args)
		}
		if _, err := parseOptions(args); err == nil {
			t.Errorf("pi-sh accepted %q %q", tc.text, tc.args)
		}
	}
}

// Documented, deliberate differences from OpenSSH 10.2, checked locally.
func TestMatchDocumentedDifferences(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	for _, tc := range []struct {
		text   string
		args   []string
		escape string // "" means pi-sh must reject
	}{
		// pi-sh is not OpenSSH: Match version compares "pi-sh_<version>".
		{"Match version OpenSSH_*\nEscapeChar !\n", []string{"pi"}, "~"},
		{"Match version pi-sh_*\nEscapeChar !\n", []string{"pi"}, "!"},
		// No SSH port, host key alias or jump host exists to expand.
		{"Match exec \"test %p = 22\"\nEscapeChar !\n", []string{"pi"}, ""},
		{"Match exec \"echo %C %k %j\"\nEscapeChar !\n", []string{"pi"}, ""},
		// Tokens are validated even when an earlier criterion skips exec.
		{"Match host x exec \"echo %z\"\nEscapeChar !\n", []string{"pi"}, ""},
		// Connect names may contain spaces; they are never spliced into a shell.
		{"HostName \"Pi #1\"\nMatch exec \"echo %h\"\nEscapeChar !\n", []string{"pi"}, ""},
		{"HostName \"x;y\"\nMatch exec \"echo %h\"\nEscapeChar !\n", []string{"pi"}, ""},
		{"HostName \"Pi #1\"\nMatch host \"pi #1\"\nEscapeChar !\n", []string{"pi"}, "!"},
		// The final pass keeps the exact Connect name; OpenSSH lowercases it.
		{"HostName Dev\nMatch final exec \"test %h = Dev\"\nEscapeChar !\n", []string{"pi"}, "!"},
	} {
		_, path := matchFixture(t, tc.text)
		args := append([]string{"-G", "-F", path}, tc.args...)
		o, err := parseOptions(args)
		if tc.escape == "" {
			if err == nil {
				t.Errorf("accepted %q", tc.text)
			}
			continue
		}
		if err != nil || o.escape != tc.escape {
			t.Errorf("%q: escape %q, %v", tc.text, o.escape, err)
		}
	}
}
