//go:build darwin || linux

package shellbroker

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

func modernTTYBash(t *testing.T) string {
	t.Helper()
	bash := os.Getenv("PI_SH_TEST_BASH")
	if bash == "" {
		var err error
		bash, err = exec.LookPath("bash")
		if err != nil {
			t.Skip("Bash unavailable")
		}
	}
	if err := exec.Command(bash, "-c", "((BASH_VERSINFO[0]>4||(BASH_VERSINFO[0]==4&&BASH_VERSINFO[1]>=3)))").Run(); err != nil {
		t.Skip("Bash >=4.3 required")
	}
	return bash
}

// A test-only foreground/job-table candidate. No script or option is enabled on
// the Pi. Anonymous pipes keep supervisor metadata out of the terminal stream.
func ttyJobsScript(text string) string {
	return `set +e; set +m
old_lc=${LC_ALL-}; lc_set=${LC_ALL+x}; export LC_ALL=C
command_text=` + quote(text) + `
pid=; meta_pid=; done=0
cleanup() {
  trap - EXIT CHLD HUP TERM
  if [[ $done != 1 && $pid =~ ^[0-9]+$ ]]; then kill -KILL -- "-$pid" "$pid" 2>/dev/null; fi
  [[ $meta_pid =~ ^[0-9]+$ ]] && kill "$meta_pid" 2>/dev/null
  wait "$pid" "$meta_pid" 2>/dev/null
  :
}
trap cleanup EXIT
trap - HUP TERM
trap ':' INT QUIT
saved=$(stty -g) || exit 125
worker() {
  set +m
  trap - EXIT CHLD HUP TERM INT QUIT
  trap ':' INT QUIT
  exec 0</dev/tty 1>/dev/tty 2>/dev/tty 8<&-
  stty "$saved" || exit 125
  coproc JOBS { trap '' INT QUIT; exec cat 2>/dev/null; }
  local metadata_pid=$JOBS_PID
  exec 3<&${JOBS[0]} 4>&${JOBS[1]}
  eval "exec ${JOBS[0]}<&- ${JOBS[1]}>&-"
  exec 2>&4
  trap 'jobs -l >&4' CHLD
  coproc CMD {
    IFS= read -r start || exit 125
    trap - INT QUIT PIPE
    exec 3<&- 4>&- 9>&-
    if [[ $lc_set == x ]]; then export LC_ALL=$old_lc; else unset LC_ALL; fi
    exec /bin/sh -c "$command_text" </dev/tty >/dev/tty 2>/dev/tty
  }
  local command_pid=$CMD_PID command_in=${CMD[1]} command_out=${CMD[0]}
  printf 'start\n' >&$command_in
  eval "exec $command_in>&- $command_out<&-"
  local kind=unknown line state status
  while [[ $kind == unknown ]]; do
    line=
    if IFS= read -r -t 0.05 line <&3; then
      if [[ $line =~ ^\[[0-9]+\][+-]?[[:space:]]+([0-9]+)[[:space:]]+(.*)$ && ${BASH_REMATCH[1]} == "$command_pid" ]]; then
        state=${BASH_REMATCH[2]}
        case $state in Done*|'Exit '*) kind=exit;; Running*|Stopped*) :;; *) kind=signal;; esac
      elif [[ $line =~ ^.*:[[:space:]]+line[[:space:]]+[0-9]+:[[:space:]]+([0-9]+)[[:space:]]+(.*)$ && ${BASH_REMATCH[1]} == "$command_pid" ]]; then
        kind=signal
      fi
    fi
  done
  wait "$command_pid"; status=$?
  trap - CHLD
  exec 2>/dev/null 4>&-
  kill "$metadata_pid" 2>/dev/null
  wait "$metadata_pid" 2>/dev/null
  printf 'TTY_RESULT %s %s\n' "$kind" "$status" >&9 || exit 125
  [[ $kind == signal ]] && status=255
  exit "$status"
}
coproc META { exec cat 2>/dev/null; }
meta_pid=$META_PID
exec 8<&${META[0]} 9>&${META[1]}
eval "exec ${META[0]}<&- ${META[1]}>&-"
exec 2>&9
set -m
printf '\036PISH_TTY_CANDIDATE_READY\037'
( exec "$BASH" --noprofile --norc -c "$(declare -f worker); saved=\$1; old_lc=\$2; lc_set=\$3; command_text=\$4; worker" bash "$saved" "$old_lc" "$lc_set" "$command_text" 9>&2 ) &
pid=$!
fg %+ >/dev/null 2>&9
status=$?
trap - CHLD
exec 2>/dev/null 9>&-
kind=unknown
reported_status=
while IFS= read -r line <&8; do
  if [[ $line =~ ^TTY_RESULT[[:space:]]+(exit|signal)[[:space:]]+([0-9]+)$ ]]; then
    kind=${BASH_REMATCH[1]}; reported_status=${BASH_REMATCH[2]}
  fi
done
exec 8<&-
if [[ $kind == signal && $status == 255 ]]; then done=1
elif [[ $kind == exit && $status == "$reported_status" ]]; then done=1
else status=125
fi
stty -echo -onlcr || exit 125
printf '\036PISH_TTY_CANDIDATE_EXIT:%d\037' "$status"
exit "$status"`
}

func runTTYJobsCandidate(t *testing.T, text string, input []byte, resize bool) (int, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, modernTTYBash(t), "--noprofile", "--norc", "-m", "-c", ttyJobsScript(text))
	processCode, raw := runPTYFixture(t, cmd, "", input, resize)
	start := strings.Index(raw, ttyCandidateReady)
	end := strings.Index(raw, ttyCandidateExit)
	if start < 0 || end < start+len(ttyCandidateReady) {
		t.Fatalf("missing Bash TTY result: code%d %q", processCode, raw)
	}
	value := raw[end+len(ttyCandidateExit):]
	stop := strings.IndexByte(value, '\x1f')
	if stop < 0 {
		t.Fatal("truncated Bash TTY result")
	}
	code, err := strconv.Atoi(value[:stop])
	if err != nil || code != processCode {
		t.Fatalf("Bash TTY status mismatch: %d %q", processCode, value)
	}
	return code, raw[start+len(ttyCandidateReady) : end]
}

func TestTTYJobTableCandidateExitSignal(t *testing.T) {
	reference := ttySSHCommand(t)
	for _, tc := range []struct {
		name, text string
		input      []byte
		code       int
	}{
		{"normal-zero", "printf 'fixture-ready\\n'; exit 0", nil, 0},
		{"normal-130", "printf 'fixture-ready\\n'; exit 130", nil, 130},
		{"normal-143", "printf 'fixture-ready\\n'; exit 143", nil, 143},
		{"self-int", "printf 'fixture-ready\\n'; kill -INT $$", nil, 255},
		{"self-term", "printf 'fixture-ready\\n'; kill -TERM $$", nil, 255},
		{"self-kill", "printf 'fixture-ready\\n'; kill -KILL $$", nil, 255},
		{"terminal-int", "printf 'fixture-ready\\n'; sleep 60", []byte{3}, 255},
		{"caught-int", "trap 'exit 42' INT; printf 'fixture-ready\\n'; IFS= read -r x", []byte{3}, 42},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, out := runTTYJobsCandidate(t, tc.text, tc.input, false)
			if code != tc.code {
				t.Fatalf("Bash TTY code%d want%d: %q", code, tc.code, out)
			}
			refCode, refOut := runPTYFixture(t, reference(tc.text), "", tc.input, false)
			if refCode != tc.code {
				t.Fatal(fmt.Sprintf("reference=%d want%d %q", refCode, tc.code, refOut))
			}
		})
	}
}

func TestTTYJobTableCandidateTerminalBehavior(t *testing.T) {
	reference := ttySSHCommand(t)
	for _, tc := range ttyFunctionalCases() {
		t.Run(tc.name, func(t *testing.T) {
			code, out := runTTYJobsCandidate(t, tc.text, tc.input, tc.resize)
			if code != tc.code || !strings.Contains(out, tc.want) {
				t.Fatalf("Bash TTY code%d want%d: %q", code, tc.code, out)
			}
			refCode, refOut := runPTYFixture(t, reference(tc.text), "", tc.input, tc.resize)
			if refCode != tc.code || !strings.Contains(refOut, tc.want) {
				t.Fatalf("reference code%d: %q", refCode, refOut)
			}
		})
	}
}
