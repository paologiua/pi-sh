# Native pi-cp / pi-sftp CLI verification — owner approved, executed 2026-10-22

Approved by the owner ("Autorizzo il piano CLI e la pulizia") and executed once;
all checks passed and cleanup verified root absence (REAL_DEVICE_TESTS.md). A new
run needs a new approval and a fresh root name. This plan exercises the production `pi-sftp` and `pi-cp`
executables on the Pi (the earlier approved plan used `transfer-probe`), including
the new glob/tilde, diagnostics and connection-policy code. It runs only after the
owner approves this exact plan.

```text
Scratch root: /tmp/pi-sh-cli-1368401b474527931b94c1e129f64cbf
Create exclusively with `mkdir` (the batch stops if it already exists); chmod 700.
Synthetic local sources only: a.bin 64KiB, b.bin 1MiB, d.bin 4KiB (all byte values),
"c.txt", and tree/ (tree/x.bin 1KiB, tree/empty/, "tree/s p ' 世界.bin" 16B).
At most about 1.15MiB of remote file contents at once, plus filesystem overhead.
Known children: a.bin b.bin d.bin c.txt "n ' 世界.txt" sub/ sub/d.bin tree/ tree/x.bin
tree/empty/ "tree/s p ' 世界.bin".
Cleanup: explicit rm of the known files, rmdir of the known directories, rmdir of the
root, then a separate session confirms the root is absent. No recursive remote delete.
No remote installs, persistent scripts, configuration/service changes or SSH transport.
```

Sessions, each a normal authenticated Connect session (browser companion):

1. `pi-sftp -b` upload batch: `mkdir` root, `chmod 700`, `cd`, `put` a/b/c/d (d into
   `sub/`), `ls *.bin` (remote glob), `chmod 600 *.bin` (glob), `rename c.txt` to a
   name with space/quote/Unicode, `get *.bin` into a local directory (glob download),
   `reget b.bin` on a truncated local copy (resume).
2. `pi-cp -r -p` upload of `tree/` into the root; `pi-cp` download of a quoted remote
   glob `root/*.bin`; `pi-cp` of `~/pi-sh-cli-missing-1368401b` (tilde, expected
   "not found" error, reads nothing in the home directory).
3. `pi-sh -v --experimental-command -T` running `exit 0` (diagnostics on a real
   connection, no file access).
4. `pi-sftp -b` cleanup batch: remove only the known entries, then `rmdir` the root.
5. `pi-sftp -b` verification: `ls` of the root must fail with "not found".

Local checks: SHA-256 of every downloaded file equals its source; modes 0600 after
`chmod`; `-p` mtimes on the tree; diagnostics output contains no SDP/ICE/address/
token. Only fixed outcomes are recorded. On any failure or lost connection the
script stops, does not replay, and reports the root for a separately approved
cleanup session. The driver is `scripts/native-cli-check.sh`; without
`--execute-approved` it only prints this plan.
