# Legacy POSIX command adapter

The current shell-only command broker is documented in [SHELL_BROKER.md](SHELL_BROKER.md).
This page preserves the older adapter and its narrower behavior.

The later [TTY command investigation](TTY_COMMANDS.md) measures inherited input,
canonical/raw EOF, resize and signals against local OpenSSH. Its test-only simple
supervisor loses signal identity and is not enabled as command -t/-tt support.

Implemented and first verified on macOS against the unchanged Raspberry Pi on
2026-10-20. This is an explicit PTY shell adaptation, not a native Connect exec,
no-PTY request, subsystem or agent-provided exit-status channel.

```sh
./bin/pi-sh --browser --legacy-command raspberrypi 'printf "hello\n"; exit 23'
echo $?
```

Complete the same browser companion step as the interactive client. The example
prints hello and returns 23 after the framed result and channel EOF arrive.
The CLI's own authentication/experimental diagnostics go to local stderr.

## Execution and framing

The explicit device is followed by the command and optional arguments. Arguments
are joined with spaces before remote shell evaluation, matching the composition
documented by [OpenSSH ssh(1)](https://man.openbsd.org/ssh.1#DESCRIPTION). Local
quoting has already been processed; callers must preserve intended remote quotes.
Full OpenSSH option parsing is still incomplete: put CLI options before the device.

`internal/command` independently creates a fresh 128-bit random marker for each
plan. It sends a quoted bootstrap through the already observed text `shell`
channel. The existing login shell disables its history-file persistence and is
replaced, for this session only, by `/bin/sh`. No remote file, helper, package,
daemon, configuration or service is installed or changed. The adapter uses the
existing `/bin/sh`, `stty` and shell builtins, verified on this device.

The supervisor runs `stty -echo -onlcr` on its own temporary PTY, prints READY,
invokes a child `/bin/sh -c` with stdin from `/dev/null`, captures `$?`, prints a
framed decimal status, and exits with that status. This only changes the current
PTY's ephemeral modes. There is no trap injected into the ordinary interactive
shell. Echoed bootstrap text contains escaped marker notation, not the actual
control-byte frame, so echo alone cannot satisfy readiness/completion.

The streaming parser discards startup/echo bytes before READY, forwards output
until completion, validates canonical decimal 0–255, and requires channel EOF within five seconds
after completion. The deadline is a local safety policy, not a service contract.
It handles markers split at every byte boundary and does not buffer the entire
command output. Startup discard is bounded at 64 KiB, and the quoted bootstrap
is capped at 2048 bytes to stay conservatively below common PTY canonical-line
limits. That is a local policy, not a discovered service limit. Literal control
characters and invalid UTF-8 in command text are rejected before connecting.
Use shell printf escape syntax rather than literal control bytes in command text.

Missing/truncated/malformed markers, unexpected bytes after completion,
authorization loss, cancellation or transport errors return local failure 255.
Partial output may already have been written before failure. A valid adapter
status 255 also returns 255, without the unknown-status diagnostic; the number
alone is therefore ambiguous, as with the usual SSH convention.

## Compatibility limits

- The remote command still has a PTY for stdout/stderr, which remain merged.
  This is neither `ssh -T` nor a separate-stderr stream.
- stdin is always `/dev/null`. Pipes, interactive prompts, arbitrary stdin,
  forwarding, file transfer and stdin half-close are not implemented by this mode.
- The child shell is `/bin/sh`, not necessarily the user's configured login
  shell. Startup environment/options, `SHLVL`, job control, buffering, TTY tests
  and signal behavior may differ from SSH command execution.
- Programs that change PTY settings can transform their own output. Known byte
  fixtures passed; arbitrary binary-safe streams are not claimed.
- Background jobs, a command that destroys the supervisor/PTY, and asynchronous
  output after completion can cause unknown status. There is no post-completion
  close guarantee from the service; failure to close within the local five-second
  completion deadline returns unknown status.
- A marker prevents accidental collisions; it does not authenticate output
  against a hostile command able to inspect its supervisor. The remote endpoint
  and command remain within the trust boundary. Markers are never service tokens.
- Shell status from a signal is the shell's numeric result; no distinct SSH
  exit-signal message or reliable remote signal forwarding is implemented.
- The ordinary interactive shell's Ctrl-D/exit still has unknown status and
  returns 255. This adapter does not silently alter that mode.

## Evidence

Local tests run actual `/bin/sh` sessions under PTYs, including success, false,
exit 23/255, exec returning 42, quotes/Unicode, combined stderr, null stdin, and
known binary output. Killing only the local test supervisor yields unknown status.
Scripted stream tests cover every frame split, truncation, invalid status,
duplicate/trailing data, limits and cancellation. A simulated integration connects
real local Pion peers to a local shell PTY and runs the actual CLI coordinator.
It is not an independent simulator of Connect's HTTP or authentication service.

Authorised native-device checks returned 23, 7 and 0. The status-7 case redirected
stdout through an in-memory local pipe and exactly matched 24 expected bytes:
Unicode/quotes, NUL, 0xff, CR/LF and merged stderr. The status-0 case also printed
the numeric result 1 of a preceding false builtin. These are narrow measured
results, not a complete SSH differential test or other-platform runtime proof.
See [REAL_DEVICE_TESTS.md](REAL_DEVICE_TESTS.md) and the separate
[agent-source investigation](AGENT_EXIT_STATUS.md).
