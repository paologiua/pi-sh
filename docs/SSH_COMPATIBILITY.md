# OpenSSH behavioral compatibility matrix

Baseline inventory reviewed 2026-10-19 against the upstream
[ssh](https://man.openbsd.org/ssh.1),
[ssh_config](https://man.openbsd.org/ssh_config.5),
[scp](https://man.openbsd.org/scp.1), and
[sftp](https://man.openbsd.org/sftp.1) manuals. The initial execution differential reference is OpenSSH_10.2p1 / LibreSSL 3.3.6
on macOS; the broader inventory manuals evolve. This inventory
covers the documented feature families and command options; new options must be
added as the reference evolves. Configuration keywords within a family still need
individual parser cases and differential fixtures before that family can pass.

**An experimental `pi-sh --browser [device]` interactive shell works on macOS.**
It is a partial implementation of the suite, not complete OpenSSH compatibility.
Experimental `pi-cp`/`pi-sftp` now use an existing SFTP server over the broker,
with native evidence from the approved transfer plan. The earlier intermittent
1MiB stall was traced to Bash 5.2 `read -t` losing a record newline
(SHELL_BROKER.md); after the fix, read-only 1MiB stdin passed 3/3 and the full
plan passed once on 2026-10-22 through the shared transfer-probe backend: empty,
64KiB, 1MiB roundtrips with modes and SHA-256, preservation, resume, advertised
flush, a small recursive tree, missing-source failure and verified cleanup. One
run is not a stress bound, and the CLIs were not each exercised natively. Earlier owner-reported read-only metadata returned0. A later owner-run read-only pwd/quit batch
also returned 0 after private-creation startup. An intervening HTTPS failure
remains unclassified for the owner; credential-free probes recovered to
password_required/status 0. Those earlier metadata runs do not prove content transfer. Real-device evidence is dated
2026-10-19/21 in [REAL_DEVICE_TESTS.md](REAL_DEVICE_TESTS.md); local component tests
must not be substituted for that evidence.

Status `U` = unimplemented; `—` = no test/verification. Blockers: **P** = verified
protocol/agent capability needed; **L** = local implementation needed; **R** =
remote write-test permission/path needed; **H** = SSH-specific mechanism has no
established Connect equivalent; **S** = service-scope/publication interpretation
unresolved. S is not a blanket technical blocker for the user-authorised private
work. A missing capability remains unknown until evidence rules it out.

## SSH syntax, configuration and lifecycle

| Feature | Intended equivalent behavior | Status | Auto tests | Native device | Differences / blocker |
| --- | --- | --- | --- | --- | --- |
| Destination `[user@]host`, URI, aliases, IPv6 | Familiar parsing and device resolution | Exact names/IDs and HostName aliases | Discovery limits; alias/config tests | Exact name browser/direct on macOS, 2026-10-19/20; aliases pending | No user@/URI/SSH endpoint mapping; Connect name case preserved; L/P |
| `--`, option clusters, attached/separate operands | Preserve argument boundaries and errors | Documented subset before/after destination until command or -- | `TestOptionsBoundariesAndValidation`, prior config comparisons plus 13 post-host references and inspection/command/config boundary tests | Prior `--browser -e none` only | General option inventory, user/address/URI syntax remain incomplete; L |
| Remote command and arguments | OpenSSH-style command composition and remote execution | Experimental shell-only broker | Real PTY and 14 local OpenSSH differential fixtures; legacy WebRTC integration | Binary stdin/stdout 2048 bytes, stderr 256 bytes, no controlling TTY, exits 23/143, 2026-10-20 | Existing Bash >=4.3/cat/setsid/stty required; native 64 KiB stdin/128KiB stderr duplex passed after queue fix; CLI auth notices remain; P/L |
| `-l`, User, HostName | Resolve target identity explicitly | HostName aliases implemented; User/-l U | Config/alias tests | Alias path pending | HostName selects exact device, not Unix user; agent account identity remains fixed/unverified; P/H |
| `-p`, Port | Honour meaningful transport selection or reject precisely | U | — | — | SSH port does not select a Connect shell endpoint; H |
| `-F`, `-F none`, `-o` | Config file selection and per-option overrides | Explicit root/Include regular files; four -o scalars | Twelve valid/six invalid file differentials; 18 Include comparisons; aggregate bounds/redaction; ten CLI comparisons | — | No default root files; most directives U; inactive unknown/invalid settings fail; CONFIGURATION.md; L |
| Host, Match, negation, patterns | Conditional configuration and first-value precedence | Host, */? and negation implemented; Match U | Host/first-value/wildcard tests and config differential | — | Original destination matching; no canonical/final pass or Match exec; L |
| Include, quoting, comments, case, tokens, env expansion | Same parsing/expansion where applicable | Nested file Include, ordered globs, caller Host scopes, ~/home-relative paths; Include ${NAME}, %%/%d/%n/%h; existing quotes/comments/CRLF and HostName %h/%% | 18 offline OpenSSH comparisons; aggregate budget/cycle/symlink/FIFO/redaction tests; frontend included-policy refusal before login | — | No -oInclude, named-user tilde, other Include tokens, POSIX named/collating classes or backslash grammar; home source/limits/strict validation differ; CONFIGURATION.md; L |
| `-P`, Tag, Match tagged | Select tagged configuration | U | — | — | L |
| `-G` | Print effective configuration without connection | Offline Host/file-aware hostname and three fields | CLI/file differentials; 127 ASCII escape-byte comparisons | Not applicable: offline | No Match/full SSH field list; omitted target means no hostname line; command TTY may remain unexecutable; L |
| `-V`, `-Q` | Version and supported-capability queries | Offline version and help/namespaced pi-sh query families | Ten local OpenSSH stream/status/order comparisons; real subprocess no-config/no-auth, redaction, write/short-write and operand tests | Not applicable: offline | SSH algorithm queries explicitly refused; compiled implementation is not runtime/native verification; supported post-host options/inspection now parsed until command/--; LOCAL_INSPECTION.md; L/H |
| `-v`, `-vv`, `-vvv`, `-q`, `-E`, `-y`, LogLevel, SyslogFacility | Diagnostics and logging controls | U | — | — | Secrets and remote terminal data always redacted; L |
| BatchMode, prompt/askpass policy, attempts | Predictable interactive/noninteractive auth | Direct local TTY prompts; BatchMode/askpass U | Synthetic TLS flow; hidden prompt and restoration tests | Direct password/TOTP login and command 23, 2026-10-20 | No unattended auth, persistence or automatic retries; direct web session uses normal callback; L |
| ConnectTimeout, ConnectionAttempts | Bounded connect and explicit retries | U | — | — | Session creation retry semantics unknown; P/L |
| ServerAliveInterval/CountMax, TCPKeepAlive | Detect dead sessions and terminate predictably | U | — | — | WebRTC/service liveness differs from SSH/TCP; P |
| ChannelTimeout, UnusedConnectionTimeout | Bound idle resource lifetime | U | — | — | Channel model unverified; P/L |
| `-4`, `-6`, AddressFamily | Control address-family selection consistently | U | — | — | Must consider HTTP, ICE and relay separately; P/L |
| `-b`, `-B`, BindAddress, BindInterface | Constrain local routing/binding | U | — | — | Browser and Pion support need verification; P/L |
| CanonicalizeHostname family, CanonicalDomains, CanonicalizePermittedCNAMEs | Explicit name canonicalization policy | U | — | — | Device identifiers differ from DNS; H/L |
| Hostname canonical passes, Match final/canonical | Re-evaluate config consistently | U | — | — | Requires chosen discovery semantics; L/P |
| `-f`, ForkAfterAuthentication | Background only after successful authentication/setup | U | — | — | Platform/process lifecycle differences; L |
| `-n`, StdinNull | Do not consume local stdin | Broker EOF; forced TTY stops sending without Ctrl-D; legacy always null | Unreadable-input/config tests; Pion/TTY pending-read/cancel; broker EOF tests | Prior broker EOF; owner -nT command/cancel setup, no new EOF probe | Interactive shell with null stdin rejected; direct auth still prompts on controlling terminal; L |
| `-N`, SessionType none | Maintain transport without shell | U | — | — | Agent may require shell channel; P |
| LocalCommand, PermitLocalCommand, RemoteCommand | Explicit command lifecycle and config | U | — | — | No automatic local execution policy selected; L/P |
| Exit status / remote signal / connection failure | Distinguish normal exits, signals and failure | Broker status/signal classification; bounded TTY result drain after failed input send; client termination cancels with255; interactive native status U | Ordinary exit143 versus TERM/INT/KILL fixtures; four actual OpenSSH client-signal cases; pending-input arbitration and frame-budget regressions; 120 positive/120 no-status concurrent cases; negative/drain and one-byte OpenSSH reference | Prior normal23/143, self-SIGKILL255; owner Ctrl-C/status255; native browser TTY one-byte read with2048 input bytes/status23, 2026-10-21 | No agent exit-status observed; local process signals cancel rather than generic remote forwarding; P/L |
| Command cancellation / abnormal cleanup | Bound shutdown and report cleanup evidence | Broker C/D cleanup handshake, 2s abort / 3s CLI grace | PTY startup/group tests; controller HUP/TERM with protected external sentinel before carrier close; lost/silent ack; auth/cancel coordinator; Pion connect | Owner direct -nT marker/sleep, Ctrl-C -> context canceled/255, no missing-ack warning | Owner-reported path evidence; independent process inspection, revocation, network loss/detached descendants pending; L/P |
| Authentication expiry/revocation, reconnect | Clear failure; no hidden replay of commands | Partial; known cancellation/queued auth failure prevents adapter invocation | Preflight regression with100 repeated race runs; TTY coordinator auth/transport loss; browser heartbeat/direct HTTPS failure tests | Prior browser periodic check and direct command only; long direct session pending | Minute checks; browser heartbeat90s, native request30s; real revocation/reconnect/renewal/TTY teardown pending; P/L |

## Authentication and cryptography

| Feature | Intended equivalent behavior | Status | Auto tests | Native device | Differences / blocker |
| --- | --- | --- | --- | --- | --- |
| Connect account authentication | Secure authorized access with MFA | Direct HTTPS login and optional browser companion | Local TLS password/OTP/callback/CSRF/signaling tests; real PTY hidden prompts and cancellation | Owner-run direct login + WebRTC command marker/status 23, 2026-10-20 | Session memory only, fresh login per process; iSH/Windows/Linux runtime pending; independent OAuth is a separate route; L/P |
| `-i`, `-I`, IdentityFile/Agent, IdentitiesOnly, AddKeysToAgent | Explicit handling of identity options | U | — | — | SSH keys/PKCS#11 do not authenticate Connect without proven mapping; H |
| CertificateFile, pubkey algorithms, certificates, FIDO/SK, security-key provider | Report actual identity support | U | — | — | H |
| Password, keyboard-interactive, PreferredAuthentications, NumberOfPasswordPrompts | Approved interactive flow and noninteractive failure | U | — | — | SSH methods not established Connect methods; H/P |
| HostbasedAuthentication, GSSAPI, `-K`, `-k`, delegation | Explicit capability or unsupported error | U | — | — | H |
| `-A`, `-a`, ForwardAgent | Forward agent only with secure supported remote endpoint | U | — | — | Endpoint creation may violate no-write rule; P/H |
| Known hosts, StrictHostKeyChecking, HostKeyAlias, VerifyHostKeyDNS, UpdateHostKeys | Verify intended peer without disabling crypto | U | — | — | Connect DTLS/signaling trust differs from SSH host keys; H/P |
| `-c`, `-m`, Ciphers, MACs, KexAlgorithms, rekey, host/public-key algorithms | Honest selection or clear incompatibility error | U | — | — | DTLS/SCTP is not SSH crypto negotiation; H |
| `-C`, Compression | Preserve streams; enable compression only if supported | U | — | — | No observed equivalent; P/H |
| Escape warnings, fingerprint/hash display, visual host key | Accurate trust presentation | U | — | — | SSH fingerprint UI must not imply persistent SSH identity; H/P |

## Terminal and execution

| Feature | Intended equivalent behavior | Status | Auto tests | Native device | Differences / blocker |
| --- | --- | --- | --- | --- | --- |
| Interactive shell | Bidirectional native shell through unchanged Connect | Experimental implemented | Local transport/auth/input tests; native probe harness | macOS native shell and two round trips, 2026-10-19 | Browser-assisted signaling; remaining SSH behavior documented separately |
| `-t`, repeated `-tt`, `-T`, RequestTTY | Auto/forced/disabled PTY semantics | Experimental CLI command TTY integrated; -t requires terminal stdin without -n; -tt forces; auto/-T uses no-TTY broker | Actual staged adapter and 18 paired terminal/mode/stop OpenSSH cases; request-policy table; seventeen Pion/PTY coordinator cases; TERM/size, immediate input/first bytes, 128 KiB, exit/signal/EOF/cancel; separate prototype HUP/TERM tests | Native browser -tt with/without -n: TTY fds, TERM=xterm,91x29/0x0,input and status23; PENDIN bit set; 2026-10-21 | Windows/hardware/mode-error parity, nested stopped jobs, native/platform suspension, Windows/generic I/O, native cleanup and other/unclassified native WebRTC-close failures remain open; local recurring early close fixed by available-input batching; legacy requests rejected; TTY_COMMANDS.md and TTY_MODES.md; L/P |
| Raw mode, canonical flags, terminal restoration | Preserve user terminal state on handled exits | Implemented; Unix termination signals handled | `terminal.TestRawBytesResizeAndRestoration`, `TestCancelRestoresBeforeDone`, `TestTerminalQueriesPreserveDescriptorFlags`, `TestTerminationSignalsRestoreTerminal`, `TestPTYControlPasteAndDisconnect`, `TestNativeSuspendRestoresAndReacquiresAcrossCONT`, `TestSuspendTTYSessionOverWebRTC`, cancellation/failure/Close handoff tests | Native local close and SIGTERM termination, 2026-10-20 | Windows console close/logoff, runtime/iSH and Windows suspension pending; Unix local ~^Z has Mac child-process evidence; L |
| Initial rows/columns and SIGWINCH | Propagate size accurately | Existing interactive resize; command initial size via staged stty before launch, later resize channel after TR | PTY zero-size; staged initial dimensions; Pion/TTY initial 91x29 then 111x35 | Probe/interactive sizes and resize, 2026-10-20; command -tt initial91x29, -n0x0 and resize35x111/status23, 2026-10-21 | Bash WINCH report appeared after an empty input line; immediate trap timing unverified; forced pipe/-n initial size0x0; Windows polling build-only; iSH runtime pending |
| TERM, terminal modes, UTF-8, binary/control bytes | Correct terminal data and negotiation | Command TERM/size and portable macOS/Linux modes captured before raw; 34 flag vocabulary, 14 controls, size/parity/rates; interactive text and binary broker remain separate | Seven new mode/OpenSSH comparisons; all 256 control values in actual helper/kernel; source quoting; GNU rprnt/PENDIN fixtures and Linux kernel test; nondefault local-mode Pion/PTY case; validation/capture/restoration | Prior Unicode/all-byte broker plus browser command TERM, captured Mac modes, PENDIN and input/status23, 2026-10-21 | Windows mode translation U; BSD-only non-Linux controls omitted; unusual hardware/Linux speed/native parity open; stty failures stricter; invalid UTF-8 TTY input rejected; TTY_MODES.md; P/L |
| Ctrl-C, Ctrl-D, Ctrl-Z, signal delivery | Correct interrupt, EOF and suspend semantics | Interactive control bytes; local termination cancels; binary command stdin stays data | Input/PTY tests; all-byte command stdin; local OpenSSH client signals; abort tests; four same-interpreter stop/TSTP parity cases; two-carrier controller and whole-group STOP/CONT tests; two nonempty canonical EOF OpenSSH comparisons with both carriers and CLI/Pion/PTY cases; three nested Ctrl-Z/fg/TTIN/TTOU comparisons on both carriers | Prior Ctrl-C recovery/Ctrl-D logout and owner Ctrl-C/status255; command TTY handled INT/TSTP and empty canonical EOF each exit23, 2026-10-21 | Caught/default TSTP fixed locally; arbitrary nested job control/native group teardown, native local suspension and explicit remote signal requests open; TTY_JOB_CONTROL.md; local process signals are not generically forwarded; P/L |
| `-e`, EscapeChar, `~.`, `~~`, `~?`, `~^Z`, `~#`, `~&`, `~C`, `~B`, `~R`, `~v`, `~V` | Line-aware escape semantics | Disconnect/doubled escapes/local help; Unix local suspension with fresh mode/flag baseline and forced resize; -e and file/-o EscapeChar ASCII/control notation/none | Seven local OpenSSH help observations; one-byte consumption/write-failure cases; Pion/PTY interactive/TTY help; real child-process suspension through TTY and interactive Pion/PTY coordinators, TTY status23/interactive disconnect and changed shell baseline; cancellation/Close/drain tests; 127-byte ASCII dump comparisons | Typed Enter/~. and -e none only, 2026-10-20 | Windows suspension consumed/diagnosed; Pi/Linux/iSH suspension unverified; other operations unimplemented and forwarded as unknown input; non-ASCII escapes/backslash config syntax unsupported; ESCAPES.md; L/P/H |
| Bracketed paste and multiline editing | Preserve terminal paste framing and text | Text wrappers forwarded unchanged | One-byte short-read and real-PTY input tests | Unicode/multiline paste and wrapped ~. forwarding, 2026-10-20 | Embedded newline/~. still triggers enabled local escape, as in upstream line filter; use -e none; actual clipboard variants unverified |
| stdin/stdout/stderr separation, half-close | Binary-correct streams with independent EOF | Experimental broker implemented | All bytes, EOF, early stdin close, duplex, blocked stdin, slow consumer; local OpenSSH comparison | Exact 2048-byte stdin/stdout and 256-byte stderr, 2026-10-20 | Native 64 KiB/128KiB duplex passed after transport correction; native 1MiB stdin stalled intermittently: Bash 5.2 `read -t` newline loss, fixed with 5.2.37 red/green regression; native read-only1MiB rerun passed 3/3, 2026-10-22; CLI setup stderr differs; L/P |
| Local stream cancellation/reuse | Stop borrowed I/O and restore local state without consuming later input | macOS/Linux file-backed interactive/TTY/binary/legacy paths; optional StopIO join contract | Real Pion/PTY idle/backpressure/reuse; all-byte synthetic binary peer; descriptor aliases/partial writes; queued-escape and uncertain-commit regressions | Native command TTY input/output/status23; stress/cancel/reuse with Pi remains unverified | Windows synchronous blocked calls, arbitrary callbacks and regular-filesystem kernel latency remain; LOCAL_IO.md; L |
| SendEnv, SetEnv, AcceptEnv-equivalent | Apply only supported environment values | U | — | — | No remote config changes allowed; P |
| SessionType subsystem, `-s` | Request existing subsystems | U | — | — | No verified subsystem mechanism; P/H |
| Multiple concurrent sessions | Isolated lifecycle and cancellation | U | — | — | Agent/service quotas and concurrency unknown; P |

## Forwarding, multiplexing and routing

| Feature | Intended equivalent behavior | Status | Auto tests | Native device | Differences / blocker |
| --- | --- | --- | --- | --- | --- |
| `-L`, LocalForward TCP | Local listener to existing remote target | U | Bash bridge EOF limitation; Go/native-local-nc half-close controls | — | Correct remote connector and concurrent carrier limits unverified; see FORWARDING.md; P/L |
| `-R`, RemoteForward TCP, allocated ports | Remote listener with deterministic status | U | — | — | Remote endpoint facility unverified; P |
| `-D`, dynamic forwarding, SOCKS4/5 | Concurrent SOCKS streams | U | — | — | Depends on remote socket support; P |
| Unix-domain forwarding and stream-local options | Correct socket permissions and cleanup | U | — | — | Remote filesystem/socket writes restricted; P/R |
| GatewayPorts, `-g`, bind addresses, IPv6, ExitOnForwardFailure | Correct binding and setup failure | U | — | — | Remote semantics unknown; P/L |
| `-W` stdio forwarding | Connect stdio to remote socket | U | All-byte local Bash bridge demonstrates missing FIN; Go CloseWrite and existing Mac nc EOF/response controls pass | — | Bash fd close cannot half-close with reader alive; remote native/utility facility and nc dialect unknown; FORWARDING.md; P/L |
| `-J`, ProxyJump, ProxyCommand, ProxyUseFdpass | Explicit routing/capability behavior | U | — | — | SSH jump transport would violate Connect-only architecture; H |
| `-X`, `-x`, `-Y`, X11 options, xauth | Secure X11 forwarding if possible without modifications | U | — | — | Remote authorization/socket setup may require prohibited writes; P/H |
| `-w`, Tunnel, TunnelDevice | Tunnel interfaces only if existing facilities permit | U | — | — | Privileged device/config changes prohibited; H |
| `-M`, `-S`, ControlMaster/Path/Persist | Local connection sharing and clean lifetime | U | — | — | Remote multiple-channel capability unverified; P/L |
| `-O` check/conninfo/channels/forward/cancel/proxy/exit/stop | Accurate control operations | U | — | — | Depends on implemented multiplexing/forwarding; P/L |
| ClearAllForwardings, PermitRemoteOpen, Escape forwarding control | Enforce selected forwarding policy | U | — | — | P/L |
| IPQoS, RekeyLimit, ObscureKeystrokeTiming, traffic padding | Honest transport/privacy option semantics | U | — | — | Requires WebRTC-specific evaluation; H/P/L |
| `-Z` command-line protection | Explicit platform behavior | U | — | — | Platform-specific OpenSSH option; L |

## SCP

| Feature | Intended equivalent behavior | Status | Auto tests | Native device | Differences / blocker |
| --- | --- | --- | --- | --- | --- |
| Local/remote operands, `user@host:path`, scp URI | Familiar paths, quoting, disambiguation | Literal device:path/alias:path, local colon/drive disambiguation | Copy operand and config tests | — | user@/URI/address syntax U; one exact authorized device; L/P |
| Upload/download, multiple sources | Correct binary bytes, destination semantics | Regular files, both directions; existing-directory preflight for multiple sources | In-memory multiple binary copy roundtrip/preflight; actual broker/server binary test | Native transfer plan 2026-10-22 (shared backend via transfer-probe): empty/64KiB/1MiB roundtrip, modes and integrity passed; earlier 1MiB stall fixed | Earlier native stall: Bash 5.2 `read -t` newline loss, fixed (SHELL_BROKER.md); single native plan pass, no larger-file stress; multiple-source/glob/tilde coverage incomplete; REAL_DEVICE_TESTS.md; L/P/R |
| Modern default SFTP backend | Existing server with verified binary carrier; native subsystem only if observed | Implemented through an existing server process over broker | SFTP memory, actual OpenSSH server and actual Bash/PTY/broker integration | Native transfer plan 2026-10-22 (shared backend via transfer-probe): all phases passed, cleanup verified, broker code0 | Earlier 1MiB stall fixed (Bash 5.2 timed read); single pass; no native Connect subsystem asserted; P |
| `-O` legacy SCP | Legacy protocol semantics only over safe streams | U | — | — | Broker pipe streams exist; remote executable presence and full transfer behavior unverified; P/L/R |
| `-p` | Preserve ordinary mode and atime/mtime | Regular files and recursive directories with documented differences | Eight regular-file paired comparisons, recursive cases below, shared SFTP metadata failures | Native transfer plan 2026-10-22 (shared backend): preservation passed | No ownership/ACL/special-bit preservation; regular uploads use combined handle FSETSTAT; directory/local timestamp paths retain races; SFTP_METADATA.md; recursive timestamp stress discrepancy pending in SFTP_LISTING.md; L/R |
| `-r` | Recursion, symlinks and directory metadata | Literal SCP trees, source links followed, directory modes/times; partial | 48 paired local OpenSSH copies; cycle/dangling/conflict/cancel/path/private-mask tests | Native transfer plan 2026-10-22 (shared backend): small recursive tree passed | First-error stop, path races, special bits/native recursion pending; incremental reader in SFTP_LISTING.md; SCP_RECURSION.md; L/P/R |
| Glob/tilde, Unicode/newlines, spaces/colons, leading dash | Correct filenames without command injection | Literal packet paths; quotes/spaces/Unicode/colon tested; no shell evaluation | Copy/console quoting and actual-server path tests | — | Glob/tilde/newline and platform filename inventory incomplete; unsafe automatic local basenames rejected; L/P/R |
| `-3`, `-R`, remote-to-remote | Route and authenticate each endpoint correctly | U | — | — | Connect capabilities/quota unresolved; P/R |
| `-A`, `-B`, `-C`, `-4`, `-6`, `-c`, `-i`, `-J`, `-P`, `-F`, `-o` | Consistent underlying session options | Explicit -F and four -o settings; others U/rejected | Shared config/alias/pipe-policy tests | — | Fresh explicit direct/browser login; no unattended batch auth, ports or SSH keys; H/P/L |
| `-D`, `-S` program overrides | Honest local executable override semantics | U | — | — | Cannot replace transport with SSH fallback; H/L |
| `-l`, `-q`, `-v`, progress, interrupted/partial files | Bandwidth/progress and accurate failure status | Flags/progress U; partial files retained | Copy/preflight failures | — | Disk-full/native interruption fixtures pending; P/R/L |
| `-T` filename checking, `-X` SFTP buffer/request options | Accurate security and buffering behavior | Flags U; literal requested paths and bounded packets | Operand/backend packet fixtures | — | No option parity; native backend verification pending; P/L |
| Exit status, permissions/errors, disk-full and cleanup | Failure cannot become success; report partial writes | File/batch error1, setup/cancel255; verified backend status | Failure/preflight, malformed handshake, cancellation and EOF tests | Earlier 1MiB deadline exit1 then separate cleanup; Native transfer plan 2026-10-22 (shared backend via transfer-probe): missing-source failure and same-session cleanup with verified absence passed | Partial/truncated files retained; disk-full and full parity/native error fixtures pending; P/R/L |

## SFTP

| Feature | Intended equivalent behavior | Status | Auto tests | Native device | Differences / blocker |
| --- | --- | --- | --- | --- | --- |
| Connection/subsystem negotiation | Existing compatible server, correct version/capabilities | v3 negotiation through binary broker; optional server -R | Memory/actual OpenSSH/broker tests, bounded malformed handshake/mask prefix | Owner direct read-only batch exit0, 2026-10-20, including mask-startup pwd/quit rerun | Default search works; exact executable path not reported; no native Connect subsystem invented; P |
| Interactive prompt, help, quit/exit/bye | Familiar commands and errors | Basic console and help/quit/exit/bye | Console and batch fixtures | — | pi-sftp prompt; many commands/flags remain U; L/P |
| `get`, `put`, `mget`, `mput`, recursive transfers | Binary streaming and correct path expansion | Single literal regular-file get/put, resume and ordinary metadata; mget/mput/recursion U | Binary memory/copy roundtrip; actual server/symlink traversal; resume comparisons below | Native transfer plan 2026-10-22 (shared backend via transfer-probe): roundtrips, preservation, resume and small recursive tree passed | No glob/tilde; single native pass; earlier failure fixed (SHELL_BROKER.md); R/P |
| `reget`, `reput`, global/command `-a` | Resume with reference offset and destination rules | Single regular-file resume implemented | 48 memory cases, 48 paired local OpenSSH/actual-server comparisons, empty-source and failed WRITE/READ tests; attached -afb- frontend | Native transfer plan 2026-10-22 (shared backend): resume passed | Prefix trusted without checksum, upload requires existing shorter target; no atomic/concurrent-write guarantee; SFTP_RESUME.md; P/R |
| Global/command `-p` | Preserve ordinary mode and initial atime/mtime | Regular-file preservation implemented with differences | 48 paired local OpenSSH/actual-server comparisons; advisory status failure, failed READ/WRITE, atime/redaction tests | Native transfer plan 2026-10-22 (shared backend): preservation passed | No ACL/ownership/special bits; combined upload handle metadata verified locally; directory/local timestamp path races and missing attribute flags remain gaps; SFTP_METADATA.md; L/R |
| `-R`, `-r` | Request limits and recursion | U | — | Native transfer plan 2026-10-22 (shared backend): small recursive tree passed | One small tree only; configurable limits unverified; P/R/L |
| Global/command `-f` flush | File sync request and reference error/status behavior | Local File.Sync; advertised remote fsync v1; advisory sync statuses, fatal copy/protocol/close errors | 14 local OpenSSH comparisons, actual server WRITE/fsync/CLOSE ordering, absent extension, write/sync/close faults | Native transfer plan 2026-10-22 (shared backend): advertised-flush passed | No guarantee from exit0 alone, no directory/crash guarantee; SFTP_FLUSH.md; P/R |
| `ls`, `pwd`, `cd`, realpath, glob, `lumask` | Correct local/remote path and listing behavior | ls/pwd/cd/realpath; incremental ls -f, other ls flags/glob/lumask U | Console/actual-server traversal, 10k-entry lazy list/error/close, raw-wire failures and OpenSSH -f comparison | Owner pwd/ls/quit exit0, 2026-10-20; later mask-startup pwd/quit exit0 | ls prints basenames one/line; default sorted ls still accumulates; -f streams; other flags/native reader pending; SFTP_LISTING.md; P/L |
| `mkdir`, `rmdir`, `rm`, `rename`, `ln`, `symlink` | Accurate mutation and error semantics | mkdir/rmdir/rm/rename/symlink/readlink; hard ln U | Memory command lifecycle; actual server rename; read-only create refusal | Probe private root creation and fixed-entry cleanup verified | Exact probe root authorized and now absent; full command/rename/link native coverage pending; R/P |
| `chmod`, `chown`, `chgrp`, stat/lstat/fstat | Preserve metadata and permission failures | Basic octal chmod, internal stat/fstat; others U | Command and backend fixtures | — | Only 000–777 mode, no uid/gid or special mode handling; P/R/L |
| `df`, `version`, extension support | Report only negotiated capabilities | U | — | — | No extension negotiation verified; P |
| `lcd`, `lls`, `lmkdir`, `lpwd`, `!`, local shell escape | Explicit local behavior without hidden execution | lcd/lpwd implemented; others U/rejected | Console/local working directory fixtures | — | No hidden local execution; L |
| `-b`, batch fail/ignore/echo prefixes, stdin batches | Predictable errors and process status | Batch file/stdin, stop/ignore '-' and quiet '@' | Batch suffix/failure/echo tests | — | Fresh auth still prompts on controlling tty; no BatchMode/askpass; L/P |
| `-B`, `-l`, `-N`, `-q`, `-v`, progress toggle | Buffering, rate, diagnostics, progress control | U | — | — | L/P |
| `-4`, `-6`, `-A`, `-C`, `-c`, `-D`, `-F`, `-i`, `-J`, `-o`, `-P`, `-S`, `-s`, `-X` | Parse all relevant connection/backend options | -F/-o subset and -s existing path; attached/separate arguments and registered short-switch clusters; others rejected | Shared cliargs boundaries, actual OpenSSH batch syntax, frontend pre-auth config/policy tests | — | Options before destination; no SSH program/key/port fallback; H/P/L |
| Binary names/data, symlinks, sparse/large files, partial failure | Correct data and filesystem semantics | Binary stdio/SFTP and selected literal names; bounded copy bypasses ReaderFrom final-short-WRITE error loss | All-byte roundtrip, symlink traversal, refused/final-short-write regressions at 1024/32768 packet sizes, preflight/handshake failures; Linux/386 build | — | No sparse/>32-bit-file/native filename/disk-full coverage; partial files remain; P/R/L |

## Verification discipline

Before marking any row implemented, name concrete tests and independently record
native-device verification (agent/client versions, date, safe evidence reference).
Before marking it unsupported, identify the observed blocker and evaluate allowed
alternatives. Never silently ignore unknown options, imply an SSH-key login
works with Connect, or turn a forced PTY into claimed no-PTY support. Update both
differences and blockers whenever a capability is confirmed or ruled out.
