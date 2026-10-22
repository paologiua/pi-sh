# Agent exit-status investigation

Research date: 2026-10-20. Scope: public official material for Raspberry Pi
Connect, with package version **2.13.0**, matching the version previously observed
on the authorised device. This investigation did not access the Pi or browser,
run the upstream agent, inspect executable instructions or copy its implementation.

## Result and confidence

**No public agent source was located. Native shell exit-status support remains
unverified, rather than proved absent.** Consequently we cannot substantiate how
this version starts its shell, invokes a PTY library, calls `cmd.Wait`, obtains
process status or decides to close a WebRTC channel. We must not describe those
internal implementation details as facts.

The [official Connect documentation](https://www.raspberrypi.com/documentation/services/connect.html#remote-shell)
describes a fresh shell per connection, the `CONNECT_TTY` environment variable,
and ending a session with `exit` or by closing its browser window. It does not
specify a shell wire protocol, an exit-status message, native exec, a no-PTY
request, separate output streams or an extension negotiation mechanism. This is
absence of a documented contract, not proof that none exists internally.

## Versioned public evidence

The official [trixie arm64 package index](https://archive.raspberrypi.com/debian/dists/trixie/main/binary-arm64/Packages.gz)
listed `rpi-connect` and `rpi-connect-lite` at 2.13.0. The downloaded compressed
index SHA-256 was
`821d8144b643cb9815b83b7a608f8b3dca7f4a05c598b051ef74807a556ad482`.

The [official 2.13.0 arm64 package](https://archive.raspberrypi.com/debian/pool/main/r/rpi-connect/rpi-connect_2.13.0_arm64.deb)
matched the index's SHA-256:
`cfdd07b70f7ea113b53ce9bfe87236913602dd675a9fea67217bda7248893894`.
Only its copyright and changelog text were read on the development Mac. The
changelog dates this release 2026-09-23. Its copyright metadata attributes the
upstream work to Raspberry Pi Ltd under BSD-3-clause, and separately lists
third-party notices. This establishes the examined package's licence declaration;
it does not publish its source or license the hosted service/web client. No
upstream assets or package files were added to this repository.

The official [trixie main source index](https://archive.raspberrypi.com/debian/dists/trixie/main/source/Sources.gz)
contained no `Package: rpi-connect` or `Package: rpi-connect-lite` stanza.
Its compressed SHA-256 was
`5c818bd1f7ac170352f84525fab13d43afea25e1df4e00a42c5fb55a7c86d1a3`.
These indexes are mutable snapshots, not permanent revision identifiers. They
were fetched over HTTPS; this investigation did not independently verify an APT
Release signature and does not claim such verification.

In the [official forum discussion about source availability](https://forums.raspberrypi.com/viewtopic.php?t=377382),
Raspberry Pi engineer `clowder` answered on 2024-10-02 that source was not
available. On 2024-10-03, Raspberry Pi CTO Gordon Hollingworth (`gsh`) explained
binary redistribution under the BSD licence; on 2024-10-04 he confirmed that
there was then no source-release plan. Those are historical first-party
statements, not a guarantee of the 2026 source-release policy.

The guessed [raspberrypi/rpi-connect repository](https://github.com/raspberrypi/rpi-connect)
returned HTTP 404 during research. A guessed repository name and an absent source
index stanza cannot exclude publication at a different location. No agent source
revision or tag could therefore be examined or cited.

## What the existing session evidence actually establishes

The project's earlier authorised [protocol observations](PROTOCOL.md) and
[real-device tests](REAL_DEVICE_TESTS.md) establish the client-created `shell`
channel, agent-created `resize` channel, text JSON dimensions, interactive PTY
behaviour and channel closure after `exit`/empty-prompt Ctrl-D. They did not yield
a distinct process exit-status message. This research adds no new live-session
evidence and does not promote the observed channel list to an exhaustive list of
everything the agent could implement.

| Question | Supported conclusion |
| --- | --- |
| How is the PTY/shell process started? | Internal implementation unavailable; externally observed shell is a TTY. |
| Is `cmd.Wait` called or its result discarded? | Unknown; do not infer from a dependency list or connection closure. |
| Is there a native status/signal channel? | None observed by existing tests; absence not proved from source. |
| Does native exec/no-PTY already exist? | No documented or observed contract identified; unsupported in pi-sh. |
| Does channel closure imply status zero? | No process result is established by closure alone. |

## Practical next step

Keep interactive sessions honest: retain unknown remote status and an explicit
diagnostic on closure. Do not install a shell exit trap or silently change the
interactive shell's lifecycle.

For an explicitly selected experimental command mode, investigate a client-owned
in-memory `/bin/sh` wrapper over the existing shell stream. A fresh random marker
and a validated status frame can report the result of its child command without
requiring an agent update or remote file. This would be pi-sh framing, **not
native Connect exec or native agent exit status**. Missing, truncated, malformed
or out-of-order completion must fail with an unknown status, never default to
success. Reject unsupported interaction rather than suggesting SSH equivalence:
PTY stdout/stderr merging, newline translation, stdin handling, startup output,
echo, signals and commands that kill their supervising shell require tests and
documented differences. A random marker avoids accidental collisions; it is not
an authenticated boundary against a command that can inspect its environment.

The exact blocker for source-level confirmation is the missing public source or
protocol specification for agent 2.13.0. The required external action is to obtain
an official public source URL/revision or an authoritative shell protocol/status
description from the Connect maintainers. That does not block independently
implemented, authorised experiments through the already observed shell channel.
Do not generalise these findings to future versions; repeat the check when the
device version changes.
