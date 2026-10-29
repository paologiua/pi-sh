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
- `PI_SH_BROKER_READER=bash` (since renamed `PI_SH_BROKER_WORKERS`, "Perl input
  writer") keeps the Bash reader. The full verification runs
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

## Window — 2026-10-28

Once Perl read the output, the eight-record window (16 KiB per direction and
stream) bounded output at any real round trip: locally 376 KiB/s at 40 ms
against 2684 KiB/s at 0 ms. The window is now 32 records, set as `_ps_win` in
broker.bash and `recordWindow` in broker.go (`TestBrokerWindowMatchesClient`),
the same as the forwarding adapter.

**In-flight bounds.**
- Up to 32 input records (128 KiB of hex) are queued in the broker's Bash
  memory.
- The Connect PTY receives input bursts of up to about 130 KiB.
- The two output streams can hold up to 256 KiB of hex in flight. A full
  transport receive queue now holds the device back instead of failing
  (TRANSPORT.md).

Local benchmark (Bash 5.3, Perl reader, KiB/s), window 8 → 32:

| RTT | Download | Roundtrip | Upload |
| --- | --- | --- | --- |
| 0 ms | 2684 → 2771 | 1136 → 1268 | 577 → 627 |
| 40 ms | 376 → 686 | 492 → 572 | 238 → 281 |
| 150 ms | 119 → 204 | 160 → 220 | 78 → 116 |

On the device (read-only, single runs) the round trip to Connect is short
enough that a stream was already near its rate. 1 MiB of stdout took 1.25 s
(1.26 s with window 8). 1 MiB of upload took 3.09 s (3.54 s). `pi-cp` of
`/usr/bin/bash` completed with the same SHA-256. Its 28.5 s wall time (22.1 s
before) includes sign-in and setup and does not isolate the window.

**Upload limit.** Upload is now bound by the Bash input writer's hex decoding,
about 600 KiB/s locally.

**Failure, explained below ("Input window").** One full verification run with the new window failed
under the Bash 5.2.37 step: `TestShellBrokerAbortAfterRemoteCompletion` ("0
local stdin read failed", cleanup not confirmed), then
`TestShellBrokerBlockedInputDoesNotBlockOutput` hung until the package timeout.
The same run's race step also had a timing failure in
`TestStagedTTYStopOpenSSHDifferential/caught-suspend`, which uses the separate
TTY adapter. The failure could not be reproduced:
- two more runs of that Bash 5.2 step, with this window and with window 8;
- 25 repetitions of each affected test under parallel load, on both;
- a full rerun of the verification, all green.

This section first recorded it as open.

## Perl input writer — 2026-10-28

With output in Perl, upload became the slow direction: the Bash writer decodes
each record with `printf %b` (about 600–850 KiB/s locally, 300 KiB/s on the
device). When perl compiles `_ps_perl_writer`, the input worker now execs it.
It keeps the same contract:
- `start` to the command first;
- for each `seq length` line and its hex, the decoded bytes, then `A seq` once
  written;
- `K` when the command stops reading;
- `F` after closing the command's input.

The command's input descriptor (6) is reopened for perl as 9 through
`/dev/fd/6`, like the readers.

Both Perl programs now start with `use strict`. The first version of the
writer declared `my $h` inside the same expression that then used `$h`, which
in Perl still means the global variable. Every record failed and the writer
exited (empty stdin in every test). Under `strict` such a mistake fails
`perl -c`, so the broker keeps the Bash worker instead.

`PI_SH_BROKER_READER` is renamed `PI_SH_BROKER_WORKERS=bash`; it keeps all three
workers in Bash. The tests are renamed `TestShellBrokerUsesPerlWorkersWhenPresent`
(three perl workers while unread input keeps the writer blocked) and
`TestShellBrokerFallsBackToBashWorkers`.

Local benchmark (Bash 5.3, 1 MiB per case, KiB/s), Bash workers → Perl workers:

| RTT | Upload | Download | Roundtrip |
| --- | --- | --- | --- |
| 0 ms | 849 → 2427 | 113 → 5093 | 222 → 4210 |
| 40 ms | 614 → 960 | 112 → 1409 | 214 → 2019 |

On the device (read-only, single run): 1 MiB to `wc -c` took 1.47 s (3.09 s
with the Bash writer), 1 MiB from `head -c` 0.91 s. `pi-cp` of
`/usr/bin/bash` had the same SHA-256 and took 19.1 s including sign-in and
setup.

Afterwards the only `perl` processes on the device were the two readers of the
listing session itself; nothing listened beyond 22 and 11434.

## Input window — 2026-10-28

**Cause of the failure under "Window".** The stack saved at the timeout shows
two goroutines blocked for three minutes:
- `Run` was in a `write` to the carrier, sending a 4 KiB input record;
- the receive pump was waiting to hand `Run` a record, with its 16-record
  queue full.

`Run` sends input on the same goroutine that consumes output, so a blocked send
also stops output from being consumed. The cycle closes like this:
1. `Run` sends input faster than the broker takes it;
2. the broker blocks writing output that nobody reads;
3. it therefore stops reading input;
4. the carrier fills and `Run`'s send blocks for good.

The cycle cannot close while all the input in flight fits the carrier's
buffers: the PTY, the relay pipe (64 KiB on Linux) and the broker's memory.
At 8 records (about 33 KiB of hex) it always fit; that was the original
design. Raising the window to 32 records (about 130 KiB) broke that bound. It
showed up only under load, when the relay `cat` and the broker run late.

On the device the same cycle could form. With the transport's new
backpressure it would hang rather than fail.

**Fix.** The two windows are now separate:
- input: 8 records, `inputWindow` in broker.go and `_ps_in_win` in
  broker.bash (`TestBrokerWindowMatchesClient`);
- output: 32 records per stream, `_ps_out_win`, set on the broker side only.
  A blocked send cannot hold output that `Run` has not asked for.

**Evidence and limits.**
- Under full CPU load (one `yes` per core, Bash 5.2.37), 20 repetitions of
  `TestShellBrokerAbortAfterRemoteCompletion` and
  `TestShellBrokerBlockedInputDoesNotBlockOutput` hung once with window 32.
- The same load passed twice with the fix, but also twice on the unfixed code,
  so the reproduction is not reliable.
- A deterministic test (slow local stdout, pending input) did not reproduce the
  hang on either version and was not kept.
- The fix rests on the stack trace and on the bound above.

On the device (read-only, single run) 1 MiB to `wc -c` took 1.77 s (1.47 s
with input window 32), and 1 MiB from `head -c` took 0.79 s.

**Next step.** Separating sending from receiving in `Run` would remove the
cycle whatever the window, as the forwarding mux already does. It is not done
yet: the abort path also writes to the stream and would have to wait for an
in-progress send.

**Unrelated failure.** In one verification run,
`TestTTYCommandSessionOverWebRTC/ctrl-z-handler` (Bash 5.2, TTY adapter)
failed with "Ctrl-Z lost command completion". It then passed 20 of 20 alone
and 15 of 15 under full load, both on this tree and on `59e152d` (this
morning). It is recorded as an intermittent timing test, with no link to
today's changes shown.

## Perl broker — 2026-10-29

**Why.** The hardest defects of the remote side came from Bash and from its
differences between versions:
- Bash 5.3 cleared a new coprocess's array when an older one of the same name
  was reaped (FORWARDING.md "Perl adapter");
- a Bash 5.2 `read -t` can consume a newline and still report a timeout
  ("Intermittent binary-input stall" above);
- Bash copies close-on-exec when it duplicates a descriptor ("Perl output
  reader");
- Bash reads one byte per call and had to poll ("Idle CPU").

Raspberry Pi OS will move to Bash 5.3. Perl 5 changes far less between
versions, and perl-base is Essential.

**What runs.** `internal/shellbroker/broker.pl` is the whole broker: one
process, with no coprocesses, relay or workers, and one blocking `select`.
broker.bash now begins by running `perl -c` on it, and on success `exec`s it.
The program travels as the single-quoted Bash word `_ps_perl_src`, so it holds
no apostrophe (`TestBrokerPerlHasNoApostrophe`). The rest of broker.bash is
unchanged and stays as the fallback, frozen except for fixes, when the device
has no perl or perl cannot compile the program. The handoff comes before
broker.bash ignores SIGPIPE: `exec` restores trapped signals but keeps ignored
ones, and the first version handed the command an ignored SIGPIPE
(`TestShellBrokerImplsGiveCommandSameContext` found it).

The records are those of the Bash broker, and broker.go reads them unchanged:
- R; the I, F, A and C records; O, E and F per stream; A and K;
- X or Y; D after C;
- the same `Z protocol`;
- the same windows, passed as arguments.

What changes inside:
- **Command.** `fork`, then `POSIX::setsid` and `exec $SHELL -c command` in
  the child. As with the Bash coprocess (`trap -`), the command gets the
  signal dispositions that the broker had on entry. Bash's `exec` lowered
  SHLVL by one, so the Perl broker raises it back.
- **Status.** `waitpid` gives the exit status or the signal directly, without
  the `jobs -l` table read in a CHLD trap. The OpenSSH differential cases
  (exits 0–255, TERM, INT, KILL) give the same results with both brokers.
- **Cleanup.** C, HUP, TERM or the loss of the carrier lead to `kill -KILL` of
  the group, of the command and of the group again, then `waitpid`, as
  `_ps_cleanup` does. On a normal end the group is left alone, as before.
- **Idle.** `select` blocks with no timeout. The only timeout, 0.1 s, applies
  while both outputs have ended and the exit is still awaited. There SIGCHLD
  interrupts `select`, and the timeout covers a wakeup lost to Perl's deferred
  signals.

**Defects found while writing it.**
- *Output blocking input.* The first version wrote to the carrier blocking and
  read the PTY directly. With slow local output it stopped taking input while
  `Run` blocked sending input: the cycle of "Input window". The Bash broker
  had escaped it only because its relay `cat` pipe absorbed up to 64 KiB of
  in-flight input. Seen as a five-minute hang of
  `TestShellBrokerCancellationReapsBeforeCarrierClose/perl` under `-race`;
  the stack showed `Run` writing an I record and the receive pump full.

  The Perl broker now writes the carrier without blocking and reads it in
  every round, also while it flushes its last records. Pending output is
  bounded by the output windows. `TestShellBrokerSlowOutputWithFastInput`
  (1 MiB through `cat` with a slow local stdout) hangs with a blocking writer
  and passes in about 1 s.
- *Exit before input.* The faster broker exposed a race in `Run`. The command
  closes its input, the broker sends K and X and exits, while `Run` is still
  sending pipelined input. The send then fails (EIO on the test PTY), and
  `Run` reported the error instead of the status
  (`TestShellBrokerEarlyCloseWithPipelinedInput/perl`). The same race existed
  with the Bash broker, only less often.

  When a send of input (I or F) fails, `Run` now waits up to one second for a
  status already in flight. Only K, input A and output F may come before it;
  then it returns that status, as OpenSSH reports the exit status. A cancelled
  context, any other record, or no status keep the error and the abort.
  Covered by `TestBrokerCompletionWinsOverFailedInputSend`.

**Staging.** The staged source grew from 16.3 to 23.0 KB: broker.bash plus
6.8 KB of Perl. On the device the loader's per-byte decoding, not the round
trips, was the cost: from channel open to the end of `true`, the old build
took 1.02 s, the Perl build 1.55 s with 896-byte chunks and 1.73 s with
2048-byte chunks. Those are 62, 99 and 134 ticks of loader and broker CPU.

On Bash 5.2 and later, the loader now decodes each chunk with one
`${h//??/\\x&}`, as the Bash input writer already did. Bash 4.3 to 5.1 keep
the per-byte loop (`TestLoaderPerByteDecodePath`). L and C chunks are now
2048 bytes (`uploadChunk`, `TestLoaderAcceptsUploadChunk`). On the device
`true` then took 0.51 s with 16 ticks (0.62 s with 896-byte chunks).
Locally a `true` takes about 60–90 ms, against about 200 ms before.

**Switch.** `PI_SH_REMOTE_IMPL=bash` keeps every remote program in Bash: here
the Bash broker with Bash workers. `PI_SH_BROKER_WORKERS=bash` now does the
same for the broker.

**Tests.**
- `eachImpl` runs the broker suite with both brokers, as `eachConnector` does
  for the forwarding adapter. It covers streams and status, every byte,
  duplex, blocked input, 1 MiB stdin with late newlines, slow consumer, early
  close, partial output, cancellation, abort after completion, the OpenSSH
  differential, SFTP and the login shell.
- `TestShellBrokerUsesPerlBrokerWhenPresent`: the command's parent is `perl`
  with no workers; with either switch it is `bash` with no Perl workers.
- `TestShellBrokerFallsBackToBash`: a perl that compiles nothing gives Bash
  with Bash workers. A perl that refuses only broker.pl gives Bash with Perl
  workers.
- `TestShellBrokerImplsGiveCommandSameContext`: environment, ignored signals,
  group, session and limits are equal.
- `TestShellBrokerCarrierLossKillsCommandGroup`: closing the PTY kills a
  command group that would outlive it. It fails, by timeout, when the kills
  are removed.

**Local measurements** (Bash 5.3, single runs, KiB/s), from `7500852`
(Bash broker with Perl workers) to the Perl broker.

`TestShellBrokerThroughputBenchmark`:

| RTT | Upload | Download | Roundtrip |
| --- | --- | --- | --- |
| 0 ms | 2272 → 4689 | 4805 → 17310 | 4167 → 9806 |
| 10 ms | 1394 → 1891 | 3562 → 5699 | 2954 → 3726 |
| 40 ms | 485 → 541 | 1501 → 1740 | 983 → 1073 |

`TestSFTPThroughputBenchmark`, 32 KiB requests:

| RTT | Upload | Download |
| --- | --- | --- |
| 0 ms | 4144 → 8540 | 8876 → 53706 |
| 40 ms | 450 → 480 | 1502 → 1595 |

At 40 ms the input window of 8 records still bounds upload.

**On the device** (Bash 5.2.37, perl 5.40.1, rpi-connect-lite 2.13.0,
read-only, single runs; REAL_DEVICE_TESTS.md):
- the broker process is `perl`, child of `rpi-connectd`;
- idle, it used 0 ticks in 10 s (27 with the Bash loop);
- 1 MiB to `wc -c` took 1.01 s (old build, same hour: 1.64 s);
- 1 MiB from `head -c` took 0.43 s (0.81 s);
- `pi-cp` of `/usr/bin/bash` took 1.59 s from channel open (2.76 s), with the
  device's SHA-256;
- with `PI_SH_REMOTE_IMPL=bash` the device ran the Bash broker: 3.4 s and
  56.4 s, the historical all-Bash figures.

## Second hangup during cleanup — 2026-10-29

`TestShellBrokerCarrierLossKillsCommandGroup` closes the carrier PTY of a
command whose group would outlive it. Under load the Bash broker sometimes
left the group running: 8 of 240 runs with one `yes` per core. A process
snapshot showed why. The command shell had PID 1 as its parent, so the broker
had exited without killing the group.

Losing the carrier delivers two hangups: the kernel's, and the one from the
process that owns the terminal (here the test harness, on the device perhaps
the agent).
- The first runs `exit 125`, which starts the EXIT trap, `_ps_cleanup`.
- If the second arrived before the cleanup reset its traps, the handler ran
  `exit 125` again inside the EXIT trap, and Bash exited at once, before the
  group was killed. Resetting HUP to its default, as the cleanup then did,
  had the same effect a moment later.

The handler now ignores HUP and TERM before it exits, and the cleanup
ignores them instead of restoring the default. forward.bash
(`_fw_cleanup`) and tty.bash (`_pt_abort`) had the same pattern and are fixed
the same way. After the fix: 0 of 240 runs under the same load. The Perl
programs already ignore HUP and TERM first in their cleanup.

**Perl broker: wait for the group.** On C the Perl broker reaped its command
and answered D at once. A descendant hit by `KILL` could still be running at
that moment: `TestShellBrokerCancellationReapsBeforeCarrierClose/perl` failed
once, "owned descendant still running". The slower Bash broker had left time
for it to die. The Perl broker now confirms only once `kill 0` finds no
process in the group, or after one second. After the change the test passed
50 of 50 runs under load.

## Run sender and input window of 32 — 2026-10-29

**Change.** After R, one goroutine in `Run` writes every record (I, F, and
the A acknowledgements of output). The loop that consumes output only queues
them, so a blocked send no longer stops output from being consumed. That was
the cycle of "Input window": `Run` blocked sending input, the broker blocked
writing output, and neither moved again.

The queue holds `inputWindow + 2*outputWindow + 1` records. While the broker
keeps its windows it cannot fill: at most `inputWindow` I records, one F, and
one A per output record of the two streams can wait. A broker that sends more
fails the protocol instead of blocking. `outputWindow` equals `_ps_out_win`
(`TestBrokerWindowMatchesClient`).

**Abort.** `abort` writes C only after the sender has returned. `stopIO`
cancels the context of the sender's `Send`, and Stream implementations honor
it, so the wait is short. A `Send` that ignored its context would leave
cleanup unconfirmed after `cleanupTimeout`, without a second writer on the
stream. A failed send of input still lets a status already in flight win
("Perl broker").

**Tests.**
- `TestBrokerConsumesOutputWhileInputSendBlocks`: the first I record's
  `Send` blocks until 40 output records reach the local stdout. The previous
  `Run` hung until its deadline (checked by putting it back); the new one
  completes.
- `TestBrokerAbortWaitsForBlockedSender`: C is never sent beside a running
  `Send`. A `Send` that ignores its context gives `ErrCleanupUnconfirmed`
  within the bound, with no C.

**Window.** With the cycle gone, the input window is back to 32 records
(`inputWindow`, `_ps_in_win`; the Perl broker takes it as an argument).

Local benchmark (Bash 5.3, Perl broker, KiB/s), input window 8 → 32:

| RTT | Upload | Download | Roundtrip | SFTP upload, 32 KiB requests |
| --- | --- | --- | --- | --- |
| 0 ms | 4689 → 3726 | 17310 → 20079 | 9806 → 10573 | 8540 → 8678 |
| 10 ms | 1891 → 3477 | 5699 → 5249 | 3726 → 6616 | |
| 40 ms | 541 → 1219 | 1740 → 1681 | 1073 → 2466 | 480 → 1202 |

At 0 ms single runs vary by about a quarter; with a round trip, upload more
than doubles.

On the device (read-only, single runs, REAL_DEVICE_TESTS.md "Input window of
32"), 1 MiB to `wc -c` took 0.47 s: 1.01 s with window 8, and 1.64 s for
`7500852`. 1 MiB from `head -c` took 0.49 s. `pi-cp` of `/usr/bin/bash` took
1.80 s from channel open (1.59 s with window 8, single runs of a download),
with the device's SHA-256.

**Loaded runs.** In the first loaded run with window 32,
`TestShellBrokerSlowOutputWithFastInput/bash` (the Bash broker with Bash
workers) went past its 60 s limit twice. Two later loaded runs, alone, passed
every time (480 of 480 for the whole timing set; that test took 16–17 s with
window 32 and with window 8). No stack was captured. The overruns are
recorded as unexplained and not reproduced. In a run with double load, one overrun of
`TestShellBrokerCancellationDuringWorkerStartup` stalled before the broker
started: the carrier shell had not printed its first record within
15 s.
