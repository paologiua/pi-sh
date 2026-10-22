# Local terminal handling

The CLI text-input filter now batches only locally available UTF-8 into at most
4KiB messages, flushing before a read can block (including a partial next rune),
local actions and terminal/error boundaries. A64KiB paste no longer produces
65,536 frames and exhausts a stalled peer's4096-frame queue before status delivery.
The queue's byte/frame limits and strict result rules are unchanged; no input or
command is replayed. This has local stress/latency/escape evidence only. See
TTY_COMPLETION.md; old native close failures and arbitrary unread-input capacity
remain separate limits. No remote file, software or configuration change occurs.

Terminal detection and mode capture now borrow descriptors with SyscallConn.Control
instead of File.Fd, which can change a Go-pollable Unix file to blocking mode.
The real PTY regression checks original flags across detection, capture and Open;
it first failed for capture/Open and passes ten race repetitions after the fix.
Windows builds use the same handle-preserving lookup; native Windows runtime
is still unverified. See LOCAL_IO.md and TESTING.md for the completed full post-correction race
integration rerun; an initial approval-review usage-limit failure delayed it.

`internal/terminal` implements local raw-mode ownership and window-size watching.
It does not allocate a remote PTY or encode remote resize/signal messages.

## Portable mode capture

`CaptureModes(input)` queries macOS/Linux input state before Open makes it raw,
then translates supported flag meanings, control characters and baud metadata
into internal/ttymode. It owns no descriptor and reads no terminal input. The
snapshot uses a fixed vocabulary with bounded cardinality; no OS struct or
stty -g encoding is transported. Windows/other platforms return nil modes rather
than inventing POSIX flags from their console fields. The command coordinator
uses this snapshot; the older ordinary interactive path is not changed.

The broker validates/quotes the snapshot and applies it before TR/command launch.
See [TTY_MODES.md](TTY_MODES.md) for all included/omitted modes, disabled-character
and speed normalization, actual stty/kernel/OpenSSH/Pion tests, and remaining
Windows/hardware/error-parity/native gaps. Local raw ownership/restoration still
belongs to Open/Close, independently of mode forwarding. Linux/386 compilation
does not verify physical iSH's ioctl semantics.

## API and lifetime

`Open(ctx, input, display)` takes actual `*os.File` handles. It rejects pipes and
files; the future CLI must choose when terminal mode is appropriate. Input and
display are separate because Windows uses an input console handle for raw mode
and an output console handle for visible dimensions. On Unix the same TTY can
serve both purposes. The caller retains file ownership and must keep both open
until `Close` returns; overlapping sessions on the same TTY are unsupported.

The session saves input mode, makes input raw, publishes the initial visible
dimensions, and watches for changes. Unix uses SIGWINCH; Windows polls every
200 ms. A setup recheck covers resizing during watcher registration. Updates
coalesce into a single latest value. Zero dimensions are preserved as the kernel
reported them, without inventing protocol defaults or bounds.

On Windows, the adapter also saves output console mode, enables virtual-terminal
processing, and restores that mode at shutdown. This code compiles for Windows;
native console behavior has not been verified here. Terminal setup returns an
error if the console does not support the requested mode.

`Close` stops watching and attempts restoration before returning. It is safe to
call concurrently or repeatedly. Cancellation of the owning context performs the
same cleanup. `Done` closes after restoration was attempted; `Wait` or `Close`
returns any query/restoration error. Cancellation itself is normal termination
of local terminal ownership, not a remote process exit status.

The CLI session coordinator defers `Close`, propagates restoration errors, and
connects handled process signals to the context. This package installs only a resize
notification handler, not a process-exit handler. `os.Exit`, SIGKILL, descriptor
reuse before cleanup and process crashes cannot be made safe by Go defers.

## Verification and limits

Real PTY tests run on macOS/Linux using test-only `github.com/creack/pty`; they do
not start a shell or write remote files. They cover raw control/binary input,
initial/changed/zero dimensions, exact saved-state restoration, cancellation and
concurrent repeated cleanup. Portable tests reject non-terminal files and an
already-cancelled context. Only macOS runtime tests have been executed locally;
Linux is configured in CI and Windows/iSH runtime verification remains pending.

This package itself does not define remote behavior or own stdin/stdout I/O.
`cmd/pi-sh` now connects its size events to the observed remote resize channel,
pumps native shell input/output, and implements the line-start `~.`/`~~`/`~?`/`~^Z`
escapes, configurable through `-e` (ASCII/control notation or `none`). Real-device
sizing, Ctrl-C, Ctrl-D and Unicode/multiline bracketed paste were verified.
Local suspension/resume is implemented on macOS/Linux with local TTY coordinator
evidence in ESCAPES.md; interactive suspension also has Mac Pion/PTY evidence.
Native TERM/environment requests, arbitrary remote signals and ordinary interactive
command status remain unimplemented. Experimental command TTY uses the separate
staged TERM/mode/status path described in TTY_COMMANDS.md and TTY_MODES.md.
This terminal API does not cancel file I/O itself. The CLI now separately uses
[localio](LOCAL_IO.md) to interrupt borrowed macOS/Linux file operations, join
workers and restore descriptor flags without closing caller files. Seven local
interactive Pion/PTY cases verify shutdown and restoration, including full output
and reuse. Windows synchronous I/O and arbitrary blocking callbacks remain open.

## Escape and shutdown verification — 2026-10-20

The local filter tracks input CR/LF boundaries, not the remote prompt or Bash's
editing state. After Ctrl-C or editing, press Enter before typing the disconnect
escape. Bracketed paste wrappers are forwarded unchanged. A wrapped single-line
`~.` reaches the remote shell; an embedded CR/LF followed by `~.` can still trigger
the filter. `-e none` disables both disconnect detection and doubled-escape
unquoting. This matches the line-based approach in upstream
[OpenSSH clientloop.c](https://github.com/openssh/openssh-portable/blob/master/clientloop.c).
No upstream implementation is copied. ASCII/control-notation parsing follows the
observed rules in [readconf.c](https://github.com/openssh/openssh-portable/blob/master/readconf.c);
non-ASCII byte escapes remain unsupported by the verified UTF-8 input path.

New CLI tests join a real Unix raw PTY with the actual input filter: Ctrl-D is
transmitted as a byte, Unicode/paste wrappers survive, local disconnect stops
input, and saved modes are restored. Short-read tests split every UTF-8 character
and escape across reads. Existing transport tests independently cover WebRTC;
these PTY tests are not a complete simulated Connect service.

Unix process termination now handles SIGTERM/SIGHUP/SIGQUIT as well as SIGINT.
Tests deliver real signals to the test process and check exact PTY restoration.
Windows handles os.Interrupt; console close/logoff remains unverified. Unix local
escape suspension has macOS child-process evidence; Linux/iSH remains unverified. SIGKILL cannot be handled.

For no-TTY broker commands, local termination signals now initiate bounded
acknowledged cleanup while the carrier remains available, then return failure
255. They are not arbitrary remote signal requests; stdin control bytes remain
binary data. An independent OpenSSH_10.2p1 client test verifies local signal
termination separately from remote PTY control-byte semantics. The owner-run
direct -nT Ctrl-C check returned 255 without missing-cleanup confirmation;
independent process inspection remains pending. See COMMAND_CANCELLATION.md.

A regression first demonstrated that context cancellation and terminal `Done`
could become ready together, allowing the old coordinator to return nil. The
coordinator now checks the cancellation cause after terminal restoration and
never treats an unsolicited terminal stop as success. Real native SIGTERM
termination returned 255 with a cancellation diagnostic. Intentional local escape
still returns 0; remote close/Ctrl-D still returns 255 without inventing status.

Primary references: [x/term v0.47.0](https://pkg.go.dev/golang.org/x/term@v0.47.0),
[Microsoft console modes](https://learn.microsoft.com/en-us/windows/console/setconsolemode),
and [PTY test library](https://github.com/creack/pty/tree/v1.1.24).

Canonical Ctrl-D with pending text has local production/reference evidence in
TTY_COMMANDS.md: flushing a partial record keeps the next line available, and
a subsequent empty EOF record lets read return1 while retaining partial text.
Both cases also pass through the CLI/Pion/PTY coordinator with restored local
modes. Native Pi and Linux/iSH behavior remain unverified.
