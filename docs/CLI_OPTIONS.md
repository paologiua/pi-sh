# CLI option subset — 2026-10-21

Experimental command TTY yes/force now uses the staged Bash adapter through the
CLI terminal/resize coordinator. [TTY_COMMANDS.md](TTY_COMMANDS.md) records the
exact policy, local evidence and remaining mode/lifecycle/native gaps.

The CLI now uses an explicit bounded parser instead of Go's flag parser.
This is a partial behavioral implementation, not an SSH transport. The reference
is [ssh(1)](https://man.openbsd.org/ssh.1) and
[ssh_config(5)](https://man.openbsd.org/ssh_config.5), checked on 2026-10-20;
ten local differential invocations use the installed OpenSSH_10.2p1.

```sh
pi-sh --direct-login --experimental-command -nT device 'cat; exit 23'
pi-sh --browser -enone device
pi-sh -G -Fnone -o 'EscapeChar = "none"' -oStdinNull=yes device
```

The first command supplies immediate stdin EOF without reading local stdin.
Direct login still prompts on the controlling terminal; `-n` is not unattended
authentication. The broker gives the command pipe descriptors and no controlling
terminal, while Connect's shell carrier still has a PTY.

| Option | Behavior and boundary |
| --- | --- |
| `-e value`, `-evalue` | Existing ASCII/control-notation/none escape selection |
| `-n` | No local stdin reads; no-TTY broker sends EOF; forced TTY stops input without inventing Ctrl-D; auth may still prompt |
| `-T` | Explicit no-TTY command through the broker; interactive no-TTY shell and legacy adapter reject it |
| `-t`, `-tt` | Experimental broker command TTY: -t requires terminal stdin without -n; -tt forces it, including pipes/-n; legacy TTY requests rejected |
| `-o` | HostName, EscapeChar, StdinNull and RequestTTY; keywords case-insensitive, values case-sensitive |
| `-F path`, `-Fpath`, `-F none` | Read explicit root/Include files, or disable file loading; no default root config files |
| `-V` | Offline unofficial build version on stderr; immediate exit before file/auth/network setup |
| `-Q query`, `-Qquery` | Offline help/namespaced client capabilities on stdout; SSH algorithm queries explicitly refused; LOCAL_INSPECTION.md |
| `-G` | Print hostname (with a destination), escapechar, stdinnull and requesttty; exit before login/discovery/network |
| Clusters and `--` | Supported for the listed short options; operand-taking options consume the rest of the cluster or the next argument |

`-o` accepts a single scalar with `Keyword=value`, `Keyword value`, whitespace
around `=`, and optional surrounding double quotes. Duplicate directives use the
first obtained value. Short flags assign directly and take precedence over later
directives for the same field, matching the tested OpenSSH cases. Unsupported
directives and invalid duplicate values still fail. Double quotes, comments at
token boundaries and Host blocks are supported; see
[configuration files](CONFIGURATION.md) for the complete current grammar and
boundaries. Include has bounded file/path/token support; Match and backslash
escaping remain unimplemented. Include does not become an accepted -o scalar.

Supported options can precede or follow the first destination, until the first
non-option command word. After that word all later tokens remain command operands.
An explicit `--` ends option parsing globally, whether before or after the host;
it is removed as a separator. Thus `device -T printf -n` selects no TTY while
retaining `-n` as a printf argument; `device -- -T` passes `-T` as command text.
A later `--` after a command word remains part of the command. Unknown options
before command start are rejected before authentication rather than sent remotely.
Custom --browser/--direct-login/adapter flags follow the same boundary. -V/-Q
still exit immediately, including after the host, before file loading or auth.

The broker/legacy command remains an explicit opt-in; command arguments are joined
with single spaces as previously. user@host/URI/address mapping and the general
option inventory remain gaps. Aliases use Host/HostName in an explicit file or
`-o HostName=value`. No default SSH or pi-sh config is read.

`-G` is a subset report, not OpenSSH's complete output. It can display a requested
TTY policy independently of whether a non-forced request is suppressed by stdin.
Legacy incompatible policies fail before authentication. It accepts an omitted
destination when no file or HostName override is selected.
It deliberately reports no SSH key, algorithm or host-key properties.

## Evidence

- `TestOptionsOpenSSHDifferential`: ten offline `ssh -G -F none` comparisons of
  reported fields, compact/separate operands, quoting, repeated directives,
  mixed short/directive precedence and TTY ordering. No SSH server or Pi used.
- `TestOptionsBoundariesAndValidation`: exact command argument boundaries,
  option terminator and refusal of unsupported options/modes or malformed values.
- `TestStdinNullNeverReadsInput`: a reader that fails the test if touched verifies
  null stdin; ordinary stdin preserves binary bytes. Existing broker tests
  independently cover remote EOF and binary streams.
- File-based Host/HostName behavior, bounds and error cases are separately
  tested as described in [CONFIGURATION.md](CONFIGURATION.md).

New option combinations have local evidence only. No new Pi session, file write,
transfer or forwarding test was performed in this increment. Existing native
broker evidence does not establish native `-n`/`-T` CLI verification.
An attempt to inspect the authorized browser with Chrome DevTools `list_pages`
timed out after 60 seconds before reaching a browser page. No CLI/Pi connection
was attempted. Native confirmation requires restored DevTools connectivity or an
owner-run direct-login command; this is a test-access blocker, not an observed
Connect protocol limitation.

## Offline inspection

Version and query options terminate processing immediately, following the locally
measured OpenSSH10.2p1 ordering. An earlier malformed option still fails; a valid
-F selection is not opened for -V/-Q, and subsequent tokens are unprocessed.
Queries list compiled implementation rather than per-device support. They use
no authentication mode and cannot execute local/remote commands. See
[LOCAL_INSPECTION.md](LOCAL_INSPECTION.md) for names, redaction, tests and gaps.

Thirteen additional offline OpenSSH_10.2p1 comparisons cover post-destination
TTY/stdin/escape options, cross-boundary precedence/clusters, explicit separators
and command operands. Separate subprocess checks cover post-host immediate -V/-Q,
missing config bypass, custom modes, unsupported/missing operands and explicit
-F aliases after the host. No network/authentication is used by these comparisons.
