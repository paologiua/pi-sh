# In-memory command broker research

Research date: 2026-10-20. This document is a design proposal, not implemented
protocol evidence or a claim of OpenSSH compatibility. No Raspberry Pi commands,
browser operations or device changes were performed for this research. Availability
of Python and its modules on the target must be measured separately.

## Conclusion and scope

An existing Python 3 interpreter is a promising way to turn the observed Connect
text/PTY shell into a byte-preserving command transport. The command itself need
not inherit the outer PTY: an ephemeral broker can create three anonymous pipes
and a new child session. This removes the current merged-output and null-stdin
limitations in principle. It is an inference from the documented OS interfaces,
not yet a measured property of the target. Python must already exist; installation
is forbidden, and its absence must produce an explicit capability error.

This is not a route to literal equivalence for every OpenSSH feature. Connect
identity, session environment and service behavior differ. Some SSH features also
conflict with the project's persistent-write restrictions. The achievable target
is demonstrated behavior for a documented feature set.

## Reference behavior

OpenSSH joins command arguments with spaces and executes a remote command rather
than a login shell. `-T` disables PTY allocation; no-PTY communication is intended
to transfer arbitrary bytes. Normal SSH termination includes waiting for forwarded
connections. These are separate requirements, not one exit marker.
[OpenSSH ssh(1)](https://man.openbsd.org/ssh.1)

SSH distinguishes stdin EOF, channel closure, stderr, numeric exit status and
termination by signal. Its data windows bound outstanding traffic. A replacement
protocol needs corresponding distinctions, even though its encoding is independent
of SSH. An exit status alone must not discard unread output or imply that all
forwarded channels have closed.
[RFC 4254 §§5.2–5.3, 6.6, 6.9–6.10, 7.2](https://www.rfc-editor.org/rfc/rfc4254.html)

OpenSSH's server tests `WIFEXITED` versus `WIFSIGNALED` and sends different status
messages. Shell convention `128+signal` is not equivalent to that distinction.
Its cleanup deliberately leaves pending pipe data readable. The broker should
retain wait status separately from stream completion.
[OpenSSH session.c, session_exit_message](https://raw.githubusercontent.com/openssh/openssh-portable/master/session.c)

Do not unconditionally map a remote signal to `128+N` locally. OpenSSH's client
initializes its remote exit status to -1 and updates it on an exit-status request;
its process termination path also treats received local signals specially. Pin
the reference OpenSSH version and verify the actual visible result for both a
self-signalled shell and a shell waiting for a signalled child. These cases can
differ without either being a transport error.
[OpenSSH clientloop.c](https://raw.githubusercontent.com/openssh/openssh-portable/master/clientloop.c)

## Why an existing Python interpreter helps

`Popen` offers binary stdin/stdout/stderr pipes, explicit executable and argv,
descriptor closure, signal restoration and `start_new_session`. Its negative
return codes represent signal termination on POSIX. Use the configured user shell
with `-c`, rather than assuming Python's default `/bin/sh` matches SSH. Never use
`communicate()` as an unbounded whole-session buffer. Its documentation warns that
pipe handling can deadlock; concurrent draining and writing are required.
[Python subprocess](https://docs.python.org/3/library/subprocess.html)

Merely redirecting descriptors 0–2 does not remove an inherited controlling
terminal. `setsid()` creates a new session without one; `start_new_session=True`
performs that operation in the child. Otherwise a command can open `/dev/tty`,
change the carrier modes or inject bytes outside framing. This is a transport
isolation measure, not a sandbox against malicious code running as the same user.
[Linux setsid(2)](https://www.man7.org/linux/man-pages/man2/setsid.2.html),
[Python os](https://docs.python.org/3/library/os.html)

For full duplex, a selector can multiplex the carrier and pipe descriptors on the
Linux remote host; partial writes, EOF and EPIPE must be explicit states. Every
queue needs a byte cap and every independently multiplexed stream needs credit.
One blocked stdin pipe must not prevent draining child stderr or handling cancel.
[Python selectors](https://docs.python.org/3/library/selectors.html)

The remote interpreter can execute a short `-c` bootstrap, with `-I -B -u`.
`-B` suppresses bytecode-file writes; `-I` excludes the current directory and user
site paths and ignores Python environment settings. `-u` avoids output buffering.
Consider `-S` as well so site customization does not execute during bootstrap;
verify required standard modules still import on the actual installation.
These are remote runtime options, not an added Python dependency for the Go CLI
or iSH client.
[Python command-line options](https://docs.python.org/3/using/cmdline.html)

## Shell-only alternatives

POSIX shell command substitution is not a byte container: trailing newlines are
removed and NUL output is unspecified. It can still launch byte-oriented tools
through pipelines, but that requires checking those tools and coordinating their
life cycles. This research does not prove a shell-only broker impossible; it does
not establish a safe portable implementation with the required primitives.
[POSIX shell command substitution](https://pubs.opengroup.org/onlinepubs/9799919799/utilities/V3_chap02.html)

Bash process substitution may use `/dev/fd` or named FIFOs. Its use is therefore
not evidence of zero filesystem writes. Here-documents/here-strings also must not
be assumed to remain in memory across shell versions. Avoid these constructs in
the broker bootstrap unless the exact implementation is separately verified.
[GNU Bash process substitution](https://www.gnu.org/s/bash/manual/html_node/Process-Substitution.html)

Bash `coproc` creates anonymous input/output pipes and exposes descriptors and a
PID; it offers a potential fallback, but is not POSIX shell. It does not itself
solve arbitrary-byte encoding, a third independent stream, nonblocking full duplex,
controlling-terminal detachment or reliable wait-status distinction. Avoid
declaring fallback parity from the existence of `coproc` alone.
[GNU Bash coprocesses](https://www.gnu.org/s/bash/manual/html_node/Coprocesses.html)

## Proposed protocol, independently designed

All names and limits below are proposals owned by pi-sh, not Connect protocol
observations. Start with an exec-only version; do not enable forwarding implicitly.

1. Send a small quoted bootstrap command after suppressing persistence in the
   existing interactive shell. Keep its first canonical input line below 2048
   bytes. Linux canonical input is limited to 4096 characters; a long Python
   program pasted as one shell line is unsafe. The bootstrap saves the outer PTY
   attributes and configures a carrier appropriate for ASCII framed input.
   [Linux termios(3)](https://www.man7.org/linux/man-pages/man3/termios.3.html)
2. Emit a versioned READY carrying a fresh random session marker. Do not put the
   literal READY frame in the echoed shell command. Bound startup-noise discard.
   Upload bounded ASCII/base64 source chunks into a RAM buffer, checking sequence,
   total length and digest before `compile`/execution. Use stop-and-wait bootstrap
   acknowledgements; never rely on one huge terminal write or a heredoc. On
   unsupported interpreter/module, fail before running the user command.
3. Negotiate capabilities, version and limits. Send command bytes separately from
   bootstrap source, so shell quoting cannot turn command data into broker source.
   The exact choice of remote shell, cwd and environment must be explicit and
   reported for differential testing.
4. Use bounded ASCII records with direction, session marker, sequence, type,
   logical stream ID and base64 payload. Newline-delimit records; initially keep
   each at most 1024 encoded bytes. Validate canonical encoding and size before
   allocation. Parse across arbitrary Connect message boundaries. Base64 carries
   NUL and invalid UTF-8 without trusting the PTY's treatment of raw bytes.
5. Define START, STDIN_DATA, STDIN_EOF, STDOUT_DATA, STDERR_DATA, STREAM_EOF,
   EXIT_STATUS, EXIT_SIGNAL, CANCEL, CREDIT and ERROR. EOF closes only the child's
   stdin writer, retaining output reads. Preserve ordering within each stream;
   promise no total stdout/stderr ordering that independent SSH pipes cannot give.
6. Spawn the user shell with three binary pipes, a new session and no carrier FD
   inheritance. Drain output concurrently, close finished streams, retain wait
   result and complete only after the chosen reference stream-lifecycle behavior.
   Credit must limit input/output memory and avoid starvation of control messages.
7. On carrier EOF/error, attempt defined process-group cleanup and reap the direct
   child. Track what happens to descendants that detach into a new session;
   killing the original group cannot promise to remove them. Do not equate this
   policy with every sshd/PAM/systemd deployment. Do not introduce privileged
   process inspection or modify system service settings to close that gap.

Random markers prevent accidental startup collisions; they do not authenticate
messages against a hostile same-user command. Neither a marker nor an ordinary
checksum is a security boundary. The authenticated WebRTC transport remains the
network security boundary. No source, command, frame or session secrets should be
persisted in diagnostics.

## PTY, forwarding and hard limits

Interactive execution can later use a nested PTY whose master the broker owns;
the child gets the inner controlling terminal and the outer PTY remains a carrier.
Resize, foreground-process signals and terminal modes then refer to the inner
PTY. Python exposes fork/PTY primitives, but this would create another terminal,
with observable device/PID/session differences. It requires separate tests rather
than an assertion that the current interactive shell has become identical.
[Python pty](https://docs.python.org/3/library/pty.html)

TCP forwarding is feasible in principle through broker sockets and logical
streams, including half-close. Networking policy, DNS, bind errors and connection
limits must remain explicit. A remote Unix-domain pathname socket normally creates
a filesystem object, and is incompatible with the current no-persistent-files
restriction unless a separately authorized exception exists. Agent/X11 support
also needs independent lifecycle and filesystem analysis, not just copying bytes.
[Python socket](https://docs.python.org/3/library/socket.html)

An unprivileged wrapper cannot reproduce sshd authentication, PAM environment,
privilege changes, audit/login accounting, server-admin restrictions or exact
SSH_CONNECTION/SSH_CLIENT values. Faking those environment values would conceal
the difference. The extra broker process, timing, memory, throughput and encoded
transport overhead are observable. No finite test proves universal behavioral
equivalence for arbitrary commands that inspect their execution environment.

## Proposed differential acceptance suite

Run first locally with real PTYs/pipes, then through authorized Connect. Use
authorized SSH only as a read-only diagnostic reference, never as pi-sh transport.
Record versions, selected shell, environment differences and exact fixtures. Do
not change SSH options that bypass host-key verification. All test programs must
run in memory, create no device files and avoid shell-history persistence.

| Case | Required observations |
| --- | --- |
| Byte streams | stdin/stdout/stderr exact bytes 0–255, NUL, invalid UTF-8, CR/LF, no final newline, empty input/output |
| Full duplex | simultaneous streams larger than pipe/carrier windows; deliberately slow local reader/writer; bounded memory |
| EOF and EPIPE | child reads until EOF; closes stdin early; emits output after input EOF; stdout closes before stderr |
| Status | 0, 1, 23, 126, 127, 255, shell syntax failure, `exec`, direct signal versus shell-reported signal |
| TTY isolation | isatty on all three FDs false for exec; `/dev/tty` unavailable; carrier unchanged by child operations |
| Process lifetime | finite background child retaining output FDs; redirected detached child; cancellation; sudden transport loss; no leaked test processes |
| Parsing | every record split, coalescing, bad base64, oversized record, wrong marker/sequence, truncated status, noise after completion |
| Remote shell | configured-shell quoting, arguments joined as OpenSSH does, cwd, locale, PATH, startup behavior differences |
| Terminal mode later | nested PTY, Ctrl-C/D/Z, resume, resize, paste, full-screen tools, terminal restoration |

Do not run file-transfer write fixtures until their location and cleanup are
authorized. Keep forwarding and long-lived-process tests disabled until the
implementation and cleanup plan are concrete. Update the compatibility matrix
only with results actually measured, retaining explicit unsupported cases.
