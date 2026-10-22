# SFTP flush requests — 2026-10-20

pi-sftp now accepts top-level `-f` for all transfers and per-command `get -f` /
`put -f`. Repeated switches/clusters are accepted, including `-fb-` (flush with
stdin batch) and `get -ff`. `--` ends command options, so `get -- -f local-file`
requests the literal remote filename `-f`. Other transfer command flags remain
rejected before file I/O. These are requests matching the tested OpenSSH model,
not a durable-storage guarantee.

## Behavior

Downloads call the open local destination's File.Sync after a successful copy,
before closing it. Uploads use the open remote handle's fsync@openssh.com request
only when the server advertised extension version 1. An absent/unrecognized
version is skipped silently. Like the selected OpenSSH reference, an upload
attempts an available flush even after a data-write error; that error remains
fatal. Downloads do not sync after a failed copy.

Local sync errors and received remote fsync failure statuses produce a diagnostic
without independently making an otherwise successful transfer fail. Tests cover
SFTP unsupported, permission-denied and EOF statuses. This matches OpenSSH's
advisory error treatment; exit 0 alone does not prove fsync succeeded or occurred.
The CLI prints only fixed error classes or numeric statuses, never remote text,
packet contents or transfer paths in these diagnostics.

Data errors, malformed protocol replies, connection loss and failed destination
close remain errors. A destination is always closed; copy errors cannot be
erased by an advisory sync result. pkg/sftp normalizes protocol EOF status and
stream EOF to the same error. A successful later CLOSE reply is therefore needed
to finish successfully; a dropped connection fails that requirement. Final
server/broker completion and cancellation checks remain in place too.

Only the file is synced, not a parent directory. No atomic replacement,
crash-recovery, device/cache/power-loss or whole-filesystem guarantee is made.
Failed copies may leave partial/truncated files as documented in SFTP_BACKEND.md.
The default remains no flush request. pi-cp gains no new flag or flush policy.

## Evidence and limits

- Fourteen local OpenSSH CLI comparisons use a test-only in-memory server over
  stdio: absent extension; advertised unsupported/permission/EOF status failures;
  dropped/malformed reply; and data-write failure. Each runs with command or global
  -f. Successful cases verify every byte value across an 8192-byte round trip.
  Failed writes remain failed while still attempting the advertised flush.
  These compare success/nonzero outcome, diagnostics and data, not complete
  numeric connection-failure exit-code parity through Connect/authentication.
- Matching Go-client fixtures verify advisory diagnostics, redaction of a
  synthetic secret server string, data integrity and fatal protocol loss.
- An actual existing local OpenSSH sftp-server verifies successful negotiated
  fsync. Its transient debug stream records all writes, then fsync, then CLOSE.
  With server -P fsync, advertisement is absent and no request is sent. The debug
  data stay in test memory; no account/production session capture is retained.
- Fault fixtures verify local sync/close ordering, advisory policy, retention of
  original copy errors and propagation of close errors. This is synthetic failure
  evidence; actual disk-full, failing local disk flush and power-loss tests are
  not claimed.
- Registered short-switch clusters and attached arguments are tested separately;
  the pi-sftp frontend reaches configuration-policy rejection with -fb- before
  any authentication. Values for short switches such as -f=false are rejected,
  as in the local OpenSSH reference.

The tests above use local temporary files or memory. Later owner-authorized
native empty/64KiB copies passed, but the1MiB check failed before the flush phase.
Separate cleanup verified absence of the exact approved root; see
NATIVE_TRANSFER_PLAN.md and REAL_DEVICE_TESTS.md. No native flush or extension
negotiation success is claimed from those copies or the earlier metadata batch.
Linux/Windows runtime and physical iSH filesystem/sync behavior remain unverified.
No dependency, remote software, SSH fallback or agent change is introduced.

## Shared copy error correction

The failed-write fixture initially returned success in the Go client despite a
server rejecting data. Inspection of the pinned pkg/sftp v1.13.11 sequential
ReaderFrom showed that a final short source read's EOF/UnexpectedEOF could mask
that chunk's WRITE error. Ordinary copies, not just -f, could take that path.
Both clients now use a fixed 32 KiB buffer with plain Reader/Writer interfaces,
so io.CopyBuffer arbitrates read/write errors without either optional file fast
path. No dependency source is patched or copied.

Twenty regressions cover complete refusal and a successful 1024-byte prefix
followed by refusal, with 1024/32768-byte packet sizes and source sizes below,
equal to and across packet/buffer boundaries. Copies without -f must fail and
the fixture verifies the exact partial size. Data remains partial rather than
being removed or rolled back; this fixes status propagation, not atomicity.

Primary behavior references: [OpenSSH sftp manual](https://man.openbsd.org/sftp.1)
and [OpenSSH 10.2p1 sftp-client.c](https://raw.githubusercontent.com/openssh/openssh-portable/V_10_2_P1/sftp-client.c)
(download/upload/fsync result handling). Implementation and fixtures are
independently authored; upstream code is not copied or bundled.
