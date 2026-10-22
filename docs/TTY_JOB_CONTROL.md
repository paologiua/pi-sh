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
