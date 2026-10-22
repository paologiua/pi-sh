# Architecture

Recursive downloads and ls -f now use a local ReadDirEach extension to pkg/sftp
v1.13.11. All65 copied upstream files are unchanged with per-file hashes; one
source file and parser tests are added. Original notices remain; no v2 library
is linked. NAME packets are consumed individually, malformed/unsafe entries fail,
and directory CLOSE failures are retained. Sorted ls still accumulates entries.
SFTP_LISTING.md and third_party/sftp/PI_SH_CHANGES.md record memory bounds,
cancellation limits, evidence, provenance and local-replacement audit duties.
No new remote software, authentication change, replay or Pi write is introduced.

TTYPlan.Run can drain the independent result reader for one second after a
caller-classified carrier-send error. The CLI supplies the Pion error policy;
local input errors do not opt in. A valid status and clean EOF remain required,
and commandrun retains auth/cancel arbitration. No new wire frame or replay is
introduced. See TTY_COMPLETION.md for the reproduced race and native subset.

The existing stty dialect boundary now maps GNU rprnt and handles PENDIN through
validated fresh carrier-native Linux state, with exact readback. Ordinary modes
are applied before that snapshot so the bit edit cannot overwrite them. Browser
signaling uses fresh shell-page CSRF metadata without automatic POST retry.
Basic command TTY now has real Pi browser-companion evidence for TERM, initial
size, input and status23; advanced lifecycle and one earlier WebRTC-close failure
remain open. See TTY_MODES.md and REAL_DEVICE_TESTS.md.

Local ~^Z composes localio.WithSuspended at the single input boundary with
terminal.Session.Suspend. The operation gate drains writes before handing back
flags; the terminal watcher serializes restore, self SIGTSTP, CONT and reacquisition.
Close/cancellation joins that transition. Fresh shell baselines and a forced size
refresh preserve terminal ownership across foreground resumption. See ESCAPES.md
for Mac process/TTY coordinator evidence and remaining platform/native gaps.

`internal/localio` now owns cancellable operations on grouped borrowed standard
files for macOS/Linux, leaving file lifetime with callers. Private wake-pipe
polling interrupts idle reads and backpressured writes; adapters opt into joining
workers with StopIO. Terminal state remains independently owned by terminal;
commandrun still arbitrates status and authorization. See [LOCAL_IO.md](LOCAL_IO.md)
for alias/flag restoration, the independent C/D receiver and platform limits.

Status: 2026-10-20. Implemented: `cmd/pi-sh`, the browser authentication companion,
Pion transport, local terminal handling, and the developer protocol probe.
Authentication/signaling use either a direct in-memory HTTPS login or the browser
companion; peer and shell I/O run natively in Go. Both discovery implementations
resolve an exact authorized device name/ID.
Native agent exec and forwarding remain unimplemented; experimental transfer uses
an existing SFTP server through the verified broker, with local file-transfer
tests and owner-reported native read-only metadata success, including a later
private-creation startup rerun. File creation/content/preservation remain locally
tested only.
`internal/shellbroker` provides binary command streams over the existing carrier;
`internal/command` preserves the older limited PTY adapter.

Experimental command TTY requests now use shellbroker.NewTTYConfigured through
cmd/pi-sh/tty_command.go. The staged source saves/restores carrier modes around
loading and applies quoted TERM/initial size. One controller and its command
share the dedicated carrier group; asynchronous jobs metadata travels through
one anonymous helper pipe. Interactive Bash initialization normalizes inherited
job-control signals, then monitor mode is disabled before child creation.
Controller startup files/history are isolated. See TTY_JOB_CONTROL.md for the
private-group abort guard, real local cleanup/STOP/CONT evidence and native gaps.
command.ReadResult validates TR/TX frames. The CLI owns local raw mode, the existing
text/escape filter and readiness-gated resize updates over the observed channel;
commandrun arbitrates completion/auth loss/cancellation. Only the no-TTY broker
has a cleanup acknowledgment: the TTY adapter reports unconfirmed cleanup after
post-commit failure and the CLI then closes its peer. Local EOF never invents byte4.
Mode capture now precedes local raw mode: terminal translates macOS/Linux kernel
fields into internal/ttymode, which validates a fixed vocabulary; shellbroker
quotes a bounded Bash array applied before readiness. The carrier's own stty
format handles the literal-caret BSD/GNU parser difference without transporting
an OS-specific snapshot. See TTY_MODES.md for the inventory and missing Windows/
unusual-hardware/error-parity coverage.
Local staged-PTY and actual Pion/PTY coordinator tests remain separate from the
older numeric/Bash prototype comparisons. Remaining Windows/hardware/mode-error
parity, nested stopped jobs/native suspension, Windows/generic blocking-I/O cancellation, real
agent teardown and native TTY command evidence remain open. See TTY_COMMANDS.md.

`internal/connectsession` shares direct/browser source authentication and a
single-use broker connection for pi-cp/pi-sftp; pi-sh delegates its authentication
there too. `internal/commandrun` owns the shared completion/auth/cancellation
arbitration. `internal/sftpclient` bridges owned binary protocol pipes to a single
existing server process, negotiates SFTP and verifies its final status. Frontends
use sftpcli/copycli for literal paths, commands and file operations. Copies use
plain Reader/Writer interfaces with a 32 KiB buffer, retaining final short WRITE
failures rather than library-specific fast-path arbitration. SFTP flush policy
requests local File.Sync or negotiated remote handle fsync before CLOSE;
advisory status failures cannot erase copy/protocol/close errors. Filesystem
paths are never interpolated into the server-launch command. No local SSH process
or SSH wire fallback is used; see SFTP_BACKEND.md for scope and tests.

Resume is a file-operation policy in sftpcli, separate from authentication,
broker and SFTP negotiation. It seeks to the target size, never truncates on
initial resume open, and retains a contiguous successful-byte count for cleanup
after copy errors. Upload sets append and requires an existing shorter file;
download can create a missing file. No protocol extension or retry is needed.
See SFTP_RESUME.md for measured reference rules and integrity limitations.

Regular-file -p snapshots source mode and atime/mtime before reads. Per-platform
standard-library stat adapters supply native atime. The file-operation layer
applies ordinary mode and second-resolution SFTP timestamps, reports sanitized
advisory metadata statuses, and retains data/close failures. Regular uploads use
one combined handle FSETSTAT through the documented local pkg/sftp extension.
Directory metadata and local download timestamps remain path-based; preserving
raw attribute-presence flags remains a boundary improvement.
See SFTP_METADATA.md. Authentication, agent and broker protocol are unchanged.

Transfer frontends use internal/cliargs to adapt only registered short options
with attached arguments to Go flag. Values/operands are not reinterpreted as
options. SFTP setup consumes a bounded numeric umask prefix, then negotiates v3;
only the server child uses private umask077, with the original mask available to
the file-operation layer. No persistent remote setting is changed.

Forwarding is still unimplemented. FORWARDING.md records the measured lack of
socket half-close in a simple Bash bridge and the existing-local-utility candidate;
these do not grant native agent channels or prove remote utility availability.

See [PROTOCOL.md](PROTOCOL.md), [TRANSPORT.md](TRANSPORT.md) and
[TERMINAL.md](TERMINAL.md) for implemented contracts and limits. The full intended
boundaries below remain the architectural target, not a feature-completion claim.

## Flow and boundaries

`CLI/config → session coordinator → Connect signaling → WebRTC → session codec`

The coordinator owns cancellation and teardown. Authentication grants access to
the HTTP layer; discovery supplies an authorised device identity; terminal,
execution, transfer and forwarding consume explicit session capabilities.

The current CLI separates input filtering (`input.go`), escape-option validation
(`escape.go`), bounded CLI option/configuration parsing (`options.go`),
end-of-session arbitration (`session.go`), and platform process
signals from connection orchestration. `-e none` disables local input escapes;
four `-o` scalars and offline `-G` are supported. `internal/sshconfig` parses
explicit regular root/Include files with Host selection and first values, bounds
aggregate reads/glob traversal and expands HostName %h/%% once. Include has its
own path/one-pass token/environment rules and restores caller Host scope. CLI
HostName is seeded before loading. It owns no authentication, networking or execution.
The CLI applies overrides and passes the resolved exact device separately from
unchanged remote command arguments. Default root files, Match and comprehensive
SSH parsing remain future work; see CONFIGURATION.md and CLI_OPTIONS.md.
The local-terminal `Done` event certifies restoration, never successful remote
execution. Cancellation and restoration errors are propagated independently.

`internal/command` owns quoting, bounded bootstrap generation, random application
markers and streaming completion validation. A byte-oriented interface keeps it
independent of authentication and Pion. The CLI selects it only through
`--legacy-command`, bridges existing shell messages, checks browser
authorization and maps its validated result to local exit status. It does not
open a new protocol channel or install an interactive shell trap. Its compatibility
limits are specified in [COMMAND_EXECUTION.md](COMMAND_EXECUTION.md).

`internal/shellbroker` owns capability checks, staged in-memory code upload,
anonymous-pipe worker topology, binary framing, bounded flow control and completion
classification. Its public interface is a command plan, a cancellable byte carrier
and three standard streams; callers do not coordinate individual protocol steps.
The CLI selects this with `--experimental-command`. No new Connect channel is
invented. See [SHELL_BROKER.md](SHELL_BROKER.md) for measured scope and requirements.

For broker commands the coordinator owns the peer lifetime explicitly rather
than immediately closing it on the session context's cancellation. Normal I/O
still cancels; a bounded receive pump and cleanup-only C/D handshake retain the
carrier for up to two seconds. The CLI waits up to three seconds for Run and then
closes its peer. This permits acknowledged process cleanup on auth loss or local
signals without replaying a command. Failure and missing cleanup evidence remain
distinct; caller-owned blocked I/O can still require the outer bound. Other modes
keep immediate context-driven peer closure. See COMMAND_CANCELLATION.md.

| Module | Responsibility | Must not do |
| --- | --- | --- |
| CLI/config | Parse SSH/SCP/SFTP syntax, config precedence, diagnostics and local status | Silently accept unsupported options; evaluate config commands implicitly |
| Authentication | Approved sign-in flow; in-memory secrets and OS-backed credential stores where available | Export browser cookies, request passwords in flags, save plaintext secrets |
| Discovery | Resolve authorised Connect devices and reject ambiguous names | Treat arbitrary DNS names as verified Connect identities |
| Signaling | Session-scoped offer/answer exchange, CSRF where required, bounded retries, auth expiry | Invent endpoints or replay opaque requests without evidence |
| WebRTC transport | Pion peer lifecycle, ICE/STUN/TURN, DTLS fingerprint validation, SCTP/data channels | Disable certificate/fingerprint verification or log SDP/ICE credentials |
| Session codec | Only observed, versioned channel framing and control messages | Assume SSH channels, exit status, exec or subsystem requests exist |
| Terminal | Raw mode and restoration, resize, escapes, control characters, platform adapters | Impose Unix ioctls on Windows or assume an iSH syscall works |
| Execution | Separate command and interactive modes when supported; report exit semantics precisely | Parse a prompt or connection close as a successful remote exit |
| Transfer | Exact binary streaming and metadata semantics, explicit capability checks | Install a helper or claim PTY text is a binary-safe stream |
| Forwarding | Independent byte streams, flow control and per-direction cancellation | Assume a shell channel provides socket access |

## Capability policy

A shell is not proof of exec, file transfer or forwarding. First prove a native
interactive session with reproducible authorised evidence. Then discover each
additional capability independently. If native channels do not expose it,
evaluate only existing remote facilities within AGENTS.md: no installed agent,
service, script, system changes or persistent helper. Any ephemeral command
adaptation needs explicit analysis of framing, echo, encoding, stderr, status,
shell side effects and cleanup. Do not label an approximation 1:1 compatibility.
SSH cannot be the transport fallback: this project uses Connect exclusively.

## Portability

Keep the core pure Go. OS-specific terminal and secure storage code belongs
behind build tags and narrow interfaces. Linux/386 CGO-disabled compilation is
mandatory; iSH runtime validation is a separate gate. The initial authentication path deliberately depends on a browser companion.
It makes same-origin authenticated requests without exporting account cookies
or CSRF tokens. A portable standalone authentication flow is unresolved; do not
replace this dependency with credential extraction.

## Failure and resource handling

All network work must take a context, impose time and size limits, close peers
and channels on cancellation, and bound send queues. Avoid retrying a session
creation POST unless duplicate-session behavior is understood. Validate origins
before following signaling locations or redirects. Redact errors before display.
Return SSH-like 255 for connection errors only once the CLI contract exists;
never substitute that convention for an observed remote exit code.

## Native login decision — 2026-10-20

A provider-approved public-client OAuth/PKCE or operator device grant would allow
an external browser to approve CLI access without a Chrome extension. The normal
Connect web app uses OAuth/S256, but own-client registration, callback policy and
Connect resource authorization remain unknown. Do not confuse device enrollment
with operator login. Keep the browserauth interface replaceable, but do not add
a production native provider using guessed endpoints/scopes or the web client
identity. CLIENT_AUTH_RESEARCH.md records the evidence and exact external blocker.
iSH must not depend on desktop browser automation; physical runtime/network and
background execution remain separate verification requirements.

## Direct web session implemented

The normal first-party web navigation hypothesis is now verified through a real
owner-run command/status 23. `internal/webauth` owns its own fresh in-memory cookie
jar, exact login states, discovery, shell-page identity, CSRF-bound signaling and
periodic device authorization. `internal/loginprompt` reads credentials from the
controlling terminal, preserving command stdin, and serializes terminal mode
changes against cancellation. Neither exports a browser session or writes a jar.
The coordinator consumes a sessionSource interface for browser or direct mode;
Pion/terminal/command modules are shared. `--direct-login` and `--browser` are
explicit and mutually exclusive. Login and connection share one process.
Independent native OAuth registration is still a separate unknown, not required
by this demonstrated normal web-session route. See DIRECT_LOGIN_PROBE.md.
