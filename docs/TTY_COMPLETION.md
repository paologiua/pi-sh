# TTY completion and pending input — 2026-10-21

## Subsequent recurrence: receive-frame exhaustion identified

The later full-suite failure is now reproduced and corrected locally. It printed
fixture-ready and seen:x but returned peer/closed errors and missing status23.
Two hundred single-processor repetitions passed before correction, whereas four
concurrent test processes reproduced the failure. These observations do not
implicate the coincident Go/security-dependency updates.

Temporary fixed-marker observations separated emitted TX, successful Send,
parsed TX/EOF and queue-overflow categories. The adapter emitted TX23, but the
fixture's Send failed after its receiving channel had already exhausted the
4096-message queue. No TX arrived at the parser. The second200-execution stress
batch recorded four such overflows. The old input path sent each rune in its own
text message:64KiB of x could become65,536 messages while the command read one
byte and the fixture's PTY writer stopped making progress. This exceeded the
frame bound well before the256KiB byte bound. The parser correctly rejected EOF
without TX; the one-second input-error drain cannot recover a status never sent.

The input filter now combines only already-buffered valid UTF-8 into messages of
at most4096 bytes. It flushes before any read could block, including an incomplete
next rune, so individual keys do not wait for batching timers. Pending bytes are
also flushed before local help/suspension/disconnect and at EOF/parse-error
boundaries, retaining the old byte ordering. Each sent slice stays immutable.
A64KiB ASCII paste from a buffered reader uses16 messages rather than65,536.
Line-start escapes, controls, invalid-UTF8 refusal and stdin EOF policy are
unchanged. No remote script, result frame, acknowledgment, queue limit, timeout
or result-validation rule changes.

TestBufferedPasteDoesNotExhaustReceiverFrameBudget first failed deterministically
on the old code at4096 accepted messages. New tests retain exact Unicode/control
bytes, bounded valid text frames, immediate valid input before a partial rune,
EOF/unknown-escape/error boundaries and ordering before local help/suspension.
The existing input/escape/terminal-restoration tests remain enabled.

After correction,200 instrumented concurrent executions delivered all200 TX23
results with zero overflows. After removing all diagnostics, four processes each
ran30 positive and30 no-status cases:120 expected23 completions and120 required
failures passed. The full final Go race suite, nested SFTP parser tests, root vet,
module verification and all seven CGO-disabled build/test-compilation targets
pass. Native binaries are rebuilt without diagnostic code; Linux/386 softfloat
build success is not physical iSH evidence.

This closes the observed local recurrence. Arbitrarily large unread input can
still exceed byte/backpressure limits; resource exhaustion remains an explicit
failure. Actual Pi agent queue policy and new coalesced-paste behavior need native
verification. The older uninstrumented native close error remains unclassified:
this local mechanism cannot retrospectively prove its cause. Existing native
proof below still covers only its recorded one-byte/2048-input scenario.

A TTY command may finish before consuming all local stdin. A failed send of the
remaining bytes must not override its independently verified completed result.
Conversely, a send failure alone cannot supply any remote exit status.

## Reproduction and cause

The actual CLI/Pion/PTY coordinator runs a raw, echo-disabled command that reads
one byte, prints seen:x and exits23. The context-aware fixture input waits for the
command's own readiness marker, then supplies64KiB. This excludes stty startup
flushes, canonical-line overflow and terminal echo from the failure signal.

Before the correction the reduced race test failed six of ten repeated race
runs, with WebRTC peer/closed errors and missing-cleanup evidence instead of23.
Temporary fixed-marker instrumentation then showed TX23 had arrived, sometimes
followed by normal EOF, while TTYPlan.Run selected inputError and cancelled the
reader. The original large canonical-input attempt was reduced to this raw
scenario;512 residual bytes did not keep the sender active long enough to trigger
it. The phase-boundary markers are removed from source and native binaries.

The identified reference is installed OpenSSH_10.2p1 / LibreSSL3.3.6.
TestOpenSSHCommandTTYCloseWithUnreadInput reads one byte from2048 supplied bytes
and retains remote23. It uses only the local ephemeral sshd fixture, not the Pi.

## Arbitration

TTYIO.IsInputTransportError is an optional caller policy identifying only a
failed carrier send. The CLI recognizes transport.ErrPeer/ErrClosed. The adapter
has no dependency on Pion, local terminal APIs or the source of authentication.
With no policy, other callers retain the prior immediate input-error behavior.

When that failure wins the select, the independent result reader stays alive for
at most one second, shortened by context cancellation. A completed parser result
can then win: it still requires a valid TR, exact bounded TX status and clean EOF.
Malformed/missing status, extra bytes after TX, failed output, non-EOF transport
error, timeout and cancellation remain errors with unconfirmed cleanup. No bytes
or commands are replayed, and no new remote message/acknowledgement is introduced.
Local input, help, suspend and restoration failures are not classified as transport
send failures. Outer commandrun authorization/cancellation checks retain priority.

The existing StopIO/join cleanup follows arbitration. Backpressured pollable local
output is still cancelled on return; Windows/generic I/O and filesystem-kernel
latency limits are unchanged. The one-second drain is a conservative bound, not a
guarantee that every interrupted network can deliver a result. Normal completion
does not prove detached-descendant or agent teardown. Binary no-TTY and SFTP
streams use separate completion rules and are not changed by this TTY policy.

## Evidence

- The reduced CLI/Pion/PTY success case passes20 consecutive race runs after the
  correction, without debug instrumentation.
- The companion negative coordinator case closes the peer after its one-byte
  read/output but before command completion: no valid status is invented.
- Drain tests cover status0/23, missing/malformed result, output failure, silence
  and cancellation with queued success. Reader failures retain both causes and
  the cleanup boundary. Focused positive/negative/reference tests pass five runs.
- Native browser-companion testing with the final binary sends2048 x bytes to a
  transient Bash command on the Pi that reads exactly one byte, emits its fixed
  marker and exits23. The CLI returns23. Only the outcome/count is retained.

Final validation passes the full Go race suite, vet and CGO-disabled builds and
all test-package compilations on macOS amd64/arm64, Linux amd64/arm64/386 softfloat
and Windows amd64/arm64. All three native clients are rebuilt. Cross-compilation
is not runtime proof for Linux/Windows/iSH.

The older single native post-input error was not instrumented: this mechanism is
consistent with it, but the evidence cannot retrospectively prove its cause.
Other WebRTC-close failure causes remain possible. Native extended transport,
revocation, detached groups and physical iSH still require their own checks.
No Pi file, software, persistent configuration or service was changed; no transfer
write path was approved. See REAL_DEVICE_TESTS.md for separate native evidence.
