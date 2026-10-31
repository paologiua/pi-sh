# Tests and verification — 2026-10-21

Reusable entry points: `./scripts/test.sh` for local checks and
`./scripts/test.sh full` for the documented complete checklist (explicit Bash
5.2.37/5.3 paths required). See [DISTRIBUTION.md](DISTRIBUTION.md). The scripts
do not replace the historical evidence or classify the unresolved WebRTC failure.

The SFTP integration now roundtrips1MiB of all-byte data through the actual local
Bash broker/PTY and existing OpenSSH server, both directly and over local Pion
WebRTC peers. Focused runs pass in approximately23/24 seconds. The old20-second
fixture lifetime was too short; the1MiB cases alone use90 seconds, with guaranteed
pipe release after fatal harness assertions. This is local evidence, not a
reproduction or fix of the stalled native upload. The transfer probe separately
reports fixed subphases and sentinel-based broker categories without error text.

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

## 2026-10-21 production TTY controller correction

Four same-interpreter OpenSSH comparisons now require caught/default TSTP parity.
The actual staged source runs 18 cases on each of two carriers. Four HUP/TERM
ownership tests confirm sampled processes stop before PTY close while an external
sentinel remains alive; two whole-group STOP/CONT cases observe stopped states
before retaining input/status 23 after CONT. CLI Pion/PTY now has 13 cases with
both default and caught Ctrl-Z. Focused source/cleanup/reference/CLI tests pass
three repeated race runs. TTY_JOB_CONTROL.md explains the original red cases,
the obsolete-shell exec inheritance and the foreground-group correction.
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
Selected pi-sh input/escape/session unit race tests also pass. The terminal and localio package race suites pass; all seven CGO-disabled build
and test-compilation targets, vet and module verification pass, and native clients
are rebuilt. The full post-correction networking/process race integration suite now also
passes. An initial attempt was not executed because automatic approval review
hit a usage limit; the subsequent normally approved rerun completed successfully.
This closes that validation gate only, not the remaining native/suspension/iSH work.

## 2026-10-21 job-control characterization

At the earlier checkpoint, four same-interpreter comparisons separated two
matching cases from two caught-TSTP failures. A modern-carrier extension then
exposed Ctrl-Z status loss. The controller correction above supersedes that
negative characterization with parity tests. Temporary probes and ineffective
source edits were removed. See TTY_JOB_CONTROL.md for current limits.

After the local I/O/help integration, the full race suite, vet, module verification
and Node companion regression pass. CGO-disabled builds and all test-package
compilations pass for macOS amd64/arm64, Linux amd64/arm64/386 softfloat and
Windows amd64/arm64. All three native clients are rebuilt. These are local and
compile-only results; Pi, native Linux/Windows and physical iSH gaps remain.

## 2026-10-21 local escape help

Seven actual local OpenSSH help-consumption observations pass. Production
one-byte-reader tests cover help/custom/control/disabled/quoted/midline cases,
help immediately followed by disconnect, and refused/short diagnostic writes.
The actual TTY coordinator checks stderr help separately from remote stdout and
remote status 23; interactive aliasing checks ~?~. plus terminal restoration.
Focused help/reference/TTY/interactive tests passed three repeated race runs.
See ESCAPES.md for remaining unimplemented actions and output differences.

## 2026-10-21 cancellable local file streams

The full local race suite passes after integrating grouped Unix file operations.
Focused descriptor, actual Pion/PTY TTY/interactive, legacy-command, binary-command
record-fixture and uncertain-commit cleanup tests cover cancellation under idle
input and output backpressure, joined workers, binary prefixes and file reuse.
The input regression first failed after reading an incomplete UTF-8 rune; a
separate regression first allowed a queued escape to mask known cancellation.
Both now pass. [LOCAL_IO.md](LOCAL_IO.md) identifies the exact production and
synthetic fixtures and separates Unix guarantees from Windows/generic-reader
and regular-filesystem latency limits. No new native Pi evidence is claimed.

## 2026-10-21 portable command terminal modes

Mode capture now precedes local raw mode and translates macOS/Linux termios into
the fixed ttymode vocabulary, then the existing staged adapter applies validated
stty arguments before TR/command startup. Seven new paired OpenSSH 10.2p1 cases
cover echo-off, EOF7, interrupt29/trap, raw Ctrl-D data, output processing off,
disabled interrupt and baud19200. The eighth actual Pion/PTY coordinator case
uses changed local modes and checks byte4/LF-only output/19200 plus exact local
restoration. Focused new/previous coordinator, capture, validation, quoting and
mode cases passed three repeated race runs.

The actual production dialect helper checks all256 control values against local
kernel state with no unrelated mode changes. It failed for literal caret on BSD
stty (bare caret ->0, hex text ->48), then passed after carrier-local gfmt1 field
adaptation; GNU accepts the original numeric value. Thirteen source-staging
cases check metacharacters/high bytes/disabled values. Test-only local OpenSSH
-q suppresses its close notice rather than deleting application output.

Full `go test -race ./...`, vet and seven-target CGO-disabled package build/test
compilation pass after integration. The later Darwin speed-width normalization
test additionally prevents uint64 rates truncating into valid uint32 rates;
focused terminal/ttymode regressions pass after that correction. Linux speed-code
fixtures compile but have not executed on this Mac. All native clients are
rebuilt; no Linux/Windows/physical iSH runtime or Pi evidence is claimed.
See TTY_MODES.md for the inventory, stricter remote stty failures and remaining
runtime/hardware/suspension/borrowed-I/O limits. No Pi access, new dependency,
publication, provider contact or push occurred.

## 2026-10-21 TTY CLI integration

The experimental-command CLI now selects the staged adapter for effective
RequestTTY yes/force. Configured TERM and initial dimensions are applied before
TR/command startup; resize delivery starts only after TR. Request-policy tests
cover auto/no/yes/force with terminal/pipe and StdinNull combinations. Legacy
TTY incompatibility remains a pre-auth error.

`TestTTYCommandSessionOverWebRTC` tests the actual coordinator, staged source and
local terminal with Pion peers, a remote shell PTY, a local input PTY and output
pipe. Seven cases cover quoted TERM, initial91x29, subsequent111x35, Unicode,
forced pipe/zero dimensions, -n without reading or synthesized EOF, local ~.,
authorization failure, transport loss and ordinary exit130. Every case checks
exact terminal restoration. The adapter separately tests quoted/UTF-8 TERM,
zero/nonzero dimensions and invalid settings. These new integration/settings
and exit/signal tests passed five consecutive race runs on this Mac.

Full `go test -race ./...` passed after integration. This includes no-TTY
binary/duplex/EOF/backpressure/cancellation and transfer regressions, not just
the new TTY tests. Vet, module verification and Node companion tests also pass.
CGO-disabled package builds and every test-package compilation pass for macOS
amd64/arm64, Linux amd64/arm64/386 softfloat and Windows amd64/arm64. All three
native client binaries are rebuilt. An offline -G/-tt/-n smoke prints force/yes;
an invalid TERM is rejected with255 before any authentication or network setup.
No new native Pi or Linux/Windows/iSH runtime is claimed;
full terminal-mode negotiation, stopped jobs/suspension and borrowed blocking-I/O
lifecycle remain open. No remote write, software change or new dependency.

Review also exposed that commandrun invoked an adapter despite a known cancelled
context/queued auth failure. `TestKnownSessionFailureNeverStartsAdapter` failed
before the preflight fix (starts1/context canceled) and passed100 repeated race
runs afterward, checking no invocation and preserved causes for both conditions.
This is a shared lifecycle fix for command and transfer coordinators, not proof
of zero latency for detecting actual service revocation.

## 2026-10-21 actual staged TTY adapter

`TestStagedTTY*` exercises the existing production NewTTY.Run implementation,
including actual in-memory upload, readiness and result parsing, over a local
Bash PTY. It does not substitute ttyJobsScript for the embedded source. Seven
paired functional cases use local OpenSSH_10.2p1 / LibreSSL 3.3.6; additional
cases cover normal versus signal exit, Ctrl-C/trapped INT, first bytes, early
Unicode input without a command prompt, an exactly 128 KiB command, completion
with a background child, and exhausted-input cancellation without synthesized
VEOF or claimed cleanup. See TTY_COMMANDS.md for the precise evidence limits.

All new tests passed three consecutive race runs on macOS arm64 with Go 1.27.2
and PI_SH_TEST_BASH=/private/tmp/bash-5.3/bash. Full `go test -race ./...`,
`go vet ./...`, `go mod verify` and the Node companion test passed. The initial
sandboxed tests failed because local loopback binds/process inspection were
prohibited; the same tests passed with local test permissions. No failed
sandbox run is counted as evidence of a TTY or Connect defect.

CGO-disabled package builds pass for macOS amd64/arm64, Linux amd64/arm64/386
softfloat and Windows amd64/arm64. Every test package also compiles for
Linux/386 softfloat. Cross-target execution and physical iSH remain unverified.
No production CLI behavior, Pi session, remote write,
publication, provider contact or push occurred in this increment. TTY CLI
selection/terminal/TERM/resize/lifecycle integration remains open.

## 2026-10-20 Bash foreground-worker TTY candidate

Fifteen paired PTY/OpenSSH cases pass five repeated race runs: seven terminal
cases plus ordinary 0/130/143, self INT/TERM/KILL, terminal Ctrl-C and a caught
INT exiting 42. Two further cases deliver HUP/TERM directly to the local
supervisor; exact termination signals and ps/TIOCGPGRP snapshots prove captured
owned processes are gone or reap-pending zombies before PTY close, including a
command/descendant ignoring HUP/TERM. Both also pass five repeated race runs.
Temporary debug instrumentation was removed. The fixture's failed-candidate
safeguard terminates only captured owned groups and cannot satisfy its oracle.

This is a test-only Bash/cat/stty composition, not command -t support or a native
agent teardown result. The original POSIX negative comparisons are retained.
See TTY_COMMANDS.md for rejected approaches, startup/descriptor constraints and
the production staging/control/lifecycle work still needed.

After the final HUP/TERM cases, full `go test -race ./...` and `go vet ./...`
passed. CGO-disabled package builds and every test-package compilation passed
again for darwin amd64/arm64, linux amd64/arm64/386 softfloat and windows
amd64/arm64. No production runtime/CLI code or binaries changed in this increment;
the new fixtures executed on macOS only, with explicit reference-tool/Bash skips
on environments lacking those tools. No Pi access, file write or service change.

## 2026-10-20 command TTY investigation

Nine paired real-PTY/OpenSSH candidate cases passed five repeated race runs;
seven confirm selected TTY/input/output/resize/normal-exit behavior and two
deliberately assert the candidate's SIGINT-vs-exit mismatch. A separate real
OpenSSH -tt exhausted-stdin reference passed three repeated race runs, observing
no synthesized canonical EOF during 200 ms after readiness. Passing negative
evidence is not TTY-feature support. See TTY_COMMANDS.md.

The full local `go test -race ./...` and `go vet ./...` passed. CGO-disabled
builds and every test-package compilation passed for macOS amd64/arm64, Linux
amd64/arm64/386 softfloat and Windows amd64/arm64. New fixtures execute only on
Unix with available local SSH tools; absent/permitted-account reference skips
must remain visible. They were executed on macOS here, not Linux/Windows/iSH.
No production CLI implementation, native Pi session or runtime binary changed.

## 2026-10-20 regular-file metadata

Full `go test -race ./...` and `go vet ./...` passed after adding -p to both
transfer clients and get/put/reget/reput options. 48 paired local OpenSSH
sftp/server comparisons check data, mode and pre-read atime/mtime on new/existing
targets, with global/command and resume/flush flags. Eight paired local scp -D
comparisons verify the pi-cp policy. A real server refusing SETSTAT/FSETSTAT
confirms advisory metadata status behavior; a refused READ skips preservation.
Wire/memory tests check subsequent metadata after a failed upload, data-failure
retention, redacted warnings and OS atime snapshots. Memory-server chmod/times
are not used as evidence of applied filesystem metadata.

CGO-disabled builds and every test-package compilation passed for darwin
amd64/arm64, linux amd64/arm64/386 softfloat and windows amd64/arm64. Local client
and probe binaries were rebuilt. No native Linux/Windows/iSH runtime or remote
metadata/content-write proof follows. SFTP_METADATA.md records path-based remote
timestamps and missing attribute-presence flags as remaining implementation gaps.

Separately, the owner reported rebuilt direct-login read-only pwd/quit status 0
on the Pi, now including the process-local umask-prefix startup. That metadata
check is not a content transfer, file-creation-permission or preservation test.

## 2026-10-20 SFTP resume

Full `go test -race ./...` and `go vet ./...` passed after adding global/command
-a and reget/reput. Resume tests passed 48 memory scenarios, 48 paired actual
local OpenSSH client/server comparisons, four empty-source cases, two failed-WRITE
cleanup cases and one short-READ/failure case. Comparisons check status class
and bytes, not complete diagnostic text or numeric Connect-failure exit parity.
All fixtures use memory or automatically removed local temporary directories.
No Pi transfer/write test was performed.

CGO-disabled builds and every test-package compilation passed again for darwin
amd64/arm64, linux amd64/arm64/386 softfloat and windows amd64/arm64. Local client
and login-probe binaries were rebuilt. Native Linux/Windows and physical iSH
remain untested. SFTP_RESUME.md records scope and integrity limitations.

## 2026-10-20 safe HTTPS failure categories

Seven synthetic cases failed with the original generic diagnostic, then passed
with exact allowlisted categories for DNS/timeout/certificate/connection/other
transport and response-read/size failures. Requests contain synthetic secret
callback values to check redaction. Full `go test -race ./...` and `go vet ./...`
passed; CGO-disabled package builds passed for darwin amd64/arm64, linux
amd64/arm64/386 softfloat and windows amd64/arm64. All four local client/probe
binaries were rebuilt. Test-package cross-compilation was not repeated for this
increment; prior runs are recorded below. No new runtime platform claim follows.

Credential-free preflight first reproduced the owner's generic HTTPS diagnostic;
the classified rerun showed DNS, and the final rebuilt probe reached
password_required with status 0. This is intermittent development-environment
evidence, not a confirmed diagnosis of the owner's attempt or authenticated SFTP
verification. No credentials, remote commands or Pi writes were used.

## Automated local tests

Run `go test ./...`, `go test -race ./...`, and `go vet ./...`.
The final suite covers:

The 2026-10-20 CLI-option increment also passes the full local race suite, vet
and Node companion regression. The initial sandboxed run could not bind local
TCP/UDP listeners; rerunning with local-network permission passed all packages.
`TestOptionsOpenSSHDifferential` compares the supported configuration fields
against ten offline `ssh -G -F none` invocations. This test does not connect to a
server or load user/system SSH config. CLI validation and unreadable-input tests
verify refusal and null-stdin semantics. See [CLI_OPTIONS.md](CLI_OPTIONS.md).

CGO-disabled builds and every test package compilation passed after this increment
for macOS amd64/arm64, Linux amd64/arm64/386 softfloat, and Windows amd64/arm64.
The binaries for other targets were not executed. Physical iSH is still untested.
Native binary smoke checks verified that `-G -Fnone -nTe! -oEscapeChar=none`
prints the expected settings and returns 0 without authentication; an explicit
command `-tt` request returns 255 before any login prompt or network setup.

| Package | Actual checks |
| --- | --- |
| internal/sshconfig | Explicit files, Host/negation/byte wildcards, first values, grammar/CRLF, one-pass tokens, bounds/read failures and error redaction |
| internal/platform | TCP/UDP loopback, goroutine/timer, cancellation |
| internal/transport | Real local ICE/DTLS/SCTP peers, bidirectional text/binary/empty/large messages, ordering, fingerprint rejection, close, cancellation and bounds |
| internal/terminal | Real Unix PTYs: raw control bytes, size changes, exact restoration, cancellation and concurrent cleanup; portable rejection tests |
| internal/browserauth | Real loopback HTTP: exact origin/capability boundary, preflight, configuration and offer/answer exchange, destination/body limits, error redaction and missing-browser timeout |
| internal/shellbroker | Real Bash PTYs: all byte values, independent streams, EOF, early close, no controlling TTY, duplex/blocked input/slow consumer; 14 local OpenSSH stream/status differential cases; framing validation |
| internal/command | All frame splits, startup bounds, invalid/truncated/trailing status, cancellation; actual local shell PTY command statuses/quoting/known byte output/null stdin and missing-supervisor failure |
| cmd/pi-sh | Unicode/control input including one-byte short reads, bracketed paste and configurable/disabled local escapes, invalid UTF-8 rejection, send failures; real Unix PTY input/restoration; actual termination signals; session end reasons and cancellation race regression |
| cmd/protocol-probe | Bounded/cancellable signaling input and error redaction |

These automated network tests use local peers/listeners only. They do not contact
Connect or require account credentials. Browser-companion HTTP tests use dummy
session descriptions, not captured service fixtures. Callback injection is used
only for explicit queue-overflow testing and is not called a wire test.

`node --test internal/browserauth/companion_test.mjs` executes the embedded
companion's actual control flow with a minimal Devices-page/DOM contract and a
synthetic HTTP 200 reauthentication response. It verifies the fixed failure code
and that no configuration/connection is submitted. This regression failed before
the fix and passed afterward. Node is a development-only test runtime; CI installs
Node 26. Go tests independently verify safe recovery text and redaction of unknown
codes/extra fields.

## Authorized real-device tests

[REAL_DEVICE_TESTS.md](REAL_DEVICE_TESTS.md) records actual native Go access to the
unchanged Pi through official Connect infrastructure. The developer probe verified
a TTY, CONNECT_TTY, two interactive round trips, resize to 100x32 and remote close.
The actual CLI verified markers, 80x24 initial size, Ctrl-C interrupt/recovery,
Unicode, operation after a periodic authorization check and local ~. disconnect.
The final companion implementation also stayed usable beyond its initial 90s
watchdog window, then terminated correctly when its browser page was reloaded
and authorization heartbeats ceased.

History persistence was disabled inside diagnostic shells. No test data, helper,
package, service, configuration or persistent file was installed or modified.
No raw authenticated/session captures are fixtures. These tests are distinct
from the earlier ordinary official-browser shell check.

Additional native checks on 2026-10-20 verified Ctrl-D logout, single-line wrapped
`~.` forwarding, Unicode/multiline paste, `-e none` preserving an embedded literal
tilde/dot line, live CLI resize to 111x37, and local SIGTERM exit 255. Injected paste
bytes do not establish all OS clipboard variants. See the separate dated section
in REAL_DEVICE_TESTS.md.

## Experimental command checks — 2026-10-20

`internal/command` tests run actual local shell PTYs and reject missing supervisor
status; framing tests split every byte and reject malformed/trailing completion.
`cmd/pi-sh.TestCommandOverLocalWebRTCAndPTY` joins real local Pion peers to a local
shell PTY and executes the actual coordinator. Cancellation and authorization
failure tests check that no command status is invented. This is simulated command
stream integration, not an independent Connect HTTP/auth simulator.

The legacy adapter had three authorised native sessions that returned command statuses 23, 7 and 0. The
status-7 case exactly matched known 24-byte output via a local in-memory pipe,
including Unicode, NUL, 0xff, CR/LF and merged stderr. No arbitrary output/stdin,
remote signals, background jobs, or interactive-shell exit status is inferred.

## Portable builds and CI

Local Go 1.27.2 on macOS arm64 compiled packages and every test package with
CGO_ENABLED=0 for:

- macOS amd64 and arm64;
- Linux amd64, arm64, and 386 (both sse2 and softfloat);
- Windows amd64 and arm64.

The native CLI and developer probe are included. Test binaries for other targets
were compiled, not executed. Native Linux/Windows and physical iSH runtime remain
unverified. The workflow runs unit/integration tests on Linux, macOS and Windows,
race tests on Linux, and portable build/test compilation, including Linux/386
softfloat. The workflow itself has not run on GitHub because no repository has
been published by this task.

The official Go archive used for local development was checksum-verified before
extraction. SHA-256 of go1.27.2.darwin-arm64.tar.gz:
`76812b213b1b2302c978d28fa52fa92d541704b9e7d9d5db8002c50e4018c4c5`.

## Dependency and vulnerability checks

`go mod verify` and `go vet ./...` passed. Imported package/test-module root licences
and original notices are recorded in DEPENDENCIES.md and THIRD_PARTY_NOTICES.md.
This inventory is not a file-by-file release licence audit.

The initial Pion dependency graph selected x/net v0.50.0, where govulncheck found
reachable [GO-2026-5942](https://pkg.go.dev/vuln/GO-2026-5942). Updated to fixed v0.56.0,
with x/crypto v0.53.0. Terminal support selected x/term v0.47.0 and x/sys v0.49.0.
The scan against the native CLI/companion reported zero reachable vulnerabilities
and nine module-level findings not shown as reachable. This is not a blanket
security guarantee and must be rechecked for release and other platform builds.

```sh
go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...
```

## Explicit configuration-file tests — 2026-10-20

Twelve valid and six invalid synthetic-file comparisons with local OpenSSH_10.2p1
verify Host selection, CLI/file precedence, HostName tokens and syntax. The earlier
ten CLI comparisons remain; all 127 non-NUL ASCII escape bytes have dump
comparisons. Regressions failed before correcting pattern case, attached comments
and space/backslash/DEL rendering. These offline `ssh -G` tests use no Pi, account
SSH config or SSH server. Additional module tests cover bounded reads, regular
files, redacted errors, inactive-directive refusal and unchanged commands. See
CONFIGURATION.md for incomplete directives and Connect-specific target semantics.

The later Include increment adds 18 offline OpenSSH comparisons for pattern/file
order, nested/caller/inactive Host scope, quoted/dot/class paths, effective and
original-host tokens, CLI HostName precedence, one-pass environment values,
tilde/home-relative paths and nested relative Includes. The existing ~/.ssh is
only traversed for a relative-path reference; if absent, that case explicitly
skips rather than creating it. All sources remain temporary local files.
Module fixtures exercise aggregate byte/file/depth/entry/match budgets, identity
cycles/hard links, symlink/.. traversal, FIFO refusal, override-map ownership and
redacted errors. Expanded HostName allocation has a separate 4096-byte bound.
pi-cp/pi-sftp frontend tests now load an included incompatible policy and reject
it before login. Native aliases/Includes remain unverified; this does not extend
the prior Pi shell/SFTP evidence.

After Include integration, the full local race suite and vet pass again, as do
CGO-disabled builds and every test-package compilation for macOS amd64/arm64,
Linux amd64/arm64/386 softfloat and Windows amd64/arm64. Native binaries for all
three clients are rebuilt. The offline pi-sh -G smoke resolves a synthetic alias
from an included temporary file; all smoke files are removed. Other target
binaries were not executed and no Pi session/write was performed.

The full local race suite, vet and Node test pass after explicit-file integration.
CGO-disabled builds and test-package compilation pass for macOS amd64/arm64,
Linux amd64/arm64/386 softfloat and Windows amd64/arm64. The rebuilt native binary
prints a synthetic alias's expected effective target/settings; CLI -T overrides
file RequestTTY force. Without that override, command mode returns 255 before a
login prompt or network setup. The local synthetic smoke config was removed.

## Broker cancellation and local signals — 2026-10-20

Real local Bash PTYs verify that the owned command leader is reaped and its known
descendant is not running before the still-open carrier closes, plus five early
worker cancellations. The first offers 1 MiB stdin to a nonreading child while draining
128 KiB output, then cancels, exercising a blocked input worker too.
Synthetic fixtures check launch/readiness cancellation,
local writer failure, acknowledged/closed/silent aborts, and coordinator auth-loss
and cancellation arbitration. A real Pion pair and a synthetic broker peer test
the actual connect/coordinator cleanup path. These are not Connect service or
native agent fixtures. A temporary loopback OpenSSH_10.2p1 server additionally
verifies actual INT/TERM/HUP/QUIT of the local SSH client return 255, with open
stdin and remote INT/TERM traps. All fourteen prior stream/status fixtures remain.

Race detection exposed a test-buffer bypass via bytes.Buffer's promoted ReadFrom;
composition fixes that before accepting the signal test. The corrected focused
tests pass under race. No live authentication/session material is retained, and
the owner subsequently verified the direct -nT Ctrl-C/status255 path. Native auth revocation, network loss and independent process inspection remain pending.

After this increment the complete local race suite, vet and Node companion test
pass. CGO-disabled builds and every test-package compilation pass for macOS
amd64/arm64, Linux amd64/arm64/386 softfloat and Windows amd64/arm64. Those other
target binaries were not executed. The native bin/pi-sh was rebuilt.

## SFTP and copy tests — 2026-10-20

The shared commandrun coordinator remains covered through pi-sh's cancellation/
auth arbitration tests. connectsession has a real Pion dial/readiness/single-use
cancelled-start test, alongside the existing TLS authentication/signaling suite.
SFTP pipe tests use an in-memory server for all-byte file round trips, clean EOF,
idempotent close, cancellation, oversized malformed packets and error redaction.
Console/copy tests cover regular binary files, multiple-source directory preflight,
spaces/quotes/Unicode/colon operands, aliases, batch stop/ignore/quiet, directory/
link/file operations and refusal of unsupported flags. Windows file fixtures use
valid local Windows names; no native Windows runtime result is claimed here.

Actual local OpenSSH sftp-server tests check a quote-sensitive executable path,
get/put preserving link/.. traversal, chmod 600 and server-enforced -R create
rejection. A separate actual Bash/PTY/broker/SFTP test transfers exact 8192-byte
all-byte data, renames/removes the local test file and verifies clean final status.
All files are in t.TempDir or memory; no Pi transfer write occurs.
Default-mode tests compare actual OpenSSH CLI creation for source modes
600/640/700/777 under server umasks022/077, check existing-target modes and new
directory modes, and use server -P fsetstat to verify failed uploads remain empty
and private. Setup prefix tests reject malformed/oversized masks without consuming
SFTP bytes. Private creation uses only a process-local mask, never configuration.

The full local race suite, vet, Node companion, module verification and seven-target
CGO-disabled build/test compilation pass after integration. Native binaries for
pi-sh/pi-cp/pi-sftp are built. govulncheck reports zero reachable vulnerabilities,
three imported-package and six module findings not shown reachable. The owner
reported native direct read-only metadata status0 for the initial binary. The
later mask setup, file-content transfers and physical iSH remain pending.

## Transfer option and forwarding candidate checks — 2026-10-20

The later flush increment adds 14 OpenSSH CLI comparisons (global/command -f)
against a local test-only in-memory server with absent/failed/dropped/malformed
fsync behavior and write failures. Matching Go-client fixtures verify status,
data, redaction and failure propagation. Actual local OpenSSH server debug data
(memory only) verify writes, fsync and CLOSE order, and no fsync when -P fsync
removes the advertisement. Fault fixtures verify sync/close arbitration, including
advisory sync failures that cannot erase copy errors. See SFTP_FLUSH.md.

Twenty no-flush upload regressions check complete write refusal and a retained
1024-byte prefix followed by refusal, across 1024/32768 packet and buffer limits.
They protect both frontends' shared copy against a pinned ReaderFrom final-short-
read WRITE-error loss observed before the plain-interface copy fix. All test data
remain local temporary files or memory. This is not Pi disk-full/flush evidence.

After flush/error-propagation integration the full local race suite and vet pass.
CGO-disabled builds and every test-package compilation pass for macOS
amd64/arm64, Linux amd64/arm64/386 softfloat and Windows amd64/arm64. All three
native binaries are rebuilt; pi-sftp -fb- reaches the expected incompatible-TTY
policy refusal before login. Cross-target binaries were not executed; no new Pi
session or transfer write was performed.

Transfer-option tests now exercise both frontend entry points up to configuration
rejection before login, plus cliargs attached/separate values, '--', operand
boundaries and unsupported flags. An actual local OpenSSH sftp process verifies
-b-, -Fnone, -oBatchMode=yes and the literal filename '=commands' in -b=commands.
No SSH/network authentication is used by that reference (-D runs a local server).

Forwarding candidate tests use IPv4 loopback only: Go CloseWrite proves the
EOF-driven response fixture, Bash /dev/tcp plus two cats proves missing socket EOF
with a reader still alive, and an optional existing-local-nc test measures its
actual binary/EOF/response behavior. On this Mac all three run/pass with modern
Bash and system nc. Missing/no-default-half-close nc is an explicit skip; no Pi
utility or forwarding support is inferred. See FORWARDING.md for the measured
blocker and remaining capability evidence.

After this increment, the full local race suite and vet pass again. CGO-disabled
package builds and every test-package compilation pass for macOS amd64/arm64,
Linux amd64/arm64/386 softfloat and Windows amd64/arm64. Cross-target binaries
were not run; the Bash/nc TCP fixtures above ran on this Mac only.

## Required remaining verification

No independent Connect HTTP/authentication-service simulator, full SSH differential
suite, native agent-status tests, complete binary-stream stress coverage, native transfer and forwarding tests,
forced relay, authorization revocation, ICE renewal, native Windows console or
physical iSH tests exist yet. Remote transfer-write tests require an explicitly
approved location and cleanup. Do not put live credentials into CI.

## Shell-only broker tests — 2026-10-20

On this Mac the full `go test -race ./...` suite passed with
`PI_SH_TEST_BASH=/private/tmp/bash-5.3/bash`. macOS system Bash 3.2 does not satisfy
the remote broker requirement; modern Bash tests skip with an explicit reason
when unavailable. Linux CI uses existing Bash. The local setsid helper is compiled
from the test package only and never shipped or executed on the Pi.

`TestOpenSSHDifferentialCommandBehavior` starts a temporary SSH server on IPv4 loopback with
ephemeral keys, password login disabled and strict pinned-host verification. It
uses no account SSH configuration or Pi SSH transport. The measured reference
was OpenSSH_10.2p1, LibreSSL 3.3.6. Fourteen fixtures compare bytes/status: all-byte
stdio, EOF, no controlling terminal, normal exits including 143/255 and self
TERM/INT/KILL. Missing local sshd/account support is an explicit skip, not a pass.
This is a selected execution differential suite, not full OpenSSH equivalence.

Native broker evidence and the larger duplex fixture are recorded in
REAL_DEVICE_TESTS.md. The 2048-small-fragment transport regression failed before
the queue fix; the byte-budget/accounting regression also passes under `-race`.
The native rerun after reconnection/normal reauthentication passed exact 64 KiB
stdin/stdout and 128 KiB stderr with self-SIGKILL status 255 and no failure diagnostic.

GNU Bash 5.3 was built only under the Mac's temporary directory from the official
GNU archive. Its SHA-256 matched the independently obtained Homebrew formula:
`0d5cd86965f869a26cf64f4b71be7b96f90a3ba8b3d74e27e8e9d9d5550f31ba`.
No signature verification is claimed. Bash source is not copied into pi-sh.

After the shell-broker/transport updates, CGO-disabled package builds and test
compilation passed again for macOS amd64/arm64, Linux amd64/arm64/386 softfloat,
and Windows amd64/arm64. `go vet ./...` and the Node companion regression passed.
These cross-target binaries were not executed.

## Direct HTTPS login investigation — 2026-10-20

`internal/webauth` adds local TLS fixtures using synthetic password/OTP, in-memory
cookies, server-derived CSRF and callback navigation. Regressions cover unknown
MFA routes, changed/ambiguous forms, empty device IDs, credential redirect replay,
exact origins, public-suffix cookies, HTTP200 false success and URL redaction.
The package passes with the race detector. Live default `login-probe` reached
password_required without submitting credentials; this is not a completed login.
The operator-run password/OTP attempt and shell integration remain pending.

## Direct login integrated with native CLI

The owner completed a fresh native login with password/TOTP and then ran the
updated pi-sh direct-login command on the real Pi. Expected marker and status 23
passed. This is owner-reported real-device evidence, separate from synthetic TLS
tests. It does not verify all direct interactive or long-session behavior.

The full local race suite and vet pass after integration. New tests include
CSRF/header-bound signaling with own cookies, device mismatch and polling-origin
rejection, one creation attempt, non-204 authorization failure, close/cancel, and
real PTY password/code prompts and cancellation/restoration. A cancelled-reader
race was found and fixed by serializing mode changes and restoration. The
synthetic HTTP suite does not pretend its descriptions establish real WebRTC;
transport and previous Pion/PTY tests remain distinct. No live login data are
fixtures. See DIRECT_LOGIN_PROBE.md for scope.

After direct-signaling integration, CGO-disabled package builds and every test
package compilation passed again for macOS amd64/arm64, Linux amd64/arm64/386
softfloat, and Windows amd64/arm64. Those target binaries were not executed.

## Bash 5.2 timed-read regression — 2026-10-22

`TestShellBrokerMegabyteStdinCount` sends 1MiB of every byte value to
`wc -c` (the native read-only fixture) with whole records and with a test-only
carrier that delays each record newline by 0.5–1.5 ms. Run it with the Pi's
Bash version as well as 5.3:

```sh
PI_SH_TEST_BASH=<scratch>/bash-5.2/bash go test ./internal/shellbroker -run MegabyteStdinCount -count=3
```

Bash 5.2.37 was built only in the session scratch directory from the official
GNU `bash-5.2.tar.gz` (SHA-256
`a139c166df7ff4471c5e0733051642ee5556c1cc8a4a78f145583c5c81ab32fb`) plus official
patches 001–037; no signature verification is claimed. Before the fix the
late-newline case stalled 2/2 with 5.2.37 and passed 3/3 with 5.3; after it,
3/3 with both. The whole shellbroker and cmd packages pass with 5.2.37, and the
full race suite with 5.3. That 5.2 build is ephemeral and not used by CI.

## Broker throughput benchmark and data-path regressions — 2026-10-23

`TestShellBrokerThroughputBenchmark` is opt-in. It runs the real broker over a
local PTY with an order-preserving carrier that delays each direction by half
the simulated round trip (sends return after local queuing, like WebRTC):

```sh
PI_SH_BROKER_BENCH=1 PI_SH_BROKER_BENCH_BYTES=262144 PI_SH_BROKER_BENCH_RTT=0,10,40 \
  go test ./internal/shellbroker -run ThroughputBenchmark -v -timeout 3600s
```

Results before and after the pipelined data path are in SHELL_BROKER.md
("Data-path throughput"); both Bash 5.3 and the scratch 5.2.37 were measured.
New regressions: `TestShellBrokerEarlyCloseWithPipelinedInput` (command closes
stdin with several input records in flight), `TestShellBrokerPartialOutputIsNotHeldBack`
(a short prompt reaches the client while the command waits for input),
`TestBrokerAbortAcceptsCompletionAlreadyInFlight` and
`TestShellBrokerAbortAfterRemoteCompletion` (cleanup confirmed when `X` was
already in flight). The cmd/pi-sh synthetic broker now speaks the
`I seq length hex` record. The shellbroker package passed with Bash 5.3 (under
`-race`) and 5.2.37; `TestShellBrokerMegabyteStdinCount` including
`late-newlines` passes with both. Native throughput is unmeasured;
`scripts/native-broker-throughput.sh` (dry run by default) is the read-only
procedure.

## Intermittent `caught-suspend` diagnosed — 2026-10-26

`TestStagedTTYControllerAcrossCarriers/*/caught-suspend` failed again after the
broker merge (`seen:` with an empty line). A bare-PTY script with no pi-sh code
(send `^Z`, wait for the trap's output, then `x\r`) reproduced it with macOS
`/bin/sh`, Bash 3.2: 3 of 100 runs lost the line, while Bash 5.3 lost 0 of 100.
The fixtures ran their command under `SHELL=/bin/sh`, so the loss came from
Bash 3.2 and not from the TTY adapter. A trapped TSTP also interrupts `read` in
modern Bash (status >128), which made `^Z` and the line in one write racy too.

The fixtures (`caught-suspend` in the staged controller and OpenSSH differential
tests) now run under the modern test Bash with an absolute path (the reference
sshd's PATH resolves `bash` to 3.2), retry an interrupted `read`, and send the
line only after the handler's output (`sendTTYInput`); the controller test checks
handler and line in order, since the echo now falls between them. Both tests
then passed 30 consecutive runs (`-count=30`), followed by the full race suite.
This does not test Bash 3.2 as a remote shell; the Pi's shell is Bash 5.2.37.

`openSSHDump`/`compareDump` moved from the Unix-only `match_test.go` to the
portable `openssh_dump_test.go`: the Windows test build had failed since the CLI
branch (`account_test.go`, `completeness_test.go`). The seven targets now
compile the tests.

## Verification after merging broker and terminal branches — 2026-10-27

On `main` 81df74a: root and fork vet, `go mod verify`, fork race suite and the
seven CGO-disabled targets (build and test compilation) passed. A first full race
run, while separate copycli repetitions loaded the Mac, failed three tests:
`TestRecursiveCopyAgainstLocalSCP` (one regular-file atime; control intact, the
environmental reader described in SFTP_LISTING.md hits about 65% of files),
`TestShellBrokerAbortAfterRemoteCompletion` ("cleanup was not confirmed") and
`TestSFTPMegabyteOverActualBrokerWebRTCAndOpenSSHServer` ("connection lost"),
with the shellbroker package taking 185 s instead of about 70 s. Rerun without the
extra load: the recursive differential 6/6, the two broker tests 5/5 under
`-race`, then the whole race suite passed. The broker pair is load-sensitive:
abort waits a bounded time for `X`/`D`, and reports cleanup as unconfirmed when
it does not arrive, which is the honest outcome rather than a false success.

## Perl forwarding connector — 2026-10-27

`go test -race ./internal/fwdbroker/` runs every adapter test with the netcat
double and with the local `perl` (subtests `nc` and `perl`; the perl ones skip
without a perl on PATH), under `PI_SH_TEST_BASH` 5.3 and the Bash 5.2.37 build.
To force the connector on a device for a native check, set
`PI_SH_FORWARD_CONNECTOR=perl` (or `nc`, `ncat`, `socat`); the tool must still be
detected there, and the `-v` line `forwarding carrier ready connector=…` shows
the choice.

`TestSFTPThroughputBenchmark` (internal/shellbroker, opt-in with
`PI_SH_SFTP_BENCH=1`) times 1 MiB up and down through the real broker and the
local OpenSSH sftp-server for 1 KiB and 32 KiB requests (SFTP_BACKEND.md
"Request size").

**Perl output reader (2026-10-28).** The shell broker's output workers run
a Perl reader, and its input worker a Perl writer, when perl compiles them;
`PI_SH_BROKER_WORKERS=bash` keeps the Bash workers, and the full verification
runs `internal/shellbroker` and `cmd/...` once more with it.
`TestShellBrokerUsesPerlWorkersWhenPresent` and
`TestShellBrokerFallsBackToBashWorkers` cover both paths (SHELL_BROKER.md).

**Perl adapter (2026-10-28).** `eachConnector` now runs every adapter test in
four subtests, connector/adapter: `nc/bash`, `perl/bash`, `nc/perl` (perl on
PATH beside the netcat double, `PI_SH_FORWARD_CONNECTOR=nc`) and `perl/perl`;
startMux checks the adapter it got. `PI_SH_FORWARD_ADAPTER=bash` keeps the
Bash loop (on a device too); the full verification runs `internal/fwdbroker`
and `cmd/pi-sh` once more with it. New: `TestPerlAdapterFallsBackToBash`,
`TestPerlAdapterHasNoApostrophe`, `TestCarrierLossTerminatesConnectors`,
`TestStreamReplacedInOneBatchDoesNotStall` (it hung the first Perl adapter and,
under Bash 5.3, the Bash adapter: FORWARDING.md "Perl adapter"); in
internal/transport `TestReceiveQueueHoldsSenderWithoutLoss` replaces
`TestReceiveQueueFailsWithoutSilentLoss`.

## Forwarding throughput benchmark and close regression — 2026-10-28

`TestForwardingThroughputBenchmark` (internal/fwdbroker) is opt-in and measures
one forwarded connection in each direction with the netcat double and with the
local Perl, over a PTY carrier delayed by half the simulated round trip:

```sh
PI_SH_FWD_BENCH=1 PI_SH_FWD_BENCH_BYTES=524288 PI_SH_FWD_BENCH_RTT=0,40,150 \
  go test ./internal/fwdbroker -run ThroughputBenchmark -v -timeout 600s
```

Results before and after are in FORWARDING.md "Throughput". New regression:
`TestAdapterCloseAfterEOFDeliversQueuedInput` (close right after EOF in both
directions must still deliver the queued input). The fwdbroker and cmd/pi-sh
packages pass with Bash 5.3 (also under `-race`, five repetitions) and 5.2.37.

`TestForwardingConcurrencyBenchmark` (same opt-in) runs 1, 16 and 64 concurrent
256 KiB downloads through the Perl connector with a 40 ms round trip and checks
every byte; results in FORWARDING.md "Many streams". Regression for the
connector's eager reads: `TestPerlConnectorDrainsWhileReaderStalls`.
`TestRemoteForwardingConcurrencyBenchmark` (same opt-in) does the same for -R
with 1, 16 and 64 connections exchanging 64 KiB each way; `PI_SH_FWD_BENCH_RTT`
(one value, ms) replaces its 40 ms round trip. `TestAdapterWindowMatchesMux`
checks that forward.bash and mux.go use the same window (FORWARDING.md
"Adapter window").

**SIGHUP under nohup.** A test binary started with SIGHUP ignored (for example
from `nohup`) passes that disposition to every child at exec, so the reference
ssh and the fixtures ignored the hangup cases of
`TestOpenSSHLocalSignalsTerminateClient` and
`TestTTYJobTableCandidateHUPCleanupBeforeCarrierClose`, which then failed. The
shellbroker TestMain installs a SIGHUP handler in that case, which exec resets
to the default action.

## Perl broker and the PI_SH_REMOTE_IMPL switch — 2026-10-29

The shell broker runs in Perl when the device's perl compiles broker.pl, and
otherwise in Bash (SHELL_BROKER.md "Perl broker"). `PI_SH_REMOTE_IMPL=bash`
keeps every remote program in Bash where the device has Perl, for diagnosis:
the Bash broker with Bash workers. The older `PI_SH_BROKER_WORKERS=bash` now
does the same for the broker. `PI_SH_FORWARD_ADAPTER=bash` still selects the
Bash forwarding adapter.

`eachImpl` runs the broker tests once per implementation, as `eachConnector`
does for the forwarding adapter. Further tests check which implementation is
running and the fallback with a perl that cannot compile the program
(`TestShellBrokerUsesPerlBrokerWhenPresent`, `TestShellBrokerFallsBackToBash`).

The full verification, normally required to report `fail=0` before every commit
(the owner's 2026-10-30 integration decision below is an explicit exception):
- `gofmt -l cmd internal` prints nothing;
- `go vet ./...`, and `go vet -C third_party/sftp -stdmethods=false ./...`;
- `go mod verify`;
- `go test -race ./... -timeout=300s`, and
  `go test -C third_party/sftp -race ./... -timeout=120s`;
- with Bash 5.2.37 as `PI_SH_TEST_BASH`:
  `go test -count=1 ./internal/fwdbroker ./internal/shellbroker ./cmd/pi-sh`;
- `PI_SH_FORWARD_ADAPTER=bash go test -count=1 ./internal/fwdbroker ./cmd/pi-sh`;
- `PI_SH_BROKER_WORKERS=bash go test -count=1 ./internal/shellbroker ./cmd/...`;
- `PI_SH_REMOTE_IMPL=bash go test -count=1 ./internal/shellbroker ./internal/fwdbroker ./cmd/...`,
  with Bash 5.3 and with Bash 5.2.37;
- `node --test internal/browserauth/companion_test.mjs`;
- for linux/386, linux/amd64, linux/arm64, darwin/amd64, darwin/arm64,
  windows/amd64 and windows/arm64, with `CGO_ENABLED=0` and
  `GO386=softfloat`: `go build ./...`, `go test -c` of the module and of
  third_party/sftp;
- building bin/pi-sh, bin/pi-cp, bin/pi-sftp and bin/transfer-probe.

Tests that depend on timing are also repeated under load, with one
`yes >/dev/null` per core and `-count`.

**`ctrl-z-handler` in `TestTTYCommandSessionOverWebRTC`.** This is the
intermittent failure recorded in SHELL_BROKER.md "Input window" ("Ctrl-Z lost
command completion"). It failed once more in the Bash 5.2 step of the
verification. Over 200 repetitions it failed 6 times on this branch and 4 times
on `7500852`, so it predates the Perl broker, which the TTY path does not use.

It is the `caught-suspend` defect of 2026-10-26: this fixture had not been
corrected then. It still ran its command under `/bin/sh` (Bash 3.2) and sent
`^Z`, `x` and Enter in one write. It now follows the corrected fixtures:
- the command runs under the test Bash;
- it retries an interrupted `read`;
- the line goes out only after the handler's output.

Afterwards it failed 0 of 200 times with Bash 5.2.37 and 0 of 200 with Bash
5.3.

**`TestShellBrokerAbortAfterRemoteCompletion` under load.** One verification
run failed it with the Bash broker and Bash 5.2: `Run` returned status 0 and no
error. The test delays received frames by 500 ms after `G`, so the local stdin
failure meets the broker's X only after the broker has exited. When R and X
came in one PTY read they arrived together. The failure and X then raced in
`Run`'s `select`, and X could win; reporting status 0 is correct, but the test
then skipped the abort it is about. The command now sleeps 0.2 s before
`exit 0`, which keeps X out of the frame that carries R.

**`TestShellBrokerCarrierLossKillsCommandGroup` with the Bash broker.** It
failed once in the `PI_SH_REMOTE_IMPL=bash` step with Bash 5.2, while three
packages ran at once: the command group was still there at the 8 s deadline.
With one `yes` per core it passed 120 of 120 runs with both Bash versions.
A lost cleanup would keep the group for its 300 s `sleep`, so the deadline is
now 20 s, and the test logs how long the group took to go. Whether that run
was slow or a rare missed cleanup in the frozen Bash broker is not proven.

An overlapping load run once rebuilt the shared test binary under another;
the forwarding tests' `nc` double is that binary, so those failures were
artifacts. The load script now gives every run its own binary and logs.

**`TestLoginShellSessionOverWebRTC` `waitFor`.** The session could end while
its last output was still on the way to the test's observer; `waitFor` then
failed on the ended session (once in 20 runs with the Perl TTY supervisor).
It now keeps the session's result and waits up to one second for the text.
The TTY suites are now run with both supervisors (`PI_SH_REMOTE_IMPL=bash`
for the Bash one), as TTY_JOB_CONTROL.md "Perl supervisor" records.

**The carrier-loss overrun, explained.** The 8 s overrun of
`TestShellBrokerCarrierLossKillsCommandGroup` described above was not
slowness. It was the Bash broker exiting during its own cleanup on a second
hangup; SHELL_BROKER.md "Second hangup during cleanup" has the cause and the
fix.

## Further serial load investigation — 2026-10-29

Mac, eight `yes` processes (one per logical core), only one test binary at a
time, no simultaneous verification. Unique binaries/logs in a temporary local
scratchpad; all load processes stopped after each driver. Product source
unchanged during these measurements (`c70712d`). Both Bash binaries were still
present, so no rebuild/download was needed.

### Bash broker slow output / fast input

`TestShellBrokerSlowOutputWithFastInput/bash`, 1 MiB through `cat`, slow local
stdout, input window 32, `PI_SH_REMOTE_IMPL=bash` (Bash broker and workers):

| Carrier | Repetitions | Result | Per-test time | Total wall time |
| --- | --- | --- | --- | --- |
| Bash 5.3 | 100 | 100 passed | 15.74–37.68 s | 1900.002 s |
| Bash 5.2.37 | 100 | 100 passed | 14.91–17.08 s | 1618.528 s |

A temporary Go overlay added `time.AfterFunc` timers at 45 and 55 seconds
around each subtest; each called `pprof.Lookup("goroutine").WriteTo(os.Stderr, 2)`.
Neither fired: no new 60 s overrun. A separate loaded reference run used 10 s
and 12 s timers: both captured stacks, then the exact byte comparison and exit
status passed (16.59 s test, 17.216 s driver). At both snapshots `Run` was in
its main select; its sender was waiting for queued records, rather than stuck
writing input. These are normal-run reference stacks, **not** evidence about
the historical failed run. All instrumentation stayed outside the working tree.

**Conclusion:** no reproduced deadlock, and no basis to classify the old >60 s
events as mere slowness. The maximum observed 37.68 s shows timing variability
under load but does not explain those events. The historical overruns remain
unclassified; no product change or increased timeout is justified by this run.

### Bash TTY supervisor

`PI_SH_REMOTE_IMPL=bash`, `TestTTYCommandSessionOverWebRTC`, 40 full repetitions
with each Bash, same load and serial execution:

| Carrier / run | Cases | Outcome | Wall time |
| --- | --- | --- | --- |
| Bash 5.3 | 680 | 679 passed, one `ctrl-z-handler` failure | 282.616 s |
| Bash 5.2.37 | 680 | 680 passed | 257.597 s |
| Bash 5.3, isolated `ctrl-z-handler` with diagnostics, 200 repetitions | 200 | 200 passed | 90.974 s |
| Bash 5.3, full sequence again with diagnostics, 40 repetitions | 680 | 680 passed | 250.120 s |

The failure was `session did not finish` (18.11 s including cleanup), **after**
`fixture-ready`, the TSTP handler's `caught-suspend` and `seen:x` had all arrived.
It is different from both the old missing-line Bash 3.2 fixture defect and the
five consecutive readiness timeouts. No readiness timeout recurred.

The diagnostic overlay armed a 10 s timer for each carrier, recording goroutines,
the last 4096 carrier bytes and `ps` PID/PPID/PGID/foreground-group/state/command
on a slow case. No timer fired in either rerun. It did not reproduce even when
the full case order was retained. No supervisor/transport attribution or fix is
claimed: the new isolated completion failure remains open, alongside the old
readiness episode. No synthetic fixture uses Pi credentials or a native session.

To repeat, build a **unique** test executable before starting the load, then run
it alone with `-test.v -test.count=100` and
`-test.run='^TestShellBrokerSlowOutputWithFastInput$/^bash$'`, or with
`-test.count=40 -test.run='^TestTTYCommandSessionOverWebRTC$'` for the CLI binary.
Set `PI_SH_TEST_BASH` to each test Bash and `PI_SH_REMOTE_IMPL=bash`. An optional
Go overlay can add the timers above without editing the checkout. Never rebuild
that executable while another test uses it as its `setsid`/`nc` helper. Stop all
`yes` children before starting the complete verification.

### Recursive-copy atime observation

`TestRecursiveCopyAgainstLocalSCP` now snapshots our destination immediately
after `plan.Run`, before invoking the reference `scp`; it snapshots the reference
immediately after that copy. Previously our destination was exposed to external
readers for the entire reference copy before its metadata was observed. Mode,
atime, mtime, contents, root metadata and diagnostics are still checked; the
untouched control still reports interference. There is no retry, skip, tolerance
or ignored atime. An external reader between copy completion and snapshot can
still fail this strict test; this narrows a race rather than claiming filesystem
isolation. Ten focused race repetitions (480 paired cases) passed.

The commit gate remains the complete verification checklist above, including
both forced-Bash runs, seven CGO-disabled targets and all four native binaries.
The diagnostic overlays are excluded from that verification and from the commit.

### Context fixture and macOS PTY newline processing

The first complete verification ended `RESULT fail=1`: only
`TestTTYSupervisorsGiveCommandSameContext` failed, in the forced-Bash 5.2.37
step. Both contexts had identical environment values, ignored signals, groups
and original terminal modes; one capture had one additional CR. An overlay that
reported only lengths/control-byte counts, preserving the exact assertion,
reproduced 18 failures in 100 runs, then 6 in 30. Every difference was a doubled
CR before LF; neither implementation exclusively produced it.

Reduced to a standalone local PTY, with no shell, supervisor, protocol or
personal environment: a forked process writes synthetic lines; a delayed reader
drains the master in small chunks. With OPOST/ONLCR, 3 of 200 runs gained exactly
one CR; with OPOST disabled, 200 of 200 preserved every byte. An ordinary fast
reader passed 200/200 in each mode. This reproduces the newline artifact outside
the product; it does not explain the unrelated completion/readiness episodes.

The context fixture now captures `stty -g` **before** changing anything, disables
output processing only for reporting, and prints the saved original modes at
the end. The exact environment/signal/group/mode comparison remains; no CR
normalization or ignored difference is introduced. Failure diagnostics no longer
dump inherited environment values. The corrected fixture passed 100 race
repetitions with each Bash (both carrier types and both supervisors per run).
The product's terminal handling is unchanged. A new complete verification is
required after this fixture change; the first failed run is not treated as a pass.


## Matched local comparison with main — 2026-10-29/30

**Scope and source.** Started with a clean `perl-remote` at `cec4417`;
`main` was `7500852`. A separate local `git clone --shared --no-checkout`
was checked out detached at `7500852` in
`/private/tmp/pi-sh-compare.AvaQDI/reference`. Neither branch was moved during
measurement. All sessions were synthetic local Mac PTYs and local Pion peers;
no Pi session, native plan, push or merge was performed. Eight `yes` processes,
one per logical core, ran around each serial driver and were reaped in its
`finally` block. Each comparison driver ran one test executable at a time;
loaded drivers never overlapped each other or the full verification.
Bash versions were the same existing 5.3 and 5.2.37 binaries as above.

**Reference adaptations, without branch product changes.**

- Both CLI harnesses used the `cec4417` `tty_command_unix_test.go` fixture.
  Its only original difference from main is the corrected Ctrl-Z command and
  separated signal/line writes documented above. All 17 cases and their order,
  exact output/status/restoration assertions and 15 s lifetimes were retained.
  The context/OPOST and recursive-atime fixtures were not needed or copied.
- Main lacks `TestShellBrokerSlowOutputWithFastInput`: only that test function
  was added, with one `bash` subtest instead of `eachImpl`. It uses exactly the
  branch's 1 MiB input, `cat`, existing `slowWriter` (1 ms per write), exact byte
  comparison, status and 60 s lifetime. The existing `runLocalControlledWithin`,
  `runLocalWithStream`, `testStream` and `slowWriter` harnesses match already.
- Both drivers set `PI_SH_REMOTE_IMPL=bash` **and**
  `PI_SH_BROKER_WORKERS=bash`. Main does not implement the former; the latter
  keeps its broker workers in Bash. Main's TTY supervisor is already Bash.
- Diagnostic overlays added observation hooks to each revision's own
  `broker.go`, `tty.go`, `tty.bash`, `command.go` result parser and CLI
  coordinator; they did not transplant the sender, windows, loader, Perl
  programs, completion grace or cleanup fixes from the branch. A second,
  fixture-only overlay measured main with no product instrumentation; the
  corresponding branch executables used the ordinary checkout without overlays.

The actual product differences remain part of the comparison: input window
8 versus 32, synchronous versus independent broker sender, older versus faster
source decoding/upload chunks, additional embedded Perl source even when forced
to Bash, and the branch's second-hangup cleanup fix. Normal Bash TTY status
classification is unchanged. The CLI input/coordinator, result parser,
transport, local I/O and terminal code have no product diff between these states.
Elapsed test times include startup. Serial blocks are not randomized benchmarks;
neither equal performance nor a performance regression follows from their maxima.

**TTY observation.** Fixed events on an inherited, test-only descriptor 9 record
supervisor start, TR emission, CHLD notice, command absence, classification,
command wait, helper waits, final stty and TX before/after emission. The synthetic
handler command alone gained an EXIT trace; this is an impending-exit marker,
not proof of kernel exit. Command absence and successful `wait` distinguish
actual reaping. No command arguments or jobs-table contents are logged.
Other hooks observe fixture readiness/handler/read output, signal/line writes,
carrier TR/TX, parser acceptance of TX and EOF, input return, StopIO, worker join,
monitor join, stream close and terminal restoration. These are observation
timestamps; scheduling and the separate diagnostic pipe can reorder notices.
Timers at 3/8/12 s capture goroutines and only owned-process numeric
PID/PPID/PGID/foreground-group/state columns. No environment, argv, raw carrier
tail, credentials, SDP or ICE is printed or saved.

A reduced `TestLocalCtrlZReduced` sends the identical modern-Bash handler/read
command through the production loader/supervisor on a direct `/bin/sh` carrier
PTY, removing Pion and the CLI/local-I/O coordinator. It keeps exact marker
ordering and status 23, the existing staged 10 s lifetime, and the same separated
Ctrl-Z/line input. Main's staged fixture only gained the same diagnostic fd;
no product change was needed. Timers there are at 3/8 s.

| State | Bash | Isolated CLI handler | Complete 17-case CLI sequence | Reduced direct PTY |
| --- | --- | --- | --- | --- |
| cec4417 | 5.3 | 100/100 | 340/340 (20 repetitions) | 50/50 |
| main 7500852 | 5.3 | 100/100 | 340/340 (20 repetitions) | 50/50 |
| cec4417 | 5.2.37 | 100/100 | 340/340 (20 repetitions) | 50/50 |
| main 7500852 | 5.2.37 | 100/100 | 339/340 (one different failure below) | 50/50 |

Every handler case in the complete sequences passed too. No post-`seen:x`
completion hang or readiness timeout recurred. A separate **artificial positive
control**, with no `yes` load, deliberately held the branch supervisor in a loop
after command/helper waits and before final stty/TX. The unchanged CLI assertion
failed with `session did not finish` at its original 15 s lifetime. All three
snapshots showed the remaining supervisor, the trace confirmed completed waits,
and neither TX emission nor parser acceptance occurred. This validates visibility
at that boundary; it is not a reproduction of the historical defect or evidence
of its cause. No reliable failing reduction of the real episode was found.

**Different main failure.** In the loaded 5.2.37 full sequence, one `eof-record`
case failed in 0.20 s: `ended before readiness: invalid or incomplete shell
broker exchange`, empty application output. The first staged record had a
2-byte body and failed exact validation, before any source-upload record;
there was no supervisor-start event. Its contents were not captured, so a CR
artifact or any other cause is not established. The driver exited nonzero and
kept the failure. This is a demonstrated reference failure at staging, **not**
the historical five readiness timeouts or the post-handler completion failure.
A later passing clean run does not erase it. It remains separately unclassified.

**Broker observation.** At 5/15/30/45/55 s, goroutines accompany counters for
attempted/completed sends, parsed records by kind, record-body bytes and decoded
input/output payload bytes. Ack counters distinguish staging, input and output;
actual local output writes have their own byte counter. Live queue lengths cover
PTY frames, received records, input reads/requests and, on the branch, the sender
outbox. Sequence/ack gauges are the last main-loop observation; counters and queue
samples need not represent the same instant. Stage entry/return and P/B/G/R
records separate startup from the data path. Neither framing nor timeouts changed.

| State | Bash | Passed | Per-test time | Driver wall time |
| --- | --- | --- | --- | --- |
| cec4417 | 5.3 | 10/10 | 15.58–39.13 s | 230.369 s |
| main 7500852 | 5.3 | 10/10 | 16.16–21.38 s | 182.704 s |
| cec4417 | 5.2.37 | 10/10 | 14.67–30.19 s | 194.616 s |
| main 7500852 | 5.2.37 | 10/10 | 14.69–24.43 s | 170.196 s |

For the successful 39.13 s branch/5.3 run, staging ended at 0.104 s:

| Observation | Completed input records | Input acknowledgments | Bytes actually written locally | Output acknowledgments sent |
| --- | --- | --- | --- | --- |
| 5.000 s | 249 | 217 | 311296 | 152 |
| 15.017 s | 361 | 329 | 538624 | 263 |
| 30.005 s | 497 | 465 | 817152 | 399 |
| 39.124 s, final | 512 | 512 | 1048576 | 512 |

These samples establish data-path progress during this slow successful run;
they do not establish continuous progress between samples. The 33.64 s 5.3 run
also advanced at 5/15/30 s (133120/391168/843776 local bytes), then completed.
No 45/55 s timer fired and no 60 s overrun recurred. Thus no stalled/external-wait
failed run was captured. Normal-run stacks/counters cannot explain old failures.

**Without diagnostic hooks.** One complete CLI sequence per state/Bash passed
68/68 cases under the same serial load. One broker run per state/Bash passed
4/4: branch/main 5.3 took 15.72/15.42 s, branch/main 5.2.37 took 16.53/20.97 s.
These small controls check that the shared fixture also runs without hooks;
they do not exclude an observer effect on a rare race.

**Classification and merge recommendation.** Neither requested episode was
reproduced, so neither a branch regression nor occurrence of the *same* problem
on main is demonstrated. Both remain unclassified and unresolved. The separate
main staging error must not be substituted for either. No product fix, permanent
regression test, retry, tolerance, normalization or timeout change is justified.
For these two episodes, a merge need not be blocked indefinitely on repetition
alone: retain the uncertainty and reopen diagnosis on new boundary evidence.
A demonstrated branch regression should be fixed first; the same failure proved
on main could instead be assessed as a pre-existing limit. The owner decides merge.

**Artifacts and verification.** Unique immutable test executables, final overlay
sources/mappings, drivers, logs and result JSON live only in
`/private/tmp/pi-sh-compare.AvaQDI` (temporary, not a durable repository artifact).
`comparison-summary.json` indexes results; `artifact-sha256.json` records hashes.
`branch|main/overlay.json` is the CLI diagnostic mapping, `broker-overlay.json`
the broker mapping, `reduced-overlay.json` the direct-PTY reduction and
`clean-overlay.json` the fixture-only control. To repeat, compile from the stated
revision with the relevant `go test -overlay=... -c` into a **new unique path**,
then run the selectors above with `-test.count=100` (isolated CLI), `20` (full
CLI), `50` (`^TestLocalCtrlZReduced$`) or `10` (broker). Use both Bash paths and
both environment switches, eight owned `yes` children with guaranteed teardown,
and one executable at a time. Saved drivers refuse existing log names; use new
names for another run, and never overwrite an active helper executable.

All hooks remain outside both checkouts. The artificial control's expected
failure is not part of, or a substitute for, the full verification gate.

### Investigation closure and failed final gate — 2026-10-30

The owner asked to close this investigation. The two original episodes remain
unclassified, with neither a demonstrated branch regression nor the same failure
demonstrated on main. No product fix or claim of resolution follows. Further
repetition is not scheduled as part of this investigation.

The complete uninstrumented checklist ran with **zero `yes` processes** and
ended `RESULT fail=1` in
`/private/tmp/pi-sh-compare.AvaQDI/verify-final.log`. The failed checklist command
was the three-package Bash 5.2.37 pass, without the forced-Bash switch:
`go test -count=1 ./internal/fwdbroker ./internal/shellbroker ./cmd/pi-sh`.
Forwarding's package passed; shellbroker and the CLI package failed. The SFTP
WebRTC cases closed the transport while initializing the client; several CLI
cases hit deadlines or transport closure. In
`TestCommandOverLocalWebRTCAndPTY`, the failure is at `server.Accept`, before
the fixture creates any shell PTY. That case therefore precedes Bash, broker
execution and the command. Its appearance in a Bash 5.2.37 checklist step does
not establish Bash as the cause. The other failures need their own localization;
they must not all be assigned to that same boundary without evidence.

Every other checklist command passed, including both forced-Bash versions,
seven CGO-disabled build/test-compilation targets and all four native binaries.
The root race-suite command reported 20 cached packages and 12 freshly run
packages; the nested module's result was cached. This was not a fresh repetition
of the entire race suite. Tracked-file hashes matched before/after the gate.
Those successful checks do not erase its failure. The subsequent shell-free
default-versus-loopback peer reduction was prepared but interrupted: neither
`peer-branch.log` nor `peer-main.log` exists, so it supplies no result.

**One-machine limitation.** Client, simulated counterpart, PTYs and test helpers
share CPU scheduling, memory and one kernel. The loaded comparison deliberately
adds contention; the final checklist has no added load, but includes multi-package
test invocations on that same host. Local WebRTC also exercises host interfaces
and local network paths rather than a Mac-to-Pi connection. These differences can
affect timing and expose fixture or local-environment problems. Same-host
contention or networking is therefore a plausible hypothesis, not a diagnosed
cause of the observed failures. It does not invalidate the local assertions,
establish production immunity, explain the old episodes, or waive a failed gate.
The earlier demonstrated macOS PTY newline artifact is separate evidence about
that specific fixture, not proof that these failures have the same origin.

**State at investigation closure, before the owner decision below.** Only the
four investigation documents are modified. There is
no commit, merge or push; `perl-remote` remains at `cec4417` and main at `7500852`.
Closure checked zero scratchpad processes and zero `yes` processes. The closure
edits were made after the failed gate and have only received a documentation
diff/whitespace check; they are not presented as fully verified changes. A future
commit still requires the entire checklist ending `RESULT fail=0` on exactly the
state being committed. Merge remains the owner's decision: the two unreproduced
episodes alone are not an indefinite veto, while the new WebRTC gate failure
remains a separately recorded, unclassified verification issue.

### Owner-authorized integration and deferred campaign — 2026-10-30

After reviewing the failed gate and the already completed Perl/native evidence,
the owner explicitly requested integrating the Perl work into main, continuing
the missing CLI functionality, and running further test campaigns once a first
complete version is ready. The documentation commit and local integration are
therefore authorized with the known `RESULT fail=1`, without another full gate.
This supersedes the earlier pre-commit gate requirement for this integration;
it does not reclassify any failure as a pass or establish an environmental cause.
No push, publication, new Pi session or reuse of completed native plans follows.

The merged product remains the committed Perl implementation from `cec4417`;
the integration adds investigation/decision documentation and corrects the README,
without product-code changes or temporary instrumentation. Appropriate focused
checks accompany subsequent implementation work; full/stress campaigns are
deferred as requested, and the checklist above remains the final campaign's
reference. The closed intermittencies and failed local WebRTC gate stay recorded
for that campaign rather than being repeatedly rerun now.

### SFTP console milestone — 2026-10-30 (local only)

Implemented `mget`/`mput`, top-level `-r` and command `-r`/`-R` recursion.
`SFTP_CONSOLE_RECURSION.md` records the contract and remaining differences.
The new differential runs 96 local OpenSSH 10.2p1 SFTP cases, including
nested conflicts and continuing siblings, both directions, global/command
flags, symlink policy, resume and metadata combinations. It compares status,
trees/contents/modes/links and selected preserved mtimes. The actual-server
flush fixture now also exercises a nested recursive alias upload/download,
with advertised and disabled fsync; successful advertisement verifies WRITE,
fsync and CLOSE ordering. Error-classification/redaction tests include joined
ordinary and transport failures; cancellation is checked before mutation.

Focused validation passed (the final reporting-only redaction adjustment was
then checked with the specific error/redaction tests and repeated vet/builds):

- `go test -race ./internal/sftpcli ./internal/copycli ./cmd/pi-sftp ./cmd/pi-cp -count=1 -timeout=120s`
- `go vet ./internal/sftpcli ./internal/copycli ./cmd/pi-sftp ./cmd/pi-cp`
- `CGO_ENABLED=0 GOOS=linux GOARCH=386 GO386=softfloat go build ./...`
- `CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build ./...`
- `go test -race ./internal/sftpcli -run 'TestRecursiveErrorClassificationAndRedaction|TestMultipleMatchFailuresAreEachReported' -count=1 -timeout=30s`
- `git diff --check`

No Pi/Connect/SSH session, remote writes/listeners, complete/stress campaign,
push or publication. These focused checks do not supersede the historical
`RESULT fail=1`, classify its cause, resolve the two original episodes or
provide a result for the unexecuted shell-free WebRTC reduction. The complete
checklist remains the final campaign reference. Linux/386 and Windows builds
are compile evidence; physical iSH and native platform/console recursion
verification remain pending. The product backlog is still open.

### Requested console commands/options — 2026-10-30 (essential checks only)

Each requested command has an implementation commit and an essential local
check listed in SFTP_CONSOLE_COMMANDS.md. The earlier recursion milestone was
committed separately as f206fde; its 96-case fixture was not repeatedly rerun
for each new command. No 100-count loop, full suite, stress campaign or new
WebRTC diagnosis was performed for this work.

Evidence includes 16 exact ls output cases, three additional control/Unicode
listing formats, prompt/echo/help, notices/headings and selected numeric,
missing-file, existing-file and unavailable-extension diagnostics. stdout and
stderr bytes (including spacing and CRLF) are compared with OpenSSH 10.2p1.
Only the two distinct synthetic fixture roots are normalized; the -D transport
banner is excluded from -N command-output comparisons. No production session
capture or inherited environment is saved.

Transfer options have one local binary parallel roundtrip, bounded-buffer and
short timing checks, parser/precedence checks and explicit contiguous-prefix
WRITE-error/short-READ/cancellation checks. These are not native disk-full,
network loss, exhaustive wire/fault or performance campaigns.

An aggregated new-command selector passed in internal/sftpcli,
internal/sftpclient, cmd/pi-sftp and cmd/pi-cp; internal/copycli compiled but
had no tests matching that selector. Existing tests outside the selector are
not claimed as rerun. Individual subsequent fixes ran only their specific
checks. Final focused checks passed:

- go test -race ./internal/sftpcli -run '^(TestParallelCopy.*|TestParallelTransferLocalServer|TestLocalShellAgainstOpenSSH|TestTransferNoticesAgainstOpenSSH|TestLsControlNamesAgainstOpenSSH)$' -count=1 -timeout=45s
- go vet ./internal/sftpcli ./internal/sftpclient ./internal/copycli ./cmd/pi-sftp ./cmd/pi-cp
- CGO_ENABLED=0 GOOS=linux GOARCH=386 GO386=softfloat go build ./...
- CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build ./...
- TestLocalSFTPSnapshotMatchesUpstreamManifest (upstream files unchanged)
- TestStatusTextUnknownUint32IsPortable (no signed-int overflow in status formatting)
- TestTuningValidationOrder (invalid -B/-R cannot be hidden by a later -X)
- TestSFTPProgressRequiresTerminalDiagnostics (single local PTY/redirect check)
- git diff --check

The three last focused guards above followed the earlier race selection.
The targeted vet and both build commands were then repeated after the final
source changes and passed. The final documentation-only update follows those checks and has a whitespace/
diff check, not another whole verification campaign. No test result above
supersedes the failed historical RESULT fail=1, resolves either unclassified
episode or supplies evidence for the never-executed shell-free reduction.
No Pi session, remote write/listener, push or publication. New native command/
tuning paths, arbitrary failure/lifecycle cases and physical iSH remain open;
CGO-disabled builds are compile evidence only. The complete checklist above
remains the deferred final campaign reference.
