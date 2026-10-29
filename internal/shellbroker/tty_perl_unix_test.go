//go:build darwin || linux

package shellbroker

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The Perl supervisor reaches the device inside a single-quoted Bash word.
func TestTTYPerlHasNoApostrophe(t *testing.T) {
	if strings.Contains(ttyPerlSource, "'") {
		t.Fatal("tty.pl contains an apostrophe")
	}
}

// ttySupervisorProbe prints the program that supervises the command: its
// parent, in the command's own process group.
const ttySupervisorProbe = `p=$(ps -o args= -p $PPID); p=${p%% *}; p=${p##*/}; printf 'supervisor:%s\n' "${p%%[0-9.]*}"`

func ttySupervisor(t *testing.T, carrier string) string {
	t.Helper()
	code, out, err := runStagedTTYCarrier(t, carrier, ttySupervisorProbe, nil, false)
	m := regexp.MustCompile(`supervisor:([a-z]+)`).FindStringSubmatch(out)
	if err != nil || code != 0 || m == nil {
		t.Fatalf("code %d, %v, %q", code, err, out)
	}
	return m[1]
}

// With perl on PATH the TTY supervisor is Perl, on both carriers;
// PI_SH_REMOTE_IMPL=bash keeps the Bash supervisor, as does a perl that
// cannot compile tty.pl.
func TestTTYSupervisorImplementation(t *testing.T) {
	path, err := exec.LookPath("perl")
	if err != nil {
		t.Skip("perl unavailable")
	}
	for _, carrier := range []string{"/bin/sh", modernTTYBash(t)} {
		t.Setenv("PI_SH_REMOTE_IMPL", "")
		if got := ttySupervisor(t, carrier); got != "perl" {
			t.Fatalf("%s: supervisor %q, want perl", carrier, got)
		}
		t.Setenv("PI_SH_REMOTE_IMPL", "bash")
		if got := ttySupervisor(t, carrier); got != "bash" {
			t.Fatalf("%s: switch gave supervisor %q", carrier, got)
		}
	}
	t.Setenv("PI_SH_REMOTE_IMPL", "")
	testTools = func(dir string) {
		stub := "#!/bin/sh\nfor a; do [ \"$a\" = -c ] && exit 1; done\nexec " + path + " \"$@\"\n"
		if err := os.WriteFile(filepath.Join(dir, "perl"), []byte(stub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { testTools = nil })
	if got := ttySupervisor(t, modernTTYBash(t)); got != "bash" {
		t.Fatalf("fallback supervisor %q", got)
	}
}

// The command starts with the same ignored signals, process group, terminal
// foreground group and environment under both supervisors, on both carriers
// (the legacy macOS /bin/sh carrier is the one whose exec kept job-control
// signals ignored, TTY_JOB_CONTROL.md).
func TestTTYSupervisorsGiveCommandSameContext(t *testing.T) {
	if _, err := exec.LookPath("perl"); err != nil {
		t.Skip("perl unavailable")
	}
	// macOS PTYs can duplicate CR during ONLCR output under backpressure,
	// even with a bare writer and no adapter. Capture the original modes,
	// then report the context without output processing, still comparing
	// every byte and the original modes rather than normalizing differences.
	text := `PI_SH_CONTEXT_MODES=$(stty -g) || exit; stty -opost || exit; env | LC_ALL=C sort | grep -Ev '^(_|PATH|PI_SH_[A-Z_]*)='; perl -e 'print "ignored:", join(",", grep { ($SIG{$_} // "") eq "IGNORE" } sort keys %SIG), "\n"'; ` +
		`[ "$(ps -o pgid= -p $$ | tr -d " ")" = "$(ps -o pgid= -p $PPID | tr -d " ")" ] && echo same-group-as-supervisor; ` +
		`[ "$(ps -o tpgid= -p $$ | tr -d " ")" = "$(ps -o pgid= -p $$ | tr -d " ")" ] && echo foreground; printf '%s\n' "$PI_SH_CONTEXT_MODES"`
	for _, carrier := range []string{"/bin/sh", modernTTYBash(t)} {
		got := map[string]string{}
		for _, impl := range []string{"", "bash"} {
			t.Setenv("PI_SH_REMOTE_IMPL", impl)
			code, out, err := runStagedTTYCarrier(t, carrier, text, nil, false)
			if err != nil || code != 0 {
				t.Fatalf("%s %q: code %d, %v, context output %d bytes", carrier, impl, code, err, len(out))
			}
			got[impl] = out
		}
		if got[""] != got["bash"] {
			// The context includes inherited environment values; never dump them.
			t.Fatalf("%s: command contexts differ (%d/%d output bytes)", carrier, len(got[""]), len(got["bash"]))
		}
		if !strings.Contains(got[""], "same-group-as-supervisor") || !strings.Contains(got[""], "foreground") {
			t.Fatalf("%s: command not in the foreground carrier group", carrier)
		}
	}
}
