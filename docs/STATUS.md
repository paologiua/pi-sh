# Development status — 2026-10-22

Later the same day four parallel engineering branches were merged (local evidence
only; no Pi verification yet): diagnostics -v/-q/-E/-y with structural redaction
(DIAGNOSTICS.md), ssh_config Match/Tag/-P (CONFIGURATION.md), destination parsing
and connection policy incl. BatchMode, ConnectTimeout/ConnectionAttempts and
ServerAlive via ICE (CONNECTION_POLICY.md, with owner decisions), and glob/tilde
for pi-sftp/pi-cp (GLOB_TILDE.md). Each was compared with local OpenSSH 10.2p1.

Claude Code resumed from the handoff. The intermittent binary-input stall now has a
locally demonstrated cause: Bash 5.2 (the Pi has 5.2.37) can report a `read -t`
timeout after consuming the newline, so the broker kept a complete `I` record as a
partial fragment while the client waited for its acknowledgement. A test-only
carrier that delays record newlines stalled the old broker 2/2 with locally built
Bash 5.2.37 and passed with 5.3. The broker and TTY adapter no longer let timed
reads consume protocol data; the regression now passes with both versions. Full
local race suite, root vet, module verification, the 5.2.37 broker/cmd suite and
seven CGO-disabled build/test compilations pass; binaries are rebuilt. The owner then
ran the read-only1MiB `wc -c` fixture on the Pi three times: all exited 23 with
1048576 bytes. The full approved transfer plan then
passed every phase once, including the1MiB upload, with verified cleanup and
clean broker completion. See SHELL_BROKER.md and REAL_DEVICE_TESTS.md.

## 2026-10-21

Owner requested stopping after the current experiment and handing work to Claude.
The read-only1MiB broker experiment is finished: normal CLI stalled at local
source offset196608 and was canceled at180s (255, remote cleanup unconfirmed).
Two separate counter-instrumented runs counted all1048576 bytes and exited23.
This establishes an intermittent/timing-sensitive input symptom without SFTP,
not a fix or a proven common cause for the earlier native upload failures.
Temporary instrumentation and local probe files were removed; no local probe
remains active. The existing approved root was verified absent before these
no-write experiments. See REAL_DEVICE_TESTS.md and CLAUDE_HANDOFF.md for restart.

The transfer probe now reports fixed upload/mode/download/integrity subphases,
elapsed milliseconds and sentinel-based final broker categories. No source error
text or endpoints enter that report. Session-check errors retain a typed sentinel
without changing cancellation or authorization policy. Redaction/report-failure
and active/completion-check tests pass. The actual broker/PTY/OpenSSH1MiB test
passes in approximately23 seconds; the old20-second fixture limit was too short.
A fatal assertion now releases the fixture pipes instead of stranding the client.

Full local race regression and root vet pass. All seven CGO-disabled build/test
compilation targets pass, including Linux/386 softfloat; all four native binaries
are rebuilt. The instrumented native rerun stalled in1MiB upload until the existing
process deadline; local source offset stayed589824. Same-session cleanup failed;
fresh cleanup-only verified root absence with exit0 and clean broker completion.
No live probe remains. REAL_DEVICE_TESTS.md records exact phase/category evidence;
none of these local successes explains or resolves the native stall.

Latest native transfer evidence supersedes the historical pending notes below:
the owner approved the exact scratch root; empty and64KiB all-byte roundtrips
and ordinary modes passed. The1MiB check failed with an unclassified sanitized
file-operation error. Later preservation/resume/flush/recursion checks were not
reached. Initial cleanup failed; separate cleanup-only passed and verified root
absence. No transfer was replayed. Diagnose this before claiming native transfer
readiness. REAL_DEVICE_TESTS.md is the detailed evidence record.

The combined handle-metadata increment passes the full local Go race suite,
nested-module race tests, root vet, nested vet except documented unchanged
upstream stdmethods findings, module verification and all seven CGO-disabled
build/test-compilation targets. All four native binaries were rebuilt.

Regular-file upload -p now uses one combined handle FSETSTAT for ordinary mode
and both timestamps. A local OpenSSH-server pathname-replacement regression
failed with the old path timestamp operation and passes with the handle update;
48 paired metadata comparisons, strict wire/status failures and failed-WRITE
preservation tests pass. Directory/local timestamp path races and omitted
attribute-presence flags remain open. See SFTP_METADATA.md.

The owner has explicitly approved NATIVE_TRANSFER_PLAN.md for the sole root
/tmp/pi-sh-probe-7470c80bcf6721dd82f13486e142579c and its listed operations/cleanup.
The first Connect attempt stopped at the recent-sign-in requirement before
starting a remote command or creating the root. The requested access renewal is
pending; it does not require another write-authorization confirmation. Earlier
approval-pending entries below are historical.

pi-sh now parses the supported options after the first destination until the
first command word or an explicit --, matching thirteen additional offline
OpenSSH_10.2p1 comparisons. Cross-boundary short-flag/directive precedence,
clusters/attached operands, post-host -F aliases, custom modes and immediate -V/-Q
are covered. Command arguments after their first word remain literal, including
later -- or option-like tokens. Unsupported options before command start fail
before authentication. CLI_OPTIONS.md and LOCAL_INSPECTION.md describe the boundary.

The complete final local race suite and vet pass, with all seven CGO-disabled
build/test-compilation targets including Linux/386 softfloat; pi-sh is rebuilt.
The separately prepared transfer-probe remains approved for local testing only:
owner permission for the exact native scratch root in NATIVE_TRANSFER_PLAN.md
is still pending. No file or directory has been created on the Pi.

A developer transfer-probe is prepared but has NOT run on the Pi. Default plan
mode is offline; execution requires the owner's explicit approval of the exact
scratch root in NATIVE_TRANSFER_PLAN.md. It uses the production copy/SFTP backends
for synthetic empty/64KiB/1MiB roundtrips, ordinary modes, selected preservation,
resume, advertised flush, a small recursive tree and a missing-source failure.
Cleanup inventories a fixed allowlist, refuses unknown entries/symlinks before
any deletion, removes leaves first and verifies root absence. Lost transport
reports cleanup unconfirmed rather than replaying transfers or claiming success.

The full local race suite, vet, module verification and seven CGO-disabled
build/test-compilation targets pass, including Linux/386 softfloat. The complete
probe passes against an existing local OpenSSH server over stdio. Existing-root,
unknown-entry, symlink, cancellation and lost-transport tests retain failure and
protect unrelated data. None of these results establish native content transfer,
full production-CLI parsing or iSH runtime. Approval of native temporary writes
is pending; no remote root or content was created.

The recurring TTY early-command-close failure is now reproduced and corrected
locally. Per-rune input created65,536 messages for a64KiB paste; a stalled peer
PTY writer exhausted its4096-frame queue before it could send TX23. Fixed-marker
probes showed queue overflow, emitted-but-unsent TX and EOF without parsed TX.
The parser's refusal was correct. Input now batches only available UTF-8 in4KiB
messages and flushes before a blocking read or local action. No queue limit,
remote script, status rule or acknowledgment changes. TTY_COMPLETION.md records
the deterministic frame-budget regression and native evidence boundary.

Two hundred concurrent instrumented runs then delivered all results with no
overflows. The clean final source passes120 positive and120 no-status stress
cases, full Go race suite, nested SFTP tests, vet/module verification and seven
CGO-disabled builds/test compilations including Linux/386 softfloat. All native
binaries are rebuilt and temporary diagnostics/probes are removed. This supersedes
the failed full-suite gate below; the old native close error, native coalesced
paste, timestamp stress anomaly and broader compatibility/runtime work remain
open. Goal is not complete.

Incremental remote directories are implemented for recursive pi-cp downloads and
pi-sftp ls -f. A local pkg/sftp v1.13.11 snapshot retains all65 upstream files
unchanged and adds one checked iterator plus parser tests; hashes, original
notices and fork duties are documented. Ten-thousand-entry lazy/close/error
fixtures, raw-wire failures, local OpenSSH ordering, recursive comparisons and
1,844,993 fuzz executions provide local evidence. Sorted ls remains unbounded;
no new native Pi proof or write was attempted. See SFTP_LISTING.md.

The fresh security scan found reachable Go1.27.2 x509 advisory6809. Minimum Go is
now1.27.3, with x/crypto0.57.0 and x/net0.60.0. All native binaries are rebuilt.
The final scan finds zero reachable/imported-package findings and one unimported
openpgp module finding without a fix. License hashes are unchanged; provenance
and module verification pass. CI now includes the nested module and source scan.

All seven CGO-disabled builds and both-module test compilation pass, including
Linux/386 softfloat. Transfer/parser tests and root vet pass. Nested vet's two
unchanged upstream ReadFrom-signature warnings are recorded; other analyzers pass
with stdmethods disabled for that nested module only. The full final race run
FAILED cmd/pi-sh early-command-close; thirty isolated repetitions subsequently
passed, which does not resolve the failure. TTY_COMPLETION.md records next work.
A test-side directory-atime observer was corrected before enumeration; an isolated
regular-file timestamp discrepancy remains unclassified despite eight full
recursive-matrix repetitions and the final passing transfer package run.
Earlier successful full-suite entries below are historical and do not supersede
these current unresolved regression gates. Goal remains incomplete.

Native command-TTY Ctrl-C/INT handler, empty-line Ctrl-D/EOF and Ctrl-Z/TSTP
handler checks now each return the expected marker and normal exit23. These do
not prove default signal termination or stopped-job/local suspension behavior.
No Pi file or persistent setting was changed. REAL_DEVICE_TESTS.md records the
exact scope separately from local OpenSSH comparisons.

Native command-TTY resize now has evidence after the owner's fresh sign-in:
initial29x91 became35x111 through the actual CLI/Connect path, input remained
usable and the command exited23. Bash reported its WINCH trap after an empty
input line; immediate notification timing remains unverified. No Pi file was
written. See REAL_DEVICE_TESTS.md. This supersedes the pending resize/access
notes below; other native TTY/lifecycle checks remain open.

Literal `pi-cp -r` trees are now implemented in both directions. Forty-eight
paired local OpenSSH_10.2p1 copies compare suffix/destination rules, binary/empty
contents, source links, ordinary directory modes and preserved timestamps.
Additional error/cycle/cancellation/raw-path/private-mask tests pass. SCP_RECURSION.md
records first-error stopping, whole-directory remote-listing memory, path races,
special-bit differences and pending native/other-platform evidence. SFTP console
recursion remains unimplemented. No Pi content write was performed or authorized.

Final validation for this increment passes: full Go race suite (including the
previously pending offline-inspection integration), vet, all seven CGO-disabled
builds and every test-package compilation including Linux/386 softfloat. All
three native clients are rebuilt. The earlier approval-review usage gate allowed
the new normal request; it was not bypassed. The native resize retry then stopped
at Connect's renewed recent-sign-in requirement before a remote command ran.
The owner subsequently renewed normal browser access and the resize test passed
as recorded above; independent local development continues. See REAL_DEVICE_TESTS.md for separated native evidence.


Offline -V and -Q queries are now implemented. Version uses stderr and build
metadata; namespaced pi-sh capability queries use stdout and describe compiled
implementation, never SSH algorithms or device/runtime verification. Ten actual
OpenSSH offline comparisons and subprocess/config/redaction/write-failure tests
pass. Seven CGO-disabled builds and all test-package compilations also pass,
including Linux/386 softfloat; vet passes and all native clients are rebuilt.
The native binary reports its devel/Go/target/unofficial version and lists query
names correctly. See LOCAL_INSPECTION.md. The native resize attempt was not executed because
automatic approval review hit its usage limit. That pending process/network/PTY regression was subsequently completed in the
recursive-copy increment above.

A reproducible TTY completion race is fixed: a transport send error could
cancel the reader despite received TX23/EOF. The CLI now permits a bounded one-second
result drain only for carrier-send failures, retaining strict status/EOF validation
and auth/cancel/output failure behavior. The reduced actual Pion/PTY case passes20
race repetitions; negative/reference/drain cases pass five runs. A native Pi Bash
one-byte read with2048 supplied bytes returns23 in the final binary. This does not
retrospectively identify the earlier uninstrumented native error. See TTY_COMPLETION.md.
Final validation for this correction passes: full Go race suite, vet, seven
CGO-disabled builds and all test-package compilations, including Linux/386
softfloat. All three native clients are rebuilt; no temporary diagnostic remains.

The staged command TTY now has native Pi evidence through the browser companion:
forced -tt/-n gives TTY fds, size0x0 and exit23; a Mac PTY sends TERM=xterm,
91x29 and one input line with exit23, including PENDIN enabled and a final
uninstrumented binary. Native GNU stty exposed two corrected dialect gaps:
pendin has no CLI option, and reprint is named rprnt. A fresh carrier-native
state patch preserves the PENDIN bit; unknown formats/platforms fail closed.
The companion uses fresh shell-page CSRF metadata after reauthentication.
Final post-correction validation passes: full Go race suite, four Node companion
regressions, vet/module verification, seven CGO-disabled builds and every test
package compilation including Linux/386 softfloat. Native clients are rebuilt;
Linux-only kernel tests were compiled rather than run on this Mac.
One earlier post-input WebRTC/status255 episode remains unclassified despite
later successful TX23/EOF and final normal runs. Native teardown, other control
paths/direct lifetime and broader platform checks remain open. See REAL_DEVICE_TESTS.md.

Three nested interactive job-control comparisons now pass against
OpenSSH_10.2p1 with both staged carriers: foreground Ctrl-Z/fg, background
TTIN/read resumption with echo disabled and TTOU/output resumption. Focused tests
pass three race runs. A competing stopped-job TERM/fg sequence also reproduced
its timing-sensitive exit1 directly in Bash without the adapter; it is documented
as a caveat rather than a client status fix. See TTY_JOB_CONTROL.md. Native,
arbitrary nested-job and detached-group cleanup evidence remains open.

Nonempty canonical Ctrl-D now has two same-interpreter OpenSSH_10.2p1
comparisons: flushing abc without a newline retains a subsequent next line;
two Ctrl-D records let read report EOF while retaining abc. Both legacy and
modern staged carriers match status23 and output. Two actual CLI/Pion/PTY cases
exercise the same inputs and check exact local restoration. This is local Mac
evidence only; native Pi/Linux/iSH terminal semantics remain unverified.

The local ~^Z escape now has macOS/Linux restore/stop/CONT/reacquire support.
Real macOS child-process tests through the TTY and interactive Pion/PTY coordinators observe
stopped state, restored flags/modes, shell changes while paused, forced resize,
remote fixture output, TTY status23, interactive disconnect and preservation of
the changed baseline.
Focused suspension tests pass three race runs. Cancellation, failed stop and
concurrent Close do not reacquire raw modes after ownership ends. Windows,
native Pi/Linux and physical iSH runtime remain unverified. See [ESCAPES.md](ESCAPES.md) for semantics and limits.

Validation: the final full `go test -race ./...` passes, including both
suspension modes and the new canonical EOF comparisons/coordinator cases.
Seven CGO-disabled package builds and all test-package compilations pass;
the final CLI and shellbroker test sources also compile for all seven targets.
Vet passes and native binaries are rebuilt. Earlier checkpoint notes below retain the history
of gaps resolved by this increment; Pi/Linux/Windows/iSH runtime gaps remain.

The production TTY controller now fixes caught TSTP and Ctrl-Z status loss with
legacy and modern carriers. Four same-interpreter OpenSSH comparisons now assert
parity instead of preserving known mismatches; 36 actual staged executions cover
both carriers, 13 Pion/PTY CLI cases include both Ctrl-Z forms. HUP/TERM tests
confirm sampled group cleanup before carrier close and an unrelated sentinel
survives. Whole-group STOP/CONT retains input/status 23. These focused tests pass
three race repetitions. See [TTY_JOB_CONTROL.md](TTY_JOB_CONTROL.md) for private
history/group ownership and unverified native/nested-job/local-suspension limits.

The full final `go test -race ./...` passes on the corrected production source.
Vet, module verification and Node companion tests pass. CGO-disabled package
builds and all test-package compilations pass for macOS amd64/arm64, Linux
amd64/arm64/386 softfloat and Windows amd64/arm64; all three native clients are
rebuilt. Native Pi, Linux/Windows and physical iSH runtime remain unverified.

The latest descriptor-query correction replaces File.Fd with SyscallConn.Control
in terminal detection, mode capture and terminal ownership, including both pi-sh
TTY selection checks. A real pollable-PTY regression first observed flags6 ->2
for capture and session Open, then passed ten repeated race runs after the fix.
Detection/capture/session now preserve the caller's original nonblocking bit.
The terminal and localio package race suites pass; all seven CGO-disabled build
and test-compilation targets, vet and module verification pass, and native clients
are rebuilt. The full post-correction networking/process race integration suite now also
passes. An initial attempt was not executed because automatic approval review
hit a usage limit; the subsequent normally approved rerun completed successfully.
This closes that validation gate only, not the remaining native/suspension/iSH work.

After the local I/O/help integration, the full race suite, vet, module verification
and Node companion regression pass. CGO-disabled builds and all test-package
compilations pass for macOS amd64/arm64, Linux amd64/arm64/386 softfloat and
Windows amd64/arm64. All three native clients are rebuilt. These are local and
compile-only results; Pi, native Linux/Windows and physical iSH gaps remain.

Local ~? help now consumes the escape without sending it remotely, writes only
to stderr and preserves line-start for another action. Seven local OpenSSH
reference observations, short-read/error tests and actual interactive/TTY Pion
cases pass. The help lists implemented actions only; all other named escapes
remain explicitly inventoried in [ESCAPES.md](ESCAPES.md). This brings coordinator
coverage to eleven TTY and seven interactive cases; no new Pi proof follows.

The latest local lifecycle increment now uses grouped cancellable macOS/Linux
file streams for interactive, TTY, no-TTY and legacy commands. Idle partial UTF-8
input and full stdout/stderr pipes wake on cancellation without closing caller
files; Unix file-backed workers and TTY monitors are joined. Ten TTY and six
interactive Pion/PTY cases, three binary file-command Pion cases, descriptor
regressions and uncertain-commit cleanup tests pass. See [LOCAL_IO.md](LOCAL_IO.md)
for fixture fidelity and limits. This supersedes the reusable Unix file-I/O gap
in earlier checkpoints below; Windows synchronous I/O, arbitrary callbacks,
filesystem kernel latency, suspension and native teardown remain open.

Development resumed at the user's explicit request after discussing the uncertain
service-terms risk. The first native interactive-shell milestone is now achieved;
the full SSH/SCP/SFTP compatibility objective remains unfinished.

## Working and verified

- `cmd/pi-sh` is a usable experimental interactive shell. `bin/pi-sh` is the local
  macOS build. Direct password/TOTP authentication and signaling use memory-only
  `internal/webauth`; the optional Chrome companion uses `internal/browserauth`.
  The WebRTC peer and shell I/O run in Go in both modes.
- A native probe verified two shell round trips, a TTY, CONNECT_TTY, remote resize
  to 100 columns by 32 rows, and clean channel close after `exit`.
- Interactive CLI checks verified 80x24 sizing, Ctrl-C interrupting a shell read,
  another successful command afterward, Unicode input/output and local `~.` exit.
- A session remained usable beyond the browser companion's first periodic check.
- The final authorization watchdog was checked on the real session: continued
  browser confirmations kept it alive, and reloading the companion page caused
  the native CLI to terminate after the missing-heartbeat window.
- Local terminal/transport/authentication/input tests, race checks, vet and
  CGO-disabled cross-builds are recorded in [TESTING.md](TESTING.md).
- No Raspberry Pi software, configuration, services or persistent files were
  modified. Diagnostic sessions disabled history persistence before commands.

See [sanitized real-device evidence](REAL_DEVICE_TESTS.md). No credentials, raw
terminal captures, account/device identifiers, SDP or ICE captures are retained
in repository evidence.

## Highest-impact unresolved gaps

The next 2026-10-21 increment now captures portable macOS/Linux terminal modes
before local raw state and applies them before TTY readiness/command launch.
The fixed vocabulary covers 34 flags, 14 control characters, character-size/
parity and reference Linux baud rates. Seven paired OpenSSH cases and an eighth
Pion/PTY coordinator case verify changed modes, including canonical-off Ctrl-D
as data. All 256 control values are checked through the actual dialect helper
against the local kernel; this found/fixed literal-caret BSD stty parsing.
Three repeated focused race runs and the full race suite pass, alongside
seven-target CGO-disabled build/test compilation. See TTY_MODES.md for precise
limits: Windows translation, unusual hardware/independent Linux rates, stricter
mode errors and native execution remain open. This supersedes the full-mode
implementation gap recorded in the earlier CLI checkpoint below, not its
remaining suspension, I/O lifecycle or real-agent verification work.

The later 2026-10-21 CLI increment now integrates command -t/-tt/RequestTTY with
the staged adapter. -t needs terminal stdin without -n; -tt forces TTY. Quoted
TERM and initial dimensions are applied before command launch, later resize uses
the observed channel after TR, and local raw state is restored. Seven actual
Pion/PTY coordinator cases cover successful input/resize/pipe/status and null
input, escape, auth and transport failures; abnormal cleanup remains explicitly
unconfirmed. Full mode negotiation, stopped jobs/suspension, reusable I/O cleanup
and native TTY command/lifecycle checks are open. TTY_COMMANDS.md supersedes the
earlier unconnected-adapter checkpoint immediately below.

Full local race regression, vet, module verification and Node tests pass after
this increment; seven-target CGO-disabled builds and every test-package
compilation pass, including Linux/386 softfloat. Native binaries are rebuilt.
Offline force/null dump and invalid-TERM pre-auth refusal pass. These checks do
not close any native Pi, physical iSH, full mode or suspension verification gap.
Shared commandrun now refuses to invoke an adapter when cancellation or an auth
failure is already known. Its pre-fix regression observed an invocation after
cancellation; the fixed regression passes100 race repetitions. This applies to
both command paths and transfer sessions; later actual revocation still needs
the existing periodic checks and native evidence.

Initial reconstruction on 2026-10-21 found an unborn main branch with all project
files untracked. No existing implementation was discarded or treated as complete.
The production staged TTY adapter in internal/shellbroker (using the shared
internal/command result parser) now has actual-loader/PTY tests, including seven
paired OpenSSH cases, immediate input, first output, full-size command staging,
signal classification and EOF/cancellation. See TTY_COMMANDS.md. It remains
unconnected at that initial checkpoint; the later CLI increment above adds local
coordination, while native execution and cleanup stay unverified. The earlier
prototype tests below remain separate evidence. The full compatibility objective
is still unfinished.

Command TTY has a later Bash foreground-worker candidate with fifteen paired
local OpenSSH cases, preserving ordinary 130/143 and distinguishing INT/TERM/KILL,
Ctrl-C and a caught INT. Two local HUP/TERM fixtures verify owned-group cleanup
before carrier close. Five repeated race runs pass. The earlier simple POSIX
candidate remains an explicitly failing compatibility reference. The subsequent
staged adapter/CLI now has local coordination coverage, while real
cancellation/auth-loss/native evidence remains open. See TTY_COMMANDS.md;
direct local HUP is not a proved Connect signal or peer-close behavior.

SFTP resume now has single-file global/command -a and reget/reput implementations,
with 48 paired local OpenSSH/actual-server comparisons plus memory and failure
tests. Upload/download destination rules intentionally differ as measured; prefix
contents are trusted. No native resume/content-write evidence is claimed. See
SFTP_RESUME.md. Regular-file -p now has local reference comparisons in both clients;
handle-based remote time updates, missing-attribute parity, recursion and forwarding
remain open. See SFTP_METADATA.md for the precise preservation boundary.

The latest owner-run pi-sftp startup check stopped at a generic HTTPS login
failure. A credential-free development preflight reproduced it and its new
secret-safe diagnostic identifies DNS failure in that environment; the owner's
cause remains unclassified. No Pi access or credential submission occurred in
that probe. See DIRECT_LOGIN_PROBE.md. The owner subsequently completed the
credential-free preflight and rebuilt direct-login read-only pwd/quit SFTP batch,
each with status 0, including process-local umask-prefix startup. The earlier
failure's cause remains unclassified; no native transfer-write evidence follows.
The final rebuilt credential-free probe then reached password_required/status 0;
the observed development DNS failure was intermittent. No DNS repair is claimed.

1. Extend command behavior beyond the verified shell-only broker: verify the new
   cancellation's process-tree effects and investigate command PTY behavior.
   Binary stdin EOF, separate stderr, child pipes/no controlling terminal and
   larger native duplex streams now pass; selected local OpenSSH fixtures pass.
   Interactive closure still returns 255 without an observed native remote status.
2. Improve authentication usability and recovery without exporting browser
   account credentials or assuming an undocumented CLI login flow. Browser
   companion loss, reauthentication, ICE renewal and relay-only behavior need
   further tests.
3. Implement OpenSSH syntax/configuration and remaining terminal semantics,
   including additional escapes, suspension and robust I/O cancellation.
4. Investigate binary-safe transfer and forwarding only through evidenced
   capabilities and existing permitted remote facilities. pi-cp/pi-sftp now have
   local SFTP/broker tests and native read-only metadata success; content-transfer
   tests remain to verify natively; the later process-local mask startup passed
   an owner-run read-only pwd/quit batch with status 0.
   No invented channels,
   installed helper, persistent remote script or SSH transport fallback.
5. Run native Linux/Windows and later physical iSH tests; compile success is not
   runtime proof. Remote transfer writes still need a separately approved path.
6. Resolve project licensing, attribution, service-scope and security questions
   before any public release. No release or provider contact is authorised here.

## 2026-10-20 SFTP flush and write-error propagation

pi-sftp now accepts global -f and get/put -f, including registered short-switch
clusters such as -fb-. Downloads sync their local file after a successful copy;
uploads request the advertised fsync extension before CLOSE, including after a
data-write error. Fourteen local OpenSSH comparisons confirm absent-extension,
advisory status failure, broken/malformed reply and failed-write behavior with
global or command -f. An actual local server log confirms WRITE/fsync/CLOSE order;
server -P fsync suppresses the advertisement and no request is sent. Flush status
is advisory like the reference; copy/protocol/close failures remain fatal.

The failed-write fixture exposed a pinned pkg/sftp ReaderFrom path that could
discard the final short packet's WRITE error when source EOF occurred with data.
Both clients now use plain Reader/Writer copying with a bounded 32 KiB buffer.
Regressions cover refused writes and a retained prefix across production/default
packet boundaries, including ordinary copies without -f. No dependency change,
Pi write or native flush/transfer verification is claimed. See SFTP_FLUSH.md.

## 2026-10-20 bounded Include configuration

All three clients now share file-based Include loading: ordered patterns/globs,
nested Host scopes with caller-context restoration, first-value precedence,
home-relative and ~/ paths, ${NAME}, and %%/%d/%n/%h tokens. CLI HostName is seeded
before parsing so it also determines Include %h; config never changes command
arguments or authentication/transport endpoints. No default root config is read.

Eighteen local ssh -G differential cases pass, including nested relative paths,
inactive blocks, dotfiles, quotes, environment literals and CLI precedence.
Module fixtures cover cycles/hard links, preserved symlink/.. traversal, FIFO
refusal, redaction and aggregate resource limits. HostName expansion is limited
to 4096 bytes before appending substituted strings. Selected frontend tests
verify Include policy refusal before login. Native alias/Include and physical
iSH remain unverified; Match, named-user tilde, other tokens and POSIX named/
collating glob classes remain incomplete. See CONFIGURATION.md for exact bounds.

## 2026-10-20 transfer syntax and forwarding feasibility

The transfer frontends now accept attached short arguments (-Fnone, -oKey=value,
-b- and -s/path) with OpenSSH-style value boundaries. Actual local sftp tests
confirm attached batch/config/option syntax and literal '=' in batch filenames.
Both frontend tests verify attached configuration reaches no-TTY policy rejection
before any login. Native verification of these syntactic forms remains pending.

Forwarding investigation now has concrete local evidence: a Bash /dev/tcp/two-cat
bridge delivers binary data but cannot send socket input EOF while retaining the
response reader. A Go half-close control passes; the existing Mac nc also passes
an EOF-driven binary request/response test. Remote nc presence/dialect, native
socket capability and concurrent stream lifecycle remain unverified. No forwarding
is enabled and no Pi network test was run. See [FORWARDING.md](FORWARDING.md).

## 2026-10-20 SFTP backend and first pi-cp/pi-sftp clients

Both commands now use SFTP v3 through the binary broker, starting only an existing
compatible server. Direct/browser authentication is shared in connectsession;
completion/auth/cancellation arbitration is shared in commandrun and still used
by pi-sh. No SSH connection, new Connect channel, installed helper or Pi write.

pi-cp copies literal regular-file operands (single device, either direction), with
multiple-source directory preflight. pi-sftp implements the documented interactive/
batch command subset, quotes, ignore/quiet prefixes, file operations and local/
remote working directories. Explicit configuration/aliases are shared. A read-only
mode starts the existing server with -R and is tested to reject writes locally.
See [SFTP_BACKEND.md](SFTP_BACKEND.md) for limits and native read-only evidence.

Memory tests verify binary round trips, errors, batch control and operand safety.
Actual local OpenSSH-server tests verify quote-sensitive executable paths,
symlink traversal and read-only rejection; a real Bash/PTY/broker/SFTP integration
verifies exact binary bytes and EOF/status. The owner-run native direct-login
read-only metadata batch returned 0, verifying a server found by the default
search and the SFTP path in that binary. Pi write-test path permission is still
absent. No runtime iSH claim.
Differential OpenSSH CLI tests cover default creation permissions, remote umasks
022/077 and existing-target modes, including executable bits. The server captures
its original umask, then uses process-local umask077 for private creation; the
client applies source/original-mask permissions before data. An actual-server
fsetstat failure leaves an empty private file. This later setup has local evidence
only and awaits native rerun; ACLs/special modes/timestamps remain unverified.

pkg/sftp v1.13.11 and kr/fs v0.1.0 are pinned; required x/crypto is now v0.54.0.
Licence texts/hashes are inventoried. govulncheck reports zero reachable findings,
plus three imported-package and six module findings not shown as reachable.
This is not a full release or dependency source-file audit.

## 2026-10-20 acknowledged command cancellation

The broker now requests abnormal cleanup after launch commitment on cancellation,
authorization loss and local/protocol failures. Bash kills the owned group and
startup worker, waits for direct workers/helpers, then acknowledges. The client
drains late records for at most two seconds; the coordinator permits at most
three seconds before closing the peer. Missing confirmation is an explicit
error, never proof that the remote process was cleaned up. See
[COMMAND_CANCELLATION.md](COMMAND_CANCELLATION.md).

Real local PTYs verify cleanup while the carrier remains open and early worker
startup cancellation. Synthetic tests cover lost/silent acknowledgement, output
failure and the launch/readiness race. Real Pion peers exercise connect teardown;
an independent loopback OpenSSH test confirms local INT/TERM/HUP/QUIT yield 255.
The source/reference evidence corrects the earlier assumption that generic
local-to-remote signal forwarding should be added: termination of the client and
PTY control bytes are different paths. The initial implementation had local evidence.
The owner subsequently ran the direct-login -nT marker/sleep check: Ctrl-C
reported context canceled and exit 255 without an unconfirmed-cleanup warning.
This closes the native post-launch cancellation-path check, not independent
process-tree, revocation or network-loss verification. See REAL_DEVICE_TESTS.md.
The full local race suite, vet, Node companion and seven-target CGO-disabled
build/test compilation pass. The local binary is rebuilt; other-platform and
physical iSH runtimes remain unverified.

## 2026-10-20 explicit files and HostName aliases

`internal/sshconfig` implements explicitly selected `-F` files with Host blocks,
*/? patterns, negation, first-value precedence, quotes/comments/CRLF and four
scalar directives: HostName, EscapeChar, StdinNull and RequestTTY. CLI values win;
HostName maps to an exact Connect device and expands only %h/%% once. Default
files, Match and local-execution directives remain unsupported. Include was added
in the later increment above; this initial file/alias evidence remains distinct.
`-G` includes hostname when given a destination. See
[CONFIGURATION.md](CONFIGURATION.md) for measured scope and differences.

Twelve valid/six invalid file comparisons, the earlier ten CLI comparisons and
127 non-NUL ASCII escape-byte dumps compare against local OpenSSH. They exposed
and fixed pattern case handling, attached-comment parsing, and space/backslash/
DEL representations. Bounds/read/error and alias/command-separation tests cover
the new module. Native alias/config integration remains pending; offline tests
do not establish a new real-Pi capability.
The full local race suite, vet, Node test and seven-target CGO-disabled build/test
compilation pass. The rebuilt binary's offline alias dump and pre-login refusal
of a file-requested command TTY pass. No remote write, provider contact or push.
The browser-access check was retried after this increment: Chrome DevTools
`list_pages` again timed out after 60 seconds before reaching a page. No native
session was opened, so the config/alias real-device check remains unverified.

## 2026-10-20 CLI/configuration subset — initial increment

The CLI now accepts compact short options, clusters and `--`; `-o` implements
EscapeChar, StdinNull and RequestTTY with tested precedence. `-n` supplies broker
stdin EOF without reading local input; `-T` selects the existing broker's
no-controlling-TTY child semantics. Command TTY requests, interactive no-TTY
shells and incompatible legacy requests fail before authentication.
At this initial checkpoint, `-G` printed three settings offline and accepted
`-F none`. Files,
Host/Match/Include, aliases, options after destination and general SSH syntax
were unfinished; the explicit-file increment above supersedes those file/Host/
alias limits. See [CLI_OPTIONS.md](CLI_OPTIONS.md).

Ten local offline OpenSSH comparisons cover output/precedence; validation and
stdin tests cover argument preservation, refusal and no local input reads.
The complete race suite, vet and Node companion test pass; portable builds are
recorded in TESTING.md. No new native Pi evidence, transfer writes, provider
contact, publication or push was introduced. Full compatibility remains open.
Chrome DevTools could not establish the browser connection (60-second timeout);
no new Pi session was attempted. Native option verification is pending browser
access or an owner-run direct-login check; no protocol restriction is inferred.

## 2026-10-20 authentication setup fix

The user's first interactive attempt reproduced `browser authentication setup
failed`. Device matching succeeded, but Connect returned an HTTP 200
reauthentication form instead of the shell controller. The companion previously
reduced that response to an undifferentiated failure. A failing JavaScript
regression now covers the actual response/control-flow pattern; the fix recognises
the form and relays a fixed safe code. Go prints the required normal browser
sign-in steps, without exposing HTML, URLs, cookies or token values.

After opening the official browser Remote shell flow and refreshing Devices,
the rebuilt native CLI connected, returned a read-only marker, and disconnected
with local ~. status 0. The binary at bin/pi-sh is updated. The fix diagnoses a
recent-authentication requirement; it does not remove that service requirement.

## 2026-10-20 interactive-session hardening

Ctrl-D now has real-device evidence: it logs out of Bash on an empty prompt; the
CLI still reports 255 because no agent exit-status message has been observed.
Known bracketed-paste bytes reproduced forwarding of a pasted `~.` and verified
Unicode/multiline input. This does not establish the user's exact original input
or every OS clipboard variant. `-e none` now disables local escapes, and its live
test preserved a quoted multiline string with an embedded literal `~.` line.
Alternate ASCII/control-notation escapes are covered locally. General SSH option
parsing/configuration remains unfinished.

Changing the active native CLI's local PTY size propagated to remote `stty size`
as 37 rows by 111 columns. New real-PTY tests cover the actual input filter and
exact restoration. A failing cancellation regression exposed a race where
terminal restoration could be mistaken for successful session completion; this
is fixed. Unix termination signals now cancel and restore the terminal, errors
from restoration propagate, and a real native SIGTERM check returned 255.

The user's manual companion connection and Enter/typed-escape disconnect were
reported successful. The local binary is rebuilt with the new controls.
Remote command/status semantics and authentication usability remain the next
highest-impact gaps; file transfer still needs separately approved test writes.

## 2026-10-20 command-status investigation and adapter

No public source was located for the observed official 2.13.0 agent. Internal
shell startup/process-wait and native status/exec support remain unverified;
absence is not proved. The exact package's BSD-3-clause copyright declaration
was checked separately without executing or decompiling upstream code. See
[AGENT_EXIT_STATUS.md](AGENT_EXIT_STATUS.md) for primary sources and the required
external source/protocol documentation.

The original adapter (now `--legacy-command`) runs a bounded in-memory /bin/sh
adapter over the existing shell channel. No interactive trap, remote file,
installed software or persistent configuration is introduced. It reports a child
shell status using client-owned random frames; stdout/stderr remain merged and
stdin is /dev/null. Native real-device checks returned 23, 7 and 0. A local pipe
exactly matched all 24 expected known output bytes, including Unicode, binary
bytes and merged stderr. Ordinary interactive Ctrl-D/exit still returns 255.

Automated verification includes real local shell PTYs, frame splits/malformed
completion, supervisor-loss failure, authorization/cancellation, and real Pion
peers joined to a local shell PTY. The last is a command-stream simulator, not a
Connect HTTP/authentication-service simulator. See [COMMAND_EXECUTION.md](COMMAND_EXECUTION.md)
for constraints; no arbitrary binary-safe transfer, stdin streaming or other
platform runtime is claimed.

## Service-scope record

The earlier terms-based stop was a prudential engineering decision, not a provider
refusal, demonstrated protocol restriction, established violation or concrete ban
threat. The user subsequently authorised continuing private development after that
uncertainty was explained. This does not establish Raspberry Pi permission or a
legal exception; those remain open questions for publication review. The
[permission request](PERMISSION_REQUEST.md) remains an unsent optional draft.

## 2026-10-20 shell-only broker

`--experimental-command` now selects the Bash broker, with no Python requirement
and no remote files/installations. Native Connect checks verified exact binary
stdin/stdout (2048 bytes), independent binary stderr (256 bytes), absent
controlling terminal, status 23 and normal exit 143. A larger duplex fixture
initially failed after partial stderr. A local 32-message receive-queue regression
was reproduced and fixed with message/byte bounds. DevTools connection timeouts
and Connect's recent-sign-in requirement delayed the rerun; normal user browser
consent and official Remote shell access restored it. The corrected native rerun
passed exact 65536-byte stdin/stdout and 131072-byte concurrent stderr, followed by
self-SIGKILL and status 255 with no failure diagnostic. The external test blocker
is resolved for this run. No full OpenSSH compatibility is claimed.

The full local race suite passes. Actual local PTYs test blocked stdin, slow
consumers, binary bytes and signals. Fourteen cases compare streams/status with
a temporary loopback OpenSSH_10.2p1 server using pinned ephemeral keys. These
results do not establish complete CLI or service parity. See SHELL_BROKER.md.

## 2026-10-20 native operator authentication investigation

The requested external-browser login with link/code was investigated against
official documentation, public Imager source, a clean normal browser sign-in
redirect and the existing account's visible navigation. Connect's web login uses
OAuth with S256 PKCE. No public third-party operator client registration,
loopback callback or Connect session scope was found. Root discovery URLs
returned404, which does not prove the capability absent. The documented device
verification flow enrolls a Pi; it does not authorize an operator to create offers.

This route is blocked on provider-confirmed native-client registration and
Connect authorization, not on Go callback-server implementation. No enrollment,
new account authorization, token exchange or cookie export was attempted. No
placeholder `pi-sh login` was added. Existing companion access remains working.
See [CLIENT_AUTH_RESEARCH.md](CLIENT_AUTH_RESEARCH.md) and the precise unsent
[CLIENT_AUTH_REQUEST.md](CLIENT_AUTH_REQUEST.md). No external request was sent.

## 2026-10-20 direct web-session investigation

The owner declined provider contact and asked to investigate normal HTTPS web
login in Go. This is distinct from registering an independent OAuth application.
`internal/webauth` and `cmd/login-probe` now implement a bounded, memory-only
normal form/redirect probe. Real credential-free navigation reached the ID
password form without Chrome or JavaScript. The owner manually exposed the
normal MFA form in an isolated browser; only action/field metadata was read.

Password/MFA completion awaits an owner-run local probe. No working native login
or shell authorization is claimed yet; no browser cookie import, credentials
export, persistence, provider contact or Pi changes occurred. See
DIRECT_LOGIN_PROBE.md. The existing browser-assisted shell remains available.

The first local password attempt failed before OTP because the service renders
the observed challenge with HTTP 401. A synthetic 401 regression reproduced that
exact diagnostic; the fix accepts only the exact observed challenge. The second
owner attempt reported a missing prompt; a real local PTY displays it, and prompt
labels now use complete lines. A fresh owner retry is pending. No full native
credential login has been established yet.

## 2026-10-20 direct authentication milestone achieved

The owner-run probe completed password, TOTP and callback to Connect. The rebuilt
pi-sh then completed `--direct-login --experimental-command` on the real Pi:
expected marker direct-login-ok and local exit 23. Fresh HTTP login, device
discovery, shell authorization, ICE, CSRF-bound signaling, Pion and command status
now work without Chrome/extension or imported/persisted credentials. The earlier
native web-login blocker is resolved for this flow; independent OAuth client
registration remains a separate unimplemented option. See DIRECT_LOGIN_PROBE.md.

The same session stays in memory throughout the process. No remote packages,
files, services or configuration were changed. Remaining authentication work is
direct interactive and long-session/expiry/revocation testing, safe reuse policy
and native Windows/Linux plus physical iSH checks. Full OpenSSH compatibility is
not achieved. The local full race suite and vet passed after integration.
