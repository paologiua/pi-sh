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
| pi-sh | `host`, `user@host` (split at the last `@`), `ssh://[user[;params]@]host[:port][/]` | Scheme is case-sensitive; URI user/path percent-decoded (`+` is a space); URI host must be domain-style (alphanumeric start; letters, digits, `-`, `_`, `.`; no `..`), optionally bracketed; port 1–65535 or a service name; a URI path is an error. A plain host keeps brackets literally. |
| pi-cp | `[user@]host:path`, `[user@][addr]:path`, `scp://[user@]host[:port][/path]` | `colon()` rules: a leading `:` or a `/` before the first colon means local; an empty user before `@` is ignored; empty path means `.`; `scp://h/x` is `x`, `scp://h//abs` is `/abs`. Windows `C:\`/`C:/` stays local (project rule). |
| pi-sftp | `[user@]host[:path]`, `[user@][addr][:path]`, `sftp://[user@]host[:port][/path]` | Without a colon the whole argument is the host (brackets stripped). |

Plain hosts keep their exact case and may contain spaces, because Connect
device names are free text; OpenSSH lowercases and rejects such hostnames.
Configuration `Host` patterns match the parsed host (after removing `user@`
and URI syntax), so `ssh://pi` selects `Host pi`.

Differences: a Connect device whose name contains `@` (or, for scp/sftp, `:`)
can no longer be given literally; use its ID or a `HostName` alias. An empty
host (`a@`, `@:x`) is an immediate error rather than a later connection failure.
`pi-sftp host:path` (initial directory or file) is refused: not implemented.

## Remote user (`user@`, URI user, `-l`, `User`)

Decision: **always refused** before authentication, from any source,
including `User` in an active configuration block. The Connect agent opens the
shell as one fixed account. Connect's signaling exposes only device ID/name, so
the client cannot select a user, and cannot prove before login that a requested
name matches the agent account. An inactive `User` (another `Host` block) is
syntax-checked and ignored, so shared configurations remain usable.

Match evaluation is unaffected: a CLI or destination user is seeded into the
configuration context, so `Match user` and `%r` see it exactly as OpenSSH
would (otherwise the local login name). The refusal happens only after the
whole configuration, including any final pass, has been evaluated.

A possible future "accept only if it provably matches" mode would need a
remote `id -un` check inside the broker before the user's command; that adds
a remote command, cannot cover the interactive shell, and is left to the owner.

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

`-G` additionally prints, after the base, tag and logging fields, `batchmode`, `connectionattempts`, `connecttimeout`,
`serveralivecountmax`, `serveraliveinterval`, `tcpkeepalive` and
`channeltimeout` in OpenSSH form. `user`, `port` and
`numberofpasswordprompts` are not printed (no equivalent / differing default).

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

## Owner decisions — 2026-10-22

1. **Remote user:** refuse every `user@`, URI user, `-l` and `User` before login
   (current behaviour). No remote `id -un` check is added. The owner suggested
   exploring a destination form that identifies the Connect account and device,
   for example `email#device`; that is an open design item, not implemented.
2. **Stricter limits:** ConnectionAttempts capped at 10 and
   NumberOfPasswordPrompts above 1 refused are confirmed.
3. **ServerAlive:** using WebRTC/ICE connectivity checks as the equivalent of
   ServerAliveInterval/CountMax is accepted, with the documented differences.
