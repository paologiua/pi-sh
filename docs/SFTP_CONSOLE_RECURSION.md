# SFTP console transfers — local milestone, 2026-10-30

`mget` and `mput` are aliases of `get` and `put`, as in OpenSSH 10.2p1:
one source pattern and an optional destination, not a list of source operands.
The existing tokenizer/glob rules, multi-match preflight, batch prefixes and
per-file errors also apply to the aliases. Extra operands are refused;
OpenSSH silently ignores operands beyond the destination. This deliberate
validation difference prevents accidentally treating a source list as a copy.

Console `get`/`put`, their `mget`/`mput` aliases and `reget`/`reput` accept
`-r` or `-R` for recursion, including combined flags. Top-level `pi-sftp -r`
enables recursion for all transfers. Top-level `-R` still means request-count
tuning and is refused; it does not mean recursion. `--` and the first operand
end command flag parsing. Regular files still work with recursion enabled.

The source pattern is expanded before copying. An explicit existing destination
directory receives the source basename; a missing destination is the new tree
root. With no destination, that basename is used directly, including an existing
same-named directory. A source ending in `/` retains its basename; `/.` merges
contents. A multiple-match explicit destination must already be a directory,
checked before any transfer. Paths are never evaluated in a shell.

## Traversal and metadata

A top-level source symlink can select a regular file or a directory. Inside a
recursive tree, source symlinks (including dangling links and cycles) and special
entries are skipped with a sanitized warning, without making the command fail.
They are neither recreated nor followed. This differs from `pi-cp -r`, which
continues to follow source links. The shared traversal has an explicit console
policy; the SCP entry points retain their existing behavior.

Ordinary child copy/status errors are reported and siblings are attempted,
including siblings outside a failed nested directory. The command still fails
at the end. Cancellation, transport loss and malformed protocol stop traversal;
partial results remain. Directory depth is bounded to 64; existing ancestor
identity checks remain. Source/destination trees must be stable: lstat/open and
path-based directory metadata have replacement races. There is no snapshot,
rollback, atomic copy, hard-link identity or protection against concurrent edits.

`-a`, `reget` and `reput` apply existing resume rules to each regular child.
Download may create missing files; upload resume requires each target file to
exist and be strictly shorter. Missing/equal upload children fail without
preventing other children or empty directories from being attempted. Prefixes
are trusted, never compared. `-f` requests a flush on each regular child with
the existing negotiated/advisory policy; no directory fsync is promised.

`-p` applies regular-file preservation and directory timestamps. Upload restores
ordinary source directory modes even without `-p`; download only restores modes
when temporary owner write/execute bits were needed, so an existing directory
may retain its mode with `-p`. These are the shared SCP/OpenSSH SFTP directory
rules described in SCP_RECURSION.md. Private creation checks remain mandatory.
ACLs, ownership, special directory bits and missing-attribute parity remain open.

Downloads stream remote directory packets (one bounded packet per ancestor);
uploads enumerate 64 entries at a time. Glob matches are retained in memory.
The memory bounds in SFTP_LISTING.md apply; they do not prove iSH runtime support.
Windows local glob expansion remains unavailable and Windows traversal/runtime
semantics are unverified. Linux/386 builds with CGO disabled remain required.

## Local evidence and remaining work

`TestRecursiveConsoleAgainstLocalOpenSSH` compares 96 console cases with the
installed OpenSSH 10.2p1 `sftp -D /usr/libexec/sftp-server -b -` on the Mac.
It compares success/nonzero outcome and resulting tree entries, contents, modes
and link targets; selected `-p` cases also compare directory and child mtime.
Cases cover aliases, both directions, command/global recursion, `-R`, three
source suffixes, missing/existing/default destinations, globs, top-level links,
contained file/directory/dangling/cyclic links, binary/hidden files, empty and
read-only directories, resume/flush/preserve combinations, nonrecursive refusal,
file/directory conflicts and continued siblings after a nested conflict.
It does not establish complete diagnostic or numeric exit-code parity.

`TestRecursiveCommandFlagsAndCancellation` checks flags and cancellation before
filesystem access. `TestRecursiveErrorClassificationAndRedaction` checks ordinary
versus fatal errors, joined failures and synthetic-secret redaction, including
errors printed while continuing siblings. Existing
SCP differential and file-transfer failure tests remain required regressions.
All writes are to temporary Mac fixtures. No Pi session was opened and no new
native evidence is claimed for aliases or console recursion. Existing native
SCP/shared-backend evidence does not verify these new console paths. Native
plans already completed do not authorize repeating them.

References: [OpenSSH 10.2p1 command dispatch](https://github.com/openssh/openssh-portable/blob/V_10_2_P1/sftp.c)
and [directory transfers](https://github.com/openssh/openssh-portable/blob/V_10_2_P1/sftp-client.c).
Implementation and tests are independently authored; no upstream code, remote
software or dependency was added. Remaining console commands/options, sparse
and large files, bandwidth/progress and platform/native failure coverage stay
open in SSH_COMPATIBILITY.md. This milestone does not close the product backlog
or the separately recorded failed local WebRTC gate.
