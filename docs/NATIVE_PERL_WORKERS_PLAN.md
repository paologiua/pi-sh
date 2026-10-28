# Native transfers through the Perl broker workers — proposed

Proposed 2026-10-28. **Not approved; nothing has been run.**

## Why

Since `8be38b0` and `f82db76` the shell broker moves command input and output
through Perl workers when the device has Perl (SHELL_BROKER.md "Perl output
reader", "Perl input writer"). SFTP traffic passes through those workers too,
because pi-cp and pi-sftp run the device's sftp-server over the broker's stdin
and stdout.

On the device only read-only paths were checked:
- 1 MiB to `wc -c`;
- 1 MiB from `head -c`;
- one pi-cp download of `/usr/bin/bash`, with matching SHA-256.

A real upload writes on the device, so it needs this plan. The Bash workers,
which remain the fallback, were last exercised natively with uploads before
these changes.

## What runs

The existing `bin/transfer-probe` (cmd/transfer-probe) is the same tool as
NATIVE_TRANSFER_PLAN.md, unchanged. It drives the production copycli and
sftpcli backends through one browser-companion session.

Two runs, one after the other, each with its own fresh root:

```sh
./bin/transfer-probe --execute-approved --browser \
  --root /tmp/pi-sh-probe-0c24e39e2a0ed0436bb9d7622579afc1 raspberrypi
PI_SH_BROKER_WORKERS=bash ./bin/transfer-probe --execute-approved --browser \
  --root /tmp/pi-sh-probe-7a780eac33f8183383ec027fa90cf775 raspberrypi
```

The first run uses the Perl workers (the default). The second keeps the Bash
workers, for comparison and to confirm the fallback still works on the device.

Each run:
- creates its root exclusively, requires a private mode, and refuses an
  existing root;
- transfers at most 1,314,816 bytes of synthetic content;
- runs the checks of NATIVE_TRANSFER_PLAN.md: empty, 64 KiB binary and 1 MiB
  round trips by SHA-256; ordinary modes; `-p` times; reput/reget; advertised
  fsync; the small recursive tree with Unicode, space and quote names; and the
  missing-source failure.

The known child entries are the same as in NATIVE_TRANSFER_PLAN.md:
- `empty`, `binary`, `large`, `preserved`, `resume`, `flushed`;
- `tree`, `tree/empty`, `tree/nested`, `tree/nested/leaf ' 世界.bin`.

Each run's wall time is recorded, with sign-in included.

## Cleanup

Cleanup is the probe's own, in the same session. It lists only the known
entries, removes leaves before directories and verifies the root is absent.

If the transport is lost or an unexpected entry appears:
- cleanup is reported as unconfirmed;
- nothing is replayed;
- a separate `--cleanup-only` session for that exact root runs only if the
  run had created it.

Afterwards, a read-only session runs:
- `ls -ld` on both roots, which must be absent;
- `ps -eo pid,ppid,etime,comm`, which must show no `sftp-server` or `perl`
  except the listing session's own workers.

## Bounds

- **Device paths.** Only the two roots above under `/tmp`, and their listed
  children.
- **Data.** At most about 1.25 MiB per run.
- **Processes.** The existing `sftp-server`, plus the broker's Bash and Perl
  workers in memory.
- **Excluded.**
  - installs, scripts, configuration or service changes;
  - the SSH transport;
  - listening ports;
  - any other path;
  - fault injection, disk-full tests and filenames with newlines, colons or a
    leading dash (they need their own plan).
- **Credentials.** None are stored. Reports hold check names and outcomes, not
  remote contents.
