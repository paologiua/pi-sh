//go:build darwin || linux

package shellbroker

import (
	"strings"
	"testing"
)

// The nested shell is explicitly interactive and owns job control. These are
// local command fixtures, not extra adapter facilities or remote requirements.
func TestStagedTTYNestedJobControlOpenSSHDifferential(t *testing.T) {
	reference := ttySSHCommand(t)
	bash := modernTTYBash(t)
	for _, tc := range []struct {
		name, script, want string
		input              []byte
	}{
		{"foreground-stop", `/bin/sh -c 'printf "fixture-ready\n"; exec sleep 0.5'; rc=$?; if ((rc==128+$(kill -l TSTP))); then printf 'foreground-stopped\n'; else exit 24; fi; fg %1; result=$?; printf 'fg-status:%s\n' "$result"; printf 'foreground-reclaimed\n'; exit 23`, "foreground-reclaimed", []byte{26}},
		{"background-read", `stty -echo; /bin/sh -c 'while ! IFS= read -r x; do :; done; printf "background-read:%s\n" "$x"' & child=$!; wait "$child"; rc=$?; if ((rc==128+$(kill -l TTIN))); then printf 'ttin-stopped\n'; else exit 24; fi; printf 'fixture-ready\n'; fg %1; rc=$?; printf 'foreground-read:%s\n' "$rc"; exit 23`, "background-read:ttin-input\r\nforeground-read:0", []byte("ttin-input\r")},
		{"background-write", `stty tostop; /bin/sh -c 'printf background-output' & child=$!; wait "$child"; rc=$?; if ((rc==128+$(kill -l TTOU))); then printf 'ttou-stopped\n'; else exit 24; fi; stty -tostop; fg %1; printf 'foreground-write:%s\n' "$?"; printf 'fixture-ready\n'; exit 23`, "background-outputforeground-write:", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			script := `set +H; set +o history; trap 'for p in $(jobs -p); do kill -KILL -- "$p" 2>/dev/null; done' EXIT; ` + tc.script
			text := "HISTFILE=/dev/null exec " + quote(bash) + " --noprofile --norc -im -c " + quote(script)
			refCode, refOut := runPTYFixture(t, reference(text), "", tc.input, false)
			if refCode != 23 || !strings.Contains(refOut, tc.want) || (tc.name == "foreground-stop" && !strings.Contains(refOut, "fg-status:0")) {
				t.Fatalf("reference code%d out%q", refCode, refOut)
			}
			for _, carrier := range []string{"/bin/sh", bash} {
				t.Run(carrier, func(t *testing.T) {
					code, out, err := runStagedTTYPlan(t, carrier, mustTTYPlan(t, text), tc.input, false)
					if err != nil || code != refCode || !strings.Contains(out, tc.want) {
						t.Fatalf("staged code%d err%v out%q; reference%q", code, err, out, refOut)
					}
					if tc.name == "foreground-stop" && !strings.Contains(out, "fg-status:0") {
						t.Fatal("fg lost ordinary job completion", out)
					}
					if tc.name == "background-write" {
						statusLine := func(text string) string {
							_, rest, ok := strings.Cut(text, "foreground-write:")
							if !ok {
								return "missing"
							}
							line, _, _ := strings.Cut(rest, "\r\n")
							return line
						}
						if got, want := statusLine(out), statusLine(refOut); got != want {
							t.Fatalf("background printf status %q vs reference %q: %q", got, want, out)
						}
					}
					if tc.name == "foreground-stop" && !strings.Contains(out, "foreground-stopped") {
						t.Fatal("foreground job did not stop", out)
					}
					if tc.name == "background-read" && !strings.Contains(out, "ttin-input") {
						t.Fatal("resumed job lost input", out)
					}
				})
			}
		})
	}
}
