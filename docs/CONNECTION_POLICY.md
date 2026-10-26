# Destination syntax and connection policy — 2026-10-22

Reference: OpenSSH_10.2p1 (LibreSSL 3.3.6) installed on the development Mac and
the [ssh](https://man.openbsd.org/ssh.1), [scp](https://man.openbsd.org/scp.1),
[sftp](https://man.openbsd.org/sftp.1) and
[ssh_config](https://man.openbsd.org/ssh_config.5) manuals. All reference
observations were offline: `ssh -G -F none`, and `scp`/`sftp -F none -S prog`
where `prog` is a local test program that prints its arguments and exits 255.
No SSH server, Raspberry Pi or Connect service was contacted. Code:
`internal/sshdest` (syntax only) and `internal/connpolicy` (policy), wired
through `internal/connectsession` and `internal/transport`.

**Native Pi verification of everything in this document is pending.**

## Destination syntax

| Tool | Accepted forms (as OpenSSH 10.2p1) | Notes |
| --- | --- | --- |
| pi-sh | `host`, `email::host` (pi-sh account form), `user@host` (split at the last `@`; refused), `ssh://[user[;params]@]host[:port][/]` | Scheme is case-sensitive; URI user/path percent-decoded (`+` is a space); URI host must be domain-style (alphanumeric start; letters, digits, `-`, `_`, `.`; no `..`), optionally bracketed; port 1–65535 or a service name; a URI path is an error. A plain host keeps brackets literally. |
| pi-cp | `email::host:path`, `[user@]host:path`, `[user@][addr]:path`, `scp://[user@]host[:port][/path]` | `colon()` rules: a leading `:` or a `/` before the first colon means local; an empty user before `@` is ignored; empty path means `.`; `scp://h/x` is `x`, `scp://h//abs` is `/abs`. Windows `C:\`/`C:/` stays local (project rule). |
| pi-sftp | `email::host[:path]`, `[user@]host[:path]`, `[user@][addr][:path]`, `sftp://[user@]host[:port][/path]` | Without a colon the whole argument is the host (brackets stripped). |

Plain hosts keep their exact case and may contain spaces, because Connect
device names are free text; OpenSSH lowercases and rejects such hostnames.
Configuration `Host` patterns match the parsed host (after removing `user@`
and URI syntax), so `ssh://pi` selects `Host pi`.

Differences: a Connect device whose name contains `@` (or, for scp/sftp, `:`)
can no longer be given literally; use its ID or a `HostName` alias. An empty
host (`a@`, `@:x`) is an immediate error rather than a later connection failure.
`pi-sftp host:path` (initial directory or file) is refused: not implemented.

## Account selection (`email::device`, `-l`, `User`, URI user)

The Raspberry Pi ID account (an email address) takes the role of the SSH
remote user (owner decision, 2026-10-22, below). Code: `internal/account`
(syntax and default), `internal/sshdest` (`Account` destinations),
`internal/connpolicy` (`Policy.Account`, `DestinationUser`, `Admit`).

| Source | Form | Notes |
| --- | --- | --- |
| Destination | `email::device` (pi-sh), `email::device:path` (pi-cp), `email::device[:path]` (pi-sftp) | Recognized when the text before the first `::` contains `@`; the device part follows the usual host/colon rules and must not contain `@`. `email::device` without `:path` is an error for pi-cp. A slash before the first colon keeps a pi-cp operand local (`./a@b::c:d`). |
| URI | `ssh://`, `scp://`, `sftp://` userinfo, `%40`-encoded (`ssh://me%40example.com@pi`) | Decision: accepted when the decoded user is an email address; any other URI user is refused. URI syntax requires the `@` to be encoded. |
| `-l email` | pi-sh only | scp/sftp `-l` is a bandwidth limit (refused there); use `email::` or `-oUser=`. |
| `User email` | `-o` or `-F` file | A non-email active `User` (for example `User pi`) is refused; inactive blocks are syntax-checked only. |
| Local default | `PI_SH_ACCOUNT`, else `$XDG_CONFIG_HOME/pi-sh/account` (absolute XDG only), else `~/.config/pi-sh/account` | Lowest precedence. Read only, never created or written. The file holds one email line; blank and `#` lines are allowed. An invalid value, extra account line or non-regular file is an error, never ignored. `PI_SH_ACCOUNT=` (empty) disables the default including the file. |

Precedence is OpenSSH's first-obtained rule, measured with `ssh -G -F none`:
command-line sources in order of appearance (`-l a pi`, `pi -l a`,
`-oUser=c -l a`, URI user before/after `-l`), then the configuration file, then
the local default (OpenSSH's default is the local login name). The previous
pi-sh code let `-l` override an earlier destination user and let a later `-l`
override an earlier one; both now follow ssh.c.

Validation (`account.Validate`): one `@`; a nonempty ASCII local part without
spaces or `"(),:;<>[\]`, no leading/trailing/double dot; a dotted ASCII domain of
letters, digits and inner hyphens; at most 254 bytes. Raspberry Pi ID decides
whether the account exists. Internationalized addresses are not accepted.

Refusals (before any prompt, listener or request): a Unix-style `user@host`
destination is always refused, even if `-l email` was given earlier, with a
message naming `email::device` and `-l email`; a non-email `-l`/`User`/URI
user is refused the same way (`connpolicy.ErrUser`). The Connect agent still
opens the device's single fixed account, which pi-sh neither selects nor
verifies; no remote `id -un` check is added.

Direct login: the effective account (explicit, else default) replaces the
email prompt; the controlling terminal shows `Raspberry Pi ID account: <email>`
and then asks for the hidden password and one-time code as before. The email is
not secret but is never logged (diagnostics say only that an account was
preselected). Password/TOTP are never accepted from options, environment or
files.

Browser companion: the companion runs in the Chrome profile already signed in
to Connect, and no observed Connect page or endpoint lets pi-sh choose or read
that account. An explicit account (any source except the local default) is
therefore refused with status 255 before the loopback bridge starts
(`ErrBrowserAccount`). The local default is a convenience for direct login and
does not block `--browser` (a DEBUG1 line notes it is unused). Verifying the
Chrome account would require an observed, authorized account-identity source
on the Connect page; none has been observed, so it is not attempted.

Match: `Match user` and `%r` see the account obtained so far (CLI,
destination, `User`), else the local default, else the local login name, so
existing OpenSSH semantics hold when no account is configured. `-G` prints
`user <email>` when an account (explicit or default) is known and omits the
line otherwise.

Differences from OpenSSH: `email::device` is not OpenSSH syntax (ssh would read
user `local-part` and host `domain::device`); a Unix-style user is refused;
`-G` omits `user` when no account is known.

## Port (`-p`, scp/sftp `-P`, URI `:port`, `Port`)

Decision: **always refused**, including `22`. Connect does not reach an SSH port,
so accepting any port would imply a selection that does not happen. Syntax
follows OpenSSH `a2port` (`0`/out of range is a syntax error; service names are
not resolved). Inactive `Port` lines are syntax-checked and ignored.

## Network addresses

An IPv4/IPv6 literal (bracketed, or with a zone) as the effective target is
refused: Connect selects devices by name or ID. This also applies to
`HostName` values. OpenSSH itself rejects bracketed IPv6 in `ssh://` URIs
(the host must be domain-style).

## BatchMode, NumberOfPasswordPrompts

- `BatchMode yes` (yes/true/no/false, case-insensitive): every Connect sign-in
  needs a person (direct password/one-time code, or the browser companion), so
  BatchMode always fails before any prompt, loopback listener or HTTPS request,
  with status 255 in all three tools. There is no unattended authentication.
- `NumberOfPasswordPrompts 0`: direct login fails before prompting (255);
  browser mode is unaffected (it has no CLI password prompt). `1` is the
  current behaviour. Values above 1 are refused: direct login submits one
  password per process and never retries automatically.

## ConnectTimeout, ConnectionAttempts

- `ConnectTimeout` (OpenSSH time syntax, `none`, `0` = no extra bound) bounds:
  each direct sign-in HTTPS step (not the time a person types), and each
  establishment attempt as a whole: device preparation HTTPS (device list,
  shell page, ICE configuration), signaling (POST and answer polling), ICE/DTLS
  and data-channel open. Built-in limits (30 s per HTTPS request, 45 s
  preparation, 2 min establishment, ~120 s answer polling) still apply when
  shorter. The browser companion wait is the person's sign-in step and is not
  bounded by it. Timeout failure: status 255,
  `Connect connection establishment timed out before any remote command was sent`.
- `ConnectionAttempts` (1–10; above 10 refused to limit service load): repeats
  an establishment attempt that failed transiently (timeout, network request
  error, missing WebRTC answer, ICE/DTLS failure) after a 1 s pause, as OpenSSH
  does. Each attempt prepares a fresh Connect connection, since signaling is
  single-use. Authorization/HTTP-status/device-match failures and cancellation
  are not retried; sign-in is never retried. An attempt ends when the data
  channel opens, before any command byte is sent (only the empty readiness
  message), so a command is never replayed. `--browser` refuses values above 1
  because the companion signals once. A failed attempt whose offer reached
  Connect may leave the agent's side to expire as after a closed browser tab.

## ServerAliveInterval / ServerAliveCountMax

Implemented as a transport liveness bound, not an SSH-channel request:
with `ServerAliveInterval N > 0`, the WebRTC peer fails and the session ends
(status 255, "remote connection closed") after `N × max(CountMax, 1)` seconds
without any packet from the device. ICE consent checks (STUN binding requests
authenticated with the negotiated ICE credentials, RFC 7675) are sent at least
every 2 s, and the device's existing WebRTC stack answers them; nothing is
installed or changed remotely and no data is written to the shell. Pion's
other timers are unchanged.

Differences: OpenSSH's request travels inside the encrypted SSH channel and is
answered by sshd; here the agent's ICE stack answers. Even with
`ServerAliveInterval 0`, Pion's defaults end a silent session after about
30 s (OpenSSH default: no client keepalive). Exact disconnection time can
exceed the bound by the ICE check interval. The message is not OpenSSH's
"Timeout, server not responding".

## TCPKeepAlive, ChannelTimeout, UnusedConnectionTimeout

- `TCPKeepAlive yes` (default) accepted as documented: Connect HTTPS uses Go's
  default TCP keepalive; the WebRTC data path is UDP/ICE without a TCP
  connection. `TCPKeepAlive no` is refused (not implemented for HTTPS).
- `ChannelTimeout none` accepted (the default); any other value is refused:
  idle session channels are not closed automatically.
- `UnusedConnectionTimeout` is refused as an unknown directive, matching the
  OpenSSH 10.2p1 client (it is an sshd option there).

## `-G` output

`-G` prints `user <email>` first when an account is known, and additionally prints, after the base, tag and logging fields, `batchmode`, `connectionattempts`, `connecttimeout`,
`serveralivecountmax`, `serveraliveinterval`, `tcpkeepalive` and
`channeltimeout` in OpenSSH form. `port` and `numberofpasswordprompts` are not
printed (no equivalent / differing default).

## Evidence (local only)

- `internal/sshdest`: table tests plus `TestSSHDestinationOpenSSHDifferential`
  (38 `ssh -G` cases) and `TestCopyAndSFTPOperandOpenSSHDifferential` (18 scp,
  18 sftp cases with a local argument-printing `-S` program).
- `internal/connpolicy`: value/refusal tests and
  `TestPolicyOpenSSHDifferential` (32 `ssh -G -o` cases, accept/reject parity
  and printed fields).
- `internal/connectsession/policy_test.go`: local Pion fakes: hung signaling and
  dead ICE bounded by ConnectTimeout; retry after request error and dead ICE,
  with no data before readiness on the successful peer and no channel on the
  failed one; no retry for permanent errors or cancellation; BatchMode fails
  without prompt/listener.
- `internal/transport/liveness_test.go`: Pion in-memory virtual network; an
  idle healthy peer survives its bound, a silenced peer closes within it, a
  default peer remains open.
- `cmd/*/policy_test.go`: CLI refusals before login, `-G` differentials and
  BatchMode status 255 without a password prompt.
- Account: `internal/account` (syntax table, env/XDG/home sources, invalid and
  non-regular files, no writes), `TestAccountDestinations` (sshdest),
  `TestAccountPolicy` (connpolicy), `TestAccountPrecedenceOpenSSHDifferential`
  (ten `ssh -G` comparisons of `user` and `Match user`),
  `TestAccountDestinationPolicy`, `TestDefaultAccount`,
  `TestBrowserRefusesExplicitAccount` (pi-sh), pi-cp/pi-sftp account tests,
  and the PTY prompt test `TestCredentialPromptsAndRestoration/account`.

## Owner decisions — 2026-10-22

1. **Remote user / account:** Unix-style users (`user@host`, non-email `-l`,
   `User`, URI users) stay refused; no remote `id -un` check is added. The owner
   first suggested a destination form naming the Connect account and device,
   for example `email#device`, then chose **`email::device`**, together with
   `-l email` and `User email` with OpenSSH precedence and a local non-secret
   default (`PI_SH_ACCOUNT` or `~/.config/pi-sh/account`). Implemented as
   described in "Account selection" above; the browser companion refuses an
   explicit account because the Chrome account cannot be chosen or verified.
2. **Stricter limits:** ConnectionAttempts capped at 10 and
   NumberOfPasswordPrompts above 1 refused are confirmed.
3. **ServerAlive:** using WebRTC/ICE connectivity checks as the equivalent of
   ServerAliveInterval/CountMax is accepted, with the documented differences.
