# Local pi-sh extension of pkg/sftp v1.13.11

The 65 files listed in UPSTREAM.json are byte-for-byte copies of the
checksum-verified upstream module's runtime source, module metadata, README,
CONTRIBUTORS and LICENSE. Upstream tests, examples and standalone programs are
not copied. No upstream file is modified. All original source notices remain.

Upstream module checksum, also retained in the root go.sum:

    github.com/pkg/sftp v1.13.11 h1:0N92SLTB8JqASJB14ZLHHzFnBV8mG9zw4K7jghEFWuE=

The root go.mod replaces this exact dependency with this local source directory.
The source additions are client_stream.go, client_metadata.go and their two
corresponding test files:

- ReadDirEach(ctx, path, visitor) opens one directory, reads one bounded NAME
  packet at a time, invokes a synchronous visitor, and joins directory CLOSE
  failure with the primary result. Visitor requests may use the same client.
- Literal basenames are retained. Dot entries are omitted; empty/slash/NUL names,
  unknown attribute flags, malformed lengths/counts/trailing data and unsolicited
  success statuses are rejected. No command or SFTP request is retried.
- The new parser uses existing checked primitive decoders. The unchanged incoming
  packet-body bound is 256 KiB. The existing ReadDir/ReadDirContext are unchanged.
- Cancellation/blocked-I/O lifetime still relies on the caller owning and closing
  its protocol transport; background CLOSE can wait for the transport or peer.
  The pi-sh SFTP session owns cancellable protocol pipes. This API does not
  promise bounded cancellation for arbitrary borrowed blocked streams.

- File.SetPermissionsAndTimes sends one handle FSETSTAT containing only mode,
  atime and mtime. It shares File.Chmod's locking and mode conversion, retains
  v3 uint32 seconds, rejects closed handles and checks the STATUS response with
  the strict local parser. Renaming the path cannot redirect the request;
  server-side partial attribute application and blocked-I/O limits remain.

The extension follows this dependency's BSD-2-Clause license; it does not select
a license for the rest of pi-sh. Original-project licensing remains an owner
publication decision. This fork is local, unofficial and has not been submitted
or published upstream. No provider contact was made.

The root provenance test checks hashes against the recorded snapshot and rejects
unrecorded source additions. It detects accidental drift, not a malicious edit
to both source and manifest. Review against the checksum-verified upstream module
when intentionally changing this snapshot. `go mod verify` alone does not verify
local replacements. Vulnerability review must continue to identify this as
v1.13.11 plus the documented extension, including scanners that lose version
information for a local replacement.

Root integration tests cover the client/server API, stream/status/close failures,
10,000-entry lazy enumeration, actual local OpenSSH directory order and handle
metadata preservation after pathname replacement. Nested
module tests cover checked decoding, fuzz seeds and combined metadata request/
reply success and failure cases. The CI workflow runs both
modules and compiles the extension tests for every target, including Linux/386
with CGO disabled. See ../../docs/SFTP_LISTING.md for current results and limits.

2026-10-30: client_metadata.go additionally provides Lchown and Lchmod, negotiated
lsetstat v1, using the checked status parser. No upstream file changed.
Essential local actual-server ownership/link checks: TestChownLocalServer.
