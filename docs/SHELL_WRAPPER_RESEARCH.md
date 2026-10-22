# Shell-only command broker research

Research date: 2026-10-20. This is a feasibility analysis, not an implemented
protocol or a real-device result. No Raspberry Pi or browser operations were
performed. It supplements [the earlier broker proposal](WRAPPER_DESIGN_RESEARCH.md).
All proposed records belong to pi-sh, not to the observed Connect protocol.

## Recommendation

Try a Bash-only byte encoder/decoder and anonymous-pipe topology first. A shell
variable cannot hold a general binary stream, but that does not prevent a shell
from forwarding bytes through pipes or encoding one byte at a time. The candidate
is conditional on measured Bash capabilities, correct serialization, bounded
full-duplex progress and acceptable performance. It does not justify introducing
Python if it meets the required behavior. Conversely, it does not establish
universal OpenSSH equivalence or portability to every Linux distribution.

POSIX command substitution strips trailing newlines and leaves NUL output
unspecified. Do not capture arbitrary command output with `$(command)` or backticks.
Keep payload binary in anonymous pipes and encoded text in variables instead.
[POSIX shell command substitution](https://pubs.opengroup.org/onlinepubs/9799919799/utilities/V3_chap02.html)

## Byte encoding with Bash builtins

A candidate uses `LC_ALL=C`, `IFS=`, `read -r -d '' -n 1` and builtin `printf`.
Empty delimiter makes NUL end a read; ordinary newline remains data. A successful
empty read can therefore represent NUL; success with a nonempty value represents
one other byte. The result code must distinguish success from EOF and timeout.
Use character numeric conversion and hexadecimal encoding; decode with validated
escapes written directly by `printf`, without capturing the decoded bytes in a
variable. The documentation supports these primitives; their composition needs
the complete 0–255 round-trip test on each supported Bash version.
[GNU Bash builtins](https://www.gnu.org/s/bash/manual/html_node/Bash-Builtins.html)

The encoder can gather bounded hex text and flush on size or elapsed idle time.
Fractional `read -t` is documented for pipes and terminals; timeout status is above
128 and partial data can survive. An empty timeout is not a NUL byte. `read -t 0`
is only a readiness check, not a byte read. These options are not a POSIX-shell
baseline. Probe actual behavior rather than inferring it from an executable named
`bash`. Keep `LC_ALL=C` inside broker helpers so it does not silently change the
user command's locale. Invalid UTF-8 must survive as bytes, never be decoded.
[GNU Bash read](https://www.gnu.org/s/bash/manual/html_node/Bash-Builtins.html)

One-byte shell loops may be slow. Benchmark throughput, CPU, first-byte latency
and slow-producer behavior before choosing payload and timeout sizes. A large
read that drops NUL is not a safe optimization. This is an engineering limitation
to measure, not evidence that shell-only binary transport is impossible.

## Anonymous pipe topology

The following illustrates descriptor routing only, not a complete broker:

```sh
{
  {
    recv | { child; status; } 2>&1 1>&3 | encodeErr >&4
  } 3>&1 | encodeOut >&4
} 4>&1
```

Before starting this graph, preserve the incoming carrier on a dedicated FD such
as 5. `recv` reads framed stdin records there and writes decoded bytes to its
pipe. The outer pipeline captures command stdout through FD 3; the inner pipeline
captures stderr. Ordering of `2>&1 1>&3` matters: stderr first retains the inner
pipe, then stdout switches to the outer pipe. FD 4 is the outgoing carrier.
[GNU Bash redirections](https://www.gnu.org/s/bash/manual/html_node/Redirections.html)

This construction alone does **not** solve status routing, serialization or
cleanup. `status` must capture the direct child's status immediately and emit a
separate control record; printing decimal text into an output encoder would leak
it into user output. Each pipeline component is normally a subshell. Its variables
cannot communicate state back to the supervisor, and the pipeline's final status
is not the child's status. Explicitly control inherited `errexit`, `pipefail`,
job control and `lastpipe` rather than depending on the interactive parent's
settings.
[GNU Bash pipelines](https://www.gnu.org/software/bash/manual/html_node/Pipelines)

Close every unnecessary copy of carrier and pipe descriptors in the command and
helpers. Child stdin EOF occurs only when every corresponding writer closes;
closing a single nominal writer is insufficient. An accidentally inherited
descriptor can keep the entire graph alive. EPIPE/SIGPIPE must close only the
appropriate direction and must not silently truncate the other outputs.
[Linux pipe semantics](https://man7.org/linux/man-pages/man7/pipe.7.html)

`coproc` provides another file-free anonymous two-pipe arrangement and a child PID.
It is Bash-specific and its exported descriptors have subshell availability
restrictions. It does not automatically provide separate stderr, cancellation,
serialization or bounded multiplexing. Descriptor duplication must be tested
before choosing it over explicit pipeline routing.
[GNU Bash coprocesses](https://www.gnu.org/s/bash/manual/html_node/Coprocesses.html)

Do not use heredocs, here-strings, process substitution, named FIFOs, named sockets
or temporary scripts for this candidate. In particular process substitution may
use named FIFOs instead of `/dev/fd`; its spelling does not guarantee zero remote
filesystem objects.
[GNU Bash process substitution](https://www.gnu.org/s/bash/manual/html_node/Process-Substitution.html)

## Serialization and duplex progress

The illustrated encoders both write FD 4, which is a PTY. Short records do not
establish atomicity there: POSIX's `PIPE_BUF` guarantee applies to a **single write
to a pipe or FIFO**. It does not apply to terminal writes, and a shell `printf`
invocation is not specified to issue exactly one system call. A stress test passing
does not upgrade that observation into a portable guarantee.
[POSIX write](https://pubs.opengroup.org/onlinepubs/9699919799/functions/write.html)

Two candidate remedies need prototype evidence:

1. A single carrier writer receives tagged records from helpers through anonymous
   pipes. If helpers share one pipe, establish how a complete bounded record is
   emitted atomically; merely checking its length against `PIPE_BUF` is incomplete.
2. Explicit grants ensure only one helper writes carrier records at any time.
   An acknowledgement or output grant must not require the blocked stdin writer
   to make progress. Grant loss, EOF, cancellation and helper death need recovery.

Give stdin, stdout, stderr and control independent credit/state. A receiver blocked
writing child stdin cannot also be the sole reader of cancellation and output ACK
records. Separate the control reader from the potentially blocked stdin writer,
or use a design that proves that dependency cannot occur. Close child stdin on
its EOF record while keeping the carrier open for output and status. Stop reading
when downstream credit is exhausted rather than accumulating unbounded shell
strings. These are protocol design requirements inferred from the topology.

## Why `od` and `stdbuf` are not an automatic shortcut

`od -A n -t x1 -v` can encode arbitrary bytes; `-v` matters because repeated lines
otherwise collapse to an asterisk. Width options differ across implementations.
It is a viable capability-based alternative, not guaranteed installed everywhere.
[GNU od](https://www.gnu.org/s/coreutils/manual/html_node/od-invocation.html)

Line buffering controls when completed output is flushed; it does not force a
formatter to finish a partial input block. GNU `stdbuf` only works with programs
using compatible C streams that do not reset buffering. Its own manual notes that
even unbuffered input need not change a blocking stream-read's behavior. Therefore
`stdbuf -oL od ...` must pass the one-byte request/response test with the producer
remaining open. A deadlock there rules out that composition for interactive
duplex use, regardless of its bulk-throughput performance.
[GNU stdbuf](https://www.gnu.org/software/coreutils/manual/html_node/stdbuf-invocation.html)

## TTY isolation, status and signals

Anonymous pipes make descriptors 0–2 non-TTY, but do not remove an inherited
controlling terminal. An existing `setsid` utility can launch the command in a
new session without one. Do not use its `--ctty` option. Detect this utility and
its exact options; it is an additional existing-tool dependency, not a Bash
builtin. Detached descendants still require an explicit cleanup policy.
[Linux setsid](https://www.man7.org/linux/man-pages/man1/setsid.1.html),
[setsid system call](https://man7.org/linux/man-pages/man2/setsid.2.html)

Bash reports fatal signals using `128+N`, which collides with intentional numeric
exits such as `exit 137`. `$?` cannot reliably recover the raw distinction. Do not
label 137 as SIGKILL merely from the number.
[GNU Bash exit status](https://www.gnu.org/s/bash/manual/html_node/Exit-Status.html)

The util-linux `setsid --wait` source returns a normal child's exit code; abnormal
termination follows a different error path and emits diagnostics. It does not
provide the broker with a general raw `waitpid` record. Check the installed
version before using it; `--fork --wait` must not be treated as exact signal-status
preservation.
[util-linux setsid source](https://raw.githubusercontent.com/util-linux/util-linux/master/sys-utils/setsid.c)

OpenSSH distinguishes exit-status and exit-signal messages. Thus a Bash design
using only numeric `wait` results has an identified fidelity gap even if byte
streams work. Wrapper traps cannot observe SIGKILL in the killed process, but a
surviving parent may inspect Bash's job table as described below. Full parity must
not be claimed from matching numeric codes alone.
[RFC 4254 section 6.10](https://www.rfc-editor.org/rfc/rfc4254.html#section-6.10)

## Acceptance gates

Run locally first using real pipes and PTYs, then on authorized Connect without
remote file creation. Maintain explicit implementation versus device-verification
statuses in the compatibility matrix.

| Gate | Required evidence |
| --- | --- |
| Complete byte domain | Exact stdin/stdout/stderr bytes 0–255, repeated NUL, CR/LF, invalid UTF-8, trailing newlines and no final newline |
| Immediate duplex | Child prints one byte, waits for one byte, repeats while every stream stays open; bounded first-byte latency |
| Congestion | Simultaneous input/output/errors larger than all pipe and carrier windows, slow readers and writers, memory caps |
| Record integrity | Adversarial concurrent writers, split/coalesced records, malformed hex, sequence/credit violations, bounded parser |
| Independent EOF | Child reads until EOF then writes both outputs; early stdin close; separate output EOFs; exit before descendant closes output |
| Lifecycle | Exit 0/23/137/255, child SIGKILL versus exit 137, `exec`, cancellation, transport loss, no surviving test processes |
| Isolation | Child FDs 0–2 not TTY for no-PTY commands, no inherited carrier/control FDs, `/dev/tty` absent, parent terminal restored |
| Dependency matrix | Actual Bash versions, GNU/BusyBox tool availability, unsupported capability fails before user command |

Zero filesystem objects must follow from the chosen primitives and bootstrap,
not just from a passing output test. Do not assert that arbitrary user commands
cannot write files; the broker itself must not introduce writes, and authorized
test commands must stay within the project's restrictions.

## Follow-up: raw status through Bash's job table

The ambiguity of numeric `wait` is **not** proof that every shell-only approach
loses signal information. Bash retains raw process status in its job table.
`printable_job_status` checks `WIFSIGNALED` and `WIFEXITED` separately; `jobs -l`
can therefore distinguish a signalled process from an ordinary exit with the same
numeric result. The inspected Bash 5.1 source also freezes the job list while
running a CHLD trap, preventing ordinary dead-job cleanup in that interval. Job
listing marks records notified; later cleanup can remove them. These are
implementation details to test across versions, not a machine-readable API
guaranteed by POSIX.
[Bash jobs.c, original source distributed by Debian](https://sources.debian.org/src/bash/5.1-2%2Bdeb11u1/jobs.c)

The builtin source supplies long, short, changed-only and PID-only formats but
no raw numeric wait-status format. It prints command text alongside status.
Therefore parsing must identify the saved child PID and status field, control
locale, and avoid interpreting the command string or another helper's job.
`jobs -n` is notification filtering, not a reliable per-child event queue.
[Bash jobs.def](https://sources.debian.org/src/bash/5.1-2%2Bdeb11u1/builtins/jobs.def/)

Local diagnostic evidence on 2026-10-20, using the already-built Bash 5.3 on macOS
arm64, no files or remote commands:

| Shape | Child action | CHLD trap calling `jobs -l` | Numeric `wait` |
| --- | --- | --- | --- |
| Background direct child, `set +m` | `exit 143` | `Exit 143` | 143 |
| Background direct child, `set +m` | `kill -TERM $$` | `Terminated: 15` | 143 |
| Saved-PID `coproc` with `exec` | `exit 143` | `Exit 143` | 143 |
| Saved-PID `coproc` with `exec` | `kill -TERM $$` | `Terminated: 15` | 143 |

The corresponding direct-child experiment with Apple's Bash 3.2.57 did not run
the CHLD trap during `wait`. That is a concrete compatibility difference, not a
result for Linux Bash versions. Direct `jobs -l` inside the Bash 5.3 trap succeeded.
Capturing it with `s=$(jobs -l)` returned the distinction too, but also generated
`run_pending_traps` warnings on this build. Do not adopt that capture path based
on its nominal output. Redirecting the builtin's output directly to a metadata
pipe avoids needing binary variables or files, but still requires a serializer
and lifecycle tests. A pipeline placed around `jobs` must not accidentally move
the lookup into a different shell whose job table is empty.

The proposed next prototype is a dedicated supervisor with a minimal CHLD trap
that records job metadata through the single carrier writer. Save the coprocess
PID immediately: Bash may unset its named PID variable after completion. Observe
the actual executed command, not a shell wrapper that first waits for that
command and exits numerically. Test immediate child death before PID assignment,
multiple simultaneous helper exits, stopped/continued children, notification
cleanup and trap reentry. An absent or unrecognized job record must preserve
"status unknown" rather than guessing.

### Keeping the executed child observable

For util-linux `setsid`, the fork decision depends on whether the caller is
already its process-group leader. With job control disabled, the candidate is
`exec setsid ...` from the coprocess child so that a no-fork execution preserves
the PID observed by Bash. This condition must be checked in the actual launch
topology. Forced forking or an already-leader child makes Bash observe the
`setsid` supervisor instead; its abnormal-exit conversion can lose the distinction.
Do not rely on `--wait` to restore information already flattened by that process.
[util-linux setsid source](https://raw.githubusercontent.com/util-linux/util-linux/master/sys-utils/setsid.c)

There is a separate signal-disposition issue: Bash's asynchronous setup without
job control ignores INT and QUIT. In the inspected Bash 5.3
`execute_cmd.c:setup_async_signals`, special asynchronous-ignore flags permit
resetting them within the coprocess before `exec`. A local diagnostic with
`trap - INT QUIT` before exec then a child self-SIGINT produced `Interrupt: 2`
and numeric wait 130. Resetting only after starting a fresh shell can be too late
if it inherited ignored signals. This requires Linux-version differential tests.
[GNU Bash 5.3 source archive](https://ftp.gnu.org/gnu/bash/bash-5.3.tar.gz)

`kill -0` is not a replacement for status inspection. It checks existence and
permission, not exit cause; an existing PID may already be a zombie. Disappearance
does not distinguish normal exit from a signal, and checking then signalling is
subject to process exit and PID reuse. Never infer terminal status from this probe.
[Linux kill](https://man7.org/linux/man-pages/man2/kill.2.html)

### Coprocess cancellation and EOF cleanup

Keep explicit ownership of every duplicated coprocess FD. Closing the controller's
stdin writer does not unblock a writer stuck on a full child-stdin pipe while a
reader remains alive. On cancellation, terminate the intended command group and
the independently blocked helper, close their owned descriptors, then reap each
saved PID; do not wait for the writer before taking the action that can release
it. Closing a read copy can instead trigger SIGPIPE in a still-running producer.
Keep normal stdin EOF distinct from cancellation: for EOF, close all corresponding
write copies and allow remaining output to drain. Background descendants retaining
either endpoint can delay EOF even after the direct child exits. These lifecycle
properties follow from pipe semantics and need explicit graph tests.
[Linux pipe semantics](https://man7.org/linux/man-pages/man7/pipe.7.html)

This follow-up leaves the shell-only status path **promising and locally
demonstrated for two cases**, not impossible and not yet verified on the Pi. It
also does not prove full SSH process-group, PTY or forwarding compatibility.
