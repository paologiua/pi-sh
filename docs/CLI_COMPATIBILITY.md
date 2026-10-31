# CLI compatibility at a glance

Reference: the installed OpenSSH 10.2p1 clients (`ssh -?`, `scp -?`, `sftp -?`),
compared offline on 31 October 2026. This inventory covers their advertised short
options plus `ssh -Q`. It does not claim every ssh_config keyword, operand form,
output or runtime path is implemented. The detailed [compatibility matrix](SSH_COMPATIBILITY.md)
records tests, real-device evidence, differences and blockers per feature.

**All advertised short options have an explicit implementation or refusal decision.
They are not all functional.** No SSH key, port or cipher option can turn Connect
into SSH. Unsupported options fail before login instead of being silently ignored.

## pi-sh versus ssh

| Options | Current decision |
| --- | --- |
| `-D -L -R -W -g -N` | Forwarding/SOCKS/no-session implemented; connector and binding/cleanup limits in [FORWARDING.md](FORWARDING.md) |
| `-E -e -F -G -n -o -P -q -T -t -V -v -y` | Implemented with the documented subset/platform differences; `-P` is Tag, `-F` explicit files only, `-G` subset, terminal flags use the adapters |
| `-l` | Raspberry Pi ID email, rather than a remote Unix login name |
| `-Q` | Local pi-sh capability queries; SSH algorithm queries refused |
| `-a -x` | Accepted: agent/X11 forwarding is already disabled |
| `-4 -6 -B -b` | Refused: address-family/bind controls unimplemented |
| `-C -c -m` | Refused: SSH compression/cipher/MAC negotiation is not the Connect transport |
| `-I -i -K -k` | Refused: SSH key, PKCS#11 and GSSAPI authentication unavailable |
| `-A -X -Y` | Refused: no implemented agent or X11 forwarding |
| `-M -O -S` | Refused: no SSH connection multiplexing/control socket |
| `-J` | Refused: no SSH ProxyJump transport |
| `-f` | Refused: no supported post-login background handoff |
| `-p` | Refused: devices have no SSH port in this client |
| `-s` | Refused: arbitrary subsystem requests; use pi-sftp for the existing SFTP server |
| `-w` | Refused: no tun/tap network configuration |

Additional options: `--direct-login`, `--browser`, `--experimental-command`,
`--legacy-command`, `--legacy-shell`, `-h` and `--help`. Commands currently require
`--experimental-command`; normal `ssh host command` spelling alone is insufficient.
`[email::]device` selects the Connect device/account; `pi@device` is not Unix login.

## pi-cp versus scp

| Options | Current decision |
| --- | --- |
| `-F -l -o -p -q -r -v -X` | Implemented; explicit config, payload bandwidth, config subset, ordinary metadata, progress/diagnostics, recursion, transfer tuning |
| `-s -T` | Accepted with OpenSSH SFTP-mode meaning: SFTP always used; legacy filename checking does not apply |
| `-B` | Recognized BatchMode; fails before prompting because unattended Connect authentication is unavailable |
| `-3 -R` | Refused: two-device copies not implemented |
| `-O` | Refused: legacy SCP wire protocol unimplemented |
| `-4 -6 -A -C -c -i -J -P` | Refused: corresponding SSH routing/agent/compression/crypto/key/proxy/port options unavailable |
| `-D -S` | Refused: cannot replace Connect with a local SFTP/SSH transport program |

Additional options: `--direct-login`, `--browser`, `--sftp-server`, `-h`, `--help`.
Remote operands use `[email::]device:path`. No remote shell expansion is implied.

## pi-sftp versus sftp

| Options | Current decision |
| --- | --- |
| `-a -B -b -f -F -l -N -o -p -q -R -r -v -X` | Implemented; resume, buffers, batches, sync, config, bandwidth, notices, metadata, diagnostics/progress, request windows and recursion |
| `-s` | Existing absolute remote sftp-server path; no arbitrary subsystem |
| `-4 -6 -A -C -c -i -J -P` | Refused: SSH address/agent/compression/crypto/key/proxy/port features unavailable |
| `-D -S` | Refused: no local server/transport replacement |

Additional options: `--direct-login`, `--browser`, `--read-only`, `-h`, `--help`.
An initial remote path in the destination is refused; use `cd` or `get` in the console.

The console implements the command list printed by `help`/`?`, including aliases,
links/ownership, server-side copy, df/version, local shell commands, recursion,
resume and progress. Commands needing server extensions still depend on advertised
capabilities. See [command-by-command evidence](SFTP_CONSOLE_COMMANDS.md).

## Help and visual parity

```sh
pi-sh --help
pi-cp --help
pi-sftp --help
# Within pi-sftp:
help
?
# At the start of an interactive pi-sh input line:
~?
```

Offline help returns success without login. pi-sh writes help to stdout;
pi-cp/pi-sftp retain Go flag's stderr help stream. `-h`/`--help` are convenience
extensions, not claimed to match OpenSSH's help invocation or typography.

Implemented short flags keep the OpenSSH letter, attached/separate values and
clusters where applicable. The SFTP console help, prompt, selected successful
and error messages, listing layouts and transfer progress have local differential
fixtures against OpenSSH 10.2p1. That is **selected byte-for-byte parity**, not
identical output everywhere. Top-level help is explanatory and product-specific, uses canonical double-dash
Connect option names and OpenSSH-style argument names, and wraps transfer-client
descriptions to 96 columns;
connection/authentication diagnostics identify Connect. Locale/Unicode edge cases,
Windows terminals and new native-device console/tuning paths remain open.

## What remains beyond parameters

- Authentication persistence, unattended login, renewal and reconnect.
- Remaining configuration syntax/keywords, routing controls and terminal/signal semantics.
- Stronger cancellation, network-loss and descendant-process cleanup evidence.
- Remote-to-remote copies, legacy SCP, initial SFTP destination paths and selected path/metadata differences.
- Filesystem races, special metadata, disk-full and large/sparse-file coverage.
- New SFTP command/tuning checks on the Pi, physical iSH interactive/transfers,
  native Linux/Windows runtime and the deferred complete regression campaign.

Some SSH-specific features have structural restrictions; others are feasible
implementation work. Consult each matrix row instead of treating all refusals as
permanent protocol blockers. The historical WebRTC `RESULT fail=1` is unresolved.
