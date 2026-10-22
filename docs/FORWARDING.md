# Forwarding investigation — 2026-10-20

No forwarding option is implemented or accepted yet. The observed Connect
interface supplies a shell PTY and resize/control events; no native TCP/Unix
forwarding request has been observed. A working SFTP server process over the
binary broker does not establish an agent socket channel.

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
