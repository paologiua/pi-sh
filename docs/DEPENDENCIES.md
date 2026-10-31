# Dependency licence inventory

Update, 31 October 2026: the maintainer selected the [MIT License](../LICENSE)
for original pi-sh code and documentation. Third-party licences remain unchanged.
Earlier licence-decision notes below are historical; public-release review and
service-related questions remain separate.

The scp-style progress display directly imports x/text/width v0.42.0 for Unicode
column widths (pure Go, no CGO). This was already the version selected by the
transitive module graph; its source and checksums are now downloaded and pinned
for the new import. Its root BSD-3-Clause notice is included below/in
THIRD_PARTY_NOTICES.md. OpenSSH progress source and the local executable are
behavior references only; the implementation is independently authored. See
[progress behavior and evidence](SCP_PROGRESS.md).

Recursive downloads and ls -f now use a local ReadDirEach extension to pkg/sftp
v1.13.11. Regular upload preservation additionally uses a combined handle
FSETSTAT extension. All65 copied upstream files are unchanged with per-file
hashes; two source files and their tests are added. Original notices remain; no v2 library
is linked. NAME packets are consumed individually, malformed/unsafe entries fail,
and directory CLOSE failures are retained. Sorted ls still accumulates entries.
SFTP_LISTING.md and third_party/sftp/PI_SH_CHANGES.md record memory bounds,
cancellation limits, evidence, provenance and local-replacement audit duties.
No new remote software, authentication change, replay or Pi write is introduced.

The TTY controller correction reuses existing Bash/cat/stty; no helper or native
binary is added. Apple Bash 3.2 exec/jobs and GNU Bash 5.3 sources were consulted
for the inherited job-control signal difference, with links in TTY_JOB_CONTROL.md.
Implementation is independently authored; no third-party source or version pin
changed, and existing licensing/publication gates remain open.

The descriptor-query correction uses standard-library SyscallConn.Control and the
existing x/term API. Go's installed os/file_unix.go was inspected to confirm
File.Fd's blocking-mode side effect; no upstream implementation was copied and
no dependency or runtime software requirement changed.

TTY job-control diagnosis briefly used an independently authored C probe on the
Mac only, then removed it. No C source, native helper, CGO requirement or remote
dependency was added. Existing local Bash and Apple XNU sources were read as
behavior references in TTY_JOB_CONTROL.md, with no code copied.

Local escape help adds only standard-library formatting and independently
authored tests. OpenSSH 10.2p1 clientloop/manual are behavior references in
ESCAPES.md; the installed local SSH tools remain test-only. No new code or
runtime software is imported.

Grouped local stream cancellation adds no dependency or copied implementation.
Existing x/sys supplies poll/fcntl/read/write; Apple XNU fcntl.h was consulted
only to identify natural FWASWRITTEN bookkeeping, with attribution in LOCAL_IO.md.
No license selection, publication or remote-software requirement changes.

Portable terminal-mode capture/validation and the stty dialect helper add no
dependency or copied source. Existing x/sys supplies platform constants/ioctls;
the new ttymode vocabulary and adapters are independently authored. OpenSSH
10.2p1, Apple/GNU stty and glibc sources were read as behavior references, with
links and exact evidence in TTY_MODES.md. No reference source is distributed or
required on the Pi; original project licensing/publication gates remain open.

The Bash foreground-worker experiment adds no dependency. It invokes the already
present development Bash 5.3 and reads its local jobs.c/shell.c/manual as behavior
references; no GNU implementation code is copied. The GNU source copyright
identifies GPL-3.0-or-later. That development tool is not distributed with pi-sh.
The previously recorded official-archive digest and existing remote facilities
remain the evidence boundary; no Pi package is added. See TTY_COMMANDS.md.

TTY-command reference tests add no dependency or copied source. They reuse the
existing creack/pty development dependency and installed local OpenSSH tools;
all supervisor/test code is independently authored. No new runtime executable
or Pi requirement is introduced. See TTY_COMMANDS.md for evidence boundaries.

Regular-file metadata preservation adds no dependency or copied source. Native
source atime uses Go standard-library OS stat fields (macOS/Linux/Windows), while
the existing SFTP API carries chmod/timestamps. SFTP_METADATA.md attributes the
OpenSSH manual/source and local reference executables. Existing licence notices,
original-project licence decision and publication gates are unchanged.

The SFTP resume increment adds no dependency or copied third-party source.
Independently authored code is checked against the installed OpenSSH client/server
and references its manual and pinned 10.2p1 source in SFTP_RESUME.md. These are
local development fixtures; existing upstream notices and publication gates stay
applicable. Runtime uses the existing Pi server only, with no new remote tool.

The HTTPS diagnostic increment adds only independently authored code and Go
standard-library error classification; no dependency, vendored source or licence
change. Existing publication and original-project-licence decisions remain open.

SFTP adds pkg/sftp v1.13.11 (BSD-2-Clause) and kr/fs v0.1.0 (BSD-3-Clause).
pkg/sftp requires x/crypto v0.54.0; that update's root licence hash is unchanged.
Hashes below are from downloaded module files, with go.mod/go.sum pinning; the
full original notices are retained. go mod verify passes. The post-integration
native govulncheck scan reports zero reachable findings, three imported-package
and six module findings not shown reachable. This is not an all-platform security
guarantee or a complete source-file licence audit.

The command-cancellation increment adds no dependency. Its broker/control code
is independently authored. A pinned OpenSSH source reference informs signal
behavior; local SSH/ps and temporary test keys are development fixtures only,
not shipped code or new Pi requirements. Existing notices remain unchanged.

The 2026-10-20 CLI-options and explicit-file configuration parsers add only independently authored Go code and
standard-library imports. No dependency versions or third-party source changed;
the existing notice inventory still applies. OpenSSH manuals and the local
executable serve as behavioral references/tests, not copied implementation.
The original-code licence remains an owner decision before publication.

The transfer cliargs adapter and forwarding candidate fixtures add independently
authored Go and standard-library imports only. Existing local Bash/nc/OpenSSH are
test/reference executables, not bundled source or new Pi software requirements.
No dependency pin or third-party licence text changes in this increment.

Bounded Include loading/expansion/globbing and its fixtures are also independently
authored standard-library Go. OpenSSH manuals/local executable are behavioral
references, with no upstream parser/glob source copied. Dependency versions and
notice texts remain unchanged; original-code licensing and release gates remain
open. No remote executable or local runtime dependency is introduced.

The SFTP flush/short-write increment adds only independently authored Go and
standard-library APIs; versions and notice texts are unchanged. A local failure
fixture identified error loss in the pinned sequential ReaderFrom fast path;
plain-interface copying avoids that path without a library patch/fork. The prior
govulncheck result is not evidence of correct transfer/error semantics. OpenSSH
10.2p1 sftp-client.c is a behavior reference for sync result handling, not copied
or bundled implementation; SFTP_FLUSH.md records its primary link.

Recorded 2026-10-19 from checksum-verified Go module downloads.
Versions are pinned in go.mod/go.sum; the original module-root licence SHA-256 is below.
This covers modules imported by current packages and their tests on macOS, not every
upstream module test or future release executable. The PTY library is test-only.
No Connect implementation is vendored.

| Module | Version | Root licence | Licence file SHA-256 |
| --- | --- | --- | --- |
| `github.com/creack/pty` | `v1.1.24` | MIT | `a2a1cea59d9c2d71908082f516f86bb560abf857646ecf69d70c121ead14aef4` |
| `github.com/pkg/sftp` | `v1.13.11` | BSD-2-Clause | `f5b5087b8e596d015cfea7db26527fe67a196c18ca37415c288dd2534ff74c37` |
| `github.com/kr/fs` | `v0.1.0` | BSD-3-Clause | `dd26a7abddd02e2d0aba97805b31f248ef7835d9e10da289b22e3b8ab78b324d` |
| `github.com/google/uuid` | `v1.6.0` | BSD-3-Clause | `0a8d61ed3cbfd5312326e8126c31ce9c627a283adc99131b56896d29ada04b2d` |
| `github.com/pion/datachannel` | `v1.6.3` | MIT | `6483daf6c8aa2c8192528e60be098aa82dd18f6d7b96dc8246f4f7c3333fbf3f` |
| `github.com/pion/dtls/v3` | `v3.1.13` | MIT | `6483daf6c8aa2c8192528e60be098aa82dd18f6d7b96dc8246f4f7c3333fbf3f` |
| `github.com/pion/ice/v4` | `v4.4.7` | MIT | `6483daf6c8aa2c8192528e60be098aa82dd18f6d7b96dc8246f4f7c3333fbf3f` |
| `github.com/pion/interceptor` | `v0.1.49` | MIT | `6483daf6c8aa2c8192528e60be098aa82dd18f6d7b96dc8246f4f7c3333fbf3f` |
| `github.com/pion/logging` | `v0.2.4` | MIT | `87272dba8c4fcf57101a3195f4f53f7b010b6e153b9b0977bb1a3f91acddf691` |
| `github.com/pion/mdns/v2` | `v2.2.2` | MIT | `83e4dd21429a91fb7cea67a476032a9641425e5355df2e0f589a738b6ec9fd2c` |
| `github.com/pion/randutil` | `v0.1.0` | MIT | `badaf9bc72e71d5a70e7604c636b5b7adf79b9cafc188688438105d2b1e54722` |
| `github.com/pion/rtcp` | `v1.2.19` | MIT | `6483daf6c8aa2c8192528e60be098aa82dd18f6d7b96dc8246f4f7c3333fbf3f` |
| `github.com/pion/rtp` | `v1.10.5` | MIT | `b85dcd3e453d05982552c52b5fc9e0bdd6d23c6f8e844b984a88af32570b0cc0` |
| `github.com/pion/sctp` | `v1.12.0` | MIT | `6483daf6c8aa2c8192528e60be098aa82dd18f6d7b96dc8246f4f7c3333fbf3f` |
| `github.com/pion/sdp/v3` | `v3.0.20` | MIT | `6483daf6c8aa2c8192528e60be098aa82dd18f6d7b96dc8246f4f7c3333fbf3f` |
| `github.com/pion/srtp/v3` | `v3.1.3` | MIT | `6483daf6c8aa2c8192528e60be098aa82dd18f6d7b96dc8246f4f7c3333fbf3f` |
| `github.com/pion/stun/v4` | `v4.0.1` | MIT | `6483daf6c8aa2c8192528e60be098aa82dd18f6d7b96dc8246f4f7c3333fbf3f` |
| `github.com/pion/transport/v5` | `v5.1.1` | MIT | `6483daf6c8aa2c8192528e60be098aa82dd18f6d7b96dc8246f4f7c3333fbf3f` |
| `github.com/pion/turn/v5` | `v5.1.2` | MIT | `6483daf6c8aa2c8192528e60be098aa82dd18f6d7b96dc8246f4f7c3333fbf3f` |
| `github.com/pion/webrtc/v4` | `v4.2.24` | MIT | `6483daf6c8aa2c8192528e60be098aa82dd18f6d7b96dc8246f4f7c3333fbf3f` |
| `github.com/wlynxg/anet` | `v0.0.5` | BSD-3-Clause | `6b8970dbe36fe214fd02b438c06daee19897708e435d761ecd18adc9ef7e7a05` |
| `golang.org/x/crypto` | `v0.57.0` | BSD-3-Clause | `911f8f5782931320f5b8d1160a76365b83aea6447ee6c04fa6d5591467db9dad` |
| `golang.org/x/net` | `v0.60.0` | BSD-3-Clause | `911f8f5782931320f5b8d1160a76365b83aea6447ee6c04fa6d5591467db9dad` |
| `golang.org/x/sys` | `v0.49.0` | BSD-3-Clause | `911f8f5782931320f5b8d1160a76365b83aea6447ee6c04fa6d5591467db9dad` |
| `golang.org/x/term` | `v0.47.0` | BSD-3-Clause | `911f8f5782931320f5b8d1160a76365b83aea6447ee6c04fa6d5591467db9dad` |
| `golang.org/x/text` | `v0.42.0` | BSD-3-Clause | `911f8f5782931320f5b8d1160a76365b83aea6447ee6c04fa6d5591467db9dad` |
| `golang.org/x/time` | `v0.14.0` | BSD-3-Clause | `911f8f5782931320f5b8d1160a76365b83aea6447ee6c04fa6d5591467db9dad` |

Full original notices: [THIRD_PARTY_NOTICES.md](../THIRD_PARTY_NOTICES.md).
Review notices, platform-specific imports, source-file exceptions and actual linked
packages again for release. This root-licence inventory does not claim an audit of
every upstream file. Do not infer service rights or pi-sh's own licence from these
dependency licences.

Local suspension adds no dependency. It uses Go's existing syscall/self SIGTSTP
on macOS/Linux and the existing term/x/sys state and readiness backends.
OpenSSH 10.2p1 clientloop.c informed the restore/self-stop/refresh/reacquire
behavior; the implementation is independently authored. It does not copy source
or add remote software. Mac process and Pion/PTY tests are described in ESCAPES.md.

The GNU/Linux terminal dialect correction still uses only existing Bash/stty.
For PENDIN it verifies stty --version identifies GNU coreutils and accepts only
the supported native Linux format/platforms; no utility is installed or replaced.
The table/name and save-format facts were checked against the official
[GNU stty source](https://github.com/coreutils/coreutils/blob/master/src/stty.c)
and existing x/sys Linux constants; the bit translation is independently authored,
with no copied/linked upstream implementation. Native evidence and limits are in
TTY_MODES.md and REAL_DEVICE_TESTS.md. CSRF refresh adds no dependency.

The incremental-listing audit found reachable standard-library advisory
GO-2026-6809 with Go1.27.2. The project now requires Go1.27.3, whose official
release includes the x509 security fix. Root x/crypto is upgraded to0.57.0
(includes fixes for GO-2026-6354/6355/6303), and x/net to0.60.0 (fixes
GO-2026-6617/6612/6611/6610/6603). The new module-root license hashes are
unchanged from the table. No original pkg/sftp source or its module metadata
is edited; root module selection overrides its older transitive requirements.
The separate upstream module-only scan found four crypto advisories, including
unmaintained openpgp; it found no pkg/sftp advisory. That scan covers the original
upstream graph, not the production project's selected versions.

The final native source scan with Go1.27.3 reports zero reachable findings, zero
imported-package findings and one required-module finding without identified
calls: GO-2026-5932 in unimported, unmaintained crypto/openpgp (no fixed version). This is not zero vulnerability debt, all-platform coverage, nor a complete
source audit. A local replacement may obscure dependency version identity, so
its upstream checksum/provenance and separate upstream scan remain necessary.
See SFTP_LISTING.md and SECURITY.md for scope and publication gates.
