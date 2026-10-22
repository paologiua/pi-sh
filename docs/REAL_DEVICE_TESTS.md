# Authorized real-device evidence — 2026-10-19/22

## 2026-10-22 native pi-sftp / pi-cp / pi-sh CLI plan (owner approved)

The owner approved docs/NATIVE_CLI_PLAN.md ("Autorizzo il piano CLI e la pulizia")
and asked Claude to drive the browser companion; Claude ran each fetch step in
the Devices tab through Claude in Chrome. Production binaries, browser mode:

| Check | Result |
| --- | --- |
| pi-sftp batch: exclusive mkdir, chmod 700, put, `ls *.bin`, `chmod 600 *.bin`, rename to space/quote/Unicode name, `get *.bin dl` | passed; downloaded SHA-256 equal |
| pi-sftp `reget` of a truncated 1MiB copy | passed; SHA-256 equal |
| pi-cp `-r -p` upload of a tree with empty dir and Unicode/space/quote name | passed |
| pi-cp download of quoted remote glob `root/*.bin` | passed; SHA-256 equal |
| pi-cp `~/missing` (tilde) | expected "file does not exist", no local file |
| pi-sh `-v` `exit 0` | first two runs failed locally (see below); after the fix exit0, 11 debug1 lines, no SDP/ICE/address/token |
| Cleanup batch (explicit rm/rmdir of known entries and root) | exit0 |
| Separate session `ls root` | "not found": root absent |

Two earlier driver attempts failed at the companion wait before any remote
command (the driver hid the instruction in a per-session file); no remote state
was created. pi-sh then failed with "local stdin read failed" for stdin from a
background shell and from /dev/null: Darwin poll returns POLLNVAL for /dev/null,
which localio treated as a closed descriptor. Fixed in localio (regression test
TestDevNullStreams, red before/green after) and verified natively as above. Its
failed run also reported broker cleanup unconfirmed; that run executed only
`exit 0`. Over-redaction noted: diagnostics print `version=[redacted]` and
`session:[redacted]`. Only fixed outcomes are retained.

## 2026-10-22 approved native transfer plan rerun after the fix

After renewing recent sign-in, the owner ran the exact approved plan once:
`bin/transfer-probe --execute-approved --browser --root
/tmp/pi-sh-probe-7470c80bcf6721dd82f13486e142579c raspberrypi`. Fixed report:

| Phase | Result | Elapsed ms |
| --- | --- | --- |
| private-root | passed | 733 |
| empty upload/mode/download/integrity/roundtrip | passed | 1039 |
| 64KiB binary upload / download+integrity | passed | 6212 / 12467 |
| 1MiB upload / mode | passed | 92988 / 93002 |
| 1MiB download / integrity / roundtrip | passed | 193300 / 193308 |
| preservation | passed | 204879 |
| resume | passed | 216091 |
| advertised-flush | passed | 227464 |
| recursive | passed | 229228 |
| missing-source-failure | passed | 229244 |
| cleanup (inventory, removal, root absence verified) | passed | 230166 |
| broker-completion | code0, failure=none, cleanup-unconfirmed=false | 230216 |

This is the first native pass of every planned phase and of the1MiB upload that
previously stalled at local offset589824. It is one run: no stress or failure-
injection claim. The process exit status was not captured separately in the
pasted output. Throughput is low (about11KiB/s for1MiB in each direction)
because of hex framing and stop-and-wait; that is a performance limit, not a
failure. No transfers were replayed; only fixed phase reports are retained.

## 2026-10-22 read-only1MiB rerun after the Bash 5.2 timed-read fix

The owner ran the rebuilt production CLI three times, each started explicitly
with its own browser companion step: `--browser --experimental-command -T`,
synthetic1MiB of every byte value on stdin, and the existing `wc -c` (same
command as the stalled run below). No remote file was written; no SFTP.

| Run | Result | Wall time incl. manual companion step |
| --- | --- | --- |
| 1 | `read-only-mib:1048576`, CLI23 | 88s |
| 2 | `read-only-mib:1048576`, CLI23 | 83s |
| 3 | `read-only-mib:1048576`, CLI23 | 86s |

Three earlier attempts that day stopped at browser setup ("Connect requires
recent sign-in", CLI255) before any session or command; after the owner
renewed sign-in through Remote shell, the three runs above passed. Together with
the local red/green regression (SHELL_BROKER.md) this supports the identified
cause on this path. Three passes are not a statistical bound: before the fix one
of three comparable runs stalled. The SFTP upload path is not yet rerun. Only
counts, statuses and times are retained.

## Latest read-only1MiB broker comparison, before owner handoff

The exact production CLI path --browser --experimental-command -T sent a local
synthetic1MiB containing all byte values to the already-present `wc -c` through
the binary broker. The command retained only a fixed count marker and exit23;
it wrote no remote file and did not run SFTP. Each attempt was explicitly
started as an experiment, without transport replay or automatic command retries.

| Attempt | Evidence |
| --- | --- |
| Normal production binary | Local source offset196608 stopped advancing; no count marker; local180s timeout requested SIGINT; CLI255/context canceled with broker cleanup unconfirmed |
| Temporary counter binary | All1048576 bytes counted; final input sequence/ack1024/1024; fixed marker and CLI23 |
| Counter binary with buffered-amount metric | All1048576 bytes counted; final sequence/ack1024/1024; buffered bytes0; fixed marker and CLI23 |

The successful instrumented runs do not establish a fix. Intermittency or timing
perturbation remains possible. In particular, the normal binary's read-only stall
shows that this symptom can occur without SFTP or remote filesystem writes.
It does not prove the same underlying cause as either prior transfer failure.
No stalled instrumented run was obtained, so the counters have not identified
which boundary causes the hang. Buffered amount alone is not remote application
consumption or an end-to-end acknowledgement.

A separate local instrumented Pion/PTY/broker/OpenSSH1MiB test passed. Temporary
counter hooks/adapters, environment switch and probe executable/script were then
removed for the owner-requested handoff. Normal source was restored and compiled.
All local probe processes reached terminal state; Devices was reloaded to stop
the companion. The failed read-only run's remote descendant cleanup remains
unverified; later successful sessions do not retroactively prove it. No remote
file was created by these three runs. The approved scratch root had already been
verified absent after the preceding cleanup-only run. Only fixed counts/statuses
are retained, with no payload, token, SDP/ICE or raw session transcript.


## Latest instrumented native transfer run: stalled upload

An explicitly started rerun of the same approved plan used the new fixed-phase
and broker-category reports. Private-root and empty-file checks passed;64KiB
upload/mode passed by5873ms and download/integrity by12802ms. The1MiB upload
then failed to complete. Two read-only inspections of the local synthetic source
file showed the same offset589824; this is local bytes consumed, not remote
bytes acknowledged or evidence of a service size threshold.

At the existing ten-minute process deadline, the report recorded large-upload
and large-roundtrip failed at593591ms after broker setup. Same-session cleanup
failed. Final broker completion reported code0 with failure=deadline and
cleanup-unconfirmed=true: that numeric code is not a successful remote exit.
The overall transfer probe exited1. No session-check, peer, queue-overflow or
broker-protocol category was reported by that run. Absence of those categories
does not prove those components fault-free; the deadline is the terminal cause,
not an explanation of the stall. The earlier generic failure remains separately
unclassified and is not retroactively attributed to this timeout.

A fresh cleanup-only session then verified the exact approved root absent and
exited0. Its broker report was code0/failure=none/cleanup-unconfirmed=false, with
elapsed1037ms. No transfers were replayed. Both processes are terminal and the
Devices page was reloaded to stop the companion. No sensitive captures remain.

Separate local evidence:1MiB roundtrips through actual Bash/PTY/OpenSSH pass
both directly (approximately23s) and over local Pion WebRTC (approximately24s).
The final whole-project race suite, root vet and seven CGO-disabled target test
compilations pass. These narrow the evidence gap but do not reproduce or fix the
native stalled upload. Further phase/count instrumentation and a targeted
reproduction remain necessary before claiming transfer readiness.


## Earlier authorized native transfer run

The owner explicitly approved the exact NATIVE_TRANSFER_PLAN.md root and its
synthetic operations/cleanup. After normal recent-sign-in and Chrome connection
renewal, bin/transfer-probe used the official Connect carrier, existing agent,
in-memory binary broker and existing SFTP server. Fixed result categories:

| Check | Result |
| --- | --- |
| Exclusive private scratch root | passed |
| Empty-file upload/download and mode | passed |
| 64 KiB all-byte binary roundtrip, SHA-256 and mode | passed |
| 1 MiB roundtrip | failed: sanitized file-operation failure |
| Preservation, resume, advertised flush, recursive tree, missing-source test | not reached |
| Cleanup in the failed transfer session | failed; absence unconfirmed then |
| Separate cleanup-only Connect session | passed; root absence verified; exit0 |

A subsequent local-only increment adds fixed per-file upload/mode/download/
integrity outcomes and elapsed milliseconds, plus sentinel-based broker failure
categories without source text. The actual local broker/PTY/OpenSSH1MiB test
passes in approximately23 seconds. Its previous20-second fixture limit was
insufficient; this is not evidence that the native failure had that cause.
The subsequent instrumented native outcome is recorded above.

The transfer process exited1. Its final sanitized diagnostic does not identify
upload versus download or separate broker/transport/server failure. The cause
is unclassified: no claim of a size limit, data corruption, auth expiry or a
particular implementation bug is supported yet. The next task is to isolate the
failing phase with fixed, secret-free diagnostics and a red-capable reproduction.
No transfer was automatically replayed. Cleanup-only did not rerun file copies.

Approved scratch root: /tmp/pi-sh-probe-7470c80bcf6721dd82f13486e142579c.
It is now verified absent. No package, persistent script, service, configuration
or SSH transport change was made. Both local probe processes reached terminal
status; the Devices page was reloaded to stop the companion. Only fixed outcomes
are retained, without credentials, raw session data, SDP or ICE.


## 2026-10-21 native command TTY control characters

Three further fresh CLI/companion sessions used the unchanged agent and existing
noninteractive Bash, with local29x91 PTYs. Input was sent only after each fixed
readiness marker:

| Input | Remote fixture/result | Local status |
| --- | --- | --- |
| Ctrl-C byte0x03 | INT handler printed its fixed marker and exited23 | 23 |
| Ctrl-D byte0x04 on an empty canonical line | read reported EOF; branch printed its marker and exited23 | 23 |
| Ctrl-Z byte0x1a | TSTP handler printed its fixed marker and exited23 | 23 |

These establish native control-character delivery, handled INT/TSTP completion
and empty-line EOF in the actual command-TTY path. They do not establish default
signal termination, nonempty Ctrl-D behavior, stopped-job fg/bg, local suspension,
detached descendants or cleanup after transport loss. TSTP was deliberately
handled by immediate normal exit, so no stopped job was left by that fixture.
All three local processes reached terminal status23; the companion page was
reloaded and transient pairing/handles discarded. No credentials/transcripts,
Pi files, persistent scripts, packages or configuration changes were retained.

## 2026-10-21 command TTY native resize verified

After the owner renewed normal Remote shell authentication, the final native
Mac binary connected through the browser companion and official Connect agent.
The private local PTY started at29 rows/91 columns; an existing Bash command
reported29 91 and a fixed readiness marker. The test changed only that identified
local PTY to35 rows/111 columns and sent SIGWINCH to its own pi-sh PID. The remote
Bash WINCH trap checked `stty size` and emitted the expected marker for35 111.
No marker was observed while Bash remained blocked in read; sending an empty
line released the trap report. A final controlled input line then produced the
finish marker and verified local exit23.

This establishes remote size propagation through the actual command-TTY CLI,
including usable input and final status after resize. It does not establish
immediate trap execution while read is blocked or explain the observed trap
scheduling delay. General application notification timing, native Ctrl-C/D/Z,
suspension, authorization/connection loss and descendant teardown remain open.
Only transient terminal modes and in-memory commands were used. No remote file,
configuration, package or service was changed. Local process completion was
confirmed and transient pairing/PID/handles discarded; the companion page was
reloaded without retaining session output, credentials, SDP or ICE.

## 2026-10-21 resize retry: recent sign-in required again

Normal automatic review allowed a fresh native resize test after the earlier
quota failure. A local CLI/bridge started, but the companion reported the normal
recent-sign-in requirement during authentication setup. The process exited255
before any remote command readiness or resize operation. The local PID had
already disappeared when queried; no PTY change or SIGWINCH was sent. This
attempt does not establish any resize behavior. The owner was asked to reopen
Remote shell/Reconnect and complete any normal sign-in; no restriction was bypassed.
Transient pairing output/handles were discarded. There was no remote write.

The separate full local Go race suite now passes for the recursive-copy and
offline-inspection increments, following normal approval; the historical quota
failure below no longer blocks that completed local regression.

## 2026-10-21 resize attempt not executed: approval review unavailable

The next planned read-only native command TTY check would change only its local
private PTY from91x29 to111x35, observe the remote resize and return23. Automatic
approval review failed before process creation because its usage limit was hit.
No local CLI/bridge process, agent session or remote command was started by this
attempt. The test remains pending; this is not an observed service/protocol or
safety failure. The approval check was not bypassed. Offline CLI implementation
and permitted local checks continued independently.

## 2026-10-21 early TTY completion with unread input

After the local TX/input-error race correction, the final native browser-companion
CLI sent2048 x bytes to an explicitly noninteractive existing Bash command in a
fresh Connect PTY. The command enabled raw/no-echo, emitted readiness, read exactly
one byte with Bash read -N1, verified x, printed its fixed marker and exited23.
The CLI returned23 with that marker. No raw transcript or account/device/network
values are retained. This uses existing Bash/stty and transient private modes,
not a remote file, new package or configuration change. The command is not replayed.

This verifies the early-completion input scenario on the Pi. The old isolated
post-input WebRTC failure had no phase instrumentation, so its exact cause cannot
be retroactively proven by this test. TTY_COMPLETION.md separates the reproducible
local mechanism, correction, strict failure conditions and remaining limits.

## 2026-10-21 command TTY through the browser companion

After the owner renewed ordinary browser authentication, a fresh Devices reload
resolved a connection-create HTTP422 refusal. The initial auth attempt had stopped
before an agent session. Subsequent GET shell-page/ICE requests returned200 while
connection creation returned422 until reload. No response body or token was
captured. Stale page/CSRF state is consistent with these observations, not a
server-side cause proven from a retained response. The companion now chooses a
CSRF token from the freshly fetched authorized shell page; local regression tests
cover stale/fresh tokens, fallback and refusal without POST replay.

The native Mac Go CLI then used official Connect signaling/WebRTC and the existing
agent for the following checks. Only controlled markers, dimensions and statuses
are retained:

| Check | Native result |
| --- | --- |
| --browser --experimental-command -tt -n, without a local terminal snapshot | Remote fds0/1/2 are TTYs; size0x0; verified exit23. |
| Mac local PTY, -tt, TERM=xterm, initial columns91/rows29 | All three remote fds are TTYs; TERM and size29 91 match; a readiness-gated line reaches read and its marker returns; verified exit23. |
| Local PENDIN enabled before capture | Remote Linux local-flags field has bit0x4000; same TERM/size/input checks and exit23 pass. |
| Final binary without temporary diagnostics, with PENDIN enabled | All preceding checks and exit23 pass again. |

The local-mode attempt first failed before command readiness because GNU stty
rejects -pendin; a subsequent in-memory diagnostic also identified BSD reprint
versus GNU rprnt. Both adapter translations are now corrected. PENDIN changes
only the local-flags bit in a fresh carrier-native GNU/Linux state, with platform,
utility, field-count/width and exact readback checks. No foreign save format is
imported. Ordinary and enabled-PENDIN runs establish native GNU-path execution;
they do not validate every control value, unusual parity/hardware or other tools.
The observed remote Bash platform was linux-gnu/aarch64; exact current agent,
Bash and coreutils versions were not re-read and older recorded versions must
not be treated as fresh measurements.

One corrected-mode input run returned the final application marker but then
failed with the fixed WebRTC peer diagnostic and status255. It was not
instrumented, so receipt of its TX result and its failure source are unknown.
Two instrumented runs subsequently received verified TX23 followed by EOF, and
the final uninstrumented run returned23. This isolated earlier close/transport
failure is still an open robustness case, not a claimed fix. No command is
replayed automatically. Temporary fixed-marker Go diagnostics, status-only browser
fetch instrumentation and local probes were removed before the final build.

These tests exercised transient private terminal modes and in-memory adapters
only. No Pi file, package, service, account/system configuration or reboot was
introduced. No transfer-write path has been approved. They establish neither
direct-login interactive/TTY lifetime nor native Ctrl-C/D/Z, suspension,
auth revocation, network-loss/descendant cleanup or physical iSH behavior.

## Earlier 2026-10-21 attempts and pre-native checkpoint

## 2026-10-21 TTY read-only attempt: recent sign-in required

Chrome DevTools access recovered and the Devices page showed exactly one matching
authorized destination, with no top-level reauthentication form. A fresh browser
companion attempt requested only a forced TTY descriptor check, stty size and
exit23, with -n and no file/configuration writes. The companion then detected
that Connect's shell-session page required recent sign-in. The CLI returned255
with its fixed reauthentication diagnostic before opening an agent session;
no TTY marker or dimensions were received and no Pi command ran. Visible Devices
state alone therefore does not prove shell authorization freshness.

The owner was asked to open Remote shell, complete ordinary browser
reauthentication and return to Devices, and subsequently confirmed completion. No credentials, device/account IDs,
SDP/ICE, capability URL or raw browser/native transcript are retained. The local
bridge process ended and its in-memory pairing state was discarded. Native TTY
verification was still open at this failed-attempt checkpoint; this refusal establishes no TTY
support or protocol failure. Local nested-job comparisons are separate evidence.

Before the successful native checks above, command TTY CLI integration,
configured TERM/initial-size staging and Pion/PTY coordination had local evidence
only. Prior interactive/binary broker native
results below do not establish command -t/-tt, full terminal modes, direct TTY
status, connection-loss cleanup or suspension behavior. No new Pi session/write
was performed by this increment.

The later Bash TTY-worker investigation has local PTY/OpenSSH and HUP/TERM cleanup
evidence only. It has not been sent to or run on the Pi and is not a CLI command
TTY implementation. Existing interactive/cancellation/SFTP milestones below do
not prove its staging, foreground groups, signal results or agent-close behavior.

File-content transfers still have local-only evidence (memory and an actual
local OpenSSH server/Bash PTY). No Pi transfer-write path has been approved.
The subsequent flush/short-write correction also has local evidence only; the
metadata batch below establishes no native fsync or write-error behavior.

The later Include/configuration increment has temporary-local-file/OpenSSH -G
evidence only. No native Include/alias login, account-config changes or Pi writes
were performed. It does not extend the native evidence below.

## Owner-run direct SFTP read-only batch — 2026-10-20

The owner ran `printf 'pwd\nls\nquit\n' | ./bin/pi-sftp --direct-login --read-only -b - raspberrypi`
and reported exit status **0**. Only the status is retained. This verifies direct
authentication, the binary broker, an existing server found by the default search,
SFTP negotiation and the read-only metadata batch for that binary. No file
contents, listing output or identity/login information are recorded; no remote
write was requested. The actual chosen executable path was not reported.

The owner subsequently completed the rebuilt `pwd`/`quit` read-only batch with
fresh direct login and exit status **0**. This verifies process-local umask-prefix
setup, SFTP negotiation and metadata for that binary through Connect. No account
values, paths or raw transcript are retained. It does not test file creation or
permissions on an actual new file: the batch requested no writes.
Copy contents, write/mutation/error/cancellation behavior
and native Linux/Windows/iSH remain unverified. An approved Pi write-test location
and cleanup are still required for uploads or mutations.

The owner attempted the requested `pwd`/`quit` read-only rerun after that setup
change. It reported `web login HTTPS request failed`; no exit status was supplied.
No SFTP startup success or failure is established by this authentication error.
The credential-free development preflight subsequently failed with a sanitized
DNS category. This does not establish that the owner's attempt had the same
cause. No credentials were submitted by that preflight and no Pi was accessed.
The final rebuilt preflight later reached `password_required` and returned 0
without credentials; the development DNS failure was intermittent. That recovery
does not establish authenticated login or the pending SFTP startup.

The owner subsequently ran the rebuilt credential-free probe and reported
`Preflight: password_required`, then exit status 0. This verifies current
credential-free navigation in the owner's terminal. It does not establish the
cause of the earlier failure or authenticated SFTP startup. The resume increment
(`-a`, reget/reput) also has local evidence only; no resume writes on the Pi have
been authorised or performed.
The later `-p` metadata implementation likewise has local evidence only; the
successful read-only startup does not prove preservation, transfer contents or
remote mutations.

## Owner-run direct cancellation — 2026-10-20

The owner ran the rebuilt pi-sh with direct login, the experimental broker and
`-nT`, using a printf readiness marker followed by sleep 60. After the marker,
the owner pressed Ctrl-C. The CLI reported context canceled, returned 255, and
did not report unconfirmed broker cleanup. This verifies the native post-launch
cancellation/acknowledgement path in the implementation, as owner-reported
evidence. It is not independent remote process-tree inspection, auth revocation,
network-loss or detached-descendant verification. No files or Pi configuration
were written. Only the sanitized outcome is retained; no identity/login data.

The later acknowledged broker cancellation changes have local PTY/Pion/OpenSSH
evidence plus the owner-run direct check above. Auth-revocation/network-loss
and independent remote process inspection remain unverified. Earlier native
statuses/streams alone do not prove the abort handshake. See COMMAND_CANCELLATION.md.

The 2026-10-20 CLI-options increment adds local tests only. No new Pi session
or write was performed; `-n`/`-T` combinations and `-o` settings are not marked
native-verified by the previous broker evidence. See CLI_OPTIONS.md.
Chrome DevTools `list_pages` timed out establishing the browser connection after
60 seconds; no new CLI/Pi session was started. This access blocker leaves the
new option combinations pending native verification.
The subsequent explicit-file/HostName alias implementation also has local-only
evidence. Synthetic config/ssh -G comparisons and portable builds are not new
Pi sessions, and do not extend the owner-run direct-login evidence to aliases.
After config integration a fresh DevTools `list_pages` attempt again timed out
after 60 seconds before browser access. No CLI/Pi session was created; alias
integration remains pending rather than inferred from the local comparisons.

This file retains sanitized functional outcomes only. No credentials, device or
account IDs, addresses, SDP, ICE configurations, terminal captures or performance
measurements are included. No public release or provider endorsement is implied.

## Environment and restrictions

Native host: macOS arm64, Go 1.27.2, Pion WebRTC v4.2.24. The authorised Connect
dashboard reported agent version 2.13.0. The browser was already signed in after
the user completed normal login. Chrome's local-network permission was subsequently
granted by the user for the companion. Exact browser/OS versions were not recorded.

All remote checks used the existing agent. History persistence was disabled
inside each test shell before diagnostic commands. Only printf/test/read/exit
shell operations and read-only `stty size` were used. No remote file, installed
script, package, configuration, service or reboot was introduced. SSH was not
used as a transport or fallback.

## Native developer harness

`cmd/protocol-probe` ran as a native process. ICE configuration and SDP were
exchanged with the authorised browser via in-memory tool orchestration; local PTY
echo was disabled. The browser submitted the native offer to Connect and returned
the answer; the browser did not carry the shell data.

| Check | Observed result |
| --- | --- |
| Native WebRTC establishment | Passed |
| First printf marker and return output | Passed |
| Remote stdin is a TTY | Passed |
| CONNECT_TTY is set | Passed |
| Agent-originated resize channel | Received |
| Native resize to columns=100, rows=32 | Remote size reported 32 100 |
| Second distinct interactive round trip | Passed |
| Remote exit and channel closure | Observed |
| Harness result | All expected checks passed; process exited 0 |

The harness exit code reports its own acceptance checks, not an agent-provided
remote command exit code. Its signaling JSON is sensitive and is not a fixture.

## Interactive CLI

The built `pi-sh --browser <selected-device>` executable was exercised in a local
terminal. Its companion ran on the authenticated Devices page using DevTools MCP,
so no browser shell connection was needed alongside the native peer.

| Check | Observed result |
| --- | --- |
| Browser-held auth and native shell in CLI | Connected successfully |
| printf marker round trip | Passed |
| Initial window dimensions | Remote size reported 24 80 |
| Ctrl-C while shell builtin read was waiting | Interrupted the read |
| Command after that interrupt | Marker returned successfully |
| User-entered Unicode text | Accented and CJK marker returned unchanged |
| Session beyond first minute authorization check | Remained usable |
| Final companion heartbeat implementation | Session remained usable beyond 90 seconds, confirming successful periodic authorization renewal |
| Companion page reloaded during native session | Missing-heartbeat watchdog ended the CLI with 255 and an authorization-check diagnostic |
| Local line-start ~. escape | Native process ended with local exit code 0 |
| Normal remote exit | Native process ended with 255; no remote status claimed |

These results do not establish arbitrary binary stdin, separate stderr, command
exit status, non-PTY execution, transfer, forwarding, relay-only connectivity,
all authentication failures, long-lived ICE renewal or other platforms. The
manual console-companion workflow was subsequently reported successful by the
user on 2026-10-20, along with Enter followed by typed `~.` disconnect. This
user-reported evidence is distinct from the automated native checks below.

## Authentication regression — 2026-10-20

Reproduced the user's generic setup failure against the still-visible Devices
page. There was exactly one matching device, but the shell-page response was
HTTP 200 reauthentication HTML without the shell controller. The fixed CLI
reported that recent sign-in was required. Normal browser navigation through
Connect Remote shell completed the provider's authentication flow; refreshing
Devices then allowed the rebuilt native CLI to connect, return a read-only
marker, and exit through the local escape with code 0. No token/HTML/session
capture or remote file was retained. History persistence was disabled in the
diagnostic shell before commands.

## Interactive controls and paste — 2026-10-20

Additional developer-controlled native CLI sessions used the authenticated
Devices page and the same unmodified agent. History persistence was disabled
before diagnostic commands. Only shell builtins and read-only `stty size` were
executed; no remote files or configuration were written.

| Check | Observed result |
| --- | --- |
| Ctrl-D at an empty Bash prompt | Bash printed logout and closed the channel; CLI returned 255 with the missing-remote-status diagnostic |
| Bracketed-paste mode | Remote output enabled it; injected standard paste wrappers were interpreted by Bash |
| Single-line wrapped `~.` | Reached the remote editable line; no local disconnect; reproduces the reported symptom, without proving the user's exact original input |
| Wrapped accented/CJK printf | Output preserved the Unicode marker |
| Wrapped two-command multiline paste | Both builtin printf markers returned after Enter |
| `-e none`, multiline quoted string containing a line exactly `~.` | Literal tilde/dot line returned between two markers; connection remained usable |
| Live local terminal resize | Changing the developer's local PTY to 111 columns by 37 rows produced remote `stty size` of 37 111 |
| SIGTERM of the developer's own native CLI | Process returned 255 with context-cancellation diagnostic; no false success |

These paste checks inject known bytes into a real local PTY, rather than driving
the macOS clipboard. Actual clipboard/UI variants remain unverified. Exact
local terminal mode restoration under handled signals is covered separately by
automated real-PTY tests. Other escape characters/control notation have local
automated coverage only; the live configurable-escape check used `-e none`.

## Explicit experimental command adapter — 2026-10-20

Three native legacy-adapter sessions used the then-named `--experimental-command`
(now `--legacy-command`), the existing browser
companion and shell channel. The adapter disabled history-file persistence,
replaced only that session's shell with an in-memory `/bin/sh` supervisor, changed
only its temporary PTY echo/ONLCR settings, and ran child shell builtins with null
stdin. No remote persistent file, script, package, configuration or service was
modified. This is application framing, not a discovered native exec/status API.

| Check | Observed result |
| --- | --- |
| printf marker followed by exit 23 | Only command marker output reached stdout; native CLI exit code 23 |
| Quotes/Unicode, known binary bytes and stderr, exit 7 | An in-memory local stdout pipe exactly matched all 24 expected bytes; native CLI exit code 7 |
| false, capture its result, print result, then true | Printed the preceding false result 1; native CLI exit code 0 |

The byte fixture included an apostrophe, accented/CJK text, NUL, 0xff, CR/LF and
stderr merged into stdout. Local capture retained only match/count metadata; no
runtime signaling, capability URLs, raw session capture or output fixture was
written to the repository. The successful non-TTY stdout check establishes that
this mode does not require local raw-mode ownership.

Interactive Ctrl-D status remains unknown. stdin streaming, arbitrary output
from programs changing PTY modes, separate stderr, signal forwarding, background
jobs, supervisor destruction, Windows/Linux/iSH runtime and complete SSH behavior
are not proved by these cases. See [COMMAND_EXECUTION.md](COMMAND_EXECUTION.md).

## 2026-10-20 shell-only broker

The existing Pi reports Bash 5.2.37 and existing cat/setsid/stty. No dependency was
installed or persistent file changed. All broker source and command bytes stayed
in process memory; anonymous pipes carried the streams. Python was used only as
a local Mac test driver, never on the Pi or as a CLI runtime dependency.

- Native Go/Connect broker: all 256 byte values repeated eight times, 2048-byte
  stdin/stdout exact; independent all 256-byte stderr exact; exit 23. Child test
  confirmed none of descriptors 0/1/2 was a TTY and /dev/tty was unavailable.
- Separate native run: normal exit143 returned143 with no remote stream bytes.
- Larger fixture: 65536-byte stdin/stdout matched, but only part of the expected
  131072-byte stderr arrived before failure255. This did not verify SIGKILL status.
  A subsequent callback regression reproduced the transport's old 32-message
  overflow on 2048 one-byte fragments. The dual-bound queue fix passes locally;
  the subsequent rerun below supplies native validation.
- Rerun could not begin because Chrome DevTools timed out establishing its browser
  connection, including after the user's ready response. The native CLI timed out
  before connection with no command output. Restore browser DevTools connectivity
  and rerun the read-only fixture to close this blocker.

Comparisons exclude the CLI's four setup/experimental stderr lines. Counts and
boolean comparisons only were retained; no raw session captures or credentials.
Direct read-only SSH reference to raspberrypi.local failed name resolution, so
execution differential fixtures used an isolated local OpenSSH server instead.

After the user allowed Chrome's connection, DevTools reconnected with new tab IDs.
The first attempt correctly reported Connect's recent-sign-in requirement; opening
the official Remote shell flow succeeded normally and Devices was refreshed.
The native broker rerun then completed with exactly65536 stdin/stdout bytes and
131072 separate concurrent stderr bytes. The final `kill -KILL $$` returned 255
with no CLI failure diagnostic, matching the selected OpenSSH signal fixture.
The previous browser blocker is resolved. No raw byte captures were saved.

## 2026-10-20 direct HTTPS login and command, owner-operated

The owner completed a fresh local Go login-probe with password and TOTP; the
program reported the Connect devices page. A subsequent pi-sh invocation used
`--direct-login --experimental-command` and the read-only command
`printf "direct-login-ok\n"; exit 23`. It printed direct-login-ok and local
`echo $?` returned 23, as reported by the owner. Only that sanitized marker/status
is retained; no email, credential, cookie or raw authenticated transcript.

This verifies the complete direct-authentication-to-command path: new in-memory
web session, normal callback, discovery, authorized shell metadata, ICE, CSRF
offer and answer, native WebRTC and broker completion. Chrome and its extension/
console bridge were not used by that process. No Pi package, persistent file,
configuration or service was modified; the broker source remained in memory.
Selected ICE route, direct interactive mode, long-session authorization and
physical iSH behavior were not established by this short test.
