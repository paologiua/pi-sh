# Native writes through the Perl broker and adapter — proposed

Proposed 2026-10-29, awaiting the owner's approval. Nothing here runs before
it.

## Why

On branch `perl-remote` (at `7cb9889`) the shell broker runs entirely in Perl
(SHELL_BROKER.md "Perl broker"), `Run` sends from its own goroutine with an
input window of 32 ("Run sender"), and the forwarding adapter detects its
connectors in Perl as well (FORWARDING.md "Perl adapter from the start"). Two paths have so far been
checked on the device only read-only:
- uploads go through SFTP, so a real upload writes on the device;
- -R opens a listener on the device's loopback.

Both are checked here, each with the new code and with the Bash fallback
forced by `PI_SH_REMOTE_IMPL=bash`.

## 1. Transfers

The tool is the existing `bin/transfer-probe`, unchanged since
NATIVE_PERL_WORKERS_PLAN.md and built from this branch. It runs two
sessions, one after the other, each with its own fresh root:

```sh
./bin/transfer-probe --execute-approved --browser \
  --root /tmp/pi-sh-probe-72180a90b3e17ba55f300e1c49f6d284 raspberrypi
PI_SH_REMOTE_IMPL=bash ./bin/transfer-probe --execute-approved --browser \
  --root /tmp/pi-sh-probe-3497d8e819f6c8f8df424c5a53402cd2 raspberrypi
```

The checks, the known child entries, the limit of about 1.25 MiB of
synthetic content per run and the cleanup are exactly those of
NATIVE_PERL_WORKERS_PLAN.md:
- the probe creates each root exclusively and refuses one that already exists;
- it lists only the known entries, removes leaves before directories and
  verifies the root is absent;
- on a lost transport or an unexpected entry it reports cleanup as
  unconfirmed and replays nothing;
- a separate `--cleanup-only` session runs for that exact root only if the
  run had created it.

Each run's wall time is recorded.

## 2. -R with many connections

The session, the device-side client, the three rounds (16, 64 and 65
connections), the Mac-side counter and the bounds are exactly those of
NATIVE_RMANY_PLAN.md, with a new port:
- port 47151 on the device's 127.0.0.1;
- the Mac counter on 127.0.0.1:51151.

It runs twice: first with the default (Perl adapter, which now also detects
the connectors), then with `PI_SH_REMOTE_IMPL=bash` (Bash adapter).

```sh
pi-sh --browser --experimental-command -T -v \
  -R 127.0.0.1:47151:127.0.0.1:51151 raspberrypi '<C>'
PI_SH_REMOTE_IMPL=bash pi-sh --browser --experimental-command -T -v \
  -R 127.0.0.1:47151:127.0.0.1:51151 raspberrypi '<C>'
```

The `-v` line `forwarding carrier ready connector=… adapter=…` must read
`adapter=perl`, then `adapter=bash`.

## Cleanup check (read-only)

Afterwards a read-only session runs:
- `ls -ld` on both roots, which must be absent;
- `ps -eo pid,ppid,etime,comm`: no `sftp-server` and no `perl` beyond the
  listing session's own broker;
- `ss -ltn`: nothing on 47151.

## Bounds

- **Device paths.** Only the two roots above and their listed children.
- **Listeners.** Only 47151, on the device's loopback, for the length of each
  session.
- **Processes.** `sftp-server`, the broker and adapter in memory, the -R
  listener, and one Perl client at a time bounded by `timeout 120`.
- **Data.** About 1.25 MiB per transfer run, and about 18 MiB per -R run,
  generated in memory.
- **Excluded.**
  - installs, scripts, configuration or service changes;
  - the SSH transport;
  - any other path or port;
  - fault injection.
- **Credentials.** None are stored. Reports hold check names, timings and
  outcomes.
