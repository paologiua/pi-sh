# Regular-file metadata preservation — 2026-10-21

pi-cp and pi-sftp accept global `-p`; pi-sftp additionally accepts command `-p`
on get/put/reget/reput, including combined `-afp`. Permission bits (000–777),
access time and modification time come from the initial source stat snapshot.
Source atime is captured before reading contents. A successful copy overrides
the target's prior permission bits and creation umask. Setuid, setgid, sticky,
ownership, ACLs, extended attributes and nanosecond preservation are not included.
This page covers regular files. SCP directory recursion is now implemented with
its own mode/time rules in SCP_RECURSION.md; SFTP console recursion now shares those directory rules, with local evidence
in SFTP_CONSOLE_RECURSION.md.

SFTP v3 times use unsigned 32-bit seconds. Fractional seconds are discarded and
upload seconds are converted to uint32 by the pinned library, matching the
reference's wire conversion. This is not arbitrary-date, filesystem precision,
large-offset or physical iSH runtime proof. macOS/Linux source atime comes from
the OS stat snapshot; Windows uses its native file access time fields. Other
platforms without an implementation fail before truncating an upload target
when preservation is requested. Windows mode/atime runtime behavior is untested.

## Error and operation behavior

The initial private-creation check and chmod before sending a new upload's bytes
remain mandatory. Failure there is fatal and leaves an empty private file. After
copying, preservation chmod/time status failures emit sanitized warnings and do
not alone fail the transfer, matching the measured OpenSSH behavior. Metadata
success cannot erase data, protocol or CLOSE failures. An upload attempts final
preservation even after copy failure; a failed READ/WRITE download skips it.
The existing optional flush policy follows preservation.

No remote shell command contains a file path or evaluates file metadata. Existing
private process umask setup remains unchanged. `-p` explicitly requests the source
permission bits even when they are broader than the target's prior mode or umask.
There is no ownership change, checksum, atomic replacement or rollback.

## Remaining differences

Regular-file uploads now send ordinary mode and both timestamps together in one
handle FSETSTAT through a local extension of pkg/sftp v1.13.11. Replacing the
pathname after OPEN cannot redirect that request to the replacement file. The
server may still partially apply attributes before returning an error; this is
not a filesystem transaction. No UID/GID, size or extended attributes are sent.
Recursive directory metadata remains path-based. Download times also use the
path, as in the reference; mode updates use the open local handle.

The library's FileInfo/FileStat does not expose the original attribute-presence
flags. A server omitting timestamp fields is indistinguishable from epoch-zero
fields at that interface; complete missing-attribute parity needs a richer
metadata boundary. Tests use observed-style complete attributes from the installed
OpenSSH server. Advisory diagnostic wording and full connection-failure numeric
exit parity also remain different or unverified.

## Evidence and attribution

48 paired local sftp/OpenSSH-server cases cover uploads/downloads, new/existing
targets, modes 600/640/755 and set-ID source bits, command/global -p and combined
resume/flush. Eight paired pi-cp/scp -D cases cover directions, existing targets
and two modes. Every case checks binary contents, ordinary mode, initial atime
and mtime; SFTP tests also verify second precision. A server denying SETSTAT and
FSETSTAT verifies advisory final metadata errors. A denied READ verifies skipped
download preservation. A local OpenSSH-server regression replaces the upload pathname while its handle
remains open: only the originally opened file receives mode and timestamps.
Wire fixtures check the single combined request, unsigned-second conversion,
malformed replies, permission failure, connection loss and closed handles.
Memory fixtures check failed upload status, subsequent metadata requests,
platform atime snapshots and secret-free warnings. The simple
memory server does not apply chmod/time metadata and is not used as proof of
those final filesystem values.

Reference: [OpenSSH sftp manual](https://man.openbsd.org/sftp.1),
[scp manual](https://man.openbsd.org/scp.1) and
[10.2p1 transfer source](https://github.com/openssh/openssh-portable/blob/V_10_2_P1/sftp-client.c).
The implementation is independently authored; no upstream code or new dependency
is incorporated. All files/processes are local development fixtures. Native temporary writes are now approved for the exact NATIVE_TRANSFER_PLAN.md
root. Empty/64KiB copies passed, but the1MiB check failed before preservation
was reached; separate cleanup verified absence. Native -p remains unverified.
See REAL_DEVICE_TESTS.md.
