//go:build darwin || linux

package shellbroker

import (
	"os/exec"
	"strings"
	"testing"
)

func TestGNUCarrierPendinPreservesFreshState(t *testing.T) {
	source := strings.TrimSuffix(ttySource, "_pt_main\n")
	for _, arg := range []string{"pendin", "-pendin"} {
		t.Run(arg, func(t *testing.T) {
			initial, want := "0", "4000"
			if arg == "-pendin" {
				initial, want = "4000", "0"
			}
			state := "500:5:bf:" + initial + strings.Repeat(":0", 32)
			script := source + `
OSTYPE=linux-gnu; HOSTTYPE=aarch64
mock_state=$1
stty() {
 case $1 in
 -g) printf '%s\n' "$mock_state";;
 --version) printf 'stty (GNU coreutils) fixture\n';;
 pendin|-pendin|reprint) printf 'stty: invalid argument\n' >&2; return 1;;
 *:*) mock_state=$1;;
 *) [[ $# == 3 && $1 == echo && $2 == rprnt && $3 == '^R' ]] || return 1; mock_state=501:${mock_state#*:};;
 esac
}
_pt_stty_modes "$2" echo reprint '^R' || exit 125
printf '%s\n' "$mock_state"
`
			out, err := exec.Command(modernTTYBash(t), "--noprofile", "--norc", "-c", script, "pendin-fixture", state, arg).CombinedOutput()
			expected := "501:5:bf:" + want + strings.Repeat(":0", 32) + "\n"
			if err != nil || string(out) != expected {
				t.Fatalf("%v out%q want%q", err, out, expected)
			}
		})
	}
}

func TestGNUCarrierPendinRejectsUnknownOrUnappliedState(t *testing.T) {
	source := strings.TrimSuffix(ttySource, "_pt_main\n")
	for _, scenario := range []string{"unknown-platform", "unknown-utility", "malformed", "wrong-count", "oversized-word", "oversized-control", "readback-refused", "write-refused"} {
		t.Run(scenario, func(t *testing.T) {
			state := "500:5:bf:0" + strings.Repeat(":0", 32)
			script := source + `
OSTYPE=linux-gnu; HOSTTYPE=aarch64
mock_state=$1; scenario=$2
case $scenario in
 unknown-platform) HOSTTYPE=unknown;;
 malformed) mock_state='500:5:bf:not-a-number';;
 wrong-count) mock_state='500:5:bf:0:0';;
 oversized-word) mock_state=123456789:${mock_state#*:};;
 oversized-control) mock_state=${mock_state%:*}:100;;
esac
stty() {
 case $1 in
 -g) printf '%s\n' "$mock_state";;
 --version) if [[ $scenario == unknown-utility ]]; then printf 'other stty\n'; else printf 'stty (GNU coreutils) fixture\n'; fi;;
 *:*)
  [[ $scenario != write-refused ]] || return 1
  if [[ $scenario != readback-refused ]]; then mock_state=$1; fi;;
 *) return 1;;
 esac
}
_pt_stty_modes pendin
`
			if out, err := exec.Command(modernTTYBash(t), "--noprofile", "--norc", "-c", script, "pendin-fixture", state, scenario).CombinedOutput(); err == nil {
				t.Fatalf("accepted %s %q", scenario, out)
			}
		})
	}
}
