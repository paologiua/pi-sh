# iSH compatibility investigation

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
