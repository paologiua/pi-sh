# Shell-only command broker — 2026-10-20

`--experimental-command` now selects `internal/shellbroker`. It uses existing
Bash >=4.3, `cat`, `setsid --wait` and `stty` on the Raspberry Pi. Python is not
used. Missing capabilities fail explicitly; nothing is installed automatically.
Bash and these utilities are not guaranteed on every Linux distribution.

```sh
./bin/pi-sh --browser --experimental-command raspberrypi 'cat; printf "error\n" >&2; exit 23'
```

Options precede the device. Command arguments are joined with spaces for remote
shell evaluation. Complete the usual browser companion step. The CLI currently
prints authentication instructions and an experimental-mode notice to its own
stderr; these are separate from the remote stderr bytes and differ from quiet
OpenSSH execution.

## Design and remote footprint

The existing Connect shell/PTY remains the carrier. A short bootstrap disables
history persistence for this session, makes its PTY raw with echo disabled, and
executes Bash without profile/rc files. The loader receives bounded hexadecimal
chunks of independently authored broker code and command text into memory. Only
trusted broker source is evaluated by the loader. Command text is passed as an
argument to `${SHELL:-/bin/sh} -c` in a separate session created by `setsid`.

Anonymous kernel pipes created with Bash coprocesses carry child stdin, stdout,
stderr and supervisor metadata. No named pipes, here-documents, here-strings,
process substitutions, remote script files or persistent writes are used. The
command has pipe descriptors and no controlling terminal. A command can still
write files itself; project tests use read-only commands and memory-only streams.

The parent alone writes carrier records. Each session uses a random 128-bit
marker. Hex encoding preserves every byte including NUL and invalid UTF-8 in
stdio; command text itself must be UTF-8, contain no NUL and be at most 128 KiB.
Source upload is limited to 32 KiB, startup discard to 64 KiB and individual
records to 2304 ASCII bytes. These are local policy bounds, not Connect limits.

Stdin has one acknowledged block of at most 1024 bytes outstanding. Each output
has a window of four blocks of at most 128 bytes; Go acknowledges a block after
the corresponding local writer consumes it. Independent input/output workers
allow output to drain while a child stops reading input. EOF closes only child
stdin; both output streams must finish and be acknowledged before completion.
The application protocol is pi-sh's own framing inside the evidenced shell
channel; it is not an observed native agent exec/status protocol.

Numeric Bash `wait` alone confuses normal exit 143 and SIGTERM. The supervisor
also observes its own Bash job metadata (including Bash's SIGKILL job notice)
through a private anonymous pipe. Known normal exits return 0–255; a known
signal termination returns local 255, matching the measured OpenSSH behavior.
Missing or inconsistent completion, auth loss or transport failure returns 255
with a local diagnostic. It never infers success from a connection close.
Workers are killed and reaped on normal completion; abnormal cleanup also targets
the owned child process group. Arbitrarily daemonized descendants are outside
that guarantee.

Failures after launch commit now request abnormal cleanup and wait up to two
seconds for the broker's acknowledgement before carrier closure. CLI coordination
has a separate three-second bound; missing confirmation is reported explicitly.
The local-signal behavior and exact scope/tests are documented in
[COMMAND_CANCELLATION.md](COMMAND_CANCELLATION.md). These cancellation changes
have local evidence plus the owner-run direct -nT Ctrl-C/status255 check; independent remote process inspection remains pending.

## Evidence and remaining limits

Actual local PTYs cover all 256 byte values, independent output streams, input
EOF, early stdin close, absent controlling terminal, ordinary exit 143 versus
signals, blocked input, simultaneous streams and a slow output consumer. A local
loopback OpenSSH_10.2p1 server with pinned ephemeral host keys supplies 14
differential fixtures. This compares the broker's streams/status, not the CLI's
authentication instructions or every SSH behavior. On macOS these tests use a
development-only modern Bash and a test-only setsid helper; neither is required
on the client at runtime or copied to the Pi.

Native Connect checks verified exact 2048-byte binary stdin/stdout, 256-byte
independent stderr, no controlling terminal, exit 23 and ordinary exit 143.
A larger native fixture initially failed after partial stderr. A local regression
then demonstrated that the old transport's 32-message queue could overflow on
only 2 KiB of tiny fragments; the fix bounds both 4096 messages and 256 KiB of
queued payload. After normal browser reconnection/reauthentication, the actual
native rerun passed: 65536-byte stdin/stdout and 131072-byte concurrent stderr
were exact, followed by a self-SIGKILL and status 255 with no failure diagnostic.
This supports the fix on the tested route/workload; it is not a bound on every
Connect traffic pattern. See REAL_DEVICE_TESTS.md for the exact evidence.

This is a useful shell-only implementation, not universal OpenSSH parity:

- Arbitrary remote signal requests and nested command PTY execution remain
  unimplemented. Local client termination signals cancel the session, consistent
  with the selected OpenSSH reference; they do not imply generic forwarding.
  Interactive mode remains separate with no verified native remote exit status.
- User shell selection, startup environment, PAM/login policy and job semantics
  need more differential coverage. Existing user environment is trusted; this
  does not sandbox commands or authenticate a hostile process on the same account.
- Blocking stdin readers/output writers remain caller-owned and must be
  cancellable in an embedding application. CLI cleanup coordination is bounded.
- Direct in-memory login and a documented config subset are implemented;
  forwarding, SCP and SFTP remain unfinished. Small hex frames cost throughput.
- Linux/Windows and Linux/386 compilation is separate from native runtime
  validation. Physical iSH testing remains pending.

The previous POSIX shell adapter remains explicitly available with
`--legacy-command`; its merged output and null stdin limits are documented in
COMMAND_EXECUTION.md. There is no automatic downgrade that loses stream semantics.

## Intermittent binary-input stall: Bash 5.2 timed-read cause — 2026-10-22

**Cause (local evidence).** The Pi reports Bash 5.2.37. Its `read` builtin checks
the `-t` deadline once more after the loop has already consumed the delimiter
(`builtins/read.def`, unconditional `check_read_timeout()` after the character
loop). When the deadline passes during that window, `read` returns 142 with the
whole line in the variable but the newline gone. Bash 5.3 changelog entry
"don't check the timeout after breaking out of the character read loop"
(CWRU/changelog, Thomas Oettli report) removes that check.

The old parent loop read carrier records with `read -t 0.001` and treated status
>128 as a partial line kept in `fragment`. A complete `I` record whose newline was
lost therefore stayed in `fragment`, the broker waited for its end, and the
client waited for `A` under stop-and-wait input: a silent deadlock until the
local timeout, with no protocol error. On the Pi each ~2.1 KB record is read
byte by byte from a non-seekable TTY on a slow CPU, so the 1ms deadline often
expires inside a record; locally a whole record usually arrives in one read.
The same pattern affected the stdin-ack, metadata and output-byte timed reads,
and the TTY adapter's metadata loop.

Evidence, all local:

| Check | Bash 5.2.37 (official 5.2 + patches 1–37, built in scratch) | Bash 5.3 |
| --- | --- | --- |
| Bash-only reader mirroring the old loop, 20000 lines of 2100 bytes | 4 records concatenated (lost newlines) | 0 |
| `TestShellBrokerMegabyteStdinCount/late-newlines`, old broker | 2/2 stalled to the 120s limit, cleanup unconfirmed | 3/3 pass |
| Same test after the fix | 3/3 pass | 3/3 pass |

`late-newlines` uses a test-only carrier that sends each record's newline
0.5–1.5 ms after its body, reproducing a record that spans the deadline. It is
the same command as the native fixture: 1MiB of every byte value to `wc -c`.

**Fix.** No timed `read` consumes protocol data any more. Data descriptors are
polled with `read -t 0` (which never consumes) and then read without a timeout.
Records and pipe lines are written whole, so a started line completes promptly.
Loop pacing uses timed reads on a dedicated pipe that nothing writes (an idle
`cat` coprocess, killed and reaped with the other helpers). Bash buffers reads
from a seekable-looking TTY (observed on macOS), which would hide queued records
from `read -t 0`; the broker therefore relays its carrier through a `cat`
coprocess pipe, which Bash always reads unbuffered. Both helpers are existing
`cat` processes in memory; nothing is written or installed on the Pi.

In the TTY adapter, the idle `cat` ignores INT/QUIT/TSTP/TTIN/TTOU. Without the
job-control signals ignored, Bash 5.2 failed `caught-suspend` with `/bin/sh` as
carrier (the command's `read` after ^Z returned empty); probably the interactive
supervisor reacting to a stopped child, not proven. With them ignored it passes.

Native: the owner ran the read-only 1MiB fixture three times after the fix; all
counted 1048576 bytes and exited 23 (REAL_DEVICE_TESTS.md). The full approved transfer plan,
including the previously stalled 1MiB SFTP upload, then passed once with verified
cleanup. Single runs are not stress bounds. Local passes do not establish that every earlier native failure
had this cause; the original stalled run left no record-level trace.
