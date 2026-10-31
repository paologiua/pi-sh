# Raspberry Pi Connect compatibility

Reviewed: 31 October 2026. Project version: **v0.1.0-alpha.1**.

This record describes the **Connect agent on the remote device**, separately
from pi-sh's own CLI version and its OpenSSH behavior matrix. Agent versions
are evidence boundaries, not an enforced minimum/maximum version range.

## Agent version matrix

| Connect agent version | Status with pi-sh / pi-cp / pi-sftp | Evidence and limits |
| --- | --- | --- |
| **2.13.0** | Observed baseline; selected real-device checks passed | Dated development-build evidence covers shells/commands, selected transfers and forwarding. The 29 October plan explicitly identifies rpi-connect-lite 2.13.0. These are scoped runs, not complete certification of every command, package variant or the current alpha build. |
| **2.14.0** | Available in the official archive; runtime compatibility unverified | Official arm64 package metadata and the Lite package changelog inspected on the Mac. No Pi session or native pi-sh check against 2.14.0 has been run. Neither compatibility nor incompatibility is established. |
| Other versions | Not evaluated | No supported range or oldest supported agent version has been established. |

The owner reports that the current device still runs 2.13.0. This documentation
update did not query or modify the device. Do not interpret 2.13.0 as a required
pin or upper compatibility limit, or treat every 2.x release as interchangeable.

The most recent scoped native baseline is the
[29 October Perl broker plan](REAL_DEVICE_TESTS.md#native-perl-broker-plan-29-october-2026):
Bash 5.2.37, Perl 5.40.1 and rpi-connect-lite 2.13.0. Its selected transfer checks
passed with the default Perl broker and forced Bash, with cleanup verified.
Earlier shell, authentication, TTY and forwarding evidence is retained in
[REAL_DEVICE_TESTS.md](REAL_DEVICE_TESTS.md). Each entry names its own client
build, scope and remaining limitations. Relabeling the project as alpha does not
turn older results into a fresh acceptance run of the alpha binaries.

## What 2.14.0's published changes establish

The official Lite package changelog dates 2.14.0 to **21 October 2026**. Its
listed fixes concern desktop notification delivery and panel-plugin/remote-update
behavior after D-Bus restarts. It does not document a shell transport change.
That absence is **not** a protocol compatibility guarantee: internal changes,
dependencies and the separately deployed service may still affect an unofficial
client. No installed or executable upstream code was inspected for this update.

Primary-source snapshot read on the development Mac:

- [Official trixie arm64 package index](https://archive.raspberrypi.com/debian/dists/trixie/main/binary-arm64/Packages.gz): rpi-connect and rpi-connect-lite both list version 2.14.0. Compressed index SHA-256: `af87b46e8195ea3d25c405719077d297d6bdedbd59281a55361aac3bb446f982`.
- [Official rpi-connect-lite 2.14.0 arm64 package](https://archive.raspberrypi.com/debian/pool/main/r/rpi-connect-lite/rpi-connect-lite_2.14.0_arm64.deb): SHA-256 `bab2b63df2f14e523dda68647341a0dd8f497e88c8907ee99f01fa8a5dd0cea0`, matching the fetched index. Only `usr/share/doc/rpi-connect-lite/changelog.Debian.gz` was read from the archive; the package was neither installed nor run.

The package index is mutable. HTTPS retrieval and matching the package checksum
are recorded; an APT Release signature was not independently verified. No package,
upstream implementation or captured authenticated session is added to this repo.

## Agent version alone is insufficient

Connect also depends on a hosted sign-in flow, discovery/signaling APIs and relay
infrastructure. Those services can change independently of the installed agent.
Existing Bash/Perl, SFTP-server and forwarding-connector capabilities also matter.
See [protocol observations](PROTOCOL.md), [remote broker](SHELL_BROKER.md),
[SFTP backend](SFTP_BACKEND.md) and [forwarding](FORWARDING.md).

All three clients show `Tested with Raspberry Pi Connect 2.13.0.` in
`--help` / `-h`. Untested versions and the scope of the checks stay in this
document. The help line is offline information compiled into the client, not a
live query of the remote device or a claim that every alpha feature was tested.

`pi-sh --version`, `pi-cp --version` and `pi-sftp --version` identify the local
client build; they do not report the remote Connect agent version. The clients do
not currently enforce agent-version compatibility or reject an untested version
based solely on its number.

## Recording a newly tested version

Use an already configured device at that version and an authorized Connect
session. Installing/upgrading an agent to make pi-sh work is outside this
project's permitted device operations.

Record the exact package/version/architecture, existing remote prerequisites,
client version/commit and binary digest, host platform, date and test scope.
Start with no-write checks: login/discovery, interactive terminal and resize,
command stdin/stdout/stderr/status, cancellation and read-only SFTP operations.
Record failures as well as successful outcomes. A single successful shell does
not verify transfers, forwarding, failure cleanup or every client platform.

File-transfer writes, remote listeners and failure-injection tests need their own
explicitly authorized plan, bounded scope and cleanup. A completed old plan does
not authorize replay. Preserve the Raspberry Pi restrictions in AGENTS.md and
update this table only with linked evidence; untested stays untested.
