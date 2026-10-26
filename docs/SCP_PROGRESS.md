# scp-style file progress — 23 October 2026

`pi-cp` now displays a per-file progress meter on stdout when stdout is a
terminal, matching OpenSSH scp's behavior. No new command flag is needed.
The meter shows the source basename, percent, bytes transferred, smoothed rate,
ETA while incomplete and elapsed time on completion. It refreshes on one line
once per second and on Unix SIGWINCH, reports `- stalled -` after five seconds
without progress, and ends each file with a newline. Recursive copies display
each regular file separately, with no aggregate directory percentage. Empty
files display 100% with zero bytes.

`-q` disables progress regardless of later `-v` flags. Connection `LogLevel=QUIET`
does not disable the meter. Redirecting/piping stdout disables it even if stderr
is still a terminal; redirecting stderr alone does not. On macOS/Linux, output
is further limited to the terminal's foreground process group. Unknown window
size defaults to 80 columns; output is capped at 512 columns and truncated by
display width. The implementation does not change terminal modes or file flags.

## Implementation and verification

The independently authored `internal/progress` display is separate from copying.
`pi-cp` installs it on the copy plan; the shared SFTP regular-file path counts
only bytes accepted by the destination writer, retaining the existing 32 KiB
copy buffer and mandatory write-status checks. The optional meter is not enabled
for `pi-sftp` or backend users by default. Partial/error transfers keep their
actual percentage and finish the line; progress never forces the counter to the
expected size. Reaching 100% describes data copied, not successful final metadata
or file close. Display failure does not erase or invent a transfer error.
The renderer is joined at transfer end and does not own or close stdout.

Behavior references are the installed OpenSSH_10.2p1 and the upstream
[progressmeter.c](https://github.com/openssh/openssh-portable/blob/master/progressmeter.c)
(OpenBSD revision 1.57),
[scp.c](https://github.com/openssh/openssh-portable/blob/master/scp.c) and
[utf8.c](https://github.com/openssh/openssh-portable/blob/master/utf8.c), observed
on 23 October 2026. These source links move; the native test reference version
is recorded separately. No OpenSSH source is copied or linked.

`TestProgressAgainstLocalSCP` compares the actual local scp executable (with its
local `-D sftp-server`, no SSH connection) and the meter in foreground PTYs.
Fourteen fixtures cover normal/empty files, byte-unit scaling, long/control/UTF-8
names, C and UTF-8 locales, narrow/default/capped widths, redirected stdout,
`-q`, `-qv` and `-oLogLevel=QUIET`. Only variable rate and duration fields are
normalized; the remaining output bytes, spacing, CR updates and newline must
match. Deterministic state tests cover rate/ETA, the five-second stall, final
elapsed time, partial transfer and offsets above 2 GiB. Local actual-server
recursive upload/download tests verify one completed line per file and exact
binary contents; failure tests retain short-write errors and exact byte counts.
`TestProgressTerminalQueriesPreserveDescriptorFlags` verifies that terminal
detection, sizing and foreground queries use SyscallConn.Control without changing
a Go-pollable terminal's nonblocking flags.
The progress, copy, SFTP and pi-cp packages pass with the race detector on macOS.

## Portability and remaining limits

The implementation is pure Go. The CGO-disabled Linux/386 softfloat build is
required, including progress and test-package compilation. x/text/width v0.42.0
(already selected transitively before this change) supplies Unicode East Asian
width data and is now an explicit dependency. Native Windows and physical iSH
progress display remain unverified; a successful iSH shell does not verify file
transfer or the foreground-group ioctl.

UTF-8 names use Go Unicode tables with East Asian wide/fullwidth characters
occupying two columns and combining marks zero. C/non-UTF-8 locales escape
non-ASCII bytes as octal. Nonprintable/invalid bytes and format controls are
escaped; filename formatting is bounded. Platform libc Unicode versions,
non-UTF-8 locales other than C, emoji/grapheme rendering and zero-width format
characters can differ from native scp. Windows has no Unix foreground-group
check. These are known parity limits, not a claim of byte-identical behavior in
every locale/terminal. The rate measures the existing copy/write pipeline;
its throughput and acknowledgement granularity differ from OpenSSH's pipeline.

No new Pi transfer write was performed. Native pi-cp/iSH progress validation is
pending; any future Pi write test still requires an approved temporary location
and cleanup under the project's restrictions. Bandwidth limiting and pi-sftp's
progress UI remain unimplemented.
