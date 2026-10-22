# Local stream lifecycle — 2026-10-21

A TTY carrier-send failure may briefly drain the independent result reader
before local I/O shutdown. This preserves a completed command result while
remaining input becomes undeliverable. It does not suppress local file/output,
restoration, help or suspend errors. StopIO/join then retains its existing
ownership guarantees; generic/Windows limits remain. See TTY_COMPLETION.md.

## Implemented boundary

`internal/localio` owns operations on borrowed stdin/stdout/stderr during a
session; it never owns or closes the supplied files. On macOS/Linux it snapshots
all descriptors before changing any of them, sets O_NONBLOCK, then uses poll and
bounded native reads/writes with a private wake pipe. Cancellation makes that
pipe readable for every waiter. Close cancels, waits for active endpoint calls,
restores the owned O_NONBLOCK bit and closes only its private pipe. EOF stays EOF;
no terminal Ctrl-D or remote signal is synthesized. Output already written is
retained, including partial writes; pending output may be dropped on cancellation.

Callers must group all aliases together and keep the files open without concurrent
use or flag changes until Close returns, except during the explicit suspension
action described below. Separate descriptor numbers can share
one open-file description, as standard streams commonly do. Snapshotting before
mutation prevents one alias saving another alias's temporary flags. RawConn.Control
obtains descriptors without File.Fd changing Go's descriptor mode. Only the
owned flag is restored: ordinary I/O can change kernel bookkeeping such as
Darwin FWASWRITTEN ([Apple XNU fcntl.h](https://github.com/apple-oss-distributions/xnu/blob/main/bsd/sys/fcntl.h)).
No upstream implementation source is copied.

Terminal detection, mode capture and terminal ownership also use RawConn.Control:
calling File.Fd before the localio snapshot could otherwise change a Go-pollable
TTY's original nonblocking bit. A real PTY regression first observed flags6 ->2
at both capture and Open; detection/capture/Open now preserve it. The related
client selection queries use terminal.IsTerminal instead of File.Fd.

The interactive CLI, experimental TTY command, no-TTY broker and legacy command
now use this group for their actual local file streams. The UTF-8 input filter
checks cancellation before and after rune decoding, including a partial rune.
The Unix file paths join their input/output workers before returning. TTY resize
and terminal monitors are also joined. A queued disconnect escape cannot turn an
already-cancelled session into local success. Restoration failures cannot retain
a successful command-status classifier or an EOF ignored as normal input end.

The broker APIs offer an optional StopIO callback: it must promptly unblock the
caller's borrowed I/O. Providing it opts into joined worker lifetime. Generic
arbitrary Reader/Writer/callback implementations without the hook remain the
caller's cancellation responsibility. Binary broker cancellation stops local I/O
before attempting the bounded C/D cleanup acknowledgment while an independent
receive pump and carrier remain alive. Abort handling is installed before G:
a failed commit send can still have launched the worker. No command is replayed.
TTY failures still report unconfirmed remote cleanup: local worker termination
is not evidence about the unchanged agent's remote process tree.

## Evidence and limits

- A real local raw PTY regression first hung after consuming byte c3 of an
  incomplete UTF-8 rune; cancellation now needs no further byte or file close.
- Descriptor tests cover blocking/nonblocking input, simultaneous blocked stdout
  and stderr, exact partial binary prefixes, repeated concurrent Close, aliased
  TTY descriptors, regular-file append and failed setup without prior mutation.
- Thirteen real local Pion/PTY TTY coordinator cases include output backpressure/auth
  loss and reuse after remote WINCH causes exit23 while local input is idle.
- Seven interactive Pion/PTY cases cover escape, remote close, cancellation, and
  auth/cancel/escape under output backpressure, checking exact terminal restore.
- Three file-command Pion cases cover all-byte input/output and separated stderr,
  idle stdin, full stdout pipe, auth/cancel, acknowledged abort and file reuse.
  Their peer is a synthetic PISH3 record fixture, not the real Bash adapter or
  a Connect emulator. Separate actual Bash broker and Pion/PTY legacy tests cover
  those production adapters; none of these is native Pi evidence.
- Regressions cover an uncertain G send, stopped local I/O before C/D, and an
  already-cancelled context racing a queued local escape.

O_NONBLOCK does not guarantee a bound on regular-filesystem kernel calls such
as a stalled disk/NFS operation. Terminal restoration has its own cancellation
watcher, independent of a blocked stream call. Whole-process crashes/SIGKILL
cannot run cleanup. Generic callbacks, authentication prompts and startup
logging are not all covered by this standard-stream group. Without cancellation,
a slow output consumer can still delay draining a closed transport.

Windows currently uses context checks before synchronous file calls and does
not claim to interrupt an already-blocked console/pipe call or join that worker.
Native Linux/Windows and physical iSH runtime checks remain open. Linux/386
build/test compilation requires no CGO; iSH must actually exercise poll, fcntl,
pipe wakeups, aliases, backpressure and reuse. No Pi session, file write, added
remote software, dependency change, publication or push occurred in this increment.

## Suspension handoff

WithSuspended is called only between input reads. An operation gate prevents new
I/O and drains active output, restores owned flags, executes the stop/resume
action, then snapshots all descriptors before reacquiring O_NONBLOCK. Changes
made by the shell while paused become the new baseline. A failed action or
cancelled session leaves flags restored; Close does not overwrite later changes.
Cancellation wakes a backpressured drain without running its action. Generic
concurrent reads are not supported during this handoff. Files must stay open.
Terminal mode ownership is separately handed back by terminal.Session.Suspend.
Real Mac child/TTY coordinator tests check the combined handoff; Windows does
not offer it and Linux/iSH runtime checks remain open. See ESCAPES.md.
