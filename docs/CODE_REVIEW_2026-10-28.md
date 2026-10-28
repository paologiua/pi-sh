# Code review — 28 October 2026

The whole tree was reviewed in four areas (forwarding; command execution and
terminal; authentication, transport and diagnostics; transfers and
configuration), reading the code for bugs, races, leaks, error handling,
security (device data treated as untrusted, no secrets in logs) and
consistency. Every finding below was checked against the code before acting.
Each fixed defect has a test that fails without the fix (shown red by
reverting only the fix, unless noted) and passes with it.

## Fixed

| Area | Defect | Fix | Test |
| --- | --- | --- | --- |
| Auth/forwarding | Lost authorization (direct periodic check or browser heartbeat) was never read in `-N`, `-W` or the end-of-session wait, so forwarding kept serving; and preparing a second Connect session (the command next to the forwarding carrier) closed the first connection's authorization watcher | `directSource` keeps the watchers of connections whose exchange succeeded and merges their failures into one stable channel (only abandoned attempts are closed); `runForwarding` and `await` end with 255 and `session authorization check failed: …` | `TestDirectSourceKeepsWatchersOfUsedConnections`, `TestForwardingEndsOnLostAuthorization` |
| Forwarding | Read on a stream without a readiness marker (netcat to a Unix socket) closed before any output blocked forever: `d` closed `events` but not the exit wait, so `Pipe`, `Serve` and `stop` hung | `finish` closes a `goneCh` that `eof` also waits on | `TestReadAfterCloseBeforeOutputReturns` |
| Forwarding/security | The -R listener's peer text from the device reached `~#` unsanitized (terminal escape sequences) | the peer must be an IP address and a port (`parsePeer`), else protocol error | `TestParsePeerRejectsDeviceText` |
| Forwarding | The adapter's `Z protocol` failure was reported as the generic protocol error | `Z` is checked before field parsing; unknown reasons are not echoed | `TestAdapterFailureRecordIsReported` |
| Forwarding | A carrier or adapter failure during a session was never reported, and `stop` then said nothing | `forwarding.watch` reports it once (`reportFailure`), also from `await` | `TestForwardingCarrierFailureIsReported` |
| Forwarding | Staging and connector detection had no bound and ignored signals | `fwdbroker.Start` takes a setup context and a 30 s bound | `TestStartIsBounded` |
| Forwarding | Every Listen error was labelled "remote listener unavailable" at error level (ssh prints only the warning) | only the missing-Perl refusal wraps `ErrNoRemoteListener` | `TestRemoteListenRefusalIsOnlyAWarning` (written after the fix; the old code logs the error line it forbids) |
| Broker/TTY | Staging (bootstrap, loader, upload) waited without bound; a TTY session sat with the local terminal raw | `stage` is bounded by one minute (`ErrStageTimeout`) | `TestStagingIsBounded` |
| Terminal | `~.` in `--legacy-shell` exited 0; ssh and the TTY path exit 255 | `waitSession` returns `errEscape` | updated `TestInteractive…` escape cases, `TestSessionEndReasons/local-disconnect`, `TestSuspendTTYSessionOverWebRTC/interactive-*` |
| Browser bridge | An offer taken by a poll that had already ended was lost; an answer left by an abandoned exchange was handed to the next offer | undelivered offers are kept for the next poll; each exchange discards stale answers and offers | `TestUndeliveredOfferAndStaleAnswer` (written after the fix) |
| pi-sftp | `get docs` / `put x` without a target wrote into an existing directory of that name (`docs/docs`); OpenSSH fails | a destination derived from the source name is never a directory to write into | `TestGlobCommandsAgainstLocalOpenSSH/get_r/l`, `/put_r/l` (differential with OpenSSH sftp) |
| pi-cp, pi-sftp | `-A` and `-J` were refused with the X11/tunnel reason | the agent-forwarding and ProxyJump reasons used by pi-sh | `TestSCPOptionDecisions`, `TestSFTPOptionDecisions` |
| pi-sftp | `-a` help claimed prefixes are compared | help says the prefix is trusted, as documented | help text (no behavior change) |

## Found on the device after the review

| Area | Defect | Fix | Test |
| --- | --- | --- | --- |
| Transport | When the device agent ended a session (SCTP ABORT, User Initiated Abort), Pion ran OnError and OnClose concurrently; if OnError won, a finished command exited 255 with `WebRTC peer operation failed` instead of its status (seen natively in 3 of 6 runs under -tt with forwarding) | a User Initiated Abort ends the channel with EOF; other errors stay `ErrPeer` | `TestPeerCloseEndsChannelWithEOF` |
| pi-cp | With several download operands, the first failing one stopped the copy; scp reports it and copies the rest (exit 1). A missing local upload source exited 1; scp ends with 255 | continue after a failed download operand (a lost connection still stops); `ErrLocalSource` exits 255 | `TestFailingOperandDoesNotStopTheRestLikeSCP` (differential with OpenSSH scp) |
| Browser bridge | The recent sign-in and companion advice told pi-cp and pi-sftp users to "restart pi-sh" | program-neutral "run the command again" | `TestFailureAdviceNamesNoProgram` |
| Forwarding | With 16 or more concurrent downloads, servers reset connections whose data sat in the device's socket and the unread tail was lost (0 of 64 complete) | the Perl connector reads the socket eagerly into up to 2 MiB per stream, as sshd's window | `TestPerlConnectorDrainsWhileReaderStalls` |
| Forwarding | One carrier record per adapter round: the total rate fell with the number of streams (72 KiB/s for 64 on the device) | up to 64 waiting records per round (574 KiB/s for 64) | `TestForwardingConcurrencyBenchmark` (opt-in; timing is not asserted) |
| Forwarding | The same truncation with the netcat connector (3 of 48 concurrent downloads), which cannot buffer in memory | netcat requests a 4 MiB TCP receive buffer (`-I`); 0 of 80 natively | `TestNetcatRequestsLargeReceiveBuffer` (argv; the reset cannot be reproduced on macOS) |
| Forwarding (-R) | With 64 connections open, the listener accepted a 65th and closed it at once (EOF, no data); -L makes it wait and sshd has no limit | the listener stops accepting at 64, so further connections wait in the backlog | `TestRemoteForwardingConnectionBeyondLimitWaits` |

## Recorded, not changed

- A put that fails right after exclusive creation (mask not honored, fchmod
  refused) leaves the empty private file. This is the existing design, asserted
  by `TestUploadPermissionFailurePrecedesContents` and
  `TestTransferCreationPermissionsAgainstOpenSSH`; removing it was tried and
  reverted. A retry then takes the truncating path for that file.
- Invalid UTF-8 typed in a TTY session ends the session (documented limit of
  the text input path; 8-bit Meta and Latin-1 pastes are affected).
- A no-TTY stdin read error is reported as a generic failure (OpenSSH treats it
  as EOF); with a racing `K` it may be silent. Low impact, open.
- After a broker `Z`, abort waits two seconds and reports cleanup unconfirmed
  although the broker's EXIT trap ran. Distinguishing it needs a new record.
- In TTY mode the session marker is in the remote supervisor's argv, so a
  command running as the same user could forge a result record. Same-user code
  is trusted by the threat model (SECURITY.md).
- Before the command starts (`TR`), Ctrl-C and `~.` are not read in a TTY
  session; the new staging bound limits this to one minute.
- Heartbeat margin: Connect latency above about 30 s, or a throttled hidden
  tab, can expire the 90 s browser heartbeat. Not observed; open.
- A failure exactly at the ConnectTimeout deadline is reported as a timeout
  and may be retried (direct login, rare).
- Passwords and codes are copied into Go strings that cannot be cleared;
  memory only, nothing persisted.
- The redaction backstop removes long opaque tokens only if they contain a
  digit; no call site passes such a token.
- A cancelled login prompt may leave a terminal reader until the process exits
  (it exits after a failed login).
- Sorted `ls` keeps the whole listing in memory, as OpenSSH does; `ls -f` is
  bounded. After a lost connection, a recursive upload's final directory
  metadata step only warns (the session close still fails the transfer, not
  verified). The console's stdin reader outlives cancellation (the CLIs exit).
- `rconn.SetWriteDeadline` is a no-op; when a connector stops reading, `Pipe`
  aborts both directions and can drop queued output.
