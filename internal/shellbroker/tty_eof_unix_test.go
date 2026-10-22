//go:build darwin || linux

package shellbroker

import (
	"strings"
	"testing"
)

// Ctrl-D is a canonical record boundary, not an unconditional session close.
// Use the same command interpreter as the reference to isolate terminal behavior.
func TestStagedTTYNonemptyEOFOpenSSHDifferential(t *testing.T) {
	reference := ttySSHCommand(t)
	for _, tc := range []struct {
		name, text, want string
		input            []byte
	}{
		{"flush-record", `printf 'fixture-ready\n'; first=$(dd bs=16 count=1 2>/dev/null | od -An -tx1 | tr -d '[:space:]'); printf 'first:%s\n' "$first"; IFS= read -r x; printf 'next:%s\n' "$x"; exit 23`, "first:616263\r\nnext:next\r\n", []byte("abc\x04next\r")},
		{"partial-read-eof", `printf 'fixture-ready\n'; IFS= read -r x; rc=$?; printf 'read:%s:%s\n' "$rc" "$x"; exit 23`, "read:1:abc\r\n", []byte("abc\x04\x04")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			refCode, refOut := runPTYFixture(t, reference("exec /bin/sh -c "+quote(tc.text)), "", tc.input, false)
			if refCode != 23 || !strings.Contains(refOut, tc.want) {
				t.Fatalf("reference code%d out%q", refCode, refOut)
			}
			for _, carrier := range []string{"/bin/sh", modernTTYBash(t)} {
				t.Run(carrier, func(t *testing.T) {
					code, out, err := runStagedTTYPlan(t, carrier, mustTTYPlan(t, tc.text), tc.input, false)
					if err != nil || code != refCode || !strings.Contains(out, tc.want) {
						t.Fatalf("staged code%d err%v out%q; reference%q", code, err, out, refOut)
					}
				})
			}
		})
	}
}

func TestOpenSSHCommandTTYCloseWithUnreadInput(t *testing.T) {
	reference := ttySSHCommand(t)
	text := `stty raw -echo; printf fixture-ready; c=$(dd bs=1 count=1 2>/dev/null); printf 'seen:%s\n' "$c"; exit 23`
	code, out := runPTYFixture(t, reference(text), "", []byte(strings.Repeat("x", 2048)), false)
	if code != 23 || !strings.Contains(out, "seen:x\n") {
		t.Fatalf("reference code%d output%q", code, out)
	}
}
