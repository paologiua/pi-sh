# Security and publication

The CLI text-input filter now batches only locally available UTF-8 into at most
4KiB messages, flushing before a read can block (including a partial next rune),
local actions and terminal/error boundaries. A64KiB paste no longer produces
65,536 frames and exhausts a stalled peer's4096-frame queue before status delivery.
The queue's byte/frame limits and strict result rules are unchanged; no input or
command is replayed. This has local stress/latency/escape evidence only. See
TTY_COMPLETION.md; old native close failures and arbitrary unread-input capacity
remain separate limits. No remote file, software or configuration change occurs.

Recursive downloads and ls -f now use a local ReadDirEach extension to pkg/sftp
v1.13.11. All65 copied upstream files are unchanged with per-file hashes; one
source file and parser tests are added. Original notices remain; no v2 library
is linked. NAME packets are consumed individually, malformed/unsafe entries fail,
and directory CLOSE failures are retained. Sorted ls still accumulates entries.
SFTP_LISTING.md and third_party/sftp/PI_SH_CHANGES.md record memory bounds,
cancellation limits, evidence, provenance and local-replacement audit duties.
No new remote software, authentication change, replay or Pi write is introduced.

Offline -V/-Q returns before auth, discovery, terminal changes or configuration
file loading. Version output uses executable metadata only, with bounded safe
version tokens; build settings/environment/VCS paths are omitted. Unknown query
text and writer error contents are not echoed. Query names explicitly distinguish
pi-sh implementation from SSH algorithm support or verified runtime behavior.
See LOCAL_INSPECTION.md; no new remote access or credential path is introduced.

The TTY input-close correction never derives status from a transport failure.
Only the existing validated TX plus clean EOF can win the bounded result drain;
local/output errors, malformed/trailing data and auth/cancellation remain failures.
No command or input bytes are replayed, no crypto/auth check changes, and no remote
cleanup proof is inferred. See TTY_COMPLETION.md.

The companion now prefers CSRF metadata from a newly fetched same-origin shell
page after reauthentication. Tokens remain browser-local; a refused POST is not
replayed. Native testing recorded only HTTP categories/statuses, controlled
terminal markers/dimensions and exit codes. Temporary instrumentation is removed.

GNU/Linux PENDIN uses a freshly read native state with verified GNU utility,
platform and fixed 36-field/hex-width checks, and exact readback. Only the local
flag bit0x4000 is changed; reprint is translated to rprnt. Unknown formats or
failed application do not silently drop requested modes. No foreign save string,
new program or remote file is introduced. Native subset and one unclassified
WebRTC-close failure are separated in REAL_DEVICE_TESTS.md.

Local suspension sends SIGTSTP only to the client's own PID. It never accepts a
process selector or signals a group. It restores local flags/modes before stopping,
blocks new stream operations during handoff and joins the transition on shutdown.
The shell may change the borrowed descriptor/mode baseline only while handed back;
files must stay open. No authentication, remote replay or cleanup guarantee changes.
See ESCAPES.md for unsupported platforms and unverified native behavior.

The corrected TTY controller uses the dedicated fresh carrier group for command
and metadata lifetime. Direct local HUP/TERM tests sample ownership before abort,
verify sampled group members stop before PTY close and preserve an external
sentinel. Abort never accepts a process selector; own PID1 is refused to avoid
kill's special -1 meaning. This is not native peer-close/revocation proof and
does not cover detached descendants. Startup files are disabled, controller
HISTFILE points only at /dev/null, history is disabled and not exported to the
user command. See TTY_JOB_CONTROL.md for the group-ownership assumption.

Local escape help contains only fixed supported-operation text and the validated
local prefix. It writes to cancellable local stderr, never into a remote command;
short/failed diagnostic writes stop input with an error. No credentials or
remote text enter its formatting. ESCAPES.md inventories pending actions.

The Unix command/shell paths now stop and join owned borrowed-file operations
before returning. Cancellation cannot leave an idle input worker consuming the
next session's bytes; restoration failures cannot become successful status or
ignored EOF. Borrowed files stay open and only the owned O_NONBLOCK bit is
restored. No replay or new remote cleanup guarantee follows. Windows blocked
calls, generic callbacks and filesystem kernel latency remain explicit limits;
see [LOCAL_IO.md](LOCAL_IO.md).

This unofficial project has an experimental native shell client, with direct
memory-only HTTPS login or browser-assisted authentication. It is pre-release. Service terms remain a
separate [publication/scope review](SERVICE_CONSTRAINTS.md).

## Invariants

Portable command terminal modes now cross only a validated fixed vocabulary,
not OS-specific bitfields or a foreign stty snapshot. Mode/character names and
sizes/rates are bounded; unknown values fail before staging. Argument quoting
and ASCII hex upload preserve metacharacters and non-UTF-8 control values as
data. The worker applies modes before readiness/command execution; stty failure
cannot silently launch with requested modes absent. The BSD literal-caret field
form is generated from the carrier's own dialect with fixed validated names,
never accepted as a caller-provided shell fragment. Existing remote software,
ephemeral-PTY-only changes and cleanup-evidence boundaries remain unchanged.
See TTY_MODES.md for all local evidence and unverified native/runtime limits.

The 2026-10-21 staged TTY tests execute the production loader/adapter
only in local temporary PTYs. A later CLI increment enables experimental command
TTY requests with local Pion/PTY coordination evidence, still without contacting
the Pi. Post-commit cancellation explicitly retains ErrCleanupUnconfirmed; terminal
data must not be confused with a binary broker cleanup record. Immediate input,
full-size staging and first output have local evidence, while Connect lifecycle
and independent descendant cleanup remain unverified. TERM is quoted and bounded;
dimensions are validated before inclusion in staged source, and no system setting
or remote file is changed. Full local termios negotiation is not claimed.
Intentional ~. disconnect returns local0 with missing-cleanup diagnostics;
restoration failures cannot be masked by that status or by a command result.
See TTY_COMMANDS.md.

The shared command coordinator now checks an already-cancelled context or queued
authorization failure before invoking any adapter. A regression first observed
one adapter invocation despite known cancellation, then passed100 repeated race
runs after the preflight fix. This prevents launch on an already-known failure;
it does not eliminate the unavoidable race with a later remote revocation or
replace periodic service checks. Commands are still never replayed.

TTY-command research runs only local test PTYs and a pinned ephemeral loopback
OpenSSH server. The original POSIX candidate conflates 130/SIGINT; a later Bash
worker fixes that locally and checks HUP/TERM cleanup while the carrier stays open.
Neither candidate is exposed by the CLI. Direct local signals do not prove the
unchanged agent's teardown, cancellation or revocation behavior. No Pi process or
persistent file is created. TTY_COMMANDS.md separates the evidence and remaining
staging, lifecycle and native gaps; no new service signal/channel is invented.

Explicit regular-file `-p` can broaden target permission bits to match the
source, overriding umask; it excludes ownership, set-ID/sticky bits and ACLs.
Final metadata status errors warn without cancelling successful data copies.
Regular uploads send mode and timestamps in one handle FSETSTAT, so pathname
replacement cannot redirect that update. Directory metadata and local download
timestamps still use paths; server attribute updates need not be atomic. No
general race-free or all-metadata preservation is claimed; see SFTP_METADATA.md. Native preservation remains unperformed: the owner-approved temporary probe
failed its1MiB phase before -p. Separate cleanup verified the approved root absent.

Resume uses destination size without verifying its prefix, matching the measured
OpenSSH behavior. Changing files or mismatched prefixes can corrupt output.
Failures attempt truncation to the acknowledged/written prefix, but connection
loss can prevent it. No atomicity, integrity checksum or concurrent-writer
protection is provided; see SFTP_RESUME.md. Remote writes are limited to the exact owner-approved NATIVE_TRANSFER_PLAN.md
scope; its resume phase was not reached.

The latest login diagnostic increment emits only fixed transport/body categories
and numeric HTTP statuses. It never wraps underlying network errors, which can
contain callback queries or proxy details. Synthetic failure tests assert exact
redacted output. It adds no credential capture, retry or TLS/policy bypass.

- Never modify the Raspberry Pi to enable this project. No installed helpers,
  persistent files, configuration changes, service restarts or reboots.
- Remote test file writes need specific `pi-cp` authorization and an approved
  path, followed by verified cleanup. None are currently authorised or performed.
- SSH may be used only for read-only diagnosis, never as a client fallback.
- Authenticate through approved flows. Retain default HTTPS verification and
  WebRTC DTLS fingerprint checking. Do not bypass MFA, CSRF or authorisation.
- No plaintext credential storage, browser-profile reading, cookie export,
  secrets in process arguments, HAR exports or authenticated traffic fixtures.
- Log no passwords, cookies, authorization headers, CSRF tokens, TURN credentials,
  SDP, ICE candidates, session locations, device identifiers or terminal contents.
- Keep terminal input/output local by default. Redact errors and diagnostics.

## Threats and required controls for implementation

| Threat | Required control |
| --- | --- |
| Credential leakage through redirects | Same-origin signaling locations; reject cross-origin redirects before sending auth |
| Session mix-up | Bind discovery, signaling, peer and fingerprint to the intended authorised device |
| Malformed peer messages | Bounds on frame sizes, parser validation, fuzzing, bounded queues |
| Terminal escape and clipboard abuse | No automatic local command execution or clipboard access; document terminal risks |
| Transfer injection/path traversal | Correct quoting or native framing, explicit path rules, no evaluation of filenames |
| False success | Require verified remote status; distinguish disconnects from clean command exit |
| Cancellation leaving raw terminal | Restore original mode on every local error and interruption |
| Dependency compromise | Minimal dependencies, pinned versions, licence inventory and vulnerability checks |

Most are design requirements. The generic transport now retains default DTLS
verification, suppresses upstream logs, sanitizes API errors, and bounds
application queues and message sizes. Local tests cover fingerprint rejection,
cancellation and limits; see [transport scope](TRANSPORT.md). The browser companion retains account authentication/CSRF in the browser,
checks exact selected device identity, and restricts signaling to the service
origin. This is not independent SSH-style host-key authentication.

## Publication gates

Resolve Connect service terms and disclosure permission; select an original-code
licence with the owner; verify contributor rights; inventory actual dependencies
and notices; verify trademark presentation; scan the complete staged/history
content for secrets; verify test claims; and review release binaries. No public
repository, release or remote push has been created by this work. Public access
to a JavaScript bundle does not establish an open-source licence for that bundle.

There is no public security-reporting address or repository yet. Before release,
the owner must provide a private reporting channel; do not invent an address.

## Browser companion boundary

The listener binds only 127.0.0.1 at an ephemeral port, uses a fresh random
256-bit path capability, validates exact Host and Origin, bounds request bodies,
and sends no-store responses. CORS is limited to the exact Connect origin. The
local URL is a short-lived capability printed for the local operator, not an
account credential; do not share it or retain it in artifacts. Browser local
network permission is requested normally, never bypassed.

Account cookies, passwords and CSRF tokens never leave browser execution. ICE
configuration (including TURN credentials) and SDP must reach the Go peer and
are passed only in memory over loopback HTTP, not persisted. This local hop is
not TLS-encrypted and assumes a trusted local host. No local capability can
protect against a fully compromised user account or privileged host process.
The Go bridge never accepts arbitrary browser error text into diagnostics. A
fixed whitelist translates companion failure codes into actionable messages;
unknown codes and additional response/exception fields are discarded. The
reauthentication classifier preserves the provider's normal browser flow.

Browser authorization checks run every minute. A non-204/failed check reports a
failure; missing authorization confirmation for 90 seconds terminates the CLI.
Keeping a tab open is required. Native credentials/ICE refresh and recovery are
not implemented, so long-session guarantees must not be claimed.

The interactive CLI now handles Unix SIGINT/SIGTERM/SIGHUP/SIGQUIT through context
cancellation and waits for terminal restoration. Regression tests cover the race
between cancellation and restored-terminal notification, so restoration alone
cannot report session success. Restoration errors propagate to the local failure
status. Windows console close/logoff and uncatchable process termination remain
outside the verified cleanup guarantees.

The experimental command adapter accepts only explicitly supplied shell command
text. Single-quote escaping prevents that text from breaking out of its intended
child `/bin/sh -c`; shell interpretation inside that child is intentional. It
creates no remote file, uses null stdin, changes only its current PTY modes and
does not install a trap in interactive sessions. Random application markers
prevent accidental framing collisions, not malicious output forgery by the remote
command. No native agent status or separate stderr is claimed. Truncation,
unexpected trailing output and authorization/cancellation failures cannot default
to success. See [COMMAND_EXECUTION.md](COMMAND_EXECUTION.md) for limits.

## Shell-only broker boundary

The experimental command broker executes independently authored Bash source in
memory using existing tools. It creates anonymous kernel pipes and child processes,
not remote files, helpers or services. Capabilities are checked without installing
anything. Input/source/record sizes and flow-control windows are bounded; malformed
or missing completion fails closed. Command text is separate from evaluated
broker source and runs in a child shell. Random record markers prevent accidental
framing collisions, not attacks by processes within the same remote account.
User environment and the authorized command remain trusted. Abnormal cleanup
targets the owned process group; arbitrary daemonization is not contained.
No Python requirement or automatic legacy-mode downgrade is introduced.

## Direct HTTPS mode

`--direct-login` uses fresh ordinary HTTPS forms and a private memory-only cookie
jar with public-suffix checks. Password/TOTP are entered at the controlling
terminal with echo disabled, sent only to exact observed ID actions, and never
logged or persisted. Unknown/ambiguous forms, CSRF omissions and credential-body
redirects fail closed. HTTP 401 is accepted only at the password transition with
the exact observed OTP challenge; it is not a general authorization exemption.
Login uses normal server-owned OAuth parameters/callback, not an invented grant
or copied web client credential. Connect signaling validates selected device
identity, exact origins, page CSRF, bounded responses and polling locations.
Only one offer creation is attempted; commands are not replayed on auth failure.
Periodic204 checks and bounded requests terminate failed authorization.

A successful owner-run password/TOTP login and remote command 23 are recorded in
REAL_DEVICE_TESTS.md. The client session material is discarded when the process exits; no storage or refresh
feature is introduced. Clearing buffers does not erase all Go encoding copies.
Terminal restoration on cancellation is locally tested; physical iSH and Windows
console runtime and long-session/actual-revocation behavior remain pending.

Process exit discards the local cookie jar; it does not assert server-side logout
or revoke a server session. No logout/revocation request is implemented.

## Command cancellation boundary

After launch commitment the broker attempts only cleanup framing on cancellation
or auth loss; it does not authorize/replay command execution. The CLI retains its
owned transport for bounded cleanup (two-second handshake, three-second outer
grace) then closes it on all paths. Missing confirmation remains visible together
with the original failure, including after transport-error normalization.
The broker's acknowledgement follows group termination and direct-child/helper
waits; it is not a native agent status or proof about detached descendants.
Carrier loss and unresponsive/custom caller I/O can prevent confirmation.
No PID values or command output are added to production diagnostics. Local test
process inspection and ephemeral SSH keys are not remote/runtime requirements.
See COMMAND_CANCELLATION.md for evidence and scope.

## CLI configuration boundary

The supported configuration subset changes target aliases, escapes, stdin and
requested terminal policy. Only an explicit `-F path` selects the root, with
file-based Include references enabled; no default root files, Match or command
execution are enabled. Unknown directives (including ProxyCommand/LocalCommand),
even in inactive included blocks, and incompatible legacy TTY requests fail
before authentication. The whole load shares 1 MiB, 256 files, 16 Include levels
and 16384 filesystem-entry visits. Glob enumeration reads chunks of 128 entries;
paths/matches/components/lines are bounded. File identity detects recursive
aliases. Ordinary FIFO/device sources and non-directory glob parents are not
opened for configuration data. This is not a hostile/concurrently changing
filesystem sandbox or a wall-clock I/O guarantee. Errors omit paths, contents,
environment values and OS error strings. Only explicit Include paths use the
documented home/environment/token expansion; no shell interprets their text.
HostName uses one-pass %h/%% expansion and exact device matching; it does not
change transport endpoints or authorize another account/user. Expansion is
bounded to 4096 target bytes before appending substitutions. Control characters
in destinations/expanded targets are rejected. No local configuration command is
evaluated. Explicit `-G` output includes the selected target name/ID and three
terminal/stdin settings, before authentication/network setup; it is not an
implicit diagnostic. It never reports credentials, cookies or transport secrets.
StdinNull does not disable controlling-terminal login prompts or TLS/DTLS checks.

## SFTP and copy boundary

Transfer option parsing accepts attached values only for registered short
options. '--' and the first operand terminate option parsing; a consumed value
is never reinterpreted as another flag. Unsupported behavior still fails before
login. No config command execution or remote shell path expansion is introduced.

pi-cp/pi-sftp use only a server executable already present on the authorized
device, launched over the existing broker. Fixed search paths or a validated,
quoted absolute executable path are used; missing servers fail without installing
or switching to SSH. File paths and data are binary SFTP fields, never shell
arguments. No arbitrary local shell command/escape is implemented. Read-only
pi-sftp adds the existing server's -R flag; unsupported servers fail, with no
downgrade. Agent-run Pi write tests still require an approved path and cleanup;
none have occurred in this increment. Local tests use only memory or t.TempDir.

The client owns its protocol pipes and closes them on cancellation. Negotiation
and shutdown waits are bounded, and final broker/server status is checked. Partial
files and truncation remain possible on failure; no atomicity claim is made.
Both clients bypass the pinned library's optional ReaderFrom/WriterTo fast paths
using a 32 KiB plain-interface copy buffer. Failure fixtures exposed a short-read
EOF masking a final remote WRITE error; complete-refusal and partial-prefix cases
now remain errors, including copies without flush enabled. The dependency and
its source stay unchanged; a vulnerability scan alone does not prove I/O semantics.
The server child captures its original umask in bounded setup metadata, then uses
umask077 only for its process. New uploads verify private initial modes and apply
source permissions filtered by the original umask before sending contents;
chmod/stat failure aborts before data, leaving an empty private partial file.
Exclusive creation avoids truncation fallback if a new destination appears.
New downloads supply source permission bits to local creation (local umask
applies); existing targets retain their modes. New directories are initially
private and receive 0777 filtered by the original mask. Invalid/missing mask
metadata fails setup; the prefix is consumed before the SFTP stream. No persistent
umask or system configuration is changed. Selected modes are differential-tested;
ACLs, special modes and hostile-server trust remain outside those guarantees.
Automatic local basenames reject parent/root and native path/volume components;
literal symlink traversal semantics are preserved. Directory listings accumulate
in memory; no whole-directory resource bound or hostile-server sandbox is claimed.
SFTP receive packets are bounded by the pinned library to 256 KiB and configured
file requests use 1024 bytes/four requests. Remote error strings/packet data are
discarded in diagnostics; only fixed classes/numeric statuses are shown. Direct
login still uses the controlling terminal, separate from batch/protocol stdin.
The library's SSH authentication/transport APIs are not used. TLS/DTLS validation
and session authorization remain unchanged. New dependencies are checksum-pinned,
licence-inventoried and scanned; source-file release review remains required.

SFTP -f requests sync of the open destination file. Uploads send fsync only for
advertised extension version1; absence is silently skipped as in OpenSSH. Sync
status failures are advisory with redacted diagnostics; copy, protocol loss,
malformed reply, failed CLOSE and final broker failures remain fatal. Exit0 does
not prove flush happened or succeeded. No directory sync or crash guarantee is
claimed. Tests include normalization of protocol EOF versus connection EOF: only
a subsequent successful handle CLOSE can finish successfully. SFTP_FLUSH.md
records source/reference evidence, tests and the native-verification gap.

## Forwarding investigation boundary

Forwarding options remain rejected. The Bash, Go TCP and optional local nc
fixtures bind only IPv4 loopback and clean up their own processes/sockets. They
never access the Pi or alter its filesystem/services. A helper's local presence
does not authorize installing it remotely or imply remote semantics; even nc -N
differs between the observed Mac help and OpenBSD documentation. Any future
connector must verify existing remote capability, half-close, bounded cleanup,
destination/bind policy and authorization before exposing a listener. See
FORWARDING.md for evidence and remaining blockers.

## Recursive SCP local implementation

`pi-cp -r` follows source symlinks, including targets outside the requested root,
as SCP does. Use it only on stable trusted source/destination trees. Ancestor
cycles and depth64 fail; entry basenames are checked before joining, without
cleaning caller symlink/parent components. New remote directories are checked
against private creation before widening their permissions. Final directory modes
can change an existing destination even without -p in the upload direction.
Directory updates are path-based and retain replacement races. Partial trees are
left on failure; special bits/ownership/ACLs are not preserved. Recursive downloads now stream directory packets; default sorted console
listings still accumulate a whole directory.
Only local temporary files were used for recursion tests; this adds no authority
to write content on the Pi. See SCP_RECURSION.md.

## Go x509 security correction during the listing increment

A fresh govulncheck scan against Go1.27.2 found reachable GO-2026-6809, concerning
compromised intermediate-certificate distrust in crypto/x509. The project minimum
is now Go1.27.3. The official [Go release history](https://go.dev/doc/devel/release#go1.27.3)
records the x509 security release on2026-10-19. x/crypto0.57.0 and x/net0.60.0
also replace the old selected versions; no certificate check or
cryptographic verification is bypassed. Rebuild old binaries to obtain the fix.

The new native source scan reports zero reachable and zero imported-package
findings; one module-level finding remains in unimported crypto/openpgp,
GO-2026-5932, with no fixed version and no identified calls. Local
replacements need a separate origin/version check: the unchanged SFTP snapshot
was checked against its manifest and its original module was scanned separately.
No native Pi operation used the old binaries after discovery of this advisory.
This is a current scan result, not a security guarantee or release approval.

## Opt-in synthetic transfer probe

The new developer transfer-probe defaults to offline plan output. Its execute
flag is not a substitute for human authorization. It accepts only an exact
/tmp/pi-sh-probe- plus32 lowercase-hex root, creates it exclusively, requires
private directory mode before contents, and writes only fixed synthetic entries.
Its static cleanup inventory rejects unknown names, symlinks and type changes
before deletion, never uses remote recursive removal, and verifies root absence.
Lost transport preserves a failed/unconfirmed cleanup result; there is no replay.
A stable private tree is still required because inventory/delete is not atomic.
The recovery-only mode must not adopt a pre-existing root that creation refused.
Local fixtures prove those checks; NATIVE_TRANSFER_PLAN.md remains approval
pending. No write on the Pi is authorized merely by preparing this binary.

Supported options following the destination are now parsed until command start
or explicit --. Unsupported/malformed tokens in that option position fail before
authentication; first command word and explicit separators preserve later literal
operands. Post-host -V/-Q terminate before file/auth/network setup, like the
existing pre-host inspection. No local command execution or implicit config
loading is added. Offline OpenSSH/argument/subprocess tests cover this boundary.
