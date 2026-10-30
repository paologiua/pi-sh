# Glob and tilde handling in pi-sftp and pi-cp

This is a local implementation milestone. It was compared only with the
installed OpenSSH_10.2p1 `sftp`/`scp` (macOS, LibreSSL 3.3.6) talking to the
local `/usr/libexec/sftp-server` through `-D`, in temporary Mac directories. No
Raspberry Pi or Connect session was used; native Pi verification is pending.

## Expander (`internal/pathglob`)

A port of the OpenBSD glob(3) algorithm used by OpenSSH: `*`, `?`, `[...]`
with `!` negation, ranges and the twelve POSIX classes (C locale, per byte),
backslash escapes, a leading `.` that must be matched literally (an escaped
`\.` counts, as observed), GLOB_MARK (directories and links to directories get
`/`), GLOB_NOCHECK, GLOB_NOSORT and, for `ls` only, csh braces including
OpenBSD's unbalanced-brace behaviour. Segments without metacharacters are
appended literally and checked with one final lstat, so `link/../file` is
resolved by the server, not cleaned locally. Only segments with
metacharacters list a directory.

Remote directories are read with the fork's `ReadDirEach`, one NAME packet per
open directory level; no listing is retained. Matched paths are retained (as in
OpenSSH). SFTP status failures (missing, denied, not a directory) mean "no
match", like glob(3) without an error callback; transport/protocol failures and
cancellation stop the expansion. `ReadDirEach` omits `.` and `..`, so the
expander supplies them for a segment beginning with `.`, as a conventional
server would. OpenBSD's GLOB_LIMIT bounds are implemented as an option but not
used: OpenSSH 10.2 expanded 3,000 long matches in `ls`, `get` and `rm`.

Tilde is not a glob feature (OpenSSH does not pass GLOB_TILDE).

## pi-sftp console

The tokenizer now follows sftp's makeargv: inside quotes, and when
backslash-escaped outside them, `*`, `?`, `[` (and an escaped `\`) stay escaped
for glob; inside quotes, a backslash before another character is kept (so
`'a\b'` globs as `ab`, as in OpenSSH); `'\''` escapes only the enclosing quote;
an unquoted `#` ends the line even inside a word.

| Command | Expansion |
| --- | --- |
| `get`/`reget` source | Remote, GLOB_MARK; no match: `File "..." not found.` |
| `put`/`reput` source | Local, GLOB_NOCHECK\|GLOB_MARK (missing literal fails on source stat) |
| `ls` | Remote, MARK\|NOCHECK\|BRACE\|NOSORT, sorted unless `-f`; one matched directory is listed |
| `rm`, `chmod` path | Remote, GLOB_NOCHECK; batch without `-` stops at first failure |
| destinations, `cd`, `lcd`, `mkdir`, `rmdir`, `rename`, `symlink`, `readlink` | Not globbed; escaping removed |

Several matches into a destination that is not a directory fail before any
transfer, with OpenSSH's wording. Every match is attempted; each failure is
printed with its path and the command fails. Directory matches of `get`/`put`
fail as "not a regular file" without recursion. `mget`/`mput` share the same
expansion; console recursion is documented in SFTP_CONSOLE_RECURSION.md. `ls` of glob results prints paths relative to the working directory with the
`/` mark; directory arguments now preserve their requested prefix. -1 selects
one name per line; other listing modes follow SFTP_CONSOLE_COMMANDS.md. Remote `~` is literal, exactly as OpenSSH 10.2 sftp (`cd ~` and
`ls ~` fail in both).

## pi-cp

Remote download operands are globbed like scp's SFTP mode
(GLOB_NOCHECK|GLOB_MARK, then a stat of an unmatched literal). Several matches
into a missing local destination create it (mode 0777 minus umask) as scp does;
into an existing non-directory they fail. With `-r`, directory matches are
copied as trees; without it they fail as "not a regular file" while other
matches continue. Local operands and the remote upload target are literal: the
local shell expands local globs, as for scp.

Operands are recognised only by `internal/sshdest` (scp grammar and
`scp://` URIs); the glob/tilde rules below apply to the parsed remote path.
Remote `~` and `~/` handling follows scp's prepare_remote_path: `~` and `~/`
are the server's starting directory and `~/x` becomes the relative `x`, so the
result is the home directory only if the server starts there (the same
assumption scp makes). `~user` requires `expand-path@openssh.com`; the vendored
pkg/sftp fork has no generic extended-request API, so pi-cp rejects `~user`
before login rather than modify the fork. This is the main explicit gap.

## Known differences

- `~user` remote paths are rejected by pi-cp (OpenSSH uses expand-path).
- A trailing unescaped backslash is rejected; OpenSSH reads past the line end.
- `ls` with a brace alternative that matched nothing next to one that did
  (`ls r/{a1,zz}`) crashes OpenSSH 10.2 (exit 139); pi-sftp prints the matches
  and fails with `Can't ls: ".../zz" not found`.
- `rm` refuses directories after an lstat because pkg/sftp's Remove would also
  remove an empty directory; a directory substituted after the check remains a
  race. OpenSSH sends only REMOVE and reports the server's error.
- `rm`/`chmod` fail before any change if the expansion itself fails (e.g. lost
  connection); OpenSSH ignores remote_glob's status and uses a partial list.
- Fetching/Uploading, recursive headings and Changing mode/owner/group notices
  now follow the local OpenSSH reference, including batch/-q/-N policy.
  Removing notices and exhaustive error/locale/platform parity outside the new
  command fixtures remain broader compatibility work. Server-supplied status
  text is always replaced by fixed vocabulary.
- Bracket sets containing `/` are split at the `/` (undefined in OpenBSD).
- Windows: `put` does not glob locally (`\` is a separator there); the argument
  is used literally after removing sftp escaping.
- Like scp, pi-cp reports a failing download operand and continues with the
  next (since 2026-10-28; before it stopped); a missing local upload source
  ends the copy with exit 255, as scp. Within one glob operand every match is
  attempted.

## Evidence

- `internal/pathglob`: 40-case semantics table, order/mark, error/fatal/
  cancel, limit and escape tests; 20-second fuzz run (about 4.3 million
  executions, no failure).
- `TestGlobCommandsAgainstLocalOpenSSH`: 56 commands through our tokenizer and
  console and through `sftp -D ... -b -`, comparing status, `ls -1` output and
  resulting trees (contents, modes, link targets).
- `TestManyGlobMatchesAgainstLocalOpenSSH`: 2,100 matches succeed in both.
- `TestRemoteGlobAgainstLocalSCP`: 20 pi-cp download cases including `-r`,
  destination creation, escapes, `.*` and `~/` against `scp -D`.
- `TestSplitKeepsOpenSSHGlobEscaping`, `TestMultipleMatchFailuresAreEachReported`,
  `TestRemotePathTilde`.

Native Raspberry Pi, Linux client, Windows and iSH runtime checks are pending.
