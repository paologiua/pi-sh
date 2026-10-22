# Native WebRTC transport

The CLI text-input filter now batches only locally available UTF-8 into at most
4KiB messages, flushing before a read can block (including a partial next rune),
local actions and terminal/error boundaries. A64KiB paste no longer produces
65,536 frames and exhausts a stalled peer's4096-frame queue before status delivery.
The queue's byte/frame limits and strict result rules are unchanged; no input or
command is replayed. This has local stress/latency/escape evidence only. See
TTY_COMPLETION.md; old native close failures and arbitrary unread-input capacity
remain separate limits. No remote file, software or configuration change occurs.

`internal/transport` is an implemented, protocol-neutral Pion WebRTC adapter.
It uses standard WebRTC APIs, not derived Connect endpoint or shell definitions.
No service, device, user identity, channel name or ICE server is hard-coded.

## Contract

`New` owns a peer under a context. `CreateChannel` requests a reliable ordered
channel; `Accept` returns incoming channels with their original labels. A signaling
coordinator can call `Offer`, `Answer` and `ApplyAnswer`, passing the sensitive
descriptions directly in memory. Negotiation is single-shot and callers serialize
it. Candidate gathering produces a complete description; trickle ICE,
renegotiation and ICE credential renewal are not implemented in this adapter yet.

`Channel.Send` and `Receive` preserve message boundaries and the text/binary flag.
They do not claim stream, terminal, command, status, file or forwarding semantics.
Text encoding validation belongs to a future session codec. A successful send
means local queuing, not remote delivery or processing. Context cancellation
interrupts waits; cancelling the owning context closes the peer and all channels.

## Security and resource policy

- Use Pion's default DTLS fingerprint verification and fresh default certificates.
  No caller-facing setting disables cryptographic verification or logs keys.
- Suppress upstream logging, including environment-enabled debug output. Return
  fixed errors rather than exposing descriptions or credential-bearing errors.
- Bound received SDP to 256 KiB, incoming messages to 64 KiB (also advertised via
  Pion's SCTP setting), and application sends to 16 KiB each.
- Bound outstanding local sends to 256 KiB per channel; wait for the low-water
  notification with context cancellation and serialize senders.
- Limit the peer to eight channels over its lifetime and each application receive
  queue to 4096 messages and 256 KiB of payload. Both bounds apply: small PTY
  fragments must not exhaust a count-only 32-message queue, while large messages
  must not multiply its memory cost. Overflow closes the channel with an explicit error after
  queued messages are drained. This is a provisional local resource policy, not
  a Connect or OpenSSH limit, and does not bound every Pion/internal allocation.
- Close is idempotent and safe in upstream callbacks. It uses Pion's non-graceful
  close, so it does not promise that every upstream callback has already exited
  when it returns.

Before integration with a terminal, assess slow-output handling and resource
limits against observed traffic. Queue overflow is never silently treated as
successful output. No limits should be advertised as proven Connect-compatible.

## Verified locally

Tests use only IPv4 loopback ICE candidates and no ICE servers. They exchange
offers/answers in memory, exercise real ICE/DTLS/SCTP data channels, and do not
write SDP or session captures. Cases include bidirectional Unicode/text and binary
messages, empty messages, 16 KiB messages, sustained ordered traffic, remote close,
owning and per-send cancellation, malformed/oversized descriptions, resource
limits and a deliberately mismatched DTLS fingerprint. Application queue-overflow
tests inject a message callback separately and are not represented as a network
test. Tests under `-race` check the concurrent callback code.

This proves only local Pion-to-Pion behavior. Browser interoperability in the local peer tests, forced STUN/TURN/relay, IPv6,
network-failure recovery and iSH runtime remain unverified. The same adapter now
powers the real-device native shell in `cmd/pi-sh`; see REAL_DEVICE_TESTS.md.
The selected direct/relay candidate route was not retained in those checks.

Primary implementation references:
[Pion v4.2.24 API](https://pkg.go.dev/github.com/pion/webrtc/v4@v4.2.24),
[Pion source](https://github.com/pion/webrtc/tree/v4.2.24), and
[WebRTC specification](https://www.w3.org/TR/webrtc/).

## Small-fragment regression — 2026-10-20

A callback fixture enqueues 2048 one-byte messages (only 2 KiB) before draining.
It failed at byte 32 under the old count limit and passes with the new dual bound.
A separate fixture checks the byte budget and its release after draining. These
are deterministic local regressions, not proof that queue overflow caused the
partially received larger native broker fixture. The corrected native rerun
preserved 65536-byte stdin/stdout and 131072-byte stderr exactly; it verifies the
new queue on that route/workload, not every traffic pattern.
