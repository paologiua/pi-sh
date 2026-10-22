# Native transfer verification — owner approved

The owner explicitly authorized this exact plan with “Autorizzo prove e pulizia”.
The authorization covers only the root and operations below. The first native execution failed its 1 MiB check; separate cleanup succeeded.
The developer binary is `bin/transfer-probe`; default operation prints a plan
without authenticating or connecting. The same checks pass against an existing
local OpenSSH sftp-server over stdio in temporary Mac directories.

```text
Scratch root: /tmp/pi-sh-probe-7470c80bcf6721dd82f13486e142579c
Create exclusively; require private directory mode; never reuse an existing root.
Checks: empty/64KiB binary/1MiB roundtrip, ordinary modes, -p remote times and local mtime, reput/reget, advertised fsync, recursive tree, missing-source failure.
At most 1,314,816 bytes of synthetic remote file contents, plus filesystem overhead.
Cleanup: inventory only known entries, remove leaves before directories, verify root absent.
On lost transport or unexpected entries, report cleanup unconfirmed; never claim success or replay automatically.
No remote installs, persistent scripts, configuration/service changes, or SSH transport.
Known child entries:
empty
binary
large
preserved
resume
flushed
tree
tree/empty
tree/nested
tree/nested/leaf ' 世界.bin
```

The requested authorization is limited to creating this exact root and the
listed synthetic children, transferring/reading/chmod/timestamp updates on those
children, and removing those same entries afterward. It does not authorize
other paths, software installation, persistent scripts/configuration changes,
service restarts, SSH transport, or intentionally invasive failure injection.

After explicit owner approval only:

```sh
./bin/transfer-probe --execute-approved --browser --root /tmp/pi-sh-probe-7470c80bcf6721dd82f13486e142579c raspberrypi
```

One normal authenticated Connect session carries the existing binary broker and
existing SFTP server. Password/TOTP remain in owner prompts if direct mode is
chosen; browser mode retains its existing memory-only companion workflow. No
credential, cookie, SDP, ICE, account identifier or session transcript is saved.
Reports contain fixed check names and outcomes, not remote contents.

The probe uses the production copycli/sftpcli backends. It verifies empty and
all-byte synthetic binary contents by SHA-256 after roundtrips; ordinary modes;
remote -p atime/mtime and local -p mtime; reput/reget; advertised fsync requests
without advisory warnings; a small recursive tree with Unicode/spaces/quotes
and an empty directory; and a missing-source error without a local partial file.
It does not prove crash durability, local download atime, files over32-bit limits,
sparse files, hard links, arbitrary path/glob semantics, full CLI parsing, fresh
authentication for each production executable, disk-full/permission failures or
cleanup after lost transport. Native results must be recorded separately.

The content maximum is1,314,816 bytes at once (about1.25MiB), plus filesystem
overhead. It creates no temporary remote script and evaluates no filename in a
shell. It refuses an existing root rather than adopting it or choosing another
path. The server's private creation mode is checked before transferring data.

Cleanup first inventories every known directory and refuses unknown entries or
symlinks/type changes before deletion. It removes fixed leaves before directories
and checks the exact root is absent. There is no generic remote RemoveAll and
no atomic guarantee against concurrent path replacement: stable private trees
are required. If the connection is lost, absence is not assumed and no transfers
are replayed. The exact approved root is reported for explicit follow-up cleanup
through a new authorized session. A cleanup-only invocation is appropriate only
when the preceding run actually created this approved root, never to adopt a
pre-existing root that creation refused.

The existing AGENTS.md requirement is: “Temporary files may be created only for
authorized pi-cp testing, in an approved location, and must be removed afterward.”
Owner approval of this exact plan is required before any native execution.

## Execution record

The first authorized attempt stopped at Connect's recent-sign-in requirement
before a remote command or root creation. No transfer or cleanup success is
claimed. Owner browser-access renewal was requested; authorization remains valid
for this exact plan and root.

The later run reached private-root, empty-file and64KiB-binary checks: all passed.
The1MiB roundtrip failed with a sanitized file-operation error; remaining checks
were not reached. Cleanup in that session failed. A separate cleanup-only session
then passed and verified this exact root absent, with exit0. No transfers were
replayed. REAL_DEVICE_TESTS.md records scope and the unclassified failure.

The instrumented rerun isolated a1MiB upload stall. Empty/64KiB phases passed;
local source offset stayed589824 in two observations. At the existing process
deadline the upload and same-session cleanup failed; broker category was deadline
with cleanup unconfirmed. A new cleanup-only session then verified the exact root
absent with exit0 and clean broker completion. The cause remains open; this is
not proof of a size threshold or of the earlier generic failure's cause.
On 2026-10-22 a local cause was found for the same stop-and-wait stall pattern
(Bash 5.2 `read -t` newline loss, SHELL_BROKER.md) and fixed in the broker. The
plan resumes with a native rerun; until then the stall cause on the Pi is a
strong local hypothesis, not device-verified. The read-only1MiB stdin
fixture then passed 3/3 on the Pi, and a full rerun of this plan passed every
phase once, including the1MiB upload, with same-session cleanup verifying root
absence (REAL_DEVICE_TESTS.md).
