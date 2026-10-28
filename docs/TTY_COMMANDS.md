# Experimental TTY commands — 2026-10-21

The production adapter now uses one controller in its dedicated carrier group,
fixing caught TSTP and Ctrl-Z status loss observed with the older separate-worker
design. Startup files/history are isolated; no new remote software is required.
Same-interpreter OpenSSH parity, both carrier types, process-isolated HUP/TERM
cleanup and whole-group STOP/CONT have local evidence in
[TTY_JOB_CONTROL.md](TTY_JOB_CONTROL.md). Three explicit nested Ctrl-Z/fg/TTIN/TTOU cases now match local OpenSSH;
native and arbitrary nested job control remains open. Local suspend/reacquire now has
Mac process/interactive/TTY coordinator evidence in ESCAPES.md.

`--experimental-command` now selects the staged Bash TTY adapter for effective
`-t`/`-tt` or RequestTTY yes/force requests. This is an experimental CLI
implementation with native browser-companion evidence for fds, TERM, initial
size, input, PENDIN, resize, handled INT/TSTP, empty canonical EOF and status23
in REAL_DEVICE_TESTS.md. One earlier post-input
WebRTC failure remains unclassified; native advanced/control/cleanup paths remain
open. The
existing no-TTY binary broker, legacy adapter and ordinary interactive shell
remain separate paths.

A failed input carrier send now permits up to one second of independent result
drain: only a valid TX plus clean EOF can preserve the command status. Missing or
malformed results, local I/O failures, auth/cancellation and transport loss without
completion still fail. Local race/reference tests and a native2048-byte input to
one-byte read/status23 are detailed in [TTY_COMPLETION.md](TTY_COMPLETION.md).

## CLI coordination

- Command auto/no/-T uses the existing no-TTY broker. Yes/-t uses TTY only when
  stdin is a terminal and StdinNull is false; otherwise it warns and uses the
  no-TTY broker. Force/-tt uses TTY even with a pipe or -n. Existing first-value
  configuration and short-option precedence still apply. Legacy command TTY
  requests remain rejected.
- Local TERM is passed as a quoted, bounded value in the in-memory adapter, not
  a new Connect environment request. Controls/invalid UTF-8 and values over 256
  bytes fail before authentication. Empty TERM remains empty.
- Local terminal input becomes raw after authentication. Redirected output on
  Unix uses the input terminal for size queries. Without terminal input, or
  with -n, no local raw mode is acquired and the initial size is 0x0. The adapter
  applies initial rows/columns with existing stty before TR/command startup.
  Subsequent coalesced sizes use the observed agent-originated resize channel,
  gated until TR so loading/restoration cannot overwrite an early resize.
- Input waits for restored remote modes; exhausted input/-n stops sending and
  does not synthesize Ctrl-D. The remote canonical read can remain blocked.
  UTF-8/control input uses the existing text-input filter; arbitrary invalid
  UTF-8 input remains unsupported in this TTY path. The binary no-TTY path is
  unchanged. TTY stdout/stderr are merged and terminal output transformations
  apply.
- Ctrl-C/Ctrl-D bytes reach the remote terminal. External local termination,
  auth loss, transport/resize failure and local disconnect cannot invent remote
  success or confirmed cleanup. External cancellation returns 255. Intentional
  ~. disconnect returns local 255 with an explicit missing-cleanup diagnostic,
  as ssh does (no remote status exists); since 2026-10-28 also in --legacy-shell.
  No C record or guessed signal request is sent to a running TTY program.
- The coordinator restores local state on completion/error and preserves
  ordinary exits 130/143 separately from remote signal termination (255).
  Restoration failure cannot be hidden by an otherwise successful status.

`TestTTYCommandSessionOverWebRTC` joins actual local Pion peers to the real staged
adapter in a local shell PTY and drives another local input PTY plus output pipe.
Seventeen cases cover TERM/initial-size/resize/Unicode, forced pipe input, -n pending
read/cancel, ~., authorization loss, transport loss, ordinary exit 130, changed
portable modes, output backpressure, idle-input reuse after exit23 and local
help separated from remote stdout, default/caught Ctrl-Z and two nonempty
canonical Ctrl-D forms, early completion with input still sending, and
peer loss before a valid result. See ESCAPES.md. Exact
local input-state restoration is checked in every case. These are local
coordinator tests, not a Connect HTTP/auth/agent emulator or new Pi evidence.

macOS/Linux command TTY now forwards the documented portable modes captured before
local raw mode, including canonical/echo flags and control characters; see
[TTY_MODES.md](TTY_MODES.md) for the complete inventory, source/reference tests and
remaining cross-platform/error-parity boundaries. Unix file-backed stdin/output
now has cancellable, joined ownership; see [LOCAL_IO.md](LOCAL_IO.md). Generic
callbacks and Windows blocked calls remain open. Nested stopped jobs, native
suspension/resumption, direct interactive status, real agent teardown and long
native sessions remain open. Unlisted kernel modes retain carrier values.
Windows redirected-console behavior and all physical iSH runtime are unverified.
No remote script/file, package or persistent configuration is created. A first
owner-operated native TTY check can use only builtins/read-only terminal queries:

```sh
./bin/pi-sh --direct-login --experimental-command -tt raspberrypi 'printf "tty-command-ok\\n"; test -t 0 && test -t 1 && test -t 2; stty size; exit 23'
```

## Canonical EOF with pending text

`TestStagedTTYNonemptyEOFOpenSSHDifferential` compares the same /bin/sh command
through OpenSSH_10.2p1 and the production staged loader using legacy and modern
carriers. A Ctrl-D after abc flushes that record without ending the stream; a
second line remains readable. With two Ctrl-D records, shell read returns1 and
retains abc. Both commands exit23. Two CLI/Pion/PTY cases cover the same behavior
through rune input and verify output, final status and exact local restoration.
No client-synthesized Ctrl-D is used: typed byte4 remains distinct from local
stdin EOF/-n. These are Mac fixture observations, not native Pi evidence.

## Production staging checks — before CLI integration, 2026-10-21

`internal/shellbroker/tty.go` and embedded `tty.bash` provide the staged
adapter. At this earlier checkpoint the CLI did not invoke it. There is no separate
`internal/command/tty.go` in this checkout; `command.ReadResult` supplies its
shared result parser. The staged implementation must be distinguished from the
independent test-only prototype described below.

`tty_staged_unix_test.go` now executes `NewTTY.Run` over a real local Bash PTY,
including the actual bootstrap, capability check, acknowledged source/command
upload, G commit, TR readiness and TX result parsing. Seven paired cases use
the installed OpenSSH_10.2p1 / LibreSSL 3.3.6 reference: descriptors/dev/tty,
merged CRLF output, canonical Unicode input, canonical Ctrl-D, raw byte 4,
initial 80x24 followed by 111x35 resize, and ordinary exit 130. The resize is a
local PTY ioctl, **not** the Connect resize coordinator.

Additional staged cases verify ordinary 0/130/143 versus self INT/TERM/KILL,
terminal Ctrl-C, a caught INT exiting 42, first-output-byte retention and
completion after a command leaves a background child. The latter checks carrier
completion only, not independent descendant cleanup. Immediate Unicode input
after TR, without waiting for command output, survives mode restoration. An
exactly 128 KiB command containing a literal newline and UTF-8 succeeds without
using a large exec argument or canonical terminal line.

With exhausted input, a remote canonical read remains pending until the ten-second
test deadline; it does not receive an invented Ctrl-D. Cancellation returns
DeadlineExceeded and ErrCleanupUnconfirmed, never a successful adapter status.
The current TTY protocol has no safe equivalent to the no-TTY broker's C/D abort
record once terminal input is active.

All of these checks are local. Production CLI selection, local raw-mode/escape
ownership, TERM and mode negotiation, Connect initial-size/readiness ordering,
resize, suspension/resumption, bounded input-reader lifecycle, authorization loss
and native agent teardown remain open. No new Pi session, upload or remote
software requirement follows from these tests. Command -t/-tt was still rejected
at that checkpoint; the CLI increment above supersedes that parser policy.

## Bash foreground-worker candidate

The later test-only `tty_jobs_unix_test.go` resolves the earlier numeric-status
ambiguity locally. Fifteen paired cases against OpenSSH pass, repeated five
times under the race detector: seven terminal/input/resize cases plus ordinary
0/130/143, self-INT/TERM/KILL, terminal Ctrl-C and an INT trap exiting 42.
The old POSIX negative tests below are retained; their candidate is still wrong.

The new supervisor starts Bash with job control enabled at startup (`-m`), before
redirecting its metadata stderr. It places an independent Bash worker in a
separate foreground process group with `fg`. The worker observes the actual
command as an asynchronous coprocess in that same foreground group, with all
three descriptors explicitly on /dev/tty. Its own job-table and signal notices
retain exit-vs-signal information; a separate anonymous pipe reports that result
to the supervisor. Metadata helpers ignore terminal INT/QUIT while the actual
command resets those dispositions. The parent cross-checks the worker's numeric
completion against the report, not against guessed signal ranges. Command text
stays in a variable rather than expanding into job-table descriptions.

Several tempting shortcuts failed in actual PTYs: enabling `set -m` only after
redirecting fd2 did not establish correct terminal ownership; a foreground job
was already removed before the parent's CHLD trap could classify it; a Bash
subshell worker lost normal-job records that an independent Bash process retained.
The report fd must be explicitly duplicated from an inherited standard descriptor
across exec. Internal metadata stderr must be detached before terminating its
reader to avoid killing the worker with SIGPIPE. These findings constrain the
eventual implementation; no upstream implementation source was incorporated.

Two cleanup fixtures deliver HUP and TERM directly to the local supervisor while
the command and a descendant ignore both. They verify the actual foreground group
with TIOCGPGRP and snapshot the owned process tree with local ps. After supervisor
termination, every captured owned process is gone or an OS-reap-pending zombie
**before the carrier is closed**. The exact terminating signal is also checked.
Both fixtures pass five repeated race runs. A trapped HUP was initially deferred
during fg and failed this check; default HUP/TERM plus EXIT cleanup passed. The
test driver has a separate owned-group safeguard for failed candidates, which
does not satisfy the pre-close oracle.

This proves a local composition, not native Connect command support. There is
no observed Connect request that delivers a chosen signal to the supervisor.
The agent's peer-close/PTY/process teardown must not be inferred from direct
local HUP delivery, and SIGKILL cannot run a shell EXIT trap. Production staging,
bounded metadata/framing, command readiness, input/escape and resize coordination,
local terminal restoration, cancellation/auth-loss reporting, stopped/background
jobs and native tests remain required before exposing command -t/-tt. No new
channel name or signal request is invented.

Bash >=4.3, cat and stty are existing broker facilities; no tool is installed on
the Pi. This candidate was executed with local Bash 5.3 on macOS. Local GNU Bash
5.3 source/manual files informed the job-control investigation; the test script
is independently authored. See TESTING.md for the previously verified official
GNU archive digest and DEPENDENCIES.md for development-tool attribution.

## Local reference and candidate

`internal/shellbroker/tty_reference_unix_test.go` measures a test-only POSIX
supervisor against OpenSSH_10.2p1 on macOS. The reference uses pinned ephemeral
host/user keys and an isolated loopback sshd; no SSH account configuration is
changed. Both client and server PTYs are real. Candidate code replaces only the
local test shell, disables its history persistence, catches supervisor INT/QUIT,
runs a child /bin/sh with the existing PTY, and emits a numeric result afterward.
Its control frames are distinct from echoed bootstrap text. No candidate script
is installed or run on the Pi, and no production CLI option is enabled.

Seven paired cases confirm, within those fixtures:

- Descriptors 0/1/2 are TTYs, /dev/tty is available and ordinary exit 23 survives.
- stdout/stderr are merged and normal terminal output maps LF to CRLF.
- Canonical Unicode input, including an apostrophe, reaches the child read.
- Byte 4 on an empty canonical read returns read status 1.
- In raw mode the same byte is data with value 4, not EOF.
- Initial 80x24 and subsequent 111x35 resize reach the command. The reference
  client receives SIGWINCH through its local PTY/session; the candidate's existing
  PTY is resized directly. This does not test a new Connect resize coordinator.
- An ordinary exit 130 is preserved as 130.

The fixture waits for a command marker before sending control/input bytes or
resizing; this avoids testing a signal delivered before the command starts.
Short-command exit/readiness races are handled by draining the actual PTY output.
The test driver restores resources and terminates only its owned local processes.
dd/od/tr are used only inside the local raw-byte fixture, not as client or new
remote requirements.

## Verified reason not to enable the simple supervisor

Two further paired cases compare self-SIGINT and terminal Ctrl-C during sleep.
The candidate survives long enough to report a numeric status, but reports 130
in both cases. OpenSSH reports 255 for each signal, while ordinary exit 130 stays
130. Numeric `$?` therefore loses required exit-vs-signal information. Tests
explicitly assert this difference: their passing result is evidence of an
inadequate candidate, not of TTY compatibility.

The no-TTY Bash broker already obtains separate job-table signal information,
but it launches a child with no controlling terminal. Reusing only its numeric
wait result or only the legacy marker parser cannot solve both requirements.
The new Bash candidate above demonstrates foreground TTY ownership and distinct
results locally. Its integration, actual cancellation/auth-loss path, stopped jobs
and native supervisor loss still need evidence. This remains local implementation
and protocol-verification work, not proof of an immutable agent limitation.

## Local stdin EOF is a different event

An additional real OpenSSH `-tt` case uses an exhausted local input reader and a
remote canonical read. After the command readiness marker, that read remains
blocked for the measured 200 ms; no read-finished marker appears. Interrupting
the local client then returns 255. No Ctrl-D was transmitted. This is bounded
observational evidence, not a claim about every SSH version or unlimited lifetime.
It rules out blindly converting local input EOF into byte 4 as a presumed SSH
TTY equivalent. Raw-mode byte 4 is demonstrably data in the paired fixture.

## Evidence limits

The nine paired cases passed five repeated race runs; the stdin-EOF reference
passed three repeated race runs. These are local macOS observations, not native
Pi, Linux/Windows/iSH runtime, Connect signaling/transport, quota, long-session,
terminal suspension, nested job-control or arbitrary terminal-program proof.
No package, daemon, configuration, persistent remote file or additional remote
software requirement was introduced. No credentials or real account output are
retained. The supervisor/tests are independently authored; existing development
OpenSSH and creack/pty licence attribution remains applicable.

## Login shell — 2026-10-23

`pi-sh [options] device` without a command now runs the account's login shell
through the same in-memory adapters as commands, as sshd does: with a TTY
(default on a terminal, `-t`, `-tt`) through the staged TTY adapter, and without
one (`-T`, `-n`, piped stdin) through the no-TTY broker. The command text is
`shellbroker.LoginShell`: change to `$HOME` (OpenSSH's "Could not chdir to home
directory" notice otherwise), then `exec -a "-${SHELL##*/}" "$SHELL"`. Nothing is
written remotely; the process is replaced. TERM, initial size, resize, escapes,
`~^Z` and the real exit status therefore behave as for `-tt` commands. The
previous raw agent shell stream remains available as `--legacy-shell`
(refused with a command, `-T` or `-n`).

A job-control login shell leaves the controller's process group, so a carrier
hangup that killed only that group left the shell's jobs running. The controller
now sends HUP and CONT to its direct command before killing its own group, as
sshd's session leader receives HUP; Bash forwards HUP to its jobs
(`TestStagedTTYLoginShellCarrierHangup`: shell, foreground and background job
all end). The client still reports cleanup as unconfirmed in that case.

Local evidence: `TestStagedTTYLoginShell`, `TestStagedTTYLoginShellEOFLogout`,
`TestBrokerLoginShellWithoutTTY`, `TestLoginShellMissingHome`,
`TestLoginShellOpenSSHDifferential` (argv0, cwd and TTY presence against the local
OpenSSH 10.2p1 sshd) and `TestLoginShellSessionOverWebRTC` (CLI through Pion).
Differences: the inner exec requires the Bash the adapters already need;
sshd's environment (SSH_* variables, MOTD, lastlog, `~/.ssh/rc`) is not
reproduced; the login shell reads the account's startup files exactly as any
login would. Native evidence: see REAL_DEVICE_TESTS.md;
`scripts/native-shell-check.sh` is the read-only plan.
