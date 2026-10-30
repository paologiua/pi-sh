# TTY job control — 2026-10-21

The production staged adapter now uses one controller in the dedicated carrier
process group. The command and anonymous metadata helper stay in that group,
with monitor mode disabled before their launch. This fixes both the old missing
caught-TSTP behavior and the Ctrl-Z status loss found with a modern carrier.
The controller has basic native command TTY evidence in REAL_DEVICE_TESTS.md.
The stop/fg/TTIN/TTOU and group-cleanup cases below remain local runtime evidence.

## Why the previous design failed

A minimal macOS reproduction reduced the ignored-TSTP case to an interactive
/bin/sh execing a noninteractive shell, without the adapter. Apple's Bash 3.2
exec path lacks the job-control signal reset found in the current Bash source.
Reset/no-op traps at the bootstrap and worker boundaries did not repair that
inheritance. The old /bin/sh carrier therefore masked a second defect: with a
modern carrier, terminal Ctrl-Z could stop the separate foreground supervisor,
which then lost its verified command result. Caught explicit TSTP alone worked
with that modern carrier, demonstrating why the two cases must be separated.

The adapter now enters its already-required Bash as --noprofile --norc -im.
Interactive initialization normalizes the job-control dispositions before child
creation; startup files are disabled. HISTFILE=/dev/null isolates controller
history at startup, history/expansion are disabled, and HISTFILE is not exported
to the user command. This changes only this ephemeral process, not user files or
system configuration. Monitor mode is disabled before starting the command:
introducing a separate foreground worker would change orphan-group stop behavior.
The controller handles INT/QUIT while the child receives its ordinary signal
semantics. Existing jobs metadata still distinguishes exit130/143 from signals;
TR readiness, terminal modes and TX result framing remain validated.

## Actual staged/reference evidence

`TestStagedTTYStopOpenSSHDifferential` now asserts parity, replacing the earlier
two negative characterizations. The installed OpenSSH_10.2p1 / LibreSSL 3.3.6
reference explicitly execs the same /bin/sh interpreter as the test adapter.

| Case | Reference and current staged adapter |
| --- | --- |
| A shell stops its own PID with SIGSTOP; a child sends CONT | Prints resumed and exits 23. |
| Terminal Ctrl-Z, then x/Enter to an ordinary noninteractive read | Read sees x and exits 23. |
| Caught TSTP sent explicitly to the command shell | Handler runs; status 23. |
| Caught TSTP from terminal Ctrl-Z, then x/Enter | Handler runs; read sees x; status 23. |

`TestStagedTTYControllerAcrossCarriers` covers 18 terminal/status/signal/history cases
through the actual loader/controller with both the legacy Mac /bin/sh and modern
Bash 5.3 carriers (36 executions). It includes the prior TTY/Unicode/EOF/resize
cases and ordinary130/143 versus INT/TERM/KILL, Ctrl-C and a caught interrupt.
The real Pion/PTY CLI coordinator now has 15 cases, including default and caught
Ctrl-Z and two nonempty canonical EOF forms with remote read/status23 and
exact local state restoration. Focused
controller, cleanup, reference and CLI cases pass three repeated race runs.
The existing portable-mode and all-byte stty-helper tests also pass on this source.
The full final race suite, vet and seven-target CGO-disabled build/test compilation
also pass; native clients are rebuilt. Module verification and Node regression pass.

## Nested interactive jobs

`TestStagedTTYNestedJobControlOpenSSHDifferential` runs the same explicitly
interactive Bash 5.3 command through OpenSSH_10.2p1 and the actual staged loader
with both carrier types. Three cases now have local Mac evidence:

- A foreground external job receives terminal Ctrl-Z, is stopped with the TSTP
  status, resumes through fg and completes normally; the shell returns23.
- A background read stops with TTIN, resumes through fg and receives the exact
  marker with echo disabled. Its shell loop retries an interrupted read, so this
  is evidence of actual resumed input rather than terminal echo.
- With tostop enabled, a background write stops with TTOU. After the shell
  disables tostop and foregrounds the job, output and the reported utility status
  match the reference; the shell returns23. On this Mac the interrupted printf
  reports1 despite printing its output. The test does not turn that into success.

All three comparisons pass three repeated race runs, with six staged executions
per run. They establish only these explicit job-control paths, not arbitrary
nested applications, surviving background jobs, detached groups or native agent
teardown. The fixture disables history/startup files and kills only its own
remaining jobs on shell exit. No adapter code or remote requirements changed.

An earlier probe sent TERM to an already stopped job immediately before fg.
The nested shell could then report the old stopped status and refuse exit23 with
"There are stopped jobs", returning1. The same script reproduced both1 and23
when Bash 5.3 ran directly on a local PTY without this adapter or SSH. OpenSSH's
extra process/transport timing often let termination be reaped before exit.
This is a timing-sensitive shell sequence, not evidence that the protocol parser
converted a completed exit23 into1. It remains a behavioral caveat, not a claimed
client fix. The normal fg test deliberately resumes a live job without first
sending a competing TERM/CONT sequence. Temporary diagnostics and the kernel
wait-macro probe were removed; no local or remote shell binary was patched.

## Group lifetime and scope

On an abnormal controller exit or HUP/TERM, the controller kills its own fresh
carrier group. Its owner is its own PID, never a supplied process selector;
PID1 is refused before command launch to avoid kill's special -1 meaning. A
self-PID fallback exits the controller if group setup failed. This assumes the
fresh dedicated Connect PTY/carrier ownership, not an arbitrary shared terminal.
Local process snapshots verify the controller is foreground group leader and
all sampled command/helpers belong to that private group.

`TestStagedTTYControllerCleanupBeforeCarrierClose` tests HUP and TERM from both
carrier types. The command and a descendant ignore ordinary HUP/TERM. Before
closing the PTY, process snapshots confirm no sampled owned process is running;
a separate process outside the group remains alive. The fixture synchronizes
ownership validation before signaling, so an early driver error cannot become
a successful cleanup claim. These are direct local signal tests, not evidence
of what the Connect agent does when a peer or authorization disappears.

`TestStagedTTYControllerWholeGroupStopResume` sends SIGSTOP to the whole private
group, observes every sampled member stopped, then sends CONT and input. Both
carriers retain the input and verified status 23; three repeated race runs pass.
This external local driver tests remote job control. The separate local ~^Z
escape now has Mac process/TTY coordinator evidence described in ESCAPES.md.

TTY protocol failures still return ErrCleanupUnconfirmed. No C/D abort record
can safely be injected into terminal data, no command is replayed, and stopping
local I/O does not prove remote cleanup. Native ownership/hangup/revocation,
detached descendants and arbitrary nested interactive/TTIN/TTOU programs remain
open beyond the three explicit local cases above. Local suspend/reacquire is implemented with Mac evidence only. Linux/Windows/physical iSH runtime remains
unverified; cross-compilation is separate evidence. No Pi process, mode, file,
package or service was changed, and no new runtime dependency was introduced.

References consulted, without copied implementation:
[Apple Bash 3.2 exec.def](https://github.com/apple-oss-distributions/bash/blob/main/bash-3.2/builtins/exec.def),
[Apple Bash jobs.c](https://github.com/apple-oss-distributions/bash/blob/main/bash-3.2/jobs.c),
[GNU Bash exec.def](https://git.savannah.gnu.org/cgit/bash.git/tree/builtins/exec.def?h=bash-5.3),
[GNU Bash jobs.c](https://git.savannah.gnu.org/cgit/bash.git/tree/jobs.c?h=bash-5.3),
[Apple XNU signal handling](https://github.com/apple-oss-distributions/xnu/blob/main/bsd/kern/kern_sig.c).
The earlier temporary C probes, shell probes and nonassertive boundary test were
removed; current tests invoke the production loader and embedded source.

## Perl supervisor — 2026-10-29

**What changes.** tty.bash still enters through `bash --noprofile --norc -im`
and still sets the terminal modes with `stty`: the saved state, the requested
modes and the size. It then runs `perl -c` on `tty.pl` and, on success, `exec`s
it. The program arrives as the single-quoted Bash word `_pt_perl_src`, so it
holds no apostrophe (`TestTTYPerlHasNoApostrophe`).

After `exec`, the supervisor keeps the PID and process group that the
interactive Bash created: the carrier group, in the foreground. The Bash
supervisor (jobs metadata pipe, idle pipe, CHLD trap, `read -t` polling)
stays as the fallback. `PI_SH_REMOTE_IMPL=bash` keeps it.

`tty.pl`:
- prints `TR`, forks, and runs `$SHELL -c command` on /dev/tty in the same
  group, as the Bash supervisor did;
- in the child, restores the dispositions with which the program started
  for HUP, TERM, INT, QUIT, PIPE and CHLD (Bash's `exec` restored those of its
  entry), and sets TSTP, TTIN and TTOU to default, as the interactive Bash did
  for its commands even when they were ignored on entry;
- in itself, handles INT and QUIT with an empty handler (so `exec` resets
  them for the command) and ignores TSTP, TTIN and TTOU, so that Ctrl-Z stops
  the command and never the supervisor;
- waits with a blocking `waitpid`, which does not report stops. A signal
  gives 255 and an exit gives its code, as the `jobs -l` classification did;
- on HUP or TERM, sends HUP and CONT to the command, then KILL to the whole
  group, itself included, as `_pt_abort` does;
- at the end runs `stty raw -echo`, prints `TX:code`, and exits with it.

**Equivalence.** The condition was that every TTY differential test with
OpenSSH gives identical results with both supervisors.
- `TestTTYSupervisorsGiveCommandSameContext` compares, on the legacy macOS
  `/bin/sh` carrier and on Bash 5.3:
  - the command's environment;
  - its ignored signals, as Perl sees them;
  - its group: the supervisor's, and the terminal's foreground group;
  - the terminal modes (`stty -g`).

  They are identical.
- `TestTTYSupervisorImplementation`: the supervisor is `perl` on both
  carriers; it is `bash` with `PI_SH_REMOTE_IMPL=bash`, or with a perl that
  cannot compile it.
- `TestStagedTTYControllerWholeGroupStopResume` counted the Bash
  supervisor's two helper processes. It now runs with both supervisors and
  expects the supervisor and the command for Perl, plus the helpers for Bash.
- The whole TTY suite was repeated with each supervisor and each Bash: the
  internal/shellbroker tests matching `TTY|Login|OpenSSHEscape` and the
  cmd/pi-sh tests matching `TTY|Login|Suspend|TerminationSignals`. Those
  include every OpenSSH differential (stop, nested job control, modes, EOF,
  escapes, login shell, hangup) and `ctrl-z-handler`. The results are in the
  table below.

| Run | Perl, Bash 5.2 | Bash, Bash 5.2 | Perl, Bash 5.3 | Bash, Bash 5.3 |
| --- | --- | --- | --- | --- |
| 10 repetitions | 2150 passed | 2150 passed | 2150 passed | 2150 passed |
| 10 more | 2148 passed, 2 failed (one case, see below) | 2150 | 2150 | 2150 |
| 3 repetitions, one `yes` per core | 645 | 645 | 645 | 639, 6 failed (one episode, see below) |

**Failures found, both outside the supervisor.**
- *Bash supervisor, Bash 5.3, under load.* In one iteration five consecutive
  `TestTTYCommandSessionOverWebRTC` cases timed out waiting for readiness
  after 15 s, with no output. The Perl supervisor shares the loader and had no
  failure under the same load. Six more loaded repetitions of that test, on
  this branch and on `7500852`, with both Bash versions, all passed. It is
  recorded as an isolated, unreproduced episode of the fallback.
- *`TestLoginShellSessionOverWebRTC/exit-status`, Perl supervisor, Bash 5.2,
  once.* The expected `35 111` was in the final output: the session ended
  while its last output was still on the way to the test's observer, and
  `waitFor` failed on the ended session without looking again. `waitFor` now
  keeps the result and waits up to one second for the text. After the fix
  the test passed 100 of 100 runs.

**On the device** (read-only, REAL_DEVICE_TESTS.md "Perl TTY supervisor"): a
`-tt` command ran under the `perl` supervisor. It was in the carrier group,
in the foreground, on a TTY, with `TERM` set, and exited 23. Later the same day a real Mac PTY exercised the interactive login shell,
24x80 to 35x111 resize, exit 23, Ctrl-C (255), Ctrl-Z without and with a
handler (23), and `~.` (255). A separate process listing confirmed no
supervisor or command left after the tests (REAL_DEVICE_TESTS.md "Native Perl
TTY controls"). The `~.` diagnostic still reports cleanup as unconfirmed
in-band; the independent listing is evidence only for these runs. Network
loss, revocation and arbitrary detached jobs remain unverified.

**Decision.** The Perl supervisor is kept. Every local differential is
identical with both supervisors, and tty.bash stays as the fallback.

**Further load investigation (2026-10-29).** With the Bash supervisor,
40 full CLI TTY repetitions passed 680/680 cases on Bash 5.2.37; Bash 5.3
passed 679/680, with one `ctrl-z-handler` session not finishing after both
`caught-suspend` and `seen:x`. This is not the historical readiness episode.
Then 200 isolated cases and 40 full repetitions (680 cases) passed under the
same load with a 10 s timer for goroutines, carrier bytes and process state;
no timer fired. Neither cause nor fix is claimed. The completion failure
remains open (TESTING.md "Further serial load investigation").

**Context-fixture reporting (2026-10-29).** A later verification found one
extra CR in one context report, with identical values/modes. A standalone
Mac PTY reproduced it with OPOST/ONLCR under backpressure (3/200, versus
0/200 with OPOST off), without any adapter. The fixture saves the original
modes, disables output processing for reporting and compares the exact
reports including those original modes. No normalization or product change;
100 race repetitions with each Bash passed. Context failure diagnostics
report sizes instead of dumping inherited environment values. This is
separate from the still-unclassified Ctrl-Z completion failure above;
TESTING.md records the failed first gate and the reproduction.


## Matched main comparison — 2026-10-29/30

The Bash supervisor's post-handler completion failure was not reproduced on
`cec4417` or `main` (`7500852`) with identical corrected CLI fixtures, both Bash
versions and eight serially managed load processes. Each state/version passed
100 isolated handlers and every handler in 20 complete 17-case sequences;
50 direct-PTY reductions per combination also passed. Main had one distinct,
fast `eof-record` staging/protocol failure before supervisor start (339/340 with
5.2.37); it is not the historical readiness timeout or missing completion.

Temporary hooks distinguish impending command exit from absence/reaping,
classification, helper joins, TX, parser EOF and local-I/O/terminal shutdown.
A deliberately held supervisor after completed waits produced the original
`session did not finish` assertion with no TX and numeric process/goroutine
snapshots at 3/8/12 s. That is an artificial diagnostic control, not a causal
reproduction. Uninstrumented full-sequence controls passed on both states/Bashes.
No product or timeout changed. Neither a branch regression nor this same failure
on main is established; the historical completion and readiness episodes remain
open. TESTING.md "Matched local comparison" gives adaptations, results, artifact
locations and the merge recommendation; no Pi session was opened.

The owner closed this investigation on 2026-10-30 with the original episodes
still unclassified. A later full checklist failed on separate local WebRTC
connection/deadline errors; one case precedes any shell PTY. A one-Mac test setup
can affect timing and local networking, but no causal attribution is established.
The shell-free peer reduction was interrupted without logs or results. No
product fix or commit follows; TESTING.md "Investigation closure and failed
final gate" records the failed gate and the uncommitted documentation state.
