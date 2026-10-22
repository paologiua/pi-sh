# Diagnostics and logging — 2026-10-22

`pi-sh`, `pi-cp` and `pi-sftp` implement OpenSSH-style diagnostics controls in
`internal/diag`. References: [ssh(1)](https://man.openbsd.org/ssh.1),
[ssh_config(5)](https://man.openbsd.org/ssh_config.5),
[scp(1)](https://man.openbsd.org/scp.1), [sftp(1)](https://man.openbsd.org/sftp.1).
Behavior was measured locally against OpenSSH_10.2p1 (`ssh -G`, loopback-only
`ssh -p 1 127.0.0.1` failures, scp/sftp to a closed loopback port). No Raspberry
Pi or Connect session was used for this increment; native verification is pending.

## Controls

| Control | pi-sh | pi-cp / pi-sftp |
| --- | --- | --- |
| `-v`, `-vv`, `-vvv` | First `-v` selects DEBUG1 from any level, each further `-v` increments the current level up to DEBUG3 (`-vvvv` = DEBUG3) | Same accumulation, applied to the connection layer, as scp/sftp pass `-v` to ssh |
| `-q` | Selects QUIET. `-v -q` = QUIET, `-q -v` = DEBUG1, `-v -q -v` = FATAL, exactly as ssh.c | Suppresses connection-layer notices/errors; a connection failure still prints `<prog>: connection closed` (as scp/sftp print "Connection closed") |
| `-E file` / `-Efile` | Append diagnostics to file instead of stderr; created `0600` (existing mode untouched); last `-E` wins; unopenable → `cannot open log file: <class>`, exit 1 | Not an scp/sftp option; rejected |
| `-y` | Send diagnostics to the local syslog (pure-Go `log/syslog`, Unix socket only). Unavailable syslog → explicit error, exit 1. Windows/Plan 9 builds reject `-y` | Not an scp/sftp option; rejected |
| `-y` with `-E` | Refused: `cannot specify both -y and -E`, exit 255 | — |
| `LogLevel` | QUIET, FATAL, ERROR, INFO, VERBOSE, DEBUG (=DEBUG1), DEBUG1, DEBUG2, DEBUG3; ASCII case-insensitive; invalid values (including OpenSSH-rejected SILENT/DEBUG4) fail before authentication | Same via `-o`/`-F` |
| `SyslogFacility` | USER (default), DAEMON, AUTH, AUTHPRIV, LOCAL0–LOCAL7; only affects `-y` | Accepted and validated; no effect (no syslog) |

Precedence follows OpenSSH: `-v`/`-q` always override LogLevel regardless of
position; otherwise the first obtained LogLevel/SyslogFacility wins (`-o` before
file values, first matching Host block in a file). `-G` prints `loglevel` and
`syslogfacility` using OpenSSH's names (DEBUG1 printed as `DEBUG`).

Ordering also mirrors ssh(1): `-V`/`-Q` exit before any log file is opened;
`-E`/`-y` are applied after command-line parsing and before configuration files,
so `-F` file errors are reported through the selected level/destination
(`-q` silences them, `-E` captures them) while command-line `-o` errors always
go to stderr. `-G` opens/creates the `-E` file, as ssh does.

## Message routing and `-q`

| Message | Level | Notes |
| --- | --- | --- |
| Fatal session/command error (exit 255) | FATAL | Suppressed only by QUIET, like ssh's `fatal()`; exit status unchanged |
| `-F` configuration file error | ERROR | No usage text; exit 255 |
| Experimental-mode notices, auth notices (`signed in directly…`, `experimental browser authentication…`), no-TTY warning, local `~.` disconnect notice | INFO | Suppressed by `-q`, LogLevel FATAL/ERROR/QUIET |
| `Authenticated to Raspberry Pi Connect method=direct-login` | VERBOSE | Analogue of ssh's VERBOSE "Authenticated to …" |
| Phase messages (below) | DEBUG1–3 | |
| Browser companion instruction (`run in the browser console` + `fetch(...)`) | — | **Never suppressed and never logged.** It is an action the user must perform and carries a temporary loopback capability, so it is written to the terminal stderr directly, even with `-q`, `-E` or `-y` |
| Direct-login prompts | — | Written to the controlling terminal as before; unaffected |
| Usage/option errors, log-file open failure, `-V`/`-Q`/`-G` output | — | Written directly, as OpenSSH does |

Stream lines end in CRLF, as OpenSSH's `log.c` writes them (also to `-E` files),
so diagnostics stay aligned while a local terminal is in raw mode. Debug lines use
`debug1:`/`debug2:`/`debug3:` prefixes; other levels use `<program>: `. Syslog
severities follow OpenSSH: FATAL→CRIT, ERROR→ERR, INFO/VERBOSE→INFO, DEBUG→DEBUG.

## Phase messages

| Level | Phase | Message (fixed text; fields are counts, durations or enum tokens) |
| --- | --- | --- |
| DEBUG1 | config | `unofficial client version=… go=… os=… arch=…`; `configuration: reading explicit configuration file` / `no configuration file selected` (path never printed) |
| DEBUG2 | config | `configuration: effective diagnostics loglevel=… syslogfacility=… logfile=… syslog=…` |
| DEBUG1 | auth | mode selected (browser/direct); `interactive direct login started` / `completed`; `loopback companion bridge listening` (no port/URL) |
| DEBUG1 | auth stage | `authentication: login stage stage=password_required|one_time_code_required|connect_devices_page_observed` |
| DEBUG1 | discovery | `resolving destination device`; `destination device prepared elapsed=… ice_servers=N` (no name/ID) |
| DEBUG1/2 | signaling | `requesting session configuration`; DEBUG2 `session configuration received ice_servers=N`; `sending local offer`; `remote answer received` |
| DEBUG1 | transport | `creating WebRTC peer and shell data channel`; `shell data channel open elapsed=…` |
| DEBUG2 | transport | `peer connection state state=new|connecting|connected|…` |
| DEBUG3 | transport | ICE connection, ICE gathering and signaling state names |
| DEBUG1/2 | broker | `starting shell-only broker`; adapter selected (`legacy`, `broker-no-tty`, `broker-tty`) or interactive shell; DEBUG2 `broker: finished exit_status=N`; pi-sh `remote command finished exit_status=N` |

## Redaction (structural)

- Messages are typed `diag.Message` constants. `TestNoDynamicMessages` parses all
  production Go files and fails on any conversion of a non-literal to a Message.
- Fields only carry integers, durations, booleans, or enum tokens matching
  `[A-Za-z0-9][A-Za-z0-9_.-]{0,47}`; anything else becomes `[redacted]`.
- Error text (`Failure`) and every final line pass through `Sanitize`, which
  removes SDP/ICE material, all URLs (including `stun:`/`turn:`), credential-like
  key/value pairs (password, otp/totp/code, token, cookie, session, authorization,
  csrf, ufrag, ice-pwd, key…), Bearer/Basic values, JWTs, long opaque tokens and
  email addresses, then replaces control/format characters and invalid UTF-8 with
  `?` and bounds the line to 1024 bytes.
- No logger API accepts remote terminal or stdio bytes. Pion's own logger stays
  disabled; only state enum names are observed.
- Tests: `internal/diag` (levels, ordering, filtering, redaction corpus, file
  append/privacy, `-y`/`-E` conflict, syslog via a private unixgram socket,
  concurrency), `internal/connectsession` (companion instruction survives `-q`
  and never reaches a log file; loopback Dial phases contain no SDP/candidate/
  address), `cmd/pi-sh` (`TestLogLevelOpenSSHDifferential`: 38 `ssh -G`
  comparisons; `TestLogOptionsRejectedLikeOpenSSH`: 15 rejections; real
  subprocess routing/exit-code checks), `cmd/pi-cp`, `cmd/pi-sftp`.

## Known differences and gaps

- QUIET suppresses pi-sh's fatal error text, matching OpenSSH's measured `-q`
  behavior; only exit status 255 remains. pi-cp/pi-sftp still say
  `connection closed`, as scp/sftp do.
- All terminal session errors are FATAL; OpenSSH logs some connection failures at
  ERROR (hidden by `LogLevel FATAL`). pi-sh shows them at FATAL.
- Log-file and syslog failures use exit 1; OpenSSH has no syslog failure path
  (openlog never fails visibly). Log-file errors report a class, not the path or
  OS text.
- OpenSSH prints the logfile path and config paths in its own messages; pi-sh
  deliberately omits paths, device names/IDs and network addresses at every level,
  so `-vvv` output is far less detailed than ssh's.
- The `-y` destination is the host's default local syslog socket; on iSH or
  systems without one, `-y` fails explicitly. Runtime behavior on iSH is untested.
- scp/sftp `-v` also print their own client debug (for example "Executing:
  program …"); pi-cp/pi-sftp have no equivalent client-side debug yet.
- Native Raspberry Pi verification of phase messages is pending.
