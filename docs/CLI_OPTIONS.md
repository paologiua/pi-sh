# CLI option subset — 2026-10-21

Experimental command TTY yes/force now uses the staged Bash adapter through the
CLI terminal/resize coordinator. [TTY_COMMANDS.md](TTY_COMMANDS.md) records the
exact policy, local evidence and remaining mode/lifecycle/native gaps.

The CLI now uses an explicit bounded parser instead of Go's flag parser.
This is a partial behavioral implementation, not an SSH transport. The reference
is [ssh(1)](https://man.openbsd.org/ssh.1) and
[ssh_config(5)](https://man.openbsd.org/ssh_config.5), checked on 2026-10-20;
ten local differential invocations use the installed OpenSSH_10.2p1.

```sh
pi-sh --direct-login --experimental-command -nT device 'cat; exit 23'
pi-sh --browser -enone device
pi-sh -G -Fnone -o 'EscapeChar = "none"' -oStdinNull=yes device
```

The first command supplies immediate stdin EOF without reading local stdin.
Direct login still prompts on the controlling terminal; `-n` is not unattended
authentication. The broker gives the command pipe descriptors and no controlling
terminal, while Connect's shell carrier still has a PTY.

| Option | Behavior and boundary |
| --- | --- |
| `-e value`, `-evalue` | Existing ASCII/control-notation/none escape selection |
| `-n` | No local stdin reads; no-TTY broker sends EOF; forced TTY stops input without inventing Ctrl-D; auth may still prompt |
| `-T` | Explicit no-TTY command through the broker; interactive no-TTY shell and legacy adapter reject it |
| `-t`, `-tt` | Experimental broker command TTY: -t requires terminal stdin without -n; -tt forces it, including pipes/-n; legacy TTY requests rejected |
| `-o` | HostName, EscapeChar, StdinNull, RequestTTY, LogLevel, SyslogFacility, User (account email), RemoteCommand (raw text) and the inert/refused keywords below; keywords case-insensitive, values case-sensitive (LogLevel/SyslogFacility values ASCII case-insensitive as in OpenSSH). Connection policy (BatchMode, ConnectTimeout, ConnectionAttempts, ServerAlive*, TCPKeepAlive, ChannelTimeout, NumberOfPasswordPrompts; User/Port always refused) per [CONNECTION_POLICY.md](CONNECTION_POLICY.md) |
| `-l email` | Raspberry Pi ID account, first obtained in command-line order like ssh.c; a non-email user is refused ([CONNECTION_POLICY.md](CONNECTION_POLICY.md)) |
| `-p port` | Parsed (first obtained) and always refused before login: there is no SSH port (pi-sh `-P` is Tag; pi-cp/pi-sftp `-P` is the refused port) |
| `-4 -6 -B -b -C -c -f -I -i -K -k -m -N -s` | Refused with a specific reason; see the decision table below |
| `-F path`, `-Fpath`, `-F none` | Read explicit root/Include files, or disable file loading; no default root config files |
| `-V` | Offline unofficial build version on stderr; immediate exit before file/auth/network setup |
| `-Q query`, `-Qquery` | Offline help/namespaced client capabilities on stdout; SSH algorithm queries explicitly refused; LOCAL_INSPECTION.md |
| `-G` | Print hostname (with a destination), escapechar, stdinnull, requesttty, loglevel, syslogfacility, tag when set, and the connection-policy fields; exit before login/discovery/network |
| `-v`, `-vv`, `-vvv` | Cumulative debug levels DEBUG1–DEBUG3 with ssh.c ordering against `-q`; override LogLevel |
| `-q` | QUIET: suppresses informational/warning notices and error text (exit status unchanged); never the browser companion instruction or login prompts |
| `-E file`, `-Efile` | Append diagnostics to file (created 0600); unopenable file exits 1; not opened for -V/-Q |
| `-y` | Diagnostics to the local syslog with SyslogFacility; explicit failure if unavailable/unsupported; refused with -E |
| Clusters and `--` | Supported for the listed short options; operand-taking options consume the rest of the cluster or the next argument |

`-o` accepts a single scalar with `Keyword=value`, `Keyword value`, whitespace
around `=`, and optional surrounding double quotes. Duplicate directives use the
first obtained value. Short flags assign directly and take precedence over later
directives for the same field, matching the tested OpenSSH cases. Unsupported
directives and invalid duplicate values still fail. Double quotes, comments at
token boundaries and Host blocks are supported; see
[configuration files](CONFIGURATION.md) for the complete current grammar and
boundaries. Include has bounded file/path/token support; Match and backslash
escaping remain unimplemented. Include does not become an accepted -o scalar.

Supported options can precede or follow the first destination, until the first
non-option command word. After that word all later tokens remain command operands.
An explicit `--` ends option parsing globally, whether before or after the host;
it is removed as a separator. Thus `device -T printf -n` selects no TTY while
retaining `-n` as a printf argument; `device -- -T` passes `-T` as command text.
A later `--` after a command word remains part of the command. Unknown options
before command start are rejected before authentication rather than sent remotely.
Custom --browser/--direct-login/adapter flags follow the same boundary. -V/-Q
still exit immediately, including after the host, before file loading or auth.

The broker/legacy command remains an explicit opt-in; command arguments are joined
with single spaces as previously. Destinations follow the OpenSSH 10.2p1 `user@host`
and `ssh://` grammar; any user, port or address literal is refused before login
([CONNECTION_POLICY.md](CONNECTION_POLICY.md)). The general
option inventory remain gaps. Aliases use Host/HostName in an explicit file or
`-o HostName=value`. No default SSH or pi-sh config is read.

`-G` is a subset report, not OpenSSH's complete output. It can display a requested
TTY policy independently of whether a non-forced request is suppressed by stdin.
Legacy incompatible policies fail before authentication. It accepts an omitted
destination when no file or HostName override is selected.
It deliberately reports no SSH key, algorithm or host-key properties.

## Evidence

- `TestOptionsOpenSSHDifferential`: ten offline `ssh -G -F none` comparisons of
  reported fields, compact/separate operands, quoting, repeated directives,
  mixed short/directive precedence and TTY ordering. No SSH server or Pi used.
- `TestOptionsBoundariesAndValidation`: exact command argument boundaries,
  option terminator and refusal of unsupported options/modes or malformed values.
- `TestStdinNullNeverReadsInput`: a reader that fails the test if touched verifies
  null stdin; ordinary stdin preserves binary bytes. Existing broker tests
  independently cover remote EOF and binary streams.
- File-based Host/HostName behavior, bounds and error cases are separately
  tested as described in [CONFIGURATION.md](CONFIGURATION.md).

New option combinations have local evidence only. No new Pi session, file write,
transfer or forwarding test was performed in this increment. Existing native
broker evidence does not establish native `-n`/`-T` CLI verification.
An attempt to inspect the authorized browser with Chrome DevTools `list_pages`
timed out after 60 seconds before reaching a browser page. No CLI/Pi connection
was attempted. Native confirmation requires restored DevTools connectivity or an
owner-run direct-login command; this is a test-access blocker, not an observed
Connect protocol limitation.

## Option and directive decisions (2026-10-22/23)

Every known OpenSSH 10.2 option and ssh_config keyword now has a decision.
Accepted values change nothing that pi-sh does not already do; refusals exit
before authentication with a specific reason (pi-sh 255, pi-cp/pi-sftp 1 like
scp/sftp usage errors). Code: `internal/sshconfig/keywords.go`,
`cmd/pi-sh/options.go` (`refusedFlag`), `internal/cliargs/refuse.go`.

| OpenSSH feature | Decision |
| --- | --- |
| `-l email`, `User`, `email::device` | Implemented: Raspberry Pi ID account ([CONNECTION_POLICY.md](CONNECTION_POLICY.md)) |
| `RemoteCommand` | Implemented: raw rest-of-line text (quotes, `#`, backslashes kept), first obtained, `none` = no command; `%%`/`%h`/`%n`/`%r`/`%u`/`%d`/`%i`/`%l`/`%L` expanded (also in `-G`, as ssh does), `%p`/`%C`/`%k`/`%j`/unknown tokens refused, `%h`/`%n`/`%r` with shell metacharacters refused. With a command-line command: `Cannot execute command-line and remote command.` (also `-G`). Running it needs `--experimental-command` like any command. pi-cp/pi-sftp refuse an effective RemoteCommand (it conflicts with the SFTP session, as in ssh). |
| `RefuseConnection` | Implemented as in OpenSSH 10.2: an obtained value (any single-word message) terminates, also for `-G`; the message is not echoed |
| `SessionType`, `-N`, `-s` | `default` accepted; `none`/`subsystem`, `-N`, `-s` refused: Connect's shell channel always starts a shell and has no subsystem request (pi-sftp covers SFTP) |
| `-f`, `ForkAfterAuthentication` | `no` accepted; `-f`/`yes` refused: the in-memory session cannot be handed to a forked process (Go cannot fork); use shell job control |
| `SetEnv`, `SendEnv` | Refused: Connect has no environment request and there is no AcceptEnv policy point; injecting `export` text into the remote shell was not adopted. Candidate: prefix broker commands with validated assignments (owner decision) |
| `LocalCommand`, `PermitLocalCommand` | `PermitLocalCommand no` accepted (OpenSSH then never runs LocalCommand); `LocalCommand` and `PermitLocalCommand yes` refused: no implicit local execution apart from Match exec |
| `-4`, `-6`, `AddressFamily` | `any` accepted; `inet`/`inet6`, `-4`, `-6` refused: not implemented for HTTPS and Pion ICE (possible future work: tcp4/tcp6 dialer plus ICE network types) |
| `-b`, `-B`, `BindAddress`, `BindInterface` | Refused: not implemented for HTTPS and ICE |
| `CanonicalizeHostname` family | `CanonicalizeHostname no` accepted; `CanonicalDomains`, `CanonicalizeMaxDots`, `CanonicalizeFallbackLocal` syntax-checked and inert as in OpenSSH while canonicalization is off; `yes`/`always` and `CanonicalizePermittedCNAMEs` refused (ssh -G resolves DNS when permitted CNAMEs are set, observed locally) |
| `-i`, `-I`, `-K`, `-k`, IdentityFile, CertificateFile, IdentitiesOnly, IdentityAgent, AddKeysToAgent, PKCS11Provider, SecurityKeyProvider, UseKeychain, PubkeyAuthentication, PasswordAuthentication, KbdInteractive*, PreferredAuthentications, ChallengeResponseAuthentication | Refused: SSH authentication does not apply to Connect sign-in. `GSSAPIAuthentication no`, `GSSAPIDelegateCredentials no`, `HostbasedAuthentication no`, `EnableSSHKeysign no` accepted (methods never used) |
| Known hosts / host keys (StrictHostKeyChecking, UserKnownHostsFile, GlobalKnownHostsFile, HostKeyAlias, HashKnownHosts, VerifyHostKeyDNS, UpdateHostKeys, KnownHostsCommand, RevokedHostKeys, NoHostAuthenticationFor*, FingerprintHash) | Refused: no SSH host key exists; Connect HTTPS and WebRTC DTLS authenticate the path. `CheckHostIP no`, `VisualHostKey no` accepted |
| `-c`, `-m`, Ciphers, MACs, KexAlgorithms, HostKeyAlgorithms, Pubkey/HostbasedAcceptedAlgorithms, CASignatureAlgorithms, RequiredRSASize, WarnWeakCrypto | Refused: TLS/DTLS negotiation is done by Go/Pion |
| `-C`, `Compression` | `no` accepted; `-C`/`yes` refused: no compression in the data channel |
| IPQoS, RekeyLimit, ObscureKeystrokeTiming | `ObscureKeystrokeTiming no` accepted; others refused |
| VersionAddendum, LogVerbose, EnableEscapeCommandline | Refused (no SSH banner; no per-function debug filters; no `~C`) |
| IgnoreUnknown | Refused: explicit `-F` files are validated completely so nothing is silently skipped (stricter than OpenSSH) |
| Obsolete keywords (Protocol, Cipher, RSAAuthentication, CompressionLevel, UseRoaming, ...) | Refused (OpenSSH warns and ignores them) |
| Forwarding/multiplexing/proxy/X11/agent/tunnel keywords and `-A -a -D -g -J -L -M -O -R -S -W -w -X -x -Y` | Owned by the forwarding work; keywords refused with a generic "not implemented (docs/FORWARDING.md)" reason, flags with the generic unsupported-option error |
| Unknown keywords | `unsupported SSH configuration directive` (OpenSSH: `Bad configuration option`) |

scp: `-B` maps to `BatchMode yes` (placed before `-o` values) and therefore
fails with 255 before any prompt; `-s` (SFTP protocol) and `-T` (no effect in
scp's SFTP mode) are accepted; `-3`/`-R` remote-to-remote, `-O` legacy
protocol, `-4`/`-6`, `-C`, `-c`, `-i`, `-l` bandwidth, `-D`/`-S` program
overrides and `-X` tuning are refused with reasons; `-A`/`-J` are forwarding.
sftp: `-4`/`-6`, `-C`, `-N`, `-r` (recursive console transfers), `-B`/`-R`/`-X`
tuning, `-c`, `-i`, `-l`, `-D`/`-S` are refused with reasons; `-A`/`-J` are
forwarding. Go's flag package prints the reason (`invalid boolean flag C:
not supported: ...`).

Evidence: `TestCompletenessOpenSSHDifferential` (seven `ssh -G` comparisons of
accepted inert keywords, raw/expanded RemoteCommand and inactive
RefuseConnection, plus four shared rejections), `TestCompletenessRefusals`
(41 flag/keyword refusals), `TestRemoteCommandExecution`,
`internal/sshconfig` `TestKeywordDecisions`/`TestExpandCommand`,
`TestSCPOptionDecisions` and `TestSFTPOptionDecisions`. All local; none needs a
device, and RemoteCommand execution on the Pi is unverified.

## Diagnostics

`pi-cp` and `pi-sftp` accept `-v` (repeatable) and `-q`, mirroring scp/sftp passing
them to ssh: they steer connection-layer notices and diagnostics only; file and
transfer errors are always printed. They reject `-E`/`-y`, as scp/sftp do.
Levels, precedence, message tables, redaction and differences are in
[DIAGNOSTICS.md](DIAGNOSTICS.md).

## Offline inspection

Version and query options terminate processing immediately, following the locally
measured OpenSSH10.2p1 ordering. An earlier malformed option still fails; a valid
-F selection is not opened for -V/-Q, and subsequent tokens are unprocessed.
Queries list compiled implementation rather than per-device support. They use
no authentication mode and cannot execute local/remote commands. See
[LOCAL_INSPECTION.md](LOCAL_INSPECTION.md) for names, redaction, tests and gaps.

Thirteen additional offline OpenSSH_10.2p1 comparisons cover post-destination
TTY/stdin/escape options, cross-boundary precedence/clusters, explicit separators
and command operands. Separate subprocess checks cover post-host immediate -V/-Q,
missing config bypass, custom modes, unsupported/missing operands and explicit
-F aliases after the host. No network/authentication is used by these comparisons.
