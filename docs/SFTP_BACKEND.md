# SFTP-backed pi-cp and pi-sftp — 2026-10-20

The suite now includes experimental `cmd/pi-cp` and `cmd/pi-sftp`. They use the
existing shell broker's binary stdin/stdout to run an **already installed**
OpenSSH-compatible sftp-server. They do not use an SSH connection or a native
Connect subsystem channel. The agent stays unchanged; no helper is installed.

## Status and remote footprint

Local tests use memory fixtures and the installed local OpenSSH sftp-server.
The owner-approved native plans already verified selected shared-backend binary
copies, preservation, resume, recursion, flush and cleanup with the Perl and
forced-Bash paths; see REAL_DEVICE_TESTS.md for exact dates and limits.

The 2026-10-30 console/option increments are local-only. They add the complete
requested console command list, listing/output parity, transfer settings and
bandwidth controls (SFTP_CONSOLE_COMMANDS.md). Prior native results do not verify
these new commands, concurrent windows or tuned settings. No new Pi session,
mutation or reuse of a completed plan occurred.

Server selection checks, in order, these existing paths:

1. `/usr/lib/openssh/sftp-server`
2. `/usr/libexec/openssh/sftp-server`
3. `/usr/libexec/sftp-server`

Missing executables cause failure (server status 127), never installation or SSH
fallback. `pi-sftp -s /absolute/existing/server` and
`pi-cp --sftp-server /absolute/existing/server` override the search. The executable
path is bounded, validated and shell-quoted; it is not a general command string.
The server receives `-e` so its diagnostics use the broker's independent stderr,
which is discarded rather than copied into file data or local diagnostics.
The broker uses its existing Bash/cat/setsid/stty facilities, memory and anonymous
pipes. Filename and file data are SFTP packets, never shell interpolation.

`pi-sftp --read-only` adds the OpenSSH server's `-R` flag. A local actual-server
test confirms metadata works and a create request fails without creating a file.
An incompatible server fails; read-only mode is never silently downgraded.

## Commands and syntax

```sh
go build -o bin/pi-sftp ./cmd/pi-sftp
go build -o bin/pi-cp ./cmd/pi-cp
./bin/pi-sftp --direct-login --read-only raspberrypi
```

Choose `--direct-login` or `--browser` explicitly. Both share pi-sh's existing
authentication and authorization logic; direct credentials are prompted only on
the controlling terminal and remain process-local. `-F file|none` and repeatable
`-o` support the explicit config subset and HostName aliases. RequestTTY yes/force
and StdinNull yes are rejected before login because this protocol requires its
private no-TTY input stream. EscapeChar has no active escape filter in this mode.
Top-level options precede operands. Registered short options taking arguments
accept attached and separate forms: `-Fnone`, `-oHostName=Pi`, `-b-`,
`-s/existing/server`. `--` ends options and following names stay literal. A value
beginning with `-` is consumed as a value; in `-b=commands`, the filename is
`=commands`, matching OpenSSH getopt rather than Go flag's usual equals syntax.
Project long options keep their existing `--name`/`--name=value` forms.
Registered short switches may be clustered, for example pi-sftp `-fb-` for flush
and stdin batch; a value-taking option consumes the rest of its cluster or the
next argument. Unsupported options, user@host/URI/port syntax and general SSH
options remain rejected; this does not enable their behavior.

pi-sftp supports `pwd`, `cd`, `ls`, `get`, `put`, `mkdir`, `rmdir`, `rm`, `rename`,
`mget`, `mput`, `reget`, `reput`, `chmod` (0000–7777, including -h),
`chown`, `chgrp`, `ln [-s]`, `copy`/`cp`, `df [-hi]`, `version`, `lumask`,
`progress`, `symlink`, `readlink`, `lpwd`, `lcd`, `lls`, `lmkdir`, `!`, help
and quit/exit/bye. Aliases chdir/lchdir/dir and case-insensitive command names
are accepted. SFTP_CONSOLE_COMMANDS.md names each implementation and essential
output/error comparison.
Remote transfer/rm/chmod/ls sources and local upload sources expand globs
as documented in GLOB_TILDE.md; destinations remain literal. Ordinary transfer paths are not shell-expanded;
explicit ! and lls run the local shell with its normal expansion rules.
Single/double quotes and backslash quoting preserve spaces/quote characters.
Commands reject unsupported flags; `--` permits a leading-dash literal operand.
`get -f`/`put -f` request file flush; top-level `-f` requests it for all transfers.
See [SFTP_FLUSH.md](SFTP_FLUSH.md) for capability negotiation, advisory errors and
the exact tested behavior. A successful copy status alone is not flush proof.
Remote/local symlink traversal components are preserved rather than lexically
cleaning away `link/..`. Automatic local basenames reject parent/root and native
path/volume components. Listing names follow OpenSSH octal escaping (TAB/CR/LF retained); diagnostic
paths retain safe display formatting.

`get`/`put` accept one source and optional destination; an existing destination
directory receives the source basename. Regular files are transferred; global `-r` or command `-r`/`-R` enables
directory transfers (SFTP_CONSOLE_RECURSION.md).
The default copy buffer is 32 KiB. pi-sftp -B or either client's -X buffer=
changes the copy and SFTP payload size, clamped to 256 KiB minus packet overhead.
pi-sftp -R or -X nrequests= (also pi-cp -X) sets concurrent windows; CLI default
is 64 requests. Ordinary stable regular files use bounded offset-based windows;
resume and bandwidth-limited copies remain sequential. Both paths bypass
pkg/sftp's optional ReaderFrom/WriterTo fast paths. A rejected final short WRITE
was observed to disappear in its sequential ReaderFrom when source EOF occurred
with data; the shared copy now propagates that write failure. Regressions cover
complete refusal and a retained 1024-byte prefix, with packet sizes 1024/32768
and source sizes crossing their boundaries. This is local error-path evidence,
not a native disk-full or Pi partial-write test.
Creation/truncation is explicit; failed transfers can leave partial files and
existing targets can already have been truncated. Resume (`-a`, reget/reput) is
implemented for regular-file glob matches and recursive children; see [rules and evidence](SFTP_RESUME.md).
No atomic replacement or fsync durability guarantee is claimed. Both clients
provide a terminal meter and -l payload bandwidth limiting; the SFTP progress
command toggles its display. -q/batch and -N use the documented OpenSSH-style
notice policy. ls implements -1afhlnrSt, columns and long layout. Sorted/column/
long listing accumulates entries; recursive downloads and ls -1f enumerate
incrementally (SFTP_LISTING.md).

`-p` now requests ordinary permission bits and access/modification times for
regular-file copies in both clients. See SFTP_METADATA.md for reference behavior,
advisory failures, combined upload handle metadata and remaining directory/local
timestamp path races.

Default creation permissions are tested against the local OpenSSH CLI. New
downloads use source permission bits, filtered by the local process umask.
Existing download/upload targets retain their modes. Before starting its server,
the in-memory command emits a bounded numeric original-umask prefix on its binary
stdout and sets **only that child process's umask to 077**. The prefix is validated
and consumed before SFTP negotiation; missing/invalid metadata fails setup.
No persistent setting or file changes. New uploads use exclusive creation,
verify private permissions and apply source permissions filtered by the original
umask using handle chmod **before any file contents are sent**. Failure leaves an
empty private partial file. Directory creation starts private and applies
0777 filtered by the original umask. Tests compare source modes 600/640/700/777,
umasks 022/077, new directories and existing targets against local OpenSSH.
An actual server rejecting fsetstat proves failed uploads stay empty and private.
The process-local mask avoids a fork of pkg/sftp, whose public OPEN API has no
creation-attribute parameter. These are selected default-mode fixtures; ACLs,
special bits, ownership, timestamps and full filesystem parity remain open.

`-b file` or `-b -` runs batch commands; the first error stops the batch unless
prefixed with `-`. Prefix `@` suppresses the command echo. Interactive errors
allow the next command. Unlike OpenSSH batch authentication, a fresh direct login
still requires controlling-terminal prompts; unattended BatchMode is absent.
Input lines are bounded to 1 MiB, with one queued line. An embedding caller must
own cancellation of any blocked local command reader/output writer.

pi-cp accepts literal `device:path`/`alias:path` remote operands and local paths.
Use `./file:with-colon` to disambiguate a local colon. Windows absolute drive paths
are recognized as local; runtime filesystem support remains platform-specific.
Copies can upload local regular files or download remote regular files from one
device. Multiple sources require an existing destination directory, checked
before file mutation. Remote-to-remote, mixed devices, local-only copies and legacy `-O` fail
explicitly. For current SCP operand syntax, recursion, preservation and
glob/tilde support, see SSH_COMPATIBILITY.md and the feature documents. It uses SFTP by default, not the legacy SCP wire protocol.

## Architecture, failure and bounds

- `internal/connectsession`: shared memory-only direct/browser auth, verified
  signaling requests and one native broker shell. A connection runs one server;
  many SFTP operations share that process. It cannot be reused for another broker.
- `internal/commandrun`: the same completion/auth-loss/cancellation arbitration
  used by pi-sh; cleanup-only grace remains bounded before peer close.
- `internal/sftpclient`: owned binary pipes, v3 negotiation, EOF and command status
  verification, explicit close/cancel and server-path selection.
- `internal/sftpcli` and `internal/copycli`: literal operands and file operations.

Negotiation is bounded to 30 seconds after authentication/connection setup.
Cancellation closes owned protocol pipes and reaches the broker's acknowledged
cleanup. Normal close sends protocol stdin EOF and requires server/broker status;
a server that does not exit is cancelled after three seconds, with a further
four-second wait bound. Missing cleanup/failed status cannot become success.
Server-controlled error text and malformed packet content are not echoed:
diagnostics retain only fixed classes or numeric SFTP status. CLI setup failures
return 255; file/batch failures return 1; cancellation returns 255. Complete exit
and interruption parity with OpenSSH remains unverified.

The pinned pure-Go pkg/sftp v1.13.11 uses SFTP v3, 32 KiB default transfer
requests and configurable bounded concurrency (64 by default in the CLIs;
unconfigured embedded sessions retain the previous limit of four). Its receiver rejects packets
over 256 KiB before allocating their bodies. No crypto verification is disabled:
HTTPS/DTLS trust remains Connect's existing path. The library's SSH client API is
not used; only NewClientPipe is used. Linux/386 without CGO remains mandatory;
physical iSH and native Linux/Windows runtime are still unverified.

References: [pkg/sftp API](https://pkg.go.dev/github.com/pkg/sftp@v1.13.11),
[OpenSSH sftp-server](https://man.openbsd.org/sftp-server.8),
[OpenSSH sftp](https://man.openbsd.org/sftp.1),
[OpenSSH scp](https://man.openbsd.org/scp.1). No upstream implementation is copied.
Dependency licences and original notices are in DEPENDENCIES.md and
THIRD_PARTY_NOTICES.md; original-code licensing and publication gates remain open.

## Native read-only check

```sh
printf 'pwd\nls\nquit\n' | ./bin/pi-sftp --direct-login --read-only -b - raspberrypi
echo $?
```

This requests metadata only and runs the server in read-only mode. It does not
authorize a Pi upload, mutation or transfer-write test. Record only success/failure
and status, without listing output, account data or authenticated captures.
The owner reported status 0 for the original binary and later for a rebuilt
pwd/quit read-only batch with the process-local umask preamble. No file-content,
preservation or remote mutation evidence follows from those metadata checks.

## Recursive SCP increment

`pi-cp -r` supersedes the earlier recursion limitation for literal SCP trees only.
See [SCP_RECURSION.md](SCP_RECURSION.md) for 48 local OpenSSH comparisons, link
following, directory metadata, memory limitations and native verification gaps.
SFTP console recursion is now implemented with its own link policy and local
OpenSSH comparisons; see SFTP_CONSOLE_RECURSION.md. New native verification
remains pending.

## Request size — 2026-10-28

The client used 1 KiB SFTP requests (`MaxPacketChecked(1024)`), a leftover
from when the broker moved input in 1 KiB records. sftpcli copies 32 KiB at a
time with plain `Write` and `Read`, so the pinned pkg/sftp split each write
into 32 sequential 1 KiB `WRITE` requests, each waiting for its status.

In the approved native run (NATIVE_PERL_WORKERS_PLAN.md) a 1 MiB upload took
17.8 s with the Perl broker workers, against 1.5 s for the same MiB written to
`wc -c`. Requests are now 32 KiB, the default of OpenSSH sftp and pkg/sftp.
Each 32 KiB copy is one request, and write errors are still checked per
request (sftpcli `copyFileData`).

`TestSFTPThroughputBenchmark` (internal/shellbroker, opt-in with
`PI_SH_SFTP_BENCH=1`) runs the real broker, the Perl workers and the local
OpenSSH sftp-server, 1 MiB, KiB/s:

| RTT | Request | Upload | Download |
| --- | --- | --- | --- |
| 0 ms | 1 KiB | 621 | 3583 |
| 0 ms | 32 KiB | 4036 | 8885 |
| 40 ms | 1 KiB | 43 | 209 |
| 40 ms | 32 KiB | 1123 | 1520 |

On the device, a read-only `pi-cp` of `/usr/bin/bash` (1,412,096 bytes) kept
its SHA-256 and took 18.0 s of wall time, against 19.1 s before. Most of that
is sign-in and session setup, so it does not isolate the transfer. The approved native
run with 32 KiB requests (NATIVE_PERL_WORKERS_PLAN.md addendum) moved 1 MiB up
in 2.1 s and down in 1.2 s (17.8 and 3.8 s before), every check passing.
