# Command cancellation and local signals — 2026-10-20

The experimental Bash broker now requests and checks abnormal cleanup before
the CLI closes its carrier. This adds no agent protocol, installed remote code,
files or dependencies. All broker changes remain in process memory.

## Behavioral contract

Unix SIGINT, SIGTERM, SIGHUP and SIGQUIT cancel pi-sh and produce local failure
255. They are not translated into arbitrary remote signal requests. Interactive
TTY control bytes remain a separate path; binary stdin bytes, including 0x03,
remain data for the no-TTY broker command.

This distinction follows the termination handler in pinned
[OpenSSH_10.2p1 clientloop.c](https://github.com/openssh/openssh-portable/blob/V_10_2_P1/clientloop.c).
The independent loopback reference test observes all four local signals ending
the OpenSSH client with 255, even when its remote command traps INT/TERM.
That test does not establish every server-side hangup or descendant behavior.

## Broker abort handshake

After successfully sending the command-launch commit, every failing Run return
attempts a `C` cancellation record. This includes cancellation before readiness
is received, auth loss through the coordinator, local stream errors and protocol
failure. The command's original failure is retained.

The Bash parent terminates its owned command process group and the startup
worker, kills/reaps its direct helpers, waits for the direct command worker and
then sends `D`. Both records belong to pi-sh's own application framing inside the
existing shell channel. `D` is cleanup acknowledgement, never a command exit
status or native Raspberry Pi agent response. It covers the owned process group,
not descendants that detach into other sessions. Orphaned descendants may remain
zombies until reaped by the OS; the broker cannot wait for nonchildren.

The receive pump remains active for up to two seconds during abort. Its send
uses a separate bounded context, while normal command traffic remains subject
to cancellation. Late output is drained without writing to caller streams or
being treated as successful command completion. An absent acknowledgement,
carrier loss or timeout adds `remote broker cleanup was not confirmed`.

The CLI owns the broker peer's closure and retains that peer until Run finishes
or its three-second coordination grace expires. It then closes the peer on all
paths. Authorization failure starts cleanup; it never authorizes another command
or replays the failed one. Cleanup-only framing is allowed during that bounded
grace. Other CLI modes retain their previous peer-cancellation policy.

Before command launch is committed, no command child should yet exist and the
startup carrier is closed on failure. Transport loss cannot guarantee delivery
of cancellation. An acknowledgement is not possible after carrier closure;
missing evidence must not be described as verified remote cleanup.

## I/O and platform limits

Stream implementations must honor their context. A caller-owned blocking stdin
reader or output writer is not forcibly closed by the library. The CLI's outer
coordination bound prevents a stuck writer from keeping the dedicated process
alive indefinitely after cancellation, but cannot prove remote cleanup in that
case. Embedded users must provide cancellable I/O and manage file ownership.

Windows currently maps only os.Interrupt to cancellation; console close/logoff
remains unverified. Cross-build success, including CGO-disabled Linux/386, is
separate from native Windows/Linux and physical iSH runtime proof.

## Evidence

- `TestShellBrokerCancellationReapsBeforeCarrierClose`: real local Bash/PTY,
  command and descendant ignoring ordinary termination, cancellation after a
  marker following 128 KiB output while stdin exceeds pipe capacity;
  acknowledgement, reaped leader and no running known descendant are
  checked while the carrier remains open. Test-only process inspection uses ps
  locally, not on the Pi or as a client runtime dependency.
- `TestShellBrokerCancellationDuringWorkerStartup`: five real PTY starts,
  cancelling before user output; confirms cleanup without hanging on a worker
  that has not yet entered setsid.
- `TestBrokerAbortWhenCancelledBeforeReady`: launch/readiness cancellation race.
- `TestBrokerAbortAfterLocalFailure`: confirmed cleanup, carrier loss and silent
  timeout; preserves the local writer error and records unconfirmed cleanup.
- `TestCoordinatorWaitsForBrokerCleanup`: cancellation and synthetic auth loss
  wait for cleanup, retain missing-ack diagnostics and never become command status.
- `TestCoordinatorBoundsUnresponsivePlan`: a plan modeling blocked caller I/O
  cannot keep the coordinator waiting indefinitely or claim confirmed cleanup.
- `TestConnectRetainsPeerUntilBrokerAbortAcknowledged`: real local Pion peers and
  a synthetic broker peer verify the actual connect/coordinator/abort path.
  This is not a Connect HTTP/authentication or remote-agent emulator.
- `TestOpenSSHLocalSignalsTerminateClient`: four actual local OpenSSH client
  signals against a temporary loopback SSH server, ephemeral pinned keys and
  open stdin. The existing fourteen stream/status comparisons remain separate.

The reference signal test exposed a race in its inherited test buffer: embedding
bytes.Buffer promoted ReadFrom and bypassed the mutex. Composition now ensures
os/exec uses the synchronized Write method; the corrected test passes under race.

The owner subsequently completed the direct-login read-only check below: marker,
Ctrl-C, context-canceled diagnostic and exit 255, with no unconfirmed-cleanup
warning. This is owner-reported native path evidence, not independent remote
process inspection. Auth revocation and network loss remain unverified. No Pi
package, file, configuration, service or reboot was involved.

## Owner-run check

The rebuilt binary can be checked through direct login without browser automation:

```sh
./bin/pi-sh --direct-login --experimental-command -nT raspberrypi 'printf "cancel-ready\n"; sleep 60'
# After cancel-ready, press Ctrl-C, then:
echo $?
```

Expected local cancellation status is 255. Record only the fixed diagnostic and
whether missing-cleanup confirmation was reported; do not record login values.
The command creates no file and only prints a marker/sleeps. The owner reported
the expected result above; raw login output and identity values are not retained.
