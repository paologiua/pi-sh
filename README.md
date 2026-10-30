# pi-sh

Unofficial, independent Go CLI project for SSH-like behavior over Raspberry Pi
Connect and its existing, unmodified agent. Not affiliated with or endorsed by
Raspberry Pi or OpenSSH.

**Native Go access now works with direct login on macOS, without Chrome or an
extension.** Enter your Raspberry Pi ID credentials and TOTP locally; the same
process uses its own in-memory web session for discovery, signaling and WebRTC.
A real Pi command printed the expected marker and returned 23. Browser-assisted
interactive access also remains available. This is an early implementation,
not complete OpenSSH compatibility.

Remote command supervision, the binary broker and forwarding run in Perl when
the existing device interpreter can compile the in-memory adapters. Bash loads
them and remains the fallback; no remote software is installed. The approved
native Perl checks are recorded in [real-device tests](docs/REAL_DEVICE_TESTS.md).

Local macOS/Linux session streams now support cancellation of idle input and
backpressured output without closing caller files; local Pion/PTY tests check
worker shutdown and terminal reuse. Windows blocked-I/O and native lifecycle
verification remain open. See [local I/O](docs/LOCAL_IO.md).

Line-start `~?` now prints local help in interactive/TTY sessions. Supported
escapes and the remaining compatibility gaps are listed in [terminal escapes](docs/ESCAPES.md).

Go 1.27.3 or newer is required, including its x509 security correction.

## Direct login

```sh
go build -o bin/pi-sh ./cmd/pi-sh
./bin/pi-sh --direct-login --experimental-command raspberrypi 'printf "direct-login-ok\n"; exit 23'
echo $?
```

The owner-run macOS check printed `direct-login-ok` and returned 23. Enter email,
password and TOTP at the local prompts; password/code input is hidden. No browser
profile is read and no cookies or credentials are saved. Each process currently
requires a fresh login. Prompts use the controlling terminal so command stdin is
kept separate. Options may precede or follow the exact device/alias until the first command
word or `--`; later tokens remain command operands.

### Choosing the Raspberry Pi ID account

The Raspberry Pi ID account plays the role of the SSH user. Give it as
`email::device` (owner decision; `email::device:path` for `pi-cp`/`pi-sftp`),
with `pi-sh -l email`, as an email `User` in a `-F` file or `-oUser=email`, or
percent-encoded in a URI (`ssh://me%40example.com@raspberrypi`). Precedence is
OpenSSH's: the first value obtained on the command line (in order, including
the destination), then the configuration file. A local non-secret default can be
set with `PI_SH_ACCOUNT=email` or a one-line file `~/.config/pi-sh/account`
(`$XDG_CONFIG_HOME/pi-sh/account` when set); pi-sh only reads it.

```sh
./bin/pi-sh --direct-login me@example.com::raspberrypi
./bin/pi-sh --direct-login -l me@example.com raspberrypi
./bin/pi-cp --direct-login ./notes.txt me@example.com::raspberrypi:notes.txt
./bin/pi-sftp --direct-login me@example.com::raspberrypi
```

With an account, direct login skips the email prompt and asks only for the
password and one-time code. `--browser` uses whatever account Chrome is signed
in to and refuses an explicit account it cannot verify. A Unix-style
`pi@raspberrypi` is refused: Connect opens the device's fixed agent account.
See [connection policy](docs/CONNECTION_POLICY.md).

Interactive mode is available with `./bin/pi-sh --direct-login raspberrypi`;
interactive behavior was previously tested with browser-assisted signaling,
while the new direct mode's real-device evidence currently covers the command
above. Physical iSH has now passed one non-TTY command via the experimental relay
workaround below; interactive iSH and native Linux/Windows remain unverified.
See [direct authentication](docs/DIRECT_LOGIN_PROBE.md) for scope and evidence.

## Testing on iSH

For the physical iSH interface-enumeration blocker and the opt-in experimental
`GOMAXPROCS=1 PI_SH_ISH_RELAY=1` relay workaround, see
[iSH investigation and test instructions](docs/ISH_COMPATIBILITY.md).
The owner verified direct login, WebRTC relay connection and one non-TTY command
on physical iSH: `ish-ok`, exit status 23. Interactive shell and broader iSH
runtime behavior remain unverified.

## Local configuration and aliases

`-F path` loads an explicit local configuration file and bounded `Include` files,
with `Host`, `HostName`,
`EscapeChar`, `StdinNull`, `RequestTTY`, `User` (account email), `RemoteCommand`,
`Match`/`Tag`, logging and connection-policy directives. Every other OpenSSH
keyword or flag is either accepted only with the value pi-sh always implements
(for example `Compression no`) or refused with a specific reason, never silently
ignored. CLI settings take precedence; no
default SSH or pi-sh files are read. `-G -F path alias` prints the supported
effective configuration without logging in or connecting. See
[configuration files](docs/CONFIGURATION.md) for an example, tests and differences.
Config-file/alias integration has local tests; new real-Pi verification is pending.

## Experimental file-transfer clients

`pi-cp` now shows [scp-style per-file progress](docs/SCP_PROGRESS.md) in a
foreground terminal: percent, size, speed and ETA. `-q` disables it; redirected
stdout stays free of progress lines. Local OpenSSH comparisons pass; physical
iSH file-transfer/progress verification remains pending.

`pi-sftp` now provides a command console/batch subset and `pi-cp` copies regular
files and literal directory trees with `-r`, using SFTP over the in-memory
Perl broker with Bash fallback. They require an existing
compatible sftp-server on the Pi; they never install one or fall back to SSH.
Local in-memory and actual OpenSSH-server/PTY tests have passed. Approved native
plans have also exercised binary transfers, preservation, resume and a small
recursive tree, including Perl and forced-Bash runs. These are scoped macOS-to-Pi
results, not complete platform or failure-path coverage; see
[real-device tests](docs/REAL_DEVICE_TESTS.md). Completed write-test plans do not
authorize further remote writes.

```sh
go build -o bin/pi-sftp ./cmd/pi-sftp
go build -o bin/pi-cp ./cmd/pi-cp
./bin/pi-sftp --direct-login --read-only raspberrypi
```

See [SFTP backend, supported operations and limits](docs/SFTP_BACKEND.md).
Recursive downloads and `ls -f` enumerate remote names incrementally; sorted `ls`
still accumulates a directory. See [listing memory and provenance](docs/SFTP_LISTING.md).
Both transfer clients accept attached forms such as `-Fnone` and
`-oHostName=raspberrypi`; pi-sftp also accepts `-b-` for a stdin batch.
pi-sftp supports global `-f` and `get -f`/`put -f` flush requests, with
[OpenSSH-style capability and error handling](docs/SFTP_FLUSH.md).
Global/command `-a` and `reget`/`reput` resume regular-file transfers from the
destination size; existing prefixes must match. See
[resume rules, local comparisons and limits](docs/SFTP_RESUME.md). Selected native
resume cases have passed in the approved plans; arbitrary interruption and
cross-platform behavior remain open.

Both clients accept `-p` for regular-file permission bits and access/modification
times. pi-sftp also supports `get -p`/`put -p`. See
[metadata behavior and remaining gaps](docs/SFTP_METADATA.md); these operations
have local reference and selected native-plan evidence, with the limits recorded
in the linked documents.

## Browser-assisted shell

A local build for this Mac is available at `bin/pi-sh` (ignored by Git).
To build from source with Go 1.27 or later:

```sh
go build -o bin/pi-sh ./cmd/pi-sh
./bin/pi-sh --browser raspberrypi
```

1. Sign in to [Raspberry Pi Connect](https://connect.raspberrypi.com/devices) in Chrome and open the Devices page.
2. Start the CLI with the exact device name or device ID. Omit it only when exactly one device is listed.
3. The CLI prints one JavaScript command. Run that command in the developer console of the authenticated Connect tab. The command loads the local companion from this CLI process. If Chrome asks for local-network access, allow it through the normal browser prompt.
4. Keep that browser tab open while using the shell. The shell traffic itself travels directly between Go and the Pi, or through the official Connect relay.

Immediately after pressing Enter, type `~.` to disconnect locally; `~~` sends a
literal tilde. A visible remote prompt alone does not reset the local escape
state (for example, after Ctrl-C or editing a line).
Ctrl-C and Ctrl-D are passed to the remote terminal. Ctrl-D on an empty Bash
prompt was verified to close the remote shell; it currently produces the remote
close diagnostic described below.

Pasting `~.` may insert bracketed-paste control sequences, so it does not act
like typing the two characters immediately after Enter. The local escape filter
is line-based, like OpenSSH: an embedded newline followed by `~.` inside a
multiline paste can still disconnect. Disable local escapes when needed:

```sh
./bin/pi-sh --browser -e none raspberrypi
```

With `-e none`, use remote `exit` or Ctrl-D to close the shell. To choose another
escape character, use `-e '!'` (then Enter followed by `!.`) or control notation
such as `-e '^]'`. ASCII escape characters are supported. Put options before
the destination. Attached forms such as `-e!` and `-o EscapeChar=none` are
supported. See the [CLI option subset](docs/CLI_OPTIONS.md) for precedence,
compact options, `-n`, `-T`, offline `-G` and explicit remaining gaps.

The alternative browser-assisted authentication needs the manual console step; both
DevTools automation and a user-reported manual connection have succeeded.

If the CLI says **Connect requires recent sign-in**, open **Remote shell** through
the normal Connect interface and complete its authentication flow. Then return to
or refresh Devices, restart the CLI, and use its newly printed companion command.
A visible Devices page alone does not prove that Connect permits a new shell;
the service can return a reauthentication page with HTTP 200.

## Offline client information

```sh
./bin/pi-sh -V
./bin/pi-sh -Q help
./bin/pi-sh -Q pi-sh-command
```

These queries need no login or network. Version is written to stderr; capabilities
to stdout. They describe compiled implementation, with runtime/native limits still
in the [compatibility matrix](docs/SSH_COMPATIBILITY.md). SSH algorithm queries
are explicitly refused. See [local inspection](docs/LOCAL_INSPECTION.md).

## Experimental single commands

`--experimental-command -t` now runs a command with the existing Connect TTY
when local stdin is a terminal; `-tt` forces that mode for pipes or `-n` too.
Native browser-companion checks now verify TTY descriptors, TERM, initial
91x29/0x0 sizes, one input line, PENDIN and exit23. Broader native control/resize
and teardown checks remain open; one earlier WebRTC-close error is unclassified.
It merges stdout/stderr, passes TERM/initial size and live resize,
and does not turn local EOF/-n into Ctrl-D. macOS/Linux forwards the documented
[portable terminal modes](docs/TTY_MODES.md); Windows translation, remaining
mode/error parity, suspension and native abnormal cleanup remain incomplete. See
[TTY commands](docs/TTY_COMMANDS.md). Without a TTY request, the binary broker
below remains the default.

```sh
./bin/pi-sh --direct-login --experimental-command raspberrypi 'printf "hello\n"; exit 23'
echo $?
```

Use direct login, or select `--browser` for the browser companion. The broker uses existing Bash,
`cat`, `setsid` and `stty`; it requires no Python or installed helper on the Pi.
It provides binary stdin with EOF, independent stdout/stderr, and command status.
Native checks verified all byte values, no controlling terminal and exit 23/143;
a larger native duplex test also preserved 64 KiB of stdin/stdout and 128 KiB of
stderr exactly, with self-SIGKILL returning 255. Local OpenSSH differential tests
cover additional cases.
See [SHELL_BROKER.md](docs/SHELL_BROKER.md) for requirements, exact evidence and
remaining differences. The older merged-stream/null-stdin adapter is explicitly
available as `--legacy-command`; there is no silent downgrade.

Cancelling a broker command now attempts acknowledged remote process cleanup
before closing the carrier. Missing confirmation is reported explicitly; local
termination still returns 255. Local PTY/WebRTC tests and an owner-run direct-login
Ctrl-C/status-255 check pass; independent remote process inspection remains
pending. See [command cancellation](docs/COMMAND_CANCELLATION.md).

## Current limits

- Interactive TTY plus an experimental command broker with pipe and requested-TTY
  paths. Native agent exec,
  forwarding is unimplemented; pi-cp/pi-sftp provide only the documented
  experimental SFTP subset, including literal `pi-cp -r` trees tested locally.
  Native content-transfer verification remains pending; see
  [recursive copy limits](docs/SCP_RECURSION.md). Configuration files support only
  the documented subset (CONFIGURATION.md); SSH keys, host keys, algorithms,
  compression, binding, address families, environment passing, -f, -N/-s and
  LocalCommand are refused with specific reasons. Unsupported options are rejected.
- Interactive stdout/stderr share the Connect PTY. The command broker separates
  them using anonymous pipes and client-defined framing. Native agent status
  remains unverified: ordinary interactive remote close returns **255**;
  intentional local `~.` disconnect returns 0.
- Authorization is checked every minute. Browser mode also fails after 90 seconds
  without companion confirmation; direct mode checks through its own HTTPS
  session. Saved login, automatic reauthentication and ICE renewal are absent.
- macOS real-device behavior is verified. Linux/Windows and Linux/386 build;
  native Linux/Windows console and broader physical iSH testing remain pending;
  one iSH non-TTY command passed with the explicit relay workaround.

## Development and evidence

```sh
go test ./...
go test -race ./...
go vet ./...
node --test internal/browserauth/companion_test.mjs
CGO_ENABLED=0 GOOS=linux GOARCH=386 GO386=softfloat go build ./...
```

Node is used only for browser-companion development tests, not by the Go CLI or
the Raspberry Pi. CI runs those tests with Node 26.

- [Current status and next gaps](docs/STATUS.md)
- [Direct HTTPS login probe](docs/DIRECT_LOGIN_PROBE.md)
- [Independent OAuth investigation](docs/CLIENT_AUTH_RESEARCH.md)
- [Real-device evidence](docs/REAL_DEVICE_TESTS.md)
- [Protocol observations](docs/PROTOCOL.md)
- [Shell-only command broker](docs/SHELL_BROKER.md)
- [Legacy command adapter](docs/COMMAND_EXECUTION.md)
- [Agent source/status investigation](docs/AGENT_EXIT_STATUS.md)
- [Architecture](docs/ARCHITECTURE.md)
- [SSH/SCP/SFTP matrix](docs/SSH_COMPATIBILITY.md)
- [Testing and build results](docs/TESTING.md)
- [Security](docs/SECURITY.md)
- [iSH constraints](docs/ISH_COMPATIBILITY.md)
- [Dependency licences](docs/DEPENDENCIES.md)

This remains a private development checkout. Service-terms interpretation,
original-code licensing, attribution and release review remain open publication
questions; no repository push or public release has occurred.
