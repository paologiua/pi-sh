# Terminal escapes — 2026-10-21, updated 2026-10-26

The text-input filter applies to interactive shells and experimental TTY commands,
not binary no-TTY stdin. EscapeChar/-e selects ASCII/control notation or none.
Recognition starts at initial input and after CR/LF; it does not track the remote
prompt, line editor or shell state. Bracketed paste does not disable recognition
after embedded newlines. Use -e none for literal multiline data containing escapes.

| Sequence with default prefix | Implementation/evidence | Remaining difference or work |
| --- | --- | --- |
| ~. | Local disconnect; existing native Enter/~. check and local Pion/PTY tests | Exits 255 like ssh(1), which has no remote status after a local disconnect (before 2026-10-23 pi-sh exited 0). TTY path still reports unconfirmed cleanup. |
| ~~ | One literal prefix, with default and custom-prefix short-read tests | Special action precedence still applies if the configured prefix itself is an action character, as in upstream. |
| ~? | Local stderr help in OpenSSH's layout; no remote bytes; leaves session open and line-start intact | Lists ~B/~C/~R/~& on one "refused" line instead of OpenSSH's descriptions; `~^Z` only where suspension exists; control prefixes render as caret notation. |
| ~^Z | Implemented on macOS/Linux: drain local writes, hand back flags/modes, stop this client process, reacquire on CONT and force resize. An external SIGTSTP now takes the same handoff | Real Mac subprocess + Pion/PTY TTY session test, terminal process test, external-dispatch tests (`TestExternalSuspendDispatch*`) and cancellation/Close tests; Windows unsupported; native Pi/Linux/iSH unverified. This is not remote Ctrl-Z. |
| ~# | Lists the single session channel (`#0 client-session`) | Accurate while pi-sh has no forwarding; must list forwarded channels once forwarding exists. |
| ~& | Consumed; refused with a notice | OpenSSH backgrounds only while waiting for forwarded connections to close. pi-sh has no forwarding and the Go client cannot fork; revisit with forwarding. |
| ~C | Consumed; prints `commandline disabled` | Matches OpenSSH's default EnableEscapeCommandline=no. Enabling the command line (EnableEscapeCommandline yes) is not implemented; its -L/-R/-D/-KL operations need forwarding. |
| ~B | Consumed; refused with a notice | OpenSSH sends a channel BREAK request. No equivalent agent capability has been observed; sending byte 0 or a guessed message is not evidence. |
| ~R | Consumed; refused with a notice | SSH rekey has no implemented Connect equivalent; DTLS renewal/reconnect needs separate investigation without command replay. |
| ~v / ~V | Raise/lower the run-time LogLevel (QUIET..DEBUG3) with OpenSSH's `[LogLevel X]` notice; with file or syslog logging prints `[Logging to syslog or file]` and changes nothing, as OpenSSH does | `TestLocalEscapeActionsAreConsumed`, `TestVerbosityEscapeRespectsLogDestination`. Redaction rules apply at every level (DIAGNOSTICS.md). |

Since 2026-10-23 every OpenSSH escape character is consumed locally, as in
OpenSSH: ~B/~R/~& print a refusal and ~C the disabled-commandline notice, so none
of them reaches the remote side (midline sequences still do, as upstream).
Other characters after the prefix keep the unknown-escape fallback and are
forwarded with the prefix, as in OpenSSH. Literal remote Ctrl-Z without a
prefix still reaches the remote terminal; simple default/caught Ctrl-Z and
whole-group STOP/CONT now have local evidence in TTY_JOB_CONTROL.md. Nested
job control and native behavior remain open.

Help diagnostics use the same cancellable borrowed-file group as standard streams.
A failed or short help write ends the local input path with an error instead of
sending the remainder as a remote command. An auth/cancel failure can interrupt a
blocked help write on macOS/Linux. Windows synchronous-call limits remain in
[LOCAL_IO.md](LOCAL_IO.md). With no diagnostic writer the library caller discards
the help; the CLI supplies its real stderr, independently of remote stdout.

## Local evidence

The installed OpenSSH_10.2p1 / LibreSSL 3.3.6 reference was run against the existing
local ephemeral sshd fixture in seven cases: help, repeated help, alternate and
control prefixes, disabled escapes, quoted prefix and midline text. The remote
read receives exactly the remaining text and exits23. The production filter's
one-byte-reader cases check matching consumption/boundaries and help followed
immediately by disconnect, plus failed/short diagnostic writes.

The eleventh actual TTY Pion/PTY coordinator case sends ~?value and verifies local
stderr help, remote seen:value/status23 and no help in stdout. A seventh interactive
Pion/PTY case uses aliased stdout/stderr on the local TTY, sends ~?~. and checks
help, disconnect and exact restoration. These are local runtime tests, not new
Pi evidence. Existing earlier native ~./-e none observations remain unchanged.

Behavior references: [OpenSSH 10.2p1 clientloop.c](https://github.com/openssh/openssh-portable/blob/V_10_2_P1/clientloop.c)
and [ssh manual](https://github.com/openssh/openssh-portable/blob/V_10_2_P1/ssh.1).
Implementation is independently authored; no source or new dependency is copied.

## Local suspension

The CLI enables ~^Z for macOS/Linux grouped file streams, including the interactive
and experimental TTY paths. It prints a local stderr notice, waits for active
output writes, restores borrowed descriptor flags and terminal modes, then sends
SIGTSTP to its own PID only. It never signals the process group. On CONT it captures
the shell's current modes/flags as the new restoration baseline, reacquires raw
mode/nonblocking I/O and publishes a fresh size even when unchanged. Buffered
input after the escape remains available to the input filter. Already sent bytes
cannot be recalled. A custom prefix and -e none retain their usual rules.

Cancellation/Close during the handoff joins the transition and prevents raw-mode
reacquisition. Draining backpressured output can wait for its consumer, as ordinary
output does; context cancellation wakes pollable writes. Only the input boundary
may initiate the handoff: no other read may be outstanding. Files remain borrowed
and must stay open. Windows omits the help action and consumes/diagnoses ~^Z.
Since 2026-10-23 a caught external SIGTSTP (for example `kill -TSTP` from
another terminal) interrupts the pending stdin read without consuming input
(`localio` InterruptRead) and runs the same handoff. Because the Go runtime keeps
its handler once SIGTSTP is caught, a permanent dispatcher emulates the default
stop, with an orphaned-group check, when no session is active. Ignored
signals and orphaned process-group behavior follow OS job-control semantics;
background terminal access/TTIN/TTOU and physical iSH remain unverified.

`TestSuspendTTYSessionOverWebRTC` starts only a child test process in a separate
nonorphan group. The parent observes its real stopped state, exact cooked modes
and restored flags, changes modes/flags/size, resumes it, then verifies resized
remote fixture output, status23 and the updated final baseline. Three race runs
pass on macOS. `terminal.TestNativeSuspendRestoresAndReacquiresAcrossCONT` checks
the actual OS stop independently. Filter tests cover custom/disabled/quoted and
midline escapes, buffered remainder, help availability and diagnostic/action
failure. Terminal tests cover cancellation, failed stop, forced size refresh and
Close joining a handoff; localio tests cover fresh baselines and cancellation of
a blocked drain. These tests use local processes only, not a Pi/Connect session.

The same child-process test now runs a second mode through runInteractiveSession,
with aliased stdin/stdout/stderr on one real local PTY. It verifies the stopped
state and changed baseline, resumed resize and remote fixture input/output, then
intentional disconnect. Both modes pass three repeated race runs. The parent
collector uses cancellable localio reads: closing a native blocking PTY alone
did not interrupt the original fixture's read. Only O_NONBLOCK is owned/restored;
Darwin's write-bookkeeping flags are allowed to change after actual output.
