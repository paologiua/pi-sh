# iSH compatibility investigation

The new pi-cp progress meter is pure Go, with 64-bit counters, a single joined
renderer per active file and the existing fixed 32 KiB transfer buffer. x/text
provides Unicode width tables; macOS/Linux use TIOCGPGRP/Getpgrp for foreground
output and SIGWINCH for resize. These operations, actual progress output and
file transfers still require physical iSH verification; the successful iSH shell
and non-TTY command below do not establish them. See SCP_PROGRESS.md.

## Experimental relay workaround

`PI_SH_ISH_RELAY=1` selects an explicit local transport workaround in `pi-sh`
(also shared by pi-cp/pi-sftp). `0` or an unset variable keeps the normal path;
other nonempty values fail without echoing their contents. Keep the observed
`GOMAXPROCS=1` startup workaround as well:

```sh
GOMAXPROCS=1 PI_SH_ISH_RELAY=1 /root/bin/pi-sh -vvv \
  --direct-login --experimental-command raspberrypi 'printf "ish-ok\n"; exit 23'
echo $?
```

The owner verified this command on physical iSH on 23 October 2026: the official
Connect session reached ICE/peer connected, opened the shell data channel in
16835 ms, printed `ish-ok` and returned status 23. This establishes one successful
non-TTY command through the configured relay, not full iSH compatibility.
Interactive shell, sustained traffic, terminal controls/restoration, transfers,
suspension, network transitions and repeatability remain unverified. App/build,
iOS version, device model and installed binary hash were not supplied.

The adapter gives Pion an empty interface list with standard-library socket and
DNS operations, requires authenticated TURN in the supplied session configuration,
forces relay-only ICE and disables mDNS. Pion binds wildcard sockets and lets
the OS route traffic to the configured TURN server. It invents no local interface
or IP address, adds no external relay and changes no TLS/DTLS verification.
Direct host/server-reflexive candidates are unavailable in this mode; lack of a
relay candidate is an explicit error before signaling an unusable offer. This
requires ordinary reachability of the authorized relay; it cannot make blocked
UDP/TCP or unsupported local socket operations work. No remote package, service,
configuration or persistent file change is involved.

The implementation relies on pinned Pion stdnet.Net zero-value socket delegation,
wrapped through transport.Net so ICE does not refresh interfaces through Netlink.
Dependency updates must retain this behavior or replace the adapter. No dependency
versions were added/upgraded; ice/v4 and turn/v5 are now direct imports for the
adapter and local integration test respectively.

`TestDefaultTransportReportsDeniedInterfaces` reproduces the observed
`create-offer errno=22` using an interface-enumeration failure. Before the fix,
`TestRelayWithoutInterfacesEncryptedSession` failed with that same error; after
the fix it creates a relay-only offer and sends/receives payloads over DTLS/SCTP
through a loopback-only authenticated TURN server. It uses no Connect session or
Pi. Additional tests require authenticated TURN, reject a missing relay candidate,
and check explicit environment selection. Local transport/session race tests pass;
these tests are evidence for the workaround design, not iSH runtime support.

## Owner-run iSH observations, 23 October 2026

The owner tested Linux/386 softfloat, CGO-disabled Go 1.27.3 binary
`pi-sh_v0.0.0-20261023072210-d67e88eed6aa` on physical iSH. App/build, iOS
version and device model were not supplied. Without GOMAXPROCS, `-V` emitted
no initialization traces before an explicit 30-second SIGKILL timeout (137).
With `GOMAXPROCS=1`, initialization completed, `-V` printed the version and
returned 0. This is a per-process diagnostic workaround, not a proven runtime fix.

The owner subsequently completed password/TOTP direct login and device discovery
(two ICE servers). The command test failed with status 255 after the local-offer
phase message, before any answer-phase message or command output. The `closed`
state can be failure cleanup; it does not identify the original error. At this
stage no successful iSH shell or command had been established. Peer-operation diagnostics
now retain a fixed operation name and a numeric syscall errno when available,
never the original Pion/network error text. The diagnostic build subsequently
reported `operation=create-offer errno=22`.

The owner then ran the isolated `ish-net-probe v1` (Go 1.27.3, Linux/386
softfloat, CGO disabled; SHA-256
`b910e7b8fbf278b750be19095e25dc70d58927bc103fdc7e616a6fd920a92b5f`):

```text
timeout -s KILL 30 env GOMAXPROCS=1 /root/bin/ish-net-probe
ish-net-probe v1 go1.27.3 linux/386 GOMAXPROCS=1
BEGIN net.Interfaces
FAIL net.Interfaces errno=22
BEGIN net.InterfaceAddrs
FAIL net.InterfaceAddrs errno=22
DONE
```

The exit status was 1, not a timeout. This reproduces local interface enumeration
failure independently of authentication, Pion, remote sessions or the Pi.
Source inspection of the pinned dependencies connects CreateOffer to
GetLocalParameters/createAgent, ICE stdnet.NewNet/UpdateInterfaces and anet's
Linux wrapper around net.Interfaces. Go's Linux implementation uses Netlink.
The exact failing syscall has not been traced on the owner's app, but interface
enumeration is now a demonstrated compatibility blocker. Native Mac execution
of the same probe succeeds; that does not establish an iSH workaround. The
experimental relay adapter described above subsequently passed one physical
command test. Disabling mDNS alone does not replace the required interface
enumeration in the normal transport path.

The successful owner-run relay test used the `+dirty` build of the version above
with `GOMAXPROCS=1 PI_SH_ISH_RELAY=1`. The provided build's SHA-256 was
`07ff8f6408150ac62057712fe63ddaa443f16692eb3838d807461640cac6612b`;
the owner did not report a hash check of the installed file. Sanitized key output:

```text
transport: experimental iSH relay mode enabled; local interface discovery disabled
transport: ICE connection state state=connected
transport: peer connection state state=connected
transport: shell data channel open elapsed=16835ms
session: starting command adapter adapter=broker-no-tty
ish-ok
session: remote command finished exit_status=23
```

The owner's immediate `echo $?` returned 23. This used an output-only command,
with no Pi installation, configuration change or file-transfer write test.

Earlier statements of untested runtime below describe the prior investigations;
these partial observations do not establish full iSH compatibility.

The GNU stty correction adds no local syscall, CGO or remote executable. Its
fresh remote snapshot is bounded to1024 bytes/36 fields, and ARM/x86 Linux
layout guards are about the destination, independent of the iSH client's386
architecture. Mac-to-Pi PENDIN/TERM/input/status evidence does not establish
physical iSH behavior. The new Linux kernel fixture is compile-checked only on
this host; actual Linux/iSH execution remains required.

Local ~^Z now uses self SIGTSTP and CONT, restores/reacquires existing termios
and descriptor flags and refreshes window size. Mac child-process and Pion/PTY
runtime tests pass; these do not establish iSH job-control, foreground-group,
SIGCONT, ioctl or poll behavior. Linux/386 CGO-disabled compilation is a separate
gate, and physical iSH suspension remains unverified. See ESCAPES.md.

The corrected remote TTY controller adds no local syscall or Go dependency.
It uses the already required remote Bash/cat/stty, private history and one carrier
group. Local Mac tests cover both carrier versions, Ctrl-Z and whole-group
STOP/CONT; these do not establish iSH local suspend/reacquire or the Pi agent's
native teardown. See TTY_JOB_CONTROL.md for current evidence and assumptions.

The new local stream backend uses existing x/sys poll, F_GETFL/F_SETFL with
O_NONBLOCK, native read/write and a private pipe on Linux, including 386 without
CGO. Physical iSH must validate pipe wakeups, aliased descriptors, partial UTF-8,
full output, exact restoration and reuse. macOS runtime tests do not establish
those syscall semantics on iSH. See [LOCAL_IO.md](LOCAL_IO.md).

Command terminal-mode capture now uses existing x/sys TCGETS fields for Linux,
translated into a bounded portable vocabulary; no C library or CGO is required.
Flags, characters and metadata rates use small fixed snapshots, with remote
application through existing Bash/stty and ASCII hex source upload. Linux/386
softfloat build/test compilation passes. Physical iSH must still verify ioctl
translation, disabled controls, CBAUD/CIBAUD, raw/canonical processing and restore;
the macOS kernel tests and Linux-only compiled fixtures do not establish that.

TTY-command reference fixtures use local Unix PTYs and a development OpenSSH
server, with no new client runtime dependency. They currently establish macOS
observations only. Linux/386 build/test compilation does not execute the PTY,
process-group, controlling-terminal or signal-delivery behavior on physical iSH.
Command -t/-tt is now experimental in the CLI, with macOS local evidence only;
see TTY_COMMANDS.md. The coordinator uses the existing pure-Go terminal/Pion
interfaces and context cancellation; no new runtime dependency is introduced.
TERM/initial sizing and resize compilation do not establish physical iSH tty,
signal, UDP or suspension behavior.
The later Bash-worker fixtures also inspect local process trees and foreground
groups with ps/TIOCGPGRP, including HUP/TERM cleanup before PTY close. These Unix
test-only operations add no Go client runtime dependency or iSH runtime proof.

pi-cp and pi-sftp now use pure-Go SFTP packets through the existing remote broker,
not a local SSH executable or cgo. Configured requests are 1024 bytes with four
per-file requests; incoming packet bodies are bounded to 256 KiB by the pinned
library. Protocol pipes are owned and cancellable. Recursive downloads and ls -f
now use the local incremental directory reader; sorted ls still accumulates
a directory. Recursive depth64 retains at most about16 MiB of response bodies
plus other memory, not a measured iSH peak. See SFTP_LISTING.md. Both new binaries and tests compile for Linux/386 softfloat
without CGO, alongside six other targets. This is not physical iSH proof, nor a
large/sparse-file or native Windows filesystem compatibility claim. The remote
requires an already installed compatible sftp-server; none is installed by pi-sh.

Command cancellation now uses standard Go contexts/channels/timers and the
existing remote Bash process-group cleanup. No local Unix-only runtime dependency
was added; CGO-disabled Linux/386 softfloat build/test compilation passes. Real
iSH signal delivery, bounded cleanup over ICE and process lifetime still require
physical testing. Unix-only ps/SSH process diagnostics occur in local tests,
never on the Pi or as CLI requirements.

The 2026-10-20 CLI option/configuration subset uses Go's standard library for
parsing and local file reads, with no new OS-specific library or executable.
Default SSH files are not required. Linux/386 softfloat with CGO disabled builds
and test-package compilation remain mandatory. `-G` is offline;
StdinNull affects command input only, leaving controlling-terminal login prompts
subject to the existing physical iSH verification requirements.

Include adds only standard-library directory reads/stat/file-identity checks.
Its aggregate 1 MiB input/256-file/16-level limits, 128-entry directory chunks and
16384 filesystem-entry visits avoid unbounded whole-directory enumeration.
Expanded paths/targets are limited before token appends. Physical iSH still
needs real filesystem/permission/symlink/dirent, home/environment and memory
tests; portable compilation does not establish those syscalls or runtime behavior.

Transfer data now use a fixed 32 KiB plain-interface buffer, avoiding optional
file-copy fast paths that hid a rejected final short write in the pinned library.
SFTP -f adds local os.File.Sync and negotiated remote fsync requests, with no CGO
or new dependency. Physical iSH still needs filesystem/sync/error/cancellation
tests; no bounded local-kernel I/O latency or runtime support is inferred.

Research date: 19 October 2026. Runtime status: **not tested on iSH**. This document defines a target and a verification plan; it does not certify support.

Regular-file -p adds standard-library stat access-time extraction for Linux,
macOS and Windows, os.Chtimes and existing SFTP metadata requests. Linux/386 uses
64-bit Go time values converted from syscall stat fields without CGO. Physical
iSH still needs atime, chmod, timestamp-range/rounding, filesystem and permission
tests. Source atime cannot be inferred from mtime if metadata is unavailable.

SFTP resume adds ordinary file Seek/Stat/Truncate operations and 64-bit Go
offsets, without new dependencies or executables. Linux/386 compilation is not
evidence of working large offsets or iSH filesystem/truncation/error semantics;
those still require physical tests. Memory per copy remains a fixed 32 KiB
buffer plus the existing protocol queues.

## Verified upstream facts

iSH implements a Linux environment through x86 user-mode emulation and syscall translation. Its upstream FAQ says the guest architecture is x86_32 and that 64-bit ELF executables cannot run. Therefore the target for the existing iSH app is `GOOS=linux GOARCH=386`, even on an ARM64 iPhone. [Upstream README](https://github.com/ish-app/ish), [upstream FAQ](https://github.com/ish-app/ish/wiki/FAQ).

Go supports 386 and Linux targets. `CGO_ENABLED=0` disables cgo; the current Go command documentation lists `GO386=sse2` as default and `GO386=softfloat` as an alternative. The upstream minimum-requirements page specifies SSE2 for the default 386 target and Pentium MMX or later for softfloat. These are compiler/runtime requirements, not a statement about iSH's implementation. [Go build environment](https://pkg.go.dev/cmd/go#hdr-Environment_variables), [Go target documentation](https://go.dev/doc/install/source#environment), [minimum requirements](https://go.dev/wiki/MinimumRequirements#386).

The project-hosted iSH Go wiki page, last edited in March 2022, reports problems running and compiling Go and discusses an alternative fork. This is historical evidence of risk, not a current-version result or a reason to require a fork. No fork, runtime patch or settings change is part of pi-sh's baseline. [Historical upstream Go page](https://github.com/ish-app/ish/wiki/Installing-Golang).

## Source-level risk inventory

Source inspected at upstream `master` on the research date; these moving links must be replaced or supplemented with commit permalinks when recording reproducible runtime evidence.

| Area | Primary evidence | Implication and required runtime test |
| --- | --- | --- |
| Threads, locks, memory, signals | iSH registers `clone`, `futex`, `set_thread_area`, `mmap2`, `mprotect`, signal and clock handlers. [Syscall table](https://raw.githubusercontent.com/ish-app/ish/master/kernel/calls.c); [Go Linux/386 assembly](https://go.dev/src/runtime/sys_linux_386.s). | Handler presence is not conformance. Test startup, concurrent timers, cancellation, sustained goroutines, allocation/GC and clean shutdown on the actual app version. |
| Network polling | Go's Linux netpoll implementation creates epoll and eventfd descriptors, registers edge-triggered socket events, and waits for readiness. [Go netpoll source](https://go.dev/src/runtime/netpoll_epoll.go). iSH implements epoll using its poll layer. [iSH epoll source](https://raw.githubusercontent.com/ish-app/ish/master/kernel/epoll.c). | Exercise read/write readiness, deadlines, closed descriptors and cancellation; compilation cannot discover poll wakeup failures. |
| Socket syscall variants | The inspected iSH syscall table maps `accept4` to an ENOSYS stub, while providing socket/socketcall, connect, accept, eventfd2 and getrandom entries. [Syscall table](https://raw.githubusercontent.com/ish-app/ish/master/kernel/calls.c). | Check selected Go/dependency fallback paths when local listeners are implemented. Never infer that every newer Linux syscall works. |
| Networking | iSH maps socket operations and address families to host sockets, including IPv4/IPv6 and Unix-domain address translation. [Socket implementation](https://raw.githubusercontent.com/ish-app/ish/master/fs/sock.c). | Independently test DNS, validated HTTPS, UDP, STUN/TURN, WebRTC DTLS/SCTP, IPv4/IPv6, cellular/Wi-Fi transitions and VPN conditions. Host sockets alone do not prove WebRTC connectivity. |
| Terminal and signals | iSH provides ioctl and signal syscall dispatch. [Syscall table](https://raw.githubusercontent.com/ish-app/ish/master/kernel/calls.c). | Test raw mode, size reporting, resize, Ctrl-C, Ctrl-D, escape handling and terminal restoration after interruption. |
| Background lifetime | The upstream wiki describes iOS suspension and an optional location-based workaround. [Background discussion](https://github.com/ish-app/ish/wiki/Running-in-background). | Treat foreground operation as the initial target. Test lock/background/resume and report connection loss; do not require location access or silently enable a workaround. |

All implications in the final column are project engineering judgments, not observed failures. No networking or iSH test was performed in this investigation.

## Architecture requirements

Keep transport, authentication, terminal access and secure credential storage behind narrow interfaces. Prefer pure-Go implementations and bounded buffers. Stream large data rather than allocating according to an untrusted length or transferring entire files into memory; the 32-bit target makes size conversions and overflow especially relevant. Avoid requiring external browser executables, system keychains or C libraries in the portable core. A platform without a secure persistent credential backend must permit an explicit ephemeral session rather than plaintext fallback.

Keep OS-specific terminal and credential implementations in separate files. Unsupported functionality must produce an actionable error. iSH's guest Linux identity must not accidentally select assumptions that require a full native Linux kernel. Do not downgrade TLS, DTLS or randomness to obtain compatibility.

## Automated build verification

CI now runs native unit/local peer integration tests and cross-builds repository
packages and test executables, including `linux/386` with cgo disabled. The
Pion transport and browser-assisted `pi-sh` command are included. The browser
companion adds an iOS Safari/Chrome-to-iSH loopback and local-network-permission
requirement that needs physical testing; building the Go executable cannot prove
that authentication workflow works on iOS.
The desktop developer-console startup step must not be assumed usable on iOS;
an iOS-appropriate companion entry point remains an authentication/UI design gap.
Example commands from the repository root:

```sh
go version
go test ./...
CGO_ENABLED=0 GOOS=linux GOARCH=386 GO386=sse2 go build ./...
CGO_ENABLED=0 GOOS=linux GOARCH=386 GO386=softfloat go build ./...
```

Cross-compilation of tests must compile each test package separately (for example, `go test -c` with an output path per package). A cross-target `go test ./...` on a Mac attempts to execute Linux test binaries and is not a valid runtime test. Cross-build macOS amd64/arm64, Linux amd64/arm64 and Windows amd64/arm64 as independent checks. Record exact Go version and dependency versions with results. A passing cross-build means **build verified**, not **iSH runtime verified**. [Go command build/test documentation](https://pkg.go.dev/cmd/go).

## Deferred physical-device acceptance

Record iSH app version/distribution, iOS version, device model, Go version, build flags and a binary digest. Begin with offline `--help`/parsing and local simulated sessions, then DNS/TLS and authorized Connect sessions within the user-approved development scope. Measure startup, idle CPU/memory, sustained bidirectional traffic, cancellation and terminal restoration. Repeat on Wi-Fi and cellular. Capture only sanitized outcomes; no credentials, SDP, ICE addresses or authenticated network dumps.

All Raspberry Pi restrictions still apply. Runtime testing must not change its software, configuration, services or persistent files. Temporary transfer tests require separate authorization and an approved path with cleanup. Physical iSH access is deferred by the user; this is the outstanding runtime-verification blocker.

## Shell-only broker

The client broker adds no native client runtime or CGO dependency. Bash, cat,
setsid and stty are requirements on the existing remote Pi, not on iSH. Hex framing
and anonymous pipes occur remotely. CGO-disabled Linux/386 compilation remains
a required check; browser-companion reachability, Pion networking and terminal
syscalls still require physical iSH validation. No iSH runtime claim is made.

## Direct login no longer requires browser automation

Normal HTTPS password/TOTP login and Connect signaling now work in the macOS Go
CLI, with own cookies kept only in memory. No Chrome extension or desktop browser
is required by this route. Prompts use the controlling terminal and pure-Go
terminal/network libraries. This removes a browser-automation dependency from the
iSH design; it does not prove iSH TLS/DNS/entropy, tty restoration, UDP/ICE/TURN or
iOS suspension behavior. Physical iSH testing is still required.
