# Explicit configuration files — 2026-10-20

`pi-sh -F path` reads an explicitly selected regular local root file and its
supported Include directives. No default
`~/.ssh/config`, system SSH config or pi-sh config is loaded. `-F none` disables
file loading; if repeated, the last `-F` wins. A destination is required when a
file or HostName override is used. Parsing and policy validation finish before
login, discovery or remote command execution.

```sshconfig
# local file, for example ./pi-sh.conf
Host workpi
    HostName raspberrypi
    EscapeChar none

Host batch-*
    HostName %h
    StdinNull yes
    RequestTTY no

Host * !special
    EscapeChar ~
```

```sh
pi-sh -G -F ./pi-sh.conf workpi
pi-sh --direct-login -F ./pi-sh.conf workpi
pi-sh --direct-login --experimental-command -F ./pi-sh.conf -nT workpi 'cat; exit 23'
```

The examples exercise local parsing or existing command capabilities; these new
config-file invocations have not yet been verified against the Pi.

## Implemented behavior

- Global settings and multiple `Host` blocks, whitespace-separated patterns,
  `*`, `?` and `!` negation. A matching negation excludes the block; a list of
  only negated patterns never selects a block. Patterns do not use filesystem
  character classes or path-separator rules.
- Host patterns are case-sensitive and match the destination exactly as typed,
  as local OpenSSH 10.2 does (`Host pi-*` does not select `PI-ONE`; an earlier
  revision wrongly lowercased the destination). HostName substitution does not
  trigger another Host evaluation. Wildcards operate on bytes.
- First obtained value wins within the file. Explicit CLI settings override file
  values. Existing mixed short-flag/`-o` precedence is preserved.
- HostName, EscapeChar, StdinNull and RequestTTY. HostName resolves an alias to an
  exact Connect device name or ID, never to a DNS address or an SSH endpoint.
- Case-insensitive keywords; case-sensitive values; whitespace or `=` separator;
  double-quoted arguments; blank lines; comments starting with `#` at a token
  boundary outside quotes; CRLF. A `#` attached to a value is part of that value
  in the local OpenSSH reference, so `StdinNull yes#comment` fails.
- HostName tokens `%h` (original destination) and `%%`, expanded once. Exact
  Connect name case and quoted internal spaces are preserved. This intentionally
  differs from OpenSSH's lowercasing/validation of DNS destination strings.
- `-o` uses the same directive/value grammar and validates the same fields
  (the four scalars plus Tag).
- `-G` reports hostname when a destination is present, plus escapechar, stdinnull
  and requesttty, plus tag when set. No authentication, DNS lookup or network
  connection occurs (Match exec still runs its local command, as in ssh -G).

## Include files

`Include` is available in files selected with `-F`, shared by all three clients.
For example, `Include ~/.ssh/pi-sh.d/*.conf` explicitly selects additional local
configuration; it does not enable automatic loading of ~/.ssh/config. Multiple
patterns are processed in written order, each glob's matches in lexical order.
Supported globs use `*`, `?`, ranges/classes and `[!...]` negation. Hidden names
need a literal leading dot; `[.]hidden` does not match a hidden file. No-match
patterns are ignored. Every selected source must be a readable regular file.

Each included file starts in the caller's Host context, shares first-obtained
settings, and restores the caller's context afterward. The next included file
also starts in that caller context. An inactive Include cannot activate settings
through a child's Host block; its files are still parsed/validated under this
project's strict policy. Nested relative paths are based on the local home's
`.ssh` directory, never the containing file's directory. Symlink/.. path
components are left for the filesystem to resolve.

Paths support `~/`, `${NAME}`, `%%`, `%d` (local home), `%n` (original destination)
and `%h` (effective HostName at that point, including a CLI HostName override).
Expansion is one pass; environment contents are not recursively interpreted as
tokens or shell commands. Include paths are local filesystem paths, not device
paths. The home comes from Go's os.UserHomeDir, which may differ from OpenSSH's
account database if the home environment is overridden. No environment variables
are modified by loading configuration.

Named-user tilde references, other Include tokens (including SSH user/port/key
context), POSIX named/collating classes, backslash grammar and malformed glob
brackets are not implemented and fail explicitly. `${NAME}` must be set; an unset
variable fails the whole load rather than silently skipping a directive.
Variable names are limited to nonempty ASCII letters, digits and underscores.
`-oInclude` and the reader-only Parse API do not load files. Full shell/locale/
Windows path equivalence is not claimed; native Windows/Linux and physical iSH
checks are pending. Connect name case remains exact in token expansion.

## Boundaries and differences

The root and all includes share a 1 MiB read budget and a 256-file budget, with
at most 16 nested Include levels. Repeated files count again; cycles are rejected
using file identity, including hard-link aliases. Include paths are bounded to
4096 bytes and 64 components, filesystem traversal to 16384 entry visits, and glob
results to 256 matches. Directory enumeration uses chunks of 128 entries, not a
whole-directory allocation. Lines remain limited to less than 64 KiB. Files open
read-only. Errors report a line number and fixed reason, not paths, contents or
OS exception text. Control characters in targets and expanded HostName values
are rejected. No credentials belong in config files.
Expanded HostName targets are limited to 4096 bytes, checked before appending
substitutions to avoid expansion-driven allocation spikes.

All lines, including inactive blocks and duplicate/overridden values, are
validated. Unsupported directives fail rather than silently discarding behavior.
This is stricter than OpenSSH for some inactive directives/values.
IgnoreUnknown, User, LocalCommand, ProxyCommand, RemoteCommand, CanonicalizeHostname
and all unlisted directives remain unsupported. Apart from Match exec (below),
no local command, shell expansion, general environment expansion, backslash
syntax, default file search or hostname canonicalization is implemented. Only
Include supports the documented environment/path expansion; HostName retains
its separate token set.

## Match, Tag and -P

`Match` follows local OpenSSH 10.2p1 (`ssh -G`), shared by all three clients:

```sshconfig
Match tagged work exec "test -e %d/.pi-work"
    EscapeChar none
Match host raspberrypi,!lab-* !localnetwork 192.168.1.0/24
    RequestTTY no
Match final originalhost pi
    StdinNull yes
```

- Criteria: `all`, `canonical`, `final`, `host`, `originalhost`, `user`,
  `localuser`, `localnetwork`, `tagged`, `version`, `sessiontype`, `command`,
  `exec`. Criterion names are case-insensitive; `!criterion` negates; criteria
  on one line are ANDed and all are validated even after one fails. Arguments
  may be written `criterion=value`. A token starting with `#` ends the line.
  `all` must stand alone (it may follow one criterion, as in OpenSSH).
- Arguments are comma-separated pattern lists with `*`/`?`; a matching `!`
  subpattern rejects the whole list; a list of only negations never matches.
  `host`/`originalhost` fold ASCII case on both sides; all other criteria are
  case-sensitive. An empty argument is an error except for `tagged` and
  `command`, where `""` matches only an unset tag/empty command.
- `host` uses the HostName-expanded target at that line; `originalhost` and
  Host blocks use the destination as typed.
- `user` and `%r` mean what OpenSSH means: the configured SSH user name
  (User/`-l`, when obtained) or otherwise the local login name. This is only a
  config evaluation input. A Connect session always runs as the agent's fixed
  account, which pi-sh neither selects nor verifies. `localuser` and `%u` are
  the local login name.
- `localnetwork` takes a CIDR list (no negation, no empty entries, no host bits
  set) and matches addresses of up local interfaces. Scoped (link-local) IPv6
  addresses are skipped, matching OpenSSH's observed behavior. Interface
  enumeration failure (possible on iSH, untested) is an explicit error.
- `version` compares `pi-sh_<version>` (as in `pi-sh -V`), never an OpenSSH
  version, so `Match version OpenSSH_*` is false by design.
- `sessiontype`/`command`: pi-sh reports `exec` with the command words joined
  by single spaces, or `shell` for no/empty command. pi-sftp and pi-cp report
  what OpenSSH sftp would request: subsystem `sftp`, or `exec` of the explicit
  server path given with `-s`/`--sftp-server`.
- `exec` runs `$SHELL -c command` (or `/bin/sh`) with stdin and stdout on the
  null device, stderr inherited and the environment unchanged; exit 0 matches,
  other statuses do not, and death by signal is an error. As in OpenSSH it
  runs only while preceding criteria on the line match, also inside inactive
  Include files, again during the final pass, and during `-G`. Tokens: `%%`,
  `%h`, `%n`, `%r`, `%u`, `%d`, `%i`, `%l`, `%L`. `%p`, `%C`, `%k`, `%j` have no
  pi-sh meaning (no SSH port, host key or jump host) and fail. Tokens are
  validated even when exec is skipped (stricter than OpenSSH). `%h`, `%n` and
  `%r` values containing whitespace or shell metacharacters are refused
  instead of being spliced into a shell (OpenSSH rejects such command-line
  names but splices config HostName values). Not available on Windows.
- `final`/`canonical`: a non-negated `final` anywhere (even on a failed line or
  in an inactive Include) requests a second pass over all files. Earlier
  values are kept; HostName is fixed to the effective target; `final` and
  `canonical` then match. Without CanonicalizeHostname support only Match final
  triggers the pass. The final target keeps exact Connect name case, where
  OpenSSH lowercases it.
- `Tag` sets the tag (first value wins; `none` is literal). pi-sh `-P tag`
  sets it from the command line, first obtained relative to `-o Tag`, and may
  be empty. pi-cp/pi-sftp keep `-P` for scp/sftp port semantics, so use
  `-oTag=`. `-G` prints `tag` when set.
- `-oMatch` is rejected like OpenSSH. Evidence: `TestMatchOpenSSHDifferential`
  (129 `ssh -G` fixtures), `TestMatchRejectsLikeOpenSSH` (55), and
  `TestMatchDocumentedDifferences`. Real-device and iSH verification pending.

RequestTTY yes/force now selects the experimental command TTY path with the
stdin-dependent policy in TTY_COMMANDS.md. Legacy TTY requests and interactive
no-TTY shells still fail before authentication. The broker's no-TTY
child semantics retain Connect's underlying PTY carrier. Authentication remains
explicit through `--direct-login` or `--browser`.

## Evidence

- `TestConfigFileOpenSSHDifferential`: twelve temporary-file comparisons of all
  reported fields using local OpenSSH_10.2p1 `ssh -G -F path`; Host selection,
  negation, first values, overrides, patterns, tokens, quoting and CRLF.
- `TestConfigRejectsLikeOpenSSH`: six malformed/unknown-config comparisons.
- `TestEscapeDumpOpenSSHDifferential`: all 127 non-NUL ASCII escape bytes.
  These exposed and fixed the output representations for space, backslash and
  DEL. The earlier ten CLI configuration comparisons also remain passing.
- `internal/sshconfig` tests: bounds, read failures, regular files, redacted
  errors, wildcard semantics, one-pass token substitution and refusal of
  unsupported/inactive directives.
- `TestAliasResolutionAndConfigRefusal`: resolved target passed separately from
  untouched command arguments, CLI/file selection, Connect names with spaces and
  `#`, and incompatible command TTY refusal.
- `TestIncludeOpenSSHDifferential`: 18 offline comparisons for ordering, nested
  scopes, inactive blocks, quoting/globs/dotfiles, tokens, CLI HostName precedence,
  environment literals, tilde and home-relative paths (including a nested file).
  Home-relative reference cases skip if the existing ~/.ssh directory is absent;
  tests never create or change it.
- Include module fixtures cover aggregate byte/file/depth/visit/match limits,
  cycles, symlink/.. traversal, special-file refusal, override-map ownership and
  redacted errors. Both transfer frontend tests exercise an included no-TTY policy
  failure before login; the shared destination test verifies CLI-dependent Include.

These tests use synthetic files and no network, except separate pre-existing
transport/authentication tests. No Pi software, configuration or files are
changed. Real-device alias/config integration, native Linux/Windows and physical
iSH runtime verification remain pending. The behavioral references are the
[OpenSSH configuration manual](https://man.openbsd.org/ssh_config.5) and local
OpenSSH executable, not copied upstream implementation code.

A fresh Chrome DevTools browser inventory attempt after implementation timed out
after 60 seconds before reaching the authorized browser. No Pi session was
created. This blocks that native check; it does not demonstrate a config,
transport or Connect protocol failure, and does not block further local work.
