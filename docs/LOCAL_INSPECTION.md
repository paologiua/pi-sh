# Offline version and capability queries — 2026-10-21

All three clients support `-V` and `--version`, print one line on stderr and
exit 0 without login, configuration reads or network access. pi-sh also retains
its capability queries. The line includes the client name, project version
(v0.1.0-alpha.1, plus safe Git revision/dirty metadata when available), Go build version, GOOS/GOARCH target and
the unofficial marker. This does not claim a released version or physical iSH runtime
support. Only a validated Git revision and dirty bit are read from build settings; VCS
paths, other build settings and environment values are not printed.

pi-sh -Q help lists the query names on stdout. The available queries are:

| Query | Meaning |
| --- | --- |
| help | Query names, sorted one per line |
| pi-sh-auth | Implemented direct-login and browser-companion modes |
| pi-sh-command | Implemented interactive, experimental TTY/no-TTY and legacy command paths |
| pi-sh-config | Recognized configuration keyword subset; grammar/precedence limits remain in CONFIGURATION.md |
| pi-sh-escape | Implemented action names, independent of the configured prefix; suspend is included only on macOS/Linux builds |
| pi-sh-transport | connect-webrtc; no SSH wire transport is implied |

Values describe compiled client implementation, not device authorization, remote
software availability, native-test coverage or certification of another platform.
TTY modes, cleanup and other gaps remain in SSH_COMPATIBILITY.md. Neither query
loads configuration, prompts, discovers devices or opens a network connection.
No SSH algorithm query is mapped to DTLS/TLS/SCTP algorithms. cipher, key,
protocol-version and unknown queries fail255 with a fixed -Q help diagnostic,
without echoing the supplied query. Errors/short writes also fail rather than
reporting a successful inspection.

## Parsing and reference

The identified reference is installed OpenSSH_10.2p1 / LibreSSL3.3.6. Local
execution confirms stderr/version, stdout/query and immediate termination:
the first encountered -V or -Q query ends option processing. Valid earlier -F
selection is not opened; already-invalid earlier options still fail. Later tokens
are unprocessed because the information operation has terminated. For example,
-V -z reports the version, whereas -z -V fails. -G -V reports the version;
-Qhelp -V reports query names. Operand-taking -Q accepts attached/separate values;
-Q -V queries the unsupported name -V rather than printing a version.

pi-sh now resumes option parsing after the first destination until a command
word or explicit --. Post-host -V/-Q therefore retain immediate exit; later
-V/-Qhelp tokens after the command word or -- remain command operands. Thirteen
post-host configuration comparisons and explicit inspection/operand tests cover
this boundary; general unimplemented options remain refused.
`--version` is an alias of pi-sh -V, with the same immediate-exit behavior.
pi-cp and pi-sftp now also accept -V/--version, before their first operand or --.
Their normal option parsing validates all supplied flags before the version exit;
valid -F paths are not opened and authentication is skipped. Invalid earlier or
later options still fail. Their -V/--version flags are project extensions: no
OpenSSH scp/sftp version flag parity is claimed. The `version` console command
continues to report SFTP server protocol information, not the CLI version.

## Evidence and current validation boundary

TestOfflineInspectionOrderMatchesOpenSSH compares ten actual offline reference
invocations by exit status and stdout/stderr destination, without asserting that
the tools have the same feature names/version. Executable subprocess tests also
cover all six queries, short-option clusters, nonexistent -F paths, no auth mode,
conflicting auth flags bypassed by immediate version inspection, unknown query
redaction and invalid earlier options. Command-operand boundaries and failed/short
writes have separate regressions. Runtime suspension availability is conditional
on the compiled target, with Linux/iSH runtime evidence still separate.

Focused CLI/config/parser race tests pass, along with the sshconfig and cliargs
package race suites. Seven CGO-disabled builds and all test-package compilations
pass for macOS amd64/arm64, Linux amd64/arm64/386 softfloat and Windows amd64/arm64.
Vet passes, all three native clients are rebuilt, and direct native -V/-Qhelp
invocations emit the expected version/query list. These are local checks;
physical iSH and other host runtimes are not inferred from compilation.

The earlier approval-review quota prevented one resize attempt, but that local
regression gate was subsequently completed and native command resize/control
checks are recorded in REAL_DEVICE_TESTS.md. The later input-frame exhaustion
recurrence was separately reproduced and corrected in TTY_COMPLETION.md. Current
validation and remaining native/runtime gates are listed in TESTING.md.

## Suite version flags — 31 October 2026

Shared internal/buildinfo formatting gives all three clients v0.1.0-alpha.1 and
keeps the pi-sh version token shared with Match version. TestOfflineVersionDoesNotLoadConfigOrAuthenticate covers all
three clients, including missing configuration paths, conflicting auth flags and
quiet output. Buildinfo tests cover client identity, invalid build tokens and
failed/short writes without leaking writer diagnostics. These changes have local
checks only; they do not establish a release, Pi or iSH runtime verification.
