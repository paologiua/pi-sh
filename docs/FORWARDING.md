# Forwarding — investigation 2026-10-20, implementation 2026-10-27

**Status (2026-10-27):** `-L`, `-D`, `-W`, `-N` and their configuration
directives are implemented through an in-memory Bash adapter and a connector
program already present on the device (see "Implementation" below). `-R` is
parsed and refused pending the owner's decision. Agent, X11, tunnel,
multiplexing and proxy options are accepted only with their "off" values.
Connect itself still offers no native forwarding request: the observed interface
supplies a shell PTY and resize/control events, and none has been observed since.
The investigation below explains why the plain Bash bridge was rejected.

## Required behavior

`-W host:port` needs binary duplex stdio, connection success/failure, independent
EOF in each direction and bounded cancellation/cleanup. `-L` additionally needs
local listeners, concurrent accepted streams, bind policy and per-stream errors.
`-D` needs SOCKS4/5 parsing and per-request authorization/destination handling.
`-R` needs a remote listener, accurate bound address/port reporting, concurrent
accepts and cancellation. Listener failure must not be mistaken for successful
setup. IPv4/IPv6, DNS/error policy, flow control, authorization loss and native
runtime behavior require their own evidence. None is inferred from shell access.

## Tested Bash-only candidate and its limitation

`TestBashTCPBridgeCannotHalfCloseWithReaderAlive` starts local Bash and a TCP peer
only on IPv4 loopback. The candidate opens `/dev/tcp`, runs a reader cat in the
background and a writer cat in the foreground, closes the parent's socket fd
after stdin EOF and waits for the reader. All 2048 bytes, containing every byte
value, arrive unchanged. The peer then waits for input EOF: a 200 ms read deadline
expires instead of receiving FIN. A response sent without waiting for EOF still
arrives unchanged, and peer close lets the bridge exit.

This is consistent with descriptor lifetime: the reader retains a socket
descriptor, so closing the writer/parent descriptors does not shut down just the
socket's write half. Bash redirection has no corresponding socket shutdown
operation. Closing/killing the reader would lose the still-needed response
stream. Shell pipes, extra cat processes or a timeout do not supply that missing
operation. These observations rule out this candidate as a general OpenSSH-like
forwarder; they are not a proof that every permitted implementation is impossible.

`TestNativeTCPHalfCloseKeepsReturnStream` controls the fixture with Go TCP
CloseWrite: the peer observes EOF and subsequently sends a response successfully.
The Bash candidate therefore fails an available, meaningful TCP behavior. Both
tests run locally; Windows is not covered by these Unix shell fixtures. No Pi
network connection, socket, listener, file or service change was made.

## Remaining viable investigations

- Observe a native capability only through an authorized official session; do
  not invent protocol channel names, bypass authorization or use SSH fallback.
- An **already present** socket utility with explicit half-close may supply the
  remote connector. OpenBSD nc documents `-N` as socket shutdown after stdin EOF.
  Utility presence, implementation/version, flags, binary duplex behavior and
  cleanup on the Pi are unverified. Never install it to make forwarding work.
  A utility name or help string alone will not establish runtime semantics.
  On this Mac, `TestLocalNetcatHalfCloseCandidate` passes with the existing nc
  **without flags**: the peer sees all 2048 request bytes followed by EOF, then
  returns the same binary bytes successfully. Its local help assigns `-N` to a
  write-timeout probe count, unlike OpenBSD's shutdown flag. This is concrete
  dialect divergence; never append guessed flags to an unverified remote nc.
  The test explicitly skips missing/default-no-half-close dialects, so CI must
  report those skips rather than treating them as utility support.
- If a connector is verified, prove one stream over the actual broker first,
  then concurrent stream isolation, backpressure, EOF, cancellation and failure
  propagation before exposing `-W`, `-L` or `-D`. A carrier per TCP connection may
  encounter official service session limits; no quota or concurrency is assumed.
- Bash `/dev/tcp` opens outbound sockets; it does not provide a remote listening
  primitive for `-R`. A verified native or already-present listening facility
  would still need bound-port reporting and lifecycle tests.
- Unix sockets, X11 authorization files, agent sockets and other remote filesystem
  changes remain constrained by AGENTS.md. No filesystem-write authorization is
  implied by this investigation; tunnel interfaces/configuration changes are
  prohibited under the current Pi restrictions.

The result is a tested implementation blocker for the simple Bash bridge, plus
unresolved native/utility capability evidence. It is not a service permission,
forwarding support claim, Python requirement or reason to install remote software.

Primary references: [OpenSSH forwarding options](https://man.openbsd.org/ssh.1),
[OpenBSD nc half-close option](https://man.openbsd.org/nc.1).
The local verified GNU Bash 5.3 source/manual (`redir.c`, `lib/sh/netopen.c`,
`doc/bashref.texi`) was inspected for redirection/network-open behavior. No source
is copied or linked into the CLI; these fixtures add only standard-library Go.

## Connector inventory on the Pi — 2026-10-27

Read-only, one pi-sh `-T` command (`command -v`, `readlink -f`, `nc -h | head`,
`dpkg-query -W`). Present: `nc` and `netcat` → `/usr/bin/nc.openbsd`, "OpenBSD
netcat (Debian patchlevel 1.229-1)", package `netcat-openbsd 1.229-1`, whose usage
lists `-N` (shut down the socket after stdin EOF) and `-U`; also `busybox`, `curl`,
`perl` and `python3` (3.13). Absent: `ncat`, `nc.traditional`, `socat`, `telnet`.

This settles which connector `internal/fwdbroker` (`openbsd-nc`) can use on this
device without installing anything. It does not yet verify its behavior:
binary transparency, half-close with a live reader, connect-failure status and
cleanup still need a native probe. A probe against a loopback service that
already listens (for example reading a banner) is read-only; a half-close echo
test needs a temporary remote listener (`nc -l` on 127.0.0.1), which is a new
remote process and needs the owner's approval of an exact plan first. Python and
Perl are present; see the Perl connector below.

## Implementation — 2026-10-27

**Pieces.** `internal/forwardspec` parses OpenSSH 10.2 forwarding specifications
and directives and prints them for `-G` (differential against local `ssh -G`).
`internal/forward` is the local side: listeners with ssh's bind rules, SOCKS4/4a/5
CONNECT, `-W` stdio and a relay with independent half-close per direction.
`internal/fwdbroker` stages `forward.bash` over a Connect shell carrier, detects a
connector (Perl whose Socket module loads, `nc -h` reporting OpenBSD netcat, Nmap ncat, or socat, in that order since 2026-10-28; see "Throughput"), and multiplexes
up to 64 remote connectors, each started per connection with an argv chosen by
Go (for OpenBSD netcat: `nc -N -v -- host port`, or `nc -N -U -- path`). Records are
hex text with acknowledged windows (since 2026-10-28 input and output 8×2 KiB per stream);
the adapter kills and reaps every connector on `Q`/abort. Connector failure text
is not echoed: only recognized system reasons ("Connection refused" …) are shown.

**CLI (`cmd/pi-sh/forwarding.go`).** Forwarding uses its own Connect session, opened
from the same authenticated source, separate from the shell/command channel. Two
concurrent sessions were observed natively (below). `-N`/`-W` open only that
carrier. With a shell or command, the carrier and listeners are set up first
(as ssh binds after authentication and before the session), and stopped when the
session ends: listeners close, active streams end, the adapter confirms cleanup
(an unconfirmed cleanup is reported), and the carrier closes.

**Semantics checked against OpenSSH 10.2p1** (a local sshd fixture, single runs):
`-W` with `-n` or a command still relays (the command is ignored), a refused target
prints `channel 0: open failed: connect failed: Connection refused` then
`stdio forwarding failed` and exits 255; `-N` ignores command words; `-W` clears
`-L` and implies no TTY; `-L localhost:…` succeeds if one loopback address binds;
ClearAllForwardings also drops command-line forwardings; `-N` after another
SessionType is `Cannot specify -N with -s/SessionType`; `ssh -N` exits 0 on
SIGTERM and 255 on SIGINT or SIGHUP, which pi-sh reproduces.

**Configuration.** `-o` and `-F` files accept LocalForward/DynamicForward (file
entries follow command-line ones, duplicates dropped, also across `Match final`),
GatewayPorts, ExitOnForwardFailure, ClearAllForwardings, StreamLocalBindMask/Unlink,
PermitRemoteOpen (printed; no effect without `-R`) and SessionType none/default.
pi-cp and pi-sftp validate and drop list directives, as scp/sftp run ssh with
ClearAllForwardings=yes. Unix-socket paths expand `%d %h %i %n %r %u %%`; other
tokens are refused.

**Tests.** forwardspec differentials and parser cases; forward bind/SOCKS/relay
and Unix tests; adapter tests over a real PTY and Bash with a netcat test double
(`fwdtest`), covering binary half-close, concurrency, IPv6/Unix targets,
remote-first close, open failure, backpressure isolation and cleanup;
CLI tests over local Pion peers with an agent stand-in that opens one PTY shell
per Connect session: `TestStdioForwardingThroughCLI`,
`TestNoSessionLocalForwardingThroughCLI` (three concurrent streams, one
session, listener closed afterwards), `TestExitOnForwardFailureThroughCLI`,
`TestLocalForwardingAlongsideCommand` (two sessions, forwarded traffic while
the command runs, its status 7 kept).

**Known differences.** `~#` lists forwarded connections since 2026-10-28 (ESCAPES.md), with pi-sh's own status in parentheses; `~&` (ssh forks into the background) and the escape
command line are absent. Since 2026-10-28 pi-sh waits for open forwarded
connections at the end of the session, as ssh does (see "End of session"). Throughput: see "Throughput" (2026-10-28). The device's connector must exist: pi-sh installs nothing and
refuses with ErrNoConnector otherwise.

**Browser companion and several sessions.** The companion first answered only one
offer, so the second session of one process (forwarding next to a command, or a
ConnectionAttempts retry in browser mode) timed out natively. It now runs one
heartbeat and answers every further offer with a new connection, never replaying
a create request; the bridge starts one authorization watcher, since several
split the single heartbeat (`TestSecondExchangeSharesOneAuthorizationWatcher`,
which failed before the change, and the companion's multi-offer test).
Native results: REAL_DEVICE_TESTS.md, 27 October 2026.

## Concurrent Connect sessions — 2026-10-27

Read-only, browser companion, two separate pi-sh processes. Session A ran
`sleep 40` and exited 21; session B started while A was sleeping (its remote
timestamp fell inside A's start/end timestamps) and exited 22. Each process had
its own companion step in the same Devices tab. So the agent accepts at least two
concurrent sessions for one account and device; no limit or quota was probed.
This is what lets forwarding use a carrier separate from the session.

## Perl connector — 2026-10-27

**Why.** OpenBSD netcat is installed by default on Raspberry Pi OS but can be
removed (Debian priority `important`). A read-only package query on the Pi showed
`bash` and `perl-base` 5.40.1 marked Essential (they cannot be removed without
breaking the system), `perl-base` shipping `Socket`, `IO::Socket::INET`,
`IO::Socket::IP` and `IO::Select`, `netcat-openbsd` `important`, `perl`
`standard`, `python3` only `optional`, and no `ncat` or `socat`. Bash's
`/dev/tcp` is enabled (a connect to 127.0.0.1:1 and ::1 port 1 failed with
`Connection refused`, not a missing file), but it cannot half-close or reach Unix
sockets. Perl via `perl-base` therefore is the fallback: it is always present and
can do everything the netcat connector does. Python is not used.

**How.** The adapter detects it with `perl -MSocket=getaddrinfo,pack_sockaddr_un`
printing a fixed fingerprint; a Perl without the module is not selected. The
program (`perlConnector`, 850 bytes) travels as an argv word of the open record
(`perl -e PROGRAM -- host port`, or `-- path` for a Unix socket); nothing is written
on the device. It connects in getaddrinfo order, prints a readiness line, relays
with `select`/`sysread`/`syswrite`, shuts the socket's write side at stdin EOF,
closes stdout at remote EOF while input still flows, and on failure prints the
system reason and exits 1. Dial now refuses an open record larger than the
adapter's bounds (per-target error instead of an adapter protocol failure), and
remote socket paths are limited to 107 bytes, which also fixes a path long enough
to have failed the whole adapter with the netcat connector.

`PI_SH_FORWARD_CONNECTOR=perl` (diagnostic, like `PI_SH_ISH_RELAY`) restricts the
selection to one detected tool; it still has to be recognized on the device.

**Tests.** The adapter suite (binary half-close, concurrency, IPv6 and Unix
targets, remote-first close with late input, open failure, backpressure and
cleanup, end-to-end -L/-D/-W) runs once with the netcat double and once with the
local Perl, under Bash 5.3 and 5.2; `TestSelectPrefersPerlThenNetcat` (order since 2026-10-28) and
`TestPerlOpenFitsAdapterRecord` check selection and the worst-case record size.
Native results are in REAL_DEVICE_TESTS.md.

## Native half-close — 2026-10-28

With an owner-approved temporary Perl listener on the device's loopback, 1 MiB
sent through `-L`, followed by the client's write shutdown, was answered with the
byte count on the same connection, with the netcat connector and with the Perl
one; nothing was left running (REAL_DEVICE_TESTS.md). Forwarded upload ran at
about 25 KiB/s. Re-run after the throughput change (owner-approved, same
listener): 1 MiB and the reply in 2.2 s with the Perl connector and 4.0 s with
netcat.

`-R` is the next step. Owner decision (2026-10-28): the remote listener binds the
loopback by default, and an explicit bind address (`*`, empty, or an address) is
honored, which is what sshd does with `GatewayPorts clientspecified`.

## Remote forwarding (-R) — 2026-10-28

**Reference.** Observed on a local OpenSSH 10.2p1 sshd with `GatewayPorts
clientspecified`: `-R 0:…` prints `Allocated port N for remote forward to h:p`;
a refused listen prints `Warning: remote port forwarding failed for listen port
N` and the session continues, or `Error: …` and exit 255 with
ExitOnForwardFailure (also with -N); no host binds `[::1]` and `127.0.0.1`, `*`
every address, `127.0.0.1` or `[::1]` only that; a privileged port fails for a
non-root user; a refused local target prints `connect_to h port p: failed.` and
the remote client sees a reset; a missing local socket prints `connect to PATH
port -2 failed: No such file or directory`; `-R port` (remote dynamic) answers
SOCKS only after the local connect, and closes without a reply on failure or when
PermitRemoteOpen denies the destination (debug
`rdynamic_connect_finish: requested forward not permitted`).

**Remote side.** `fwdbroker.Mux.Listen` runs `perl -e LOADER -- host port` as one
adapter stream; the loader reads a length-prefixed program (`listener.pl`) from
stdin and evaluates it, so nothing is written on the device. The listener binds
with sshd's clientspecified rules, reports `L port` or `F reason`, then
multiplexes accepted connections over its stdio with binary frames
(`pack("aNN", type, id, length)`) and a 64 KiB window per connection in each
direction, so a stalled local consumer stalls only its own connection. It
accepts at most 64 connections at once and exits at stdin EOF; closing the
stream kills it through the adapter. A device without Perl gets "remote
forwarding needs Perl (perl-base) on the device".

**Local side.** `forward.ListenRemote` requests each forwarding after the local
ones (as `ssh_init_forwarding`), with ssh's messages; `Remote.Serve` connects
each announced connection to the local target with half-close in both
directions, or performs the SOCKS exchange for remote dynamic forwarding.
Remote Unix-domain listeners (`-R /path:…`) are refused, since binding would
create a socket file on the device.

**Differences.** All connections of one -R share one adapter stream, so their
total rate is bounded like a single forwarded connection (about 450–600 KiB/s
natively with Perl since the throughput change). The listener's reason for a failed bind appears only
at debug level (sshd sends none). pi-sh waits for each listener's reply before
starting the session, while ssh may start the session first.

**Native test (2026-10-28).** With an owner-approved plan, loopback only:
256 KiB with half-close through `-R 127.0.0.1:47125:127.0.0.1:<Mac port>` and
through remote dynamic `-R 127.0.0.1:47126` (SOCKS5) both reached a Mac-local
counting server and came back as `262144` (about 21 s each, roughly 12 KiB/s);
`-R 127.0.0.1:22:…` with ExitOnForwardFailure ended with `Error: remote port
forwarding failed for listen port 22` and exit 255 (prefixed `pi-sh:` by the
logger, unlike ssh); afterwards no listener or helper process was left
(REAL_DEVICE_TESTS.md). Re-run after the throughput change (owner-approved):
1 MiB from a device client through `-R` to a Mac counter in about 1.8 s, `~#`
listing the -R connection, exit 0 under `-tt`; `-R 127.0.0.1:0:…` printed
`Allocated port 43995 …`. Bind addresses (owner-approved): no host, `[::1]`,
`*`, empty host and the LAN address bound exactly the sockets sshd binds with
`GatewayPorts clientspecified` and accepted or refused loopback IPv4/IPv6 and
LAN clients accordingly. Many connections and long sessions remain unexercised
on the device.

## Throughput — 2026-10-28

**Native method (read-only).** One `pi-sh --browser -v -N -L
127.0.0.1:P:speed.cloudflare.com:443` session per connector
(`PI_SH_FORWARD_CONNECTOR`), and curl on the Mac with `--connect-to` to
`https://speed.cloudflare.com/__down?bytes=N` (device to Mac) and a POST of
1 MiB of random bytes to `/__up` (Mac to device). The device only opened an
outbound TCP connection from the in-memory connector: no file, no listener, no
installed tool. Rates are bytes over (total − time to first byte) for the
download and over the total for the upload; single sessions, a few runs each.

| Connector | Device → Mac before | after | Mac → device before | after |
| --- | --- | --- | --- | --- |
| OpenBSD nc | 12.1 KiB/s (512 KiB, 42.3 s) | 17.4 KiB/s (1 MiB, 58.9 s) | 24.5 KiB/s (1 MiB, 41.7 s) | 243 KiB/s (1 MiB, 4.2 s) |
| Perl | 12.2 KiB/s (512 KiB, 42.2 s) | 323–648 KiB/s (1 MiB, 1.58–3.17 s, 5 runs; 4 of 5 above 580) | 24.6 KiB/s (1 MiB, 41.7 s) | 367–470 KiB/s (1 MiB, 2.18–2.79 s, 5 runs) |

**Bottlenecks found.** Both connectors gave the same rates before, so the
adapter was the limit, not the connector. Device to Mac: the adapter's main
loop read the connector's output one byte at a time (`read -t 0` plus
`read -n1`) and hex-encoded each byte with its own `printf`, for every stream
in one process. Mac to device: 1 KiB records with a window of four (4 KiB per
round trip) and a per-pair `\x` loop in the writer. A local benchmark with a
simulated 150 ms round trip reproduced the native 24 KiB/s for the old code.

**Changes.**
- Input records are `I id seq length hex`, at most 2048 bytes, eight
  unacknowledged per stream. The parent reads the body with an untimed
  `read -N` of the declared length, queues it, and hands the writer one
  record at a time, so it never blocks on the writer's pipe; `F` follows the
  last acknowledged record. The writer decodes with one `${hex//??/\\x&}` on
  Bash ≥5.2 (the loop remains for 4.3–5.1), as the command broker does.
- Raw connectors (netcat, ncat, socat) get a reader worker per stream, as in
  the broker: one token per permitted record, a blocking first byte, further
  bytes while `read -t 0` reports data, up to 2048, and one `printf` per record.
  The per-byte read remains (see SHELL_BROKER.md "Why output is still per
  byte"), so netcat downloads stay near 17 KiB/s.
- The Perl connector and the -R listener now speak the adapter's hex records
  themselves: they read hex lines and write `length\nhex\n` records of at most
  2040 bytes, each with one `syswrite` below PIPE_BUF, which the parent reads
  with `read -N`. Bash never touches single bytes on that path. The `-R`
  loader decodes the program from the same hex input.
- Perl is now preferred over netcat. perl-base is Essential on Raspberry Pi OS
  (it cannot be removed without breaking the system) and the Perl path is
  about 35 times faster for downloads; netcat remains the fallback when Perl
  or its Socket module is missing. `PI_SH_FORWARD_CONNECTOR=nc` still selects
  netcat for diagnosis. This reverses the earlier order nc, perl.

**Defect found while measuring.** With the faster path, a `-R` connection's
reply could be lost: pi-sh sends the last data, EOF and close in quick
succession, and the listener closed the socket on `C` before writing what it
had buffered (`TestRemoteForwardingHalfCloseConcurrencyAndAllocation` failed
12 times in 30 runs; 0 in 30 with the slow old path, which hid it). The same
existed for `-L`/`-D`/`-W`: `C` killed the connector while up to a window of
input was still queued in the adapter. sshd drains a channel's output when it
receives `CHANNEL_CLOSE` after EOF; pi-sh now does the same. The listener
writes buffered data and shuts the socket down before dropping it, and a
stream closed after EOF in both directions sends `G` instead of `C`: the
adapter hands over the queued input and lets the connector exit, killing it
after ten seconds at most. `TestAdapterCloseAfterEOFDeliversQueuedInput`
(90 000 bytes after the remote's EOF, then close) received 75–77 KiB of 90 KiB
before and passes after, with both connectors; the -R test passed 30 of 30.

**Local benchmark** (`TestForwardingThroughputBenchmark`, opt-in with
`PI_SH_FWD_BENCH=1`; Bash 5.2.37, real PTY, simulated round trip, one
connection, KiB/s):

| Connector, RTT | Download old → new | Upload old → new |
| --- | --- | --- |
| nc, 0 ms | 98 → 133 | 210 → 524 |
| nc, 40 ms | 81 → 136 | 76 → 316 |
| nc, 150 ms | 24 → 91 | 24 → 96 |
| Perl, 0 ms | 96 → 4648 | 205 → 3960 |
| Perl, 40 ms | 81 → 339 | 76 → 327 |
| Perl, 150 ms | 24 → 99 | 24 → 98 |

With a round trip the window (16 KiB per stream) is the limit; natively the
Perl rates imply a round trip of about 30 ms. **Bounds.** At most eight
records of 2 KiB in flight per stream and direction; 64 streams can therefore
have about 2 MiB of output hex in flight, which the receive goroutine drains
without blocking (it never waits on a consumer). Native `-R` throughput was
measured later the same day with an approved plan (about 1.8 s for 1 MiB).
Single sessions on one network.

## Many streams — 2026-10-28

**Native method (read-only).** `pi-sh --browser -v -N -L
127.0.0.1:P:speed.cloudflare.com:443` with the Perl connector; on the Mac, K
concurrent HTTPS downloads of 256 KiB through P (Python `ssl` with ragged-EOF
detection), each checked for its full length and TLS close_notify.

**Data loss found.** With 16 or more concurrent downloads, most connections
ended with a TLS EOF without close_notify after about 180–215 KB of 256 KiB;
up to 8 were complete. A diagnostic build (uncommitted, reporting only to the
Mac's log) showed the device's socket reads ending in `Connection reset by
peer` for exactly those connections. The connector read the socket only as
fast as the adapter took its records (16 KiB in flight per stream), so the
server's data waited in the device's socket; the server, having sent
everything and closed, reset the connection and Linux dropped the unread tail.
Directly from the Mac, the same server delivered 256 KiB and 8 MiB in full even
to a reader that stalled 30 s, so the slow drain was the trigger. sshd does not
hit this because it reads the socket into a channel window of up to 2 MiB.

**Fix.** The Perl connector reads the socket as data arrives and queues up to
2 MiB per stream in memory while the adapter is busy, writing records to a
non-blocking stdout (each below PIPE_BUF, so still whole).
`TestPerlConnectorDrainsWhileReaderStalls` (a 1.75 MiB target write must finish
while the local reader stalls) fails without it. The macOS kernel keeps
received data readable after a reset, so the loss itself cannot be reproduced
on the Mac. Netcat cannot buffer in memory, and Bash reads its output one byte
at a time, so with netcat the drain stays slow; it now asks for a 4 MiB TCP
receive buffer instead (`-I`, capped by the device's `net.core.rmem_max`, 4 MiB
on Raspberry Pi OS), so a server can hand over its whole reply and close
normally. Natively with `PI_SH_FORWARD_CONNECTOR=nc`: 3 of 48 downloads were
truncated without it (16 and 32 concurrent), 0 of 80 with it (16, 16, 32 and
16 concurrent; about 58 KiB/s in total, the netcat rate). A reply larger than
that buffer can still be cut if the server resets after closing; Perl, which
is Essential on Raspberry Pi OS and chosen first, has no such limit.
`TestNetcatRequestsLargeReceiveBuffer` checks the argv.
Worst case memory on the device is 2 MiB per stream (128 MiB for 64 stalled
streams), as for sshd's windows.

**Aggregate rate.** The adapter handled one record from the carrier per round
and then polled every stream, so each window update cost a poll of all streams
and the total rate fell with their number. It now handles up to 64 waiting
records per round. `TestForwardingConcurrencyBenchmark` (opt-in, 40 ms round
trip, 256 KiB per stream): 16 streams 1.9 → 3.7–4.1 MiB/s, 64 streams 0.6 →
4.1–4.3 MiB/s, one stream unchanged (about 325 KiB/s).

| Concurrent downloads (device) | Before both fixes | Connector fix only | Both fixes |
| --- | --- | --- | --- |
| 1 | — | — | 327 KiB/s, complete |
| 8 | 345 KiB/s, 8 of 8 complete | — | — |
| 16 | 225 KiB/s, 2 of 16 complete | 222 KiB/s, 16 of 16 | 635 KiB/s, 16 of 16 |
| 32 | — | 132 KiB/s, 32 of 32 | 630 KiB/s, 32 of 32 |
| 64 | 105 KiB/s, 0 of 64 complete | 72 KiB/s, 64 of 64 | 574 KiB/s, 64 of 64 |

**Limit.** A 65th connection while 64 are open waits for a free stream (it is
not refused): it completed its download after one of the 64 ended. ssh has no
such limit.

## End of session — 2026-10-28

**Reference (local OpenSSH 10.2p1 sshd, single runs).** When the command or
shell ends while forwarded connections are open, ssh keeps running until every
forwarded connection (-L, -D or -R) has closed, then exits with the session's
status (7 after `exit 7`, 8 s after the end with a connection held 8 s). It
prints nothing while it waits: the "Waiting for forwarded connections to
terminate..." message belonged to SSH-1 and is absent from the 10.2 binary
(`strings /usr/bin/ssh`). The listeners stay open and a connection made during
the wait is still forwarded (it received the sshd banner). stdin is no longer
read and the terminal leaves raw mode, so `~#` or `~?` typed then are only
echoed, never interpreted. SIGTERM, SIGINT or SIGHUP during the wait ends ssh
at once with 255 (with -tt it prints `Connection to … closed.`). The same wait
happens with -T. `~.` during the session exits 255 without waiting.

**pi-sh.** After a session that ended by itself (a status, or the shell
closing), pi-sh releases the session's channel and serves forwarded
connections until the channel table has none (`forward.Channels.WaitIdle`);
listeners stay open and new connections are forwarded and counted. The
session's input workers have already been joined and the terminal restored
before the wait, so nothing reads stdin. A termination signal ends the wait
with 255. `~.` and transport failures do not wait. A failure of the forwarding
carrier during the wait is reported and ends the wait, keeping the session's
status. Debug level logs `session ended; serving forwarded connections until
they close connections=N`.

**Tests.** `TestSessionEndWaitsForForwardedConnections` (CLI over local Pion
peers: a held -L connection keeps the wait open, a connection made during the
wait is forwarded, the wait ends after both close, a cancelled context ends it
with false; `~.` and transport failures do not wait),
`TestRemoteForwardingListsChannels` (a -R connection leaves the table when it
ends). **Device (2026-10-28, read-only, REAL_DEVICE_TESTS.md):** `-T -L` to the
device's sshd banner with `sleep 2; exit 7` and a connection held 10 s exited 7
after 10.1 s; with SIGTERM 6 s after start, 255 at 6.0 s; nothing left running.

**Difference.** ssh prints `Connection to HOST closed.` at the end of a -tt
session at Info level; pi-sh does not print it (a general difference, not
specific to forwarding). Waiting on -R connections on the device needs an
approved listener.
