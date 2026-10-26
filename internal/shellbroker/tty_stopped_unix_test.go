//go:build darwin || linux

package shellbroker

import (
	"strings"
	"testing"
)

func TestStagedTTYStopOpenSSHDifferential(t *testing.T) {
	reference := ttySSHCommand(t)
	for _, tc := range []struct {
		name, text string
		input      []byte
	}{
		{"self-stop-resume", "parent=$$; (sleep 0.2; kill -CONT \"$parent\") & kill -STOP $$; wait; printf resumed; exit 23", nil},
		{"terminal-suspend", "printf 'fixture-ready\\n'; IFS= read -r x; printf 'seen:%s\\n' \"$x\"; exit 23", []byte{26, 'x', '\r'}},
		{"caught-self-tstp", "trap 'printf caught-suspend' TSTP; kill -TSTP $$; printf resumed; exit 23", nil},
		{"caught-suspend", `exec bash -c 'trap "printf caught-suspend" TSTP; printf "fixture-ready\n"; until IFS= read -r x; do :; done; printf "seen:%s\n" "$x"; exit 23'`, []byte{26, 'x', '\r'}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// The reference's sshd PATH resolves bash to macOS Bash 3.2.
			tc.text = strings.ReplaceAll(tc.text, "exec bash ", "exec "+quote(modernTTYBash(t))+" ")
			refText := tc.text
			if !strings.Contains(refText, "fixture-ready") {
				refText = "printf 'fixture-ready\\n'; " + refText
			}
			refCode, refOut := runPTYFixture(t, reference("exec /bin/sh -c "+quote(refText)), "", tc.input, false)
			code, out, err := runStagedTTY(t, tc.text, tc.input, false)
			if refCode != 23 || err != nil || code != refCode {
				t.Fatalf("reference code%d %q; staged code%d error=%v %q", refCode, refOut, code, err, out)
			}
			if strings.Contains(refOut, "caught-suspend") != strings.Contains(out, "caught-suspend") {
				t.Fatalf("TSTP handler mismatch: reference %q; staged %q", refOut, out)
			}
			if tc.name == "self-stop-resume" && (!strings.Contains(refOut, "resumed") || out != "resumed") {
				t.Fatalf("resume mismatch %q / %q", refOut, out)
			}
			if strings.Contains(tc.text, "IFS= read") && (!strings.Contains(refOut, "seen:x") || !strings.Contains(out, "seen:x")) {
				t.Fatalf("input mismatch %q / %q", refOut, out)
			}
		})
	}
}
