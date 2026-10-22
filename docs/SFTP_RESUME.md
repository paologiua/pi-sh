# SFTP transfer resume — local evidence, 2026-10-20

pi-sftp accepts global `-a`, command `get -a`/`put -a`, and `reget`/`reput`.
These apply to single literal regular-file paths; combinations such as `-af`
retain flush handling. An operand or `--` ends command option parsing. pi-cp
does not gain resume or a new option from this change.

The offset is the existing destination size. Download can create a missing
destination and succeeds without writing when its size equals the source.
It rejects a larger destination without truncating it. Upload requires an
existing destination strictly shorter than the source; missing, equal or larger
targets fail unchanged. Existing target permissions are retained. New downloads
use the existing creation/mask policy. Upload resume sets the SFTP append flag
and starts at the observed remote size; it does not create a new remote target.

As in OpenSSH, the existing prefix is trusted rather than compared. A mismatched
prefix is retained and can yield corrupt content. Resume is appropriate only
when source and destination represent the same unchanged file. There is no
checksum, snapshot, atomic replacement, concurrent-writer protection or automatic
retry. Stat/open races and changing source/target contents remain limitations.

Copies retain the bounded shared stream path that checks WRITE errors. On copy
failure a resumed target is truncated to the original offset plus contiguous
bytes successfully written/acknowledged; truncation and close failures remain
errors. A lost connection can prevent that truncation, so cleanup is not
guaranteed. A failed download does not request a flush; a completed download
that wrote no bytes in resume mode also skips it. Upload retains negotiated
flush handling after copy failure. No new guarantee of durable data follows.

## Evidence and reference

48 memory-server cases and 48 paired comparisons against the installed local
OpenSSH sftp client and actual sftp-server cover both directions; absent, empty,
partial, equal, oversized and wrong-prefix destinations; command flags, aliases,
global settings and combined flush flags. All-byte fixtures cross 1024-byte
packet boundaries. Four extra memory cases cover empty sources. Injected failures
check a retained acknowledged prefix, removal of bytes written remotely before
a failed acknowledgement, and a short READ followed by refusal. Frontend tests
exercise `-afb-` before configuration refusal, without authentication.

Reference: [OpenSSH sftp manual](https://man.openbsd.org/sftp.1) and
[OpenSSH 10.2p1 transfer implementation](https://github.com/openssh/openssh-portable/blob/V_10_2_P1/sftp-client.c).
Implementation is independently authored; no upstream source is incorporated.
Reference executables and files are local development fixtures, not runtime
dependencies. No package was added. Native empty/64KiB content transfers are verified within the owner-approved
NATIVE_TRANSFER_PLAN.md root. The1MiB check failed before resume was reached;
separate cleanup verified root absence. Native resume remains unverified. Windows/Linux/iSH runtime and
large/sparse/concurrent-changing file behavior remain unverified.
