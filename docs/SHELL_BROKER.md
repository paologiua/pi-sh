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
records to 4224 ASCII bytes. These are local policy bounds, not Connect limits.

Since 2026-10-23 (see "Data-path throughput" below) stdin has up to eight
acknowledged blocks of at most 2048 bytes outstanding; the broker queues them in
memory and its writer holds one at a time. Each output has a window of eight
records of at most 2048 bytes, produced by a per-stream reader worker; Go
acknowledges a record after the corresponding local writer consumes it.
Independent input/output workers allow output to drain while a child stops
reading input. EOF closes only child
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
  forwarding, SCP and SFTP remain unfinished. Hex framing and per-byte output
  reads in Bash cost throughput (see "Data-path throughput").
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

## Data-path throughput — 2026-10-23

Native 1MiB transfers ran at about 11KiB/s in each direction. Local profiling
(Bash micro-benchmarks on this Mac, both 5.3 and 5.2.37) attributed the time to:

| Cost, per KiB moved | Old | New |
| --- | --- | --- |
| Input: parent line read of a 2 KiB hex record (one `read` syscall per byte) | ~0.4 ms | `read -N` exact length: ~0.035 ms |
| Input: writer line re-read | ~0.4 ms | `read -N`: ~0.035 ms |
| Input: per-pair `\x` escape loop | 5–7 ms | one `${hex//??/\\x&}` (Bash >=5.2): ~0.16 ms |
| Output: per-byte poll + `read -n1` | ~6 ms | unchanged (see below), now in a worker per stream |
| Output: per-byte `printf -v %02x` + append | ~4 ms | one `printf %02x` over the record: ~0.4 ms |
| Round trips | 1 per 1 KiB input; 1 per 512 B output | 8×2 KiB windows each way |

**Changes** (client and adapter ship together, so the record format is an
internal version, not negotiated):

- `I seq length hex`: up to eight unacknowledged 2048-byte input records. The
  parent reads the record body with an untimed `read -N` of its declared length,
  queues it in a Bash array, and forwards one record at a time to the writer,
  which still acknowledges only after writing to the command. A command that
  stops reading therefore still backpressures the local reader (at most eight
  records plus the writer's one). Records arriving after `K` are validated and
  dropped; `F` is sent only after every block is acknowledged.
- Bash >=5.2 escapes hex with one pattern substitution (`patsub_replacement`).
  Bash 4.3–5.1 keep the previous loop, selected by `BASH_VERSINFO`; that path
  is unchanged code but was not run locally on a 4.3–5.1 build.
- Output: one reader coprocess per stream receives one newline token per
  permitted record (window eight, so command output is never consumed ahead of
  the window). It blocks for the first byte, then takes further bytes only while
  `read -t 0` reports data, up to 2048, and encodes the record with one
  `printf`. A short write is therefore delivered immediately
  (`TestShellBrokerPartialOutputIsNotHeldBack`). The parent reads each
  `length / hex` pair with `read -N` and is the only carrier writer.
- The parent sleeps 1 ms (timed read on the never-written idle pipe) only when
  an iteration did nothing, instead of after every iteration without input.

No timed `read` consumes protocol or command data: `-t 0` only polls, `-N` and
`-n1` reads are untimed. No new remote tool is required: the two extra workers
are Bash subshells, in memory.

**Why output is still per byte.** Bash cannot learn how many bytes a pipe holds.
`read -n K` blocks until K bytes or the delimiter, which would hold back
interactive output; a timed multi-byte read would reintroduce the Bash 5.2
delimiter-loss defect for NUL; `read -N` uses one buffered syscall but silently
drops NUL bytes (`builtins/read.def`, 5.2); `EINTR` from a signal discards the
partial variable. So one `read -t 0` + `read -n1` pair per byte remains the
floor, about 6–8 µs per byte locally. An optional `dd bs=2048 count=1` per
record (one `read(2)` of what is available) could remove it, but it adds a
remote tool and a fork per record; it was not implemented and would need a
measured native need plus a pure-Bash fallback.

**Local measurement** (`TestShellBrokerThroughputBenchmark`, real PTY + Bash,
256 KiB per case, order-preserving simulated round trip; upload = stdin to
`wc -c`, download = 256 KiB `/dev/zero` + 256 KiB `/dev/urandom`, roundtrip =
256 KiB through `cat`, rates count bytes moved):

| Bash | RTT | Upload old → new KiB/s | Download old → new | Roundtrip old → new |
| --- | --- | --- | --- | --- |
| 5.3 | 0 ms | 96.9 → 693.6 | 92.0 → 115.5 | 166.3 → 228.7 |
| 5.3 | 10 ms | 62.1 → 539.6 | 62.7 → 112.8 | 123.3 → 219.2 |
| 5.3 | 40 ms | 28.0 → 259.2 | 20.4 → 105.4 | 41.1 → 194.9 |
| 5.2.37 | 0 ms | 115.2 → 721.2 | 94.2 → 118.4 | 176.5 → 236.5 |
| 5.2.37 | 10 ms | 68.5 → 548.7 | 63.9 → 115.9 | 131.4 → 224.8 |
| 5.2.37 | 40 ms | 30.7 → 262.1 | 20.4 → 107.2 | 40.8 → 193.6 |

Single runs on a shared Mac; treat differences under ~10% as noise. Download is
now insensitive to RTT and bound by the per-byte read loop. The Pi's CPU is
slower, so native rates are lower. Native single runs on 2026-10-26
(REAL_DEVICE_TESTS.md): upload ≈186 KiB/s, stdout and stderr ≈18 KiB/s, against
about 11 KiB/s before. Output remains the bottleneck; see "Why output is still
per byte".

**In-flight bounds.** At most eight records per direction and stream (about
66 KiB of output hex for both streams), within the transport's 256 KiB / 4096
message receive queue. The Connect PTY receives up to about 33 KiB of input
records at once instead of 2 KiB; the agent's behavior under that burst is
unverified natively.

## Abort after remote completion — 2026-10-23

Native 2026-10-22: a local stdin read failure with `exit 0` reported "remote
broker cleanup was not confirmed". Cause, from the code: the broker emits `X`/`Y`
only after reaping the command and helpers, then returns and its Bash exits
(the carrier shell was replaced by `exec bash`), so it never reads a later `C`.
When the client failed locally while `X` was in flight, `abort` ignored `X` and
waited two seconds for a `D` that could not come. `abort` now treats `X`/`Y`
as confirmation; `D` remains the only confirmation while the command runs.
Regressions: `TestBrokerAbortAcceptsCompletionAlreadyInFlight` (fake carrier)
and `TestShellBrokerAbortAfterRemoteCompletion` (real Bash; received frames are
delayed 500 ms after `G` so `C` arrives after the broker exited). Both failed
before the change and pass after it. Run still returns the local stdin error.
OpenSSH's channel code appears to treat a local read error like EOF
(`chan_read_failed`); whether pi-sh should match that is a separate, untested
compatibility question. One native run on 2026-10-26 confirmed cleanup
(REAL_DEVICE_TESTS.md).

## Idle CPU — 2026-10-28

The main loop polls its descriptors with `read -t 0` and, when a round finds
nothing, sleeps with a timed read on a pipe that nothing writes (Bash 5.2 timed
reads can lose data, so they are never used on data descriptors). That sleep
was 1 ms, so an idle session ran about a thousand rounds a second. On the
device the broker of a session that only ran `sleep 10` used 257 ticks of
CPU in 10 s (`/proc/<pid>/stat`, 100 ticks per second): a quarter of a core.

Idle rounds now sleep 1 ms for about 100 rounds, which covers the wait for an
acknowledgement or the next record of a transfer, and 20 ms after that. Any
activity resets the count. The same session's broker then used 27 ticks in
10 s. The cost is at most 20 ms more before the first event after a pause.

Transfers were unchanged on the device (read-only, old and new build, one run
each): 1 MiB to `wc -c` took 3.41 / 3.44 s and 1 MiB from `head -c` took 56.1
/ 56.2 s. The local broker and Bash forwarding benchmarks stayed within run
noise. The Bash forwarding adapter (forward.bash) now does the same with its
2 ms naps.

## Perl output reader — 2026-10-28

"Why output is still per byte" above left the Bash reader at one `read -n1` per
byte, about 18 KiB/s of command output on the device. When the device has Perl
(perl-base is Essential on Raspberry Pi OS), each output worker now execs a
short Perl program instead. It keeps the same contract: one record per newline
token, `length\nhex\n`, and `F` at the end. `sysread` takes whatever the pipe
holds, up to 2048 bytes, so short writes are still delivered at once.

- The program is the single-quoted word `_ps_perl_reader` in broker.bash, so it
  holds no apostrophe. It runs only if `perl -c` compiles it and `/dev/fd/N` is
  readable; otherwise the Bash reader stays.
- The stream descriptors come from coproc descriptors, which are close-on-exec,
  and Bash copies that flag when duplicating them. `9<&5` therefore gave perl
  nothing (every test saw empty output). The worker opens `/dev/fd/N` as
  descriptor 9 instead.
- `PI_SH_BROKER_READER=bash` keeps the Bash reader. The full verification runs
  `internal/shellbroker` and `cmd/...` once more with it.
- New tests: `TestShellBrokerUsesPerlReaderWhenPresent` (counts the broker's
  perl readers: 2, or 0 when forced to Bash) and
  `TestShellBrokerFallsBackToBashReader` (a perl that cannot compile it).

Local benchmark (`TestShellBrokerThroughputBenchmark`, Bash 5.3, KiB/s):

| RTT | Download Bash → Perl | Roundtrip Bash → Perl | Upload |
| --- | --- | --- | --- |
| 0 ms | 114.6 → 2683.9 | 231.6 → 1135.8 | unchanged (612 / 577) |
| 40 ms | 104.9 → 375.9 | 184.5 → 492.4 | unchanged (233 / 238) |
| 150 ms | 82.6 → 118.9 | 130.8 → 160.3 | unchanged (79 / 78) |

With a round trip the eight-record window, not the reader, now bounds output.

On the device (read-only, one run each; REAL_DEVICE_TESTS.md):
- 1 MiB from `head -c` took 1.26 s instead of 56.2 s;
- `pi-cp` of `/usr/bin/bash` (1,412,096 bytes) took 22.1 s instead of 94.1 s,
  both totals including sign-in and setup;
- the SHA-256 matched `sha256sum` on the device.
