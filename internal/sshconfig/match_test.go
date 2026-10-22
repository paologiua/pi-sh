package sshconfig

import (
	"errors"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type fakeEnv struct {
	commands []string
	status   int
	runErr   error
	addrs    []netip.Addr
	addrErr  error
	goos     string
}

func (f *fakeEnv) env() *localEnv {
	return &localEnv{
		user:     func() (string, error) { return "local", nil },
		hostname: func() (string, error) { return "box.example", nil },
		uid:      func() int { return 501 },
		addrs:    func() ([]netip.Addr, error) { return f.addrs, f.addrErr },
		run: func(command string, _ io.Writer) (int, error) {
			f.commands = append(f.commands, command)
			return f.status, f.runErr
		},
		goos: f.goos,
	}
}

func parseWith(text string, ctx Context, f *fakeEnv) (map[string]string, error) {
	ctx.env = f.env()
	// User/-l belong to another module; seed "user" directly, as a CLI value
	// obtained before the file would be, without requiring that keyword here.
	user, hasUser := ctx.Overrides["user"]
	overrides := map[string]string{}
	for k, v := range ctx.Overrides {
		if k != "user" {
			overrides[k] = v
		}
	}
	ctx.Overrides = overrides
	return evaluate(ctx, func(p *parser) error {
		if hasUser {
			p.values["user"] = user
		}
		return p.parse(strings.NewReader(text), true, false, 0)
	})
}

func TestMatchCriteriaWithInjectedEnvironment(t *testing.T) {
	addrs := []netip.Addr{netip.MustParseAddr("192.168.4.7"), netip.MustParseAddr("2001:db8::5")}
	for _, tc := range []struct {
		text string
		ctx  Context
		want bool
	}{
		{"Match user local", Context{Host: "pi"}, true},
		{"Match user bob", Context{Host: "pi", Overrides: map[string]string{"user": "bob"}}, true},
		{"Match user local", Context{Host: "pi", Overrides: map[string]string{"user": "bob"}}, false},
		{"Match localuser local", Context{Host: "pi", Overrides: map[string]string{"user": "bob"}}, true},
		{"Match localnetwork 192.168.4.0/24", Context{Host: "pi"}, true},
		{"Match localnetwork 2001:db8::/32", Context{Host: "pi"}, true},
		{"Match localnetwork 192.168.5.0/24,10.0.0.0/8", Context{Host: "pi"}, false},
		{"Match sessiontype subsystem command sftp", Context{Host: "pi", SessionType: SessionSubsystem, Command: "sftp"}, true},
		{"Match sessiontype exec command /usr/lib/sftp-server", Context{Host: "pi", SessionType: SessionExec, Command: "/usr/lib/sftp-server"}, true},
		{"Match sessiontype shell", Context{Host: "pi", SessionType: SessionSubsystem, Command: "sftp"}, false},
		{"Match sessiontype exec", Context{Host: "pi", Command: "uptime"}, true},
		{"Match sessiontype none", Context{Host: "pi", SessionType: SessionNone}, true},
		{"Match version " + Release(), Context{Host: "pi"}, true},
		{"Match tagged x", Context{Host: "pi", Overrides: map[string]string{"tag": "x"}}, true},
		{"Match tagged \"\"", Context{Host: "pi", Overrides: map[string]string{"tag": ""}}, true},
		{"Match host Device", Context{Host: "pi", Overrides: map[string]string{"hostname": "DEVICE"}}, true},
		{"Match host " + strings.Repeat("x", 1023) + ",pi", Context{Host: "pi"}, false},
		{"Match host " + strings.Repeat("x", 1022) + ",pi", Context{Host: "pi"}, true},
	} {
		got, err := parseWith(tc.text+"\nEscapeChar !\n", tc.ctx, &fakeEnv{addrs: addrs})
		if err != nil {
			t.Fatalf("%.80q: %v", tc.text, err)
		}
		if (got["escapechar"] == "!") != tc.want {
			t.Errorf("%.80q: matched %v", tc.text, !tc.want)
		}
	}
}

func TestMatchExecSemantics(t *testing.T) {
	f := &fakeEnv{}
	ctx := Context{Host: "PI", Overrides: map[string]string{"hostname": "Dev-%h", "user": "remote"}}
	got, err := parseWith("Match exec \"x %% %h %n %r %u %i %l %L\"\nEscapeChar !\n", ctx, f)
	if err != nil || got["escapechar"] != "!" {
		t.Fatal(got, err)
	}
	if want := []string{"x % Dev-PI PI remote local 501 box.example box"}; !reflect.DeepEqual(f.commands, want) {
		t.Fatalf("expansion %q", f.commands)
	}
	f = &fakeEnv{status: 1}
	got, err = parseWith("Match host x exec a\nMatch exec b host x exec c\nMatch !exec d\nEscapeChar !\n", Context{Host: "pi"}, f)
	if err != nil || got["escapechar"] != "!" || !reflect.DeepEqual(f.commands, []string{"b", "d"}) {
		t.Fatal("exec must run only while preceding criteria match", f.commands, got, err)
	}
	// Like OpenSSH, an exec criterion runs again during the final pass.
	f = &fakeEnv{}
	got, err = parseWith("Match exec x final\nEscapeChar !\n", Context{Host: "pi"}, f)
	if err != nil || got["escapechar"] != "!" || len(f.commands) != 2 {
		t.Fatal("exec runs in both passes", f.commands, err)
	}
	f = &fakeEnv{}
	got, err = parseWith("Match final exec x\nEscapeChar !\n", Context{Host: "pi"}, f)
	if err != nil || got["escapechar"] != "!" || len(f.commands) != 1 {
		t.Fatal("exec after a failed final criterion is skipped in the first pass", f.commands, err)
	}
	for _, tc := range []struct {
		text string
		f    *fakeEnv
	}{
		{"Match exec x", &fakeEnv{runErr: errors.New("Match exec command exited abnormally")}},
		{"Match exec x", &fakeEnv{goos: "windows"}},
		{"Match exec %p", &fakeEnv{}}, {"Match exec %C", &fakeEnv{}}, {"Match exec %k", &fakeEnv{}},
		{"Match exec %j", &fakeEnv{}}, {"Match exec %z", &fakeEnv{}}, {"Match exec x%", &fakeEnv{}},
		{"Match host x exec %T", &fakeEnv{}},
		{"Match localnetwork 10.0.0.0/8", &fakeEnv{addrErr: errors.New("cannot enumerate")}},
	} {
		if _, err := parseWith(tc.text+"\n", Context{Host: "pi"}, tc.f); err == nil {
			t.Errorf("accepted %q", tc.text)
		}
		if tc.f.runErr == nil && len(tc.f.commands) != 0 {
			t.Errorf("%q ran a command", tc.text)
		}
	}
	// Names that could inject into the shell are refused, not spliced.
	for _, ctx := range []Context{
		{Host: "a b"}, {Host: "a;b"}, {Host: "-x"}, {Host: "pi", Overrides: map[string]string{"hostname": "Pi $x"}},
		{Host: "pi", Overrides: map[string]string{"user": "a`b"}},
	} {
		f := &fakeEnv{}
		if _, err := parseWith("Match exec \"echo %h %n %r\"\n", ctx, f); err == nil || len(f.commands) != 0 {
			t.Errorf("unsafe token expanded for %+v", ctx)
		}
	}
}

func TestMatchFinalPassSemantics(t *testing.T) {
	f := &fakeEnv{}
	got, err := parseWith("HostName dev-%h-%%\nMatch final\nHostName ignored\nStdinNull yes\nMatch canonical host dev-pi-%\nEscapeChar !\n", Context{Host: "pi"}, f)
	if err != nil || got["stdinnull"] != "yes" || got["escapechar"] != "!" {
		t.Fatal(got, err)
	}
	if name, err := Hostname(got["hostname"], "pi"); err != nil || name != "dev-pi-%" {
		t.Fatal("final HostName must stay literal after re-expansion", got["hostname"], name, err)
	}
	// The final pass is triggered from an inactive Include and re-reads files.
	dir := t.TempDir()
	child := configFile(t, dir, "child", "Match final\n")
	root := configFile(t, dir, "root", "Host other\nInclude "+includeName(child)+"\nMatch canonical\nEscapeChar !\n")
	got, err = Load(root, "pi")
	if err != nil || got["escapechar"] != "!" {
		t.Fatal("final pass from inactive Include", got, err)
	}
	got, err = Load(root, "other")
	if err != nil || got["escapechar"] != "!" {
		t.Fatal("final pass from active Include", got, err)
	}
	// Negated final does not request a second pass.
	got, err = parseWith("Match !final\nMatch canonical\nEscapeChar !\n", Context{Host: "pi"}, f)
	if err != nil || got["escapechar"] != "" {
		t.Fatal(got, err)
	}
}

func TestCIDRListValidation(t *testing.T) {
	for _, ok := range []string{"127.0.0.0/8", "127.0.0.1", "::1", "fe80::/10", "0.0.0.0/0", "127.0.0.0/08", "10.0.0.0/8,::/0"} {
		if _, err := parseCIDRList(ok); err != nil {
			t.Errorf("%q rejected", ok)
		}
	}
	for _, bad := range []string{"", ",", "127.0.0.1/8", "!127.0.0.0/8", "127.0.0.0/33", "::/129", "127.0.0.0/8,", "127.0.0.0/",
		"127.0.0.0/8/8", "127.1/8", "fe80::1%lo0", "bogus", "127.0.0.0/+8", "127.0.0.0/-8", strings.Repeat("0", 50), "1.2.3.4/99999999999999999999"} {
		if _, err := parseCIDRList(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestMatchPatternListSemantics(t *testing.T) {
	for _, tc := range []struct {
		value, pattern string
		lower          bool
		want           int
	}{
		{"pi", "a,pi", false, 1}, {"pi", "*,!pi", false, -1}, {"pi", "!x", false, 0}, {"pi", "PI", true, 1},
		{"pi", "PI", false, 0}, {"", "", false, 0}, {"", ",", false, 1}, {"pi", "!!pi", false, 0}, {"pi", "[p]i", false, 0},
	} {
		if got := matchPatternList(tc.value, tc.pattern, tc.lower); got != tc.want {
			t.Errorf("%q %q: %d", tc.value, tc.pattern, got)
		}
	}
}

func TestRunShellFollowsOpenSSH(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("no POSIX shell")
	}
	t.Setenv("SHELL", "/bin/sh")
	var stderr strings.Builder
	status, err := runShell("read x || echo stdin-null >&2; echo stdout-discarded; exit 7", &stderr)
	if err != nil || status != 7 || stderr.String() != "stdin-null\n" {
		t.Fatalf("status %d err %v stderr %q", status, err, stderr.String())
	}
	if _, err := runShell("kill -9 $$", io.Discard); err == nil {
		t.Fatal("signal termination must be an error")
	}
	t.Setenv("SHELL", filepath.Join(t.TempDir(), "missing-shell"))
	if _, err := runShell("true", io.Discard); err == nil {
		t.Fatal("non-executable shell accepted")
	}
}
