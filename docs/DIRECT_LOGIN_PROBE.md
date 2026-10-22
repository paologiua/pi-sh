# Direct HTTPS login probe — 2026-10-20

## Latest HTTPS failure investigation

The owner reran the read-only pi-sftp batch after the private-creation setup
change and reported `web login HTTPS request failed`; no exit status was
reported. That attempt does not verify the new SFTP startup or identify a server
failure. A credential-free `go run ./cmd/login-probe` reproduced that exact
diagnostic in the development environment. With fixed, allowlisted error
categories it reported `web login HTTPS request failed (DNS)` before credentials.
This is evidence about that probe environment, not proof of the owner's cause.
The final rebuilt `./bin/login-probe` subsequently returned 0 with
`Preflight: password_required`, still without credentials. Thus the observed DNS
failure was intermittent; no persistent DNS/service outage or login-code fix is
claimed. The diagnostic change classifies future failures, not a DNS repair.
The owner then ran the rebuilt credential-free probe and reported
password_required with exit status 0. A subsequent owner-run direct-login
read-only SFTP pwd/quit batch also returned 0, including the private umask-prefix
startup. These recoveries do not classify the earlier failure.

Login transport diagnostics now distinguish DNS, timeout, TLS certificate
verification, connection and other transport failures. Body read/size failures
have separate fixed categories; HTTP failures retain numeric status only.
Neither underlying errors nor request URLs, proxy addresses, response bodies or
credentials are printed. Tests inject synthetic secrets into failing requests
and check exact safe output. Certificate verification, origin/redirect policy,
request limits and absence of automatic credential retries are unchanged.
Run `./bin/login-probe` without `--login` to check navigation without submitting
credentials. A failure there precedes SFTP; success is only password-form
navigation, not authenticated access or SFTP startup. The subsequent read-only
SFTP success is separate owner-operated evidence in REAL_DEVICE_TESTS.md.

Explicit CLI config/HostName aliases are now resolved before authentication;
see CONFIGURATION.md. They do not alter the HTTPS login/signaling policy or make
config-file paths, credentials or command input interchangeable. Existing native
direct-login evidence below used the exact device name; alias integration has
local evidence only. File-based Include is now shared by all three clients;
its token/scope/CLI-precedence tests finish before authentication. No fresh native
Include/alias login is inferred from the prior direct-login checks.

The same direct-login implementation is now shared by pi-sh, pi-cp and pi-sftp in
internal/connectsession. File-transfer protocol and batch input use separate owned
pipes; credentials still use the controlling terminal. The owner-run pi-sftp
read-only metadata batch returned 0 through direct login and Connect. The later
process-local umask startup also passed an owner-run pwd/quit batch, status 0;
file-content transfers remain
unverified natively. See REAL_DEVICE_TESTS.md for the precise evidence boundary.

`cmd/login-probe` investigates the normal Connect web login with a fresh,
process-local cookie jar. It imports no browser cookies and does not register
pi-sh as an OAuth application. It is separate from the working browser-assisted
CLI. The device owner ruled out contacting Raspberry Pi; no request was sent.

## Real observations

An isolated browser followed the ordinary Connect sign-in form. Its credential
form is POST `https://id.raspberrypi.com/session`, with hidden `authenticity_token`,
`email` of type email, `password` of type password, and submit `commit`.
The owner manually entered credentials on the official ID page. Only form
structure was inspected, never input values. The resulting page at `/session`
contains a URL-encoded POST form to `/session/verify`, with hidden
`authenticity_token`, required text `otp` with `autocomplete=one-time-code`, and
submit `commit`. No code was entered or submitted by the investigation agent. The browser
request metadata shows HTTP 401 for POST /session while rendering that legitimate
MFA form.

The actual Go probe, using its own fresh memory-only session and no JavaScript,
completed credential-free Connect/ID navigation and reported
`Preflight: password_required`. This proves the initial navigation works over
ordinary HTTPS; it does not establish password, MFA, callback or shell success.
Only stage identifiers were retained; no live HTML, cookies or headers are
stored as fixtures.

## Run locally

```sh
go build -o bin/login-probe ./cmd/login-probe
./bin/login-probe
./bin/login-probe --login
```

The default mode never asks for credentials. `--login` requires a local interactive
terminal: enter email, password and the TOTP at its individual prompts. Password
and TOTP echo is disabled. Do not provide credentials in chat, flags, environment
variables or redirected input. No automatic credential retry or recovery flow is
implemented. Known rejection, changed forms or unsupported challenges stop the
probe; a successful probe reaches the Connect devices page and exits.

This is not yet `pi-sh login`: its cookies disappear when the process ends.
It cannot authorize a later pi-sh process, does not test remote-shell permission,
and has not been connected to native signaling. A later implementation must
perform login and connection in one process or separately design secure session
storage. No such persistence is introduced here.

## Implemented boundaries

- Exact HTTPS Connect/ID origins, default certificate/hostname verification,
  bounded redirects, 30-second requests and 2 MiB HTML bodies.
- A private cookie jar with public-suffix validation; no export/serialization.
- Server-provided hidden controls and normal Connect initiation. No fabricated
  OAuth client, callback, verifier or grant. Password and OTP submit to the
  exact observed ID actions; no credential-body redirect replay is allowed.
- Only the observed form subset is supported. Ambiguous matching forms,
  duplicate control names, alternate encodings, base URLs, submitter overrides,
  select/textarea and fieldset semantics fail closed. Arbitrary web forms are
  not supported. Hidden/submit values are trusted only within validated forms.
- Network errors are sanitized rather than exposing callback URLs or response
  text. Unknown MFA/recovery/enrollment forms are rejected.
- Devices-page recognition requires the exact Connect path, an observed UUID
  device-link shape and no password/email/OTP form. Its status explicitly means
  page observation, not proven shell authorization.
- Unix handled signals and timeout restore terminal state; Windows console
  close/logoff and physical iSH behavior remain untested.

Input byte buffers are cleared after use, but Go form encoding creates transient
string copies. Memory-only handling is not a guarantee of perfect zeroization or
protection against a compromised local account/host.

## Verification

Local TLS fixtures with synthetic credentials cover password plus optional OTP,
fresh cookie continuity, form-derived CSRF, ordinary callback redirects, unknown
challenges/rejection without retries, credential-body redirects, origin allowlist,
error redaction, public-suffix cookies and HTTP200 login-page false success.
Review regressions failed before correcting ambiguous/changed forms, generic OTP
acceptance and empty device IDs; the corrected package passes under `-race`.
Test certificates are explicitly trusted in tests; TLS verification is retained.

Real password/MFA/callback completion was subsequently verified by the owner;
see the final dated integration section below.
Linux/Windows and Linux/386 builds are separate from runtime proof; physical iSH
has not been tested. See [DIRECT_LOGIN_DESIGN.md](DIRECT_LOGIN_DESIGN.md) for the
remaining design and test requirements.

## Operator attempt and HTTP 401 regression

The first operator-run `--login` attempt failed after the password with the fixed
message `web login HTTPS request failed`. The browser's successful transition to
MFA used HTTP 401, which the first probe rejected before parsing the challenge.
A local fixture changed only that status and reproduced the exact failure. The
fix accepts 401 only for the password POST response containing exactly one valid
observed MFA form; an unknown 401 form remains rejected. The package passes under
the race detector. Native retry is required before claiming the cause fully
resolved or password/MFA completion successful. HTTP failure diagnostics now
include only a numeric status, never URL or response text.

After the 401 correction the owner reported seeing the preflight and experimental
notice without the email prompt. A local real PTY did show the email prompt and
Ctrl-C cancellation. Prompt labels now end with newlines to accommodate consoles
that buffer incomplete display lines; the cause on the owner's console remains
unconfirmed until retry. Password and code input remain hidden.

The full local race suite and vet passed before the 401 correction; the corrected
webauth package also passes under `-race`. All packages and test packages compile
with CGO disabled for macOS amd64/arm64, Linux amd64/arm64/386 softfloat, and
Windows amd64/arm64. These cross-target binaries were not executed.

A separate actual macOS PTY check reached both email and password prompts,
confirmed ECHO disabled and synthetic partial password input absent from output,
and verified exact terminal restoration after SIGTERM (exit 255). No newline was
sent for the synthetic password, so no credential form was submitted. This
checks local input hygiene only, not a real account login.

## Verified direct-login integration

On 2026-10-20 the owner ran the rebuilt login-probe locally, entered password and
TOTP at hidden prompts and reached the Connect devices page. This resolved the
HTTP 401 regression in the real flow. No account values or raw transcript are
retained in this report.

The CLI now supports `--direct-login` in the same process as connection setup:

```sh
./bin/pi-sh --direct-login --experimental-command raspberrypi 'printf "direct-login-ok\n"; exit 23'
```

The owner ran this exact read-only check and reported `direct-login-ok`, followed
by local `echo $?` returning23. This verifies fresh password/TOTP login, normal
server callback, device resolution, shell-page authorization, ICE discovery,
CSRF-bound offer creation, answer retrieval, native Go WebRTC, the in-memory Bash
broker and a real remote command status. No browser, extension, imported browser
cookies, persisted credentials, installed helper or Pi file writes were needed.
The selected direct/relay candidate route was not retained.

`internal/webauth.Connection` owns discovery/signaling and periodic authorization;
`internal/loginprompt` uses the controlling terminal with hidden raw input,
backspace/clear-line handling and serialized restoration on cancellation. Prompt
input is separate from command stdin. A cancelled blocking read can outlive the
prompt until the dedicated CLI exits; an embedding application needs a separate
reader-lifecycle design. Local PTY tests exposed and fixed a cancellation race.

Local TLS tests cover the exact request headers and cookie continuity, mismatched
device rejection, unexpected polling locations, single creation attempt,
revocation-like responses and cancellation. These synthetic HTTP fixtures are
not a complete independent Connect emulator or a new full-network SSH test.

Direct mode checks the observed device.json endpoint every minute and requires 204;
requests have 30-second timeouts and failures reach the session coordinator. The
short real command does not verify long sessions, actual revocation, expiry or
reauthentication. A fresh login is required for every process; no session reuse
or secure persistence is implemented. Direct interactive behavior still needs a
separate owner-run check. Physical iSH and native Linux/Windows remain untested.
