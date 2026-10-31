# pi-sh

**SSH-style command-line access through Raspberry Pi Connect.**

`pi-sh` is an unofficial Go CLI suite for using the existing Raspberry Pi Connect
agent and infrastructure from a terminal. It aims for practical OpenSSH command
and interface compatibility over Connect's HTTPS/WebRTC transport.

It is independent of, and not endorsed by, Raspberry Pi or OpenSSH. It does not
implement the SSH wire protocol. **Development version: v0.1.0-alpha.1. Compatibility and runtime verification
are incomplete; a GitHub release has not been published.**

| Client | Purpose | Familiar interface |
| --- | --- | --- |
| `pi-sh` | Interactive shells, commands and TCP forwarding | `ssh` |
| `pi-cp` | Files and recursive directory copies over SFTP | `scp` |
| `pi-sftp` | Interactive and batch file operations | `sftp` |

## Installation

### Install with curl

Once the repository is public and a release with binaries is available, install
all three clients on macOS, Linux or iSH with:

```sh
curl -fsSL https://raw.githubusercontent.com/paologiua/pi-sh/main/install.sh | bash
```

### Install with wget

Alternatively, use `wget` to run the same installer:

```sh
wget -qO- https://raw.githubusercontent.com/paologiua/pi-sh/main/install.sh | bash
```

**Currently, the repository is private and release assets have not been
published.** The commands above require public access and published release
archives. For now, use an authenticated checkout and build/install locally:

```sh
./scripts/build.sh all
./install.sh --from-dir ./dist
```

The default installation directory is `~/.local/bin`, with no sudo or shell-profile
edits. The installer verifies SHA-256 before installing all three clients.
Windows users extract the `.exe` files from the Windows archive.

The installer defaults to `paologiua/pi-sh` and `v0.1.0-alpha.1`; no `--repo`
is needed for this project. Use `--repo owner/name` to select a fork. The installer
does not request or save GitHub credentials.

Build targets: macOS amd64/arm64; Linux 386/amd64/arm64; Windows amd64/arm64.
Archives and their checksum manifest are written to `dist/`. This matrix represents
supported build targets, not a promise for every architecture understood by Go.
[Distribution and installer options](docs/DISTRIBUTION.md), [iSH](docs/ISH_COMPATIBILITY.md).

## Features

- Direct Raspberry Pi ID password/TOTP login, with credentials and cookies held
  only in process memory; an authorized browser companion is also available.
- Binary command stdin with EOF, separate stdout/stderr and exit status through
  in-memory adapters; optional terminal allocation, resize and local escapes.
- SFTP-backed copying, recursive transfers, resume, ordinary permissions/timestamps,
  progress, bandwidth limits and buffer/request tuning.
- An SFTP console with file operations, listing formats, links, ownership,
  server-side copy, filesystem statistics and explicit local shell commands.
- TCP local/remote forwarding, SOCKS4/5 and stdin/stdout forwarding, subject to
  existing connector capabilities and the documented lifecycle limits.
- Explicit configuration files, aliases, Match/Tag rules and offline configuration
  and capability inspection.
- Pure-Go client builds without CGO, including Linux/386 softfloat for iSH.

No package, daemon, agent, persistent helper or configuration change is installed
on the Raspberry Pi. The in-memory adapters use existing remote facilities.
SFTP requires an already installed compatible `sftp-server`; forwarding requires
an existing compatible connector. See [architecture](docs/ARCHITECTURE.md),
[transport](docs/TRANSPORT.md) and [remote requirements](docs/SHELL_BROKER.md).

## Raspberry Pi Connect compatibility

| Connect agent on the device | Verification status |
| --- | --- |
| **2.13.0** | Baseline for the recorded real-device tests: selected shells/commands, transfers and forwarding passed in development builds. The latest native plan identifies Connect Lite 2.13.0. |
| **2.14.0** | Available upstream; not yet tested with this suite. Compatibility is unverified, rather than known to fail. |
| Other versions | Not evaluated; no minimum or maximum supported version is established. |

These results cover specific tests and client builds, not every feature of the
current alpha. Agent version alone is insufficient: hosted authentication and
signaling can change independently, and existing remote tools still matter.
Each client's `--help` states `Tested with Raspberry Pi Connect 2.13.0.` The CLI
`--version` flags report the local client, not the Connect agent.
See [Connect version evidence and limits](docs/CONNECT_COMPATIBILITY.md) and
[real-device results](docs/REAL_DEVICE_TESTS.md).

## Quick start

You need an authorized Raspberry Pi Connect account and an already connected Pi.
For local builds, install **Go 1.27.3 or newer**, as required by go.mod:

```sh
./scripts/build.sh
./bin/pi-sh --help
./bin/pi-sh --direct-login raspberrypi
```

Use the exact Connect device name/ID, or an alias from an explicit configuration
file. Direct login prompts for your account, password and one-time code locally;
password/code input is hidden. Each process currently signs in again.

Choose the account explicitly to skip the email prompt:

```sh
./bin/pi-sh --direct-login me@example.com::raspberrypi
./bin/pi-sh --direct-login -l me@example.com raspberrypi
```

The account is a Raspberry Pi ID email. A Unix-style `pi@raspberrypi` cannot select
a remote Unix user: Connect uses its agent account. A non-secret account default
can be supplied with `PI_SH_ACCOUNT` or a one-line `~/.config/pi-sh/account` file.

At the start of an input line, type `~?` for escape help or `~.` to disconnect.
Use `-e none` to disable escapes. [Terminal behavior](docs/TERMINAL.md).

## Run a command

Commands currently require the explicit experimental flag:

```sh
./bin/pi-sh --direct-login --experimental-command raspberrypi 'printf "hello\n"; exit 23'
echo $?   # 23
./bin/pi-sh --direct-login --experimental-command -tt raspberrypi 'tty'
```

Without a terminal, the broker carries binary stdin, independent stdout/stderr
and command status. With `-t`/`-tt`, the remote terminal merges the output streams,
like SSH with a PTY. [Command semantics](docs/SHELL_BROKER.md),
[TTY commands](docs/TTY_COMMANDS.md), [cancellation](docs/COMMAND_CANCELLATION.md).

## Copy files

These examples write to the destination you specify; use paths you own:

```sh
./bin/pi-cp --direct-login ./notes.txt raspberrypi:notes.txt
./bin/pi-cp --direct-login raspberrypi:notes.txt ./downloaded.txt
./bin/pi-cp --direct-login -rp ./photos raspberrypi:photos
./bin/pi-cp --direct-login -l 1024 -X buffer=32K ./notes.txt raspberrypi:notes.txt
```

`pi-cp` uses SFTP, like modern scp. It has no legacy `-O` fallback or two-device
copy mode. Recursive copies follow source symbolic links; console recursion in
`pi-sftp` skips contained source links. [Copy details](docs/SCP_RECURSION.md),
[progress](docs/SCP_PROGRESS.md), [metadata](docs/SFTP_METADATA.md).

## Use the SFTP console

```sh
./bin/pi-sftp --direct-login raspberrypi
./bin/pi-sftp --direct-login --read-only raspberrypi
```

```text
sftp> help
sftp> pwd
sftp> ls -lh
sftp> get notes.txt
sftp> quit
```

Batch commands come from a file or standard input:

```sh
printf 'pwd\nls\nquit\n' | ./bin/pi-sftp --direct-login -b - raspberrypi
```

Login still prompts on the controlling terminal. Batch commands are supported;
unattended authentication is not. Server extensions are checked before operations
such as hard links, server-side copy and df. [Console commands and evidence](docs/SFTP_CONSOLE_COMMANDS.md).

## Configuration and forwarding

Explicit configuration files preserve familiar Host/HostName syntax:

```text
Host mypi
    HostName raspberrypi
    User me@example.com
    RequestTTY auto
```

```sh
./bin/pi-sh -G -F ./pi-sh.conf mypi
./bin/pi-sh --direct-login -F ./pi-sh.conf mypi
./bin/pi-sh --direct-login -N -L 8080:127.0.0.1:80 raspberrypi
```

No default SSH configuration is read. Unsupported settings are refused rather
than silently ignored. Forwarding uses the existing Connect shell and an existing
connector, with explicit binding and cleanup limits.
[Configuration](docs/CONFIGURATION.md), [forwarding](docs/FORWARDING.md).

## Client version

Each client supports the same offline version flags:

```sh
pi-sh --version
pi-cp --version
pi-sftp --version
# Short form, matching ssh -V:
pi-sh -V
```

Version output is one line on stderr and includes the client name, project version,
Go version and OS/architecture. It exits successfully without login or network
access. The selected version is `v0.1.0-alpha.1` for all three clients. Git builds
append safe source metadata, for example `+g77699a479687.dirty`; `dirty` means
uncommitted source changes were included. This is build identification, not a
claim that a GitHub release has been published. The `version` command inside the
SFTP console reports the server protocol version instead of the CLI build.

## Compatibility and current limits

**Not every SSH/SCP/SFTP parameter is functional.** All advertised short options
from the local OpenSSH 10.2p1 baseline have an explicit implementation/refusal
decision. SSH ports, keys, cipher negotiation, agent/X11 forwarding, tunnels,
connection multiplexing and ProxyJump are among the refused features.

Implemented flags retain familiar spellings where possible. The SFTP prompt,
help, selected output/errors, listing formats and progress have differential
comparisons against OpenSSH; this does not establish universal visual parity.

- macOS-to-Pi shells, commands, selected transfers and forwarding have recorded
  real-device evidence. New console/tuning paths still need native checks.
- One physical iSH non-TTY command passed with `GOMAXPROCS=1 PI_SH_ISH_RELAY=1`.
  Interactive sessions, transfers and broader iSH behavior remain unverified.
- Native Linux/Windows runtime coverage is incomplete. Cross-build success
  establishes compilation only.
- Saved login, automatic reauthentication, ICE renewal and reconnect are absent.
- Cancellation/descendant cleanup, network loss, special metadata, filesystem
  races and large/sparse-file behavior have remaining limits.
- The historical local WebRTC verification result `RESULT fail=1` remains
  unresolved; subsequent focused checks do not replace the deferred full campaign.

Read the [CLI comparison](docs/CLI_COMPATIBILITY.md) for the short-option inventory,
the [complete SSH/SCP/SFTP matrix](docs/SSH_COMPATIBILITY.md) for detailed limits,
and [real-device evidence](docs/REAL_DEVICE_TESTS.md) for what was actually verified.

## Development

Go is needed for the clients; Node is used only for browser-companion development
tests. Local Unix integration tests also need Bash >=4.3 on the development
computer (macOS system Bash 3.2 is too old); set `PI_SH_TEST_BASH` to a suitable
existing executable if needed. Neither is installed on the Pi by this project.

```sh
./scripts/build.sh             # three local binaries
./scripts/build.sh all         # seven platform archives plus SHA256SUMS
./scripts/test.sh              # local tests and checks
./scripts/test.sh --help       # full campaign prerequisites and invocation
```

The full mode adds race tests, Bash fallback checks and cross-build/test compilation.
Optional tool-dependent tests may skip; inspect results and [testing requirements](docs/TESTING.md).
These scripts do not start a Pi session. Existing native test plans remain subject
to their exact authorization and cleanup requirements.

For browser-assisted access, use `--browser`, keep the authenticated Connect
Devices tab open, and run the companion command printed by the client in its
Chrome developer console. This is an alternative to direct login, not an iSH
browser-workflow claim. [Authentication](docs/DIRECT_LOGIN_PROBE.md).

## Security, licensing and project status

Credentials are not stored in plaintext or exported from browser profiles.
TLS/DTLS verification and Connect authorization are preserved. Do not attach
cookies, tokens, SDP/ICE details or authenticated captures to reports.
See [security](docs/SECURITY.md) and [diagnostics](docs/DIAGNOSTICS.md).

Original project code and documentation are licensed under the [MIT License](LICENSE).
Third-party components retain their own licenses and copyright notices.
Dependency licenses and the local SFTP fork are documented in
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) and [dependencies](docs/DEPENDENCIES.md).
Service/publication questions remain recorded in [service constraints](docs/SERVICE_CONSTRAINTS.md).
No public release or provider endorsement is implied by this README.
