# Connect protocol observations

2026-10-21 browser-session refresh: the visible Devices page did not establish
recent shell authorization. After owner reauthentication, shell-page/ICE GETs
returned200 but connection POST returned422 until Devices reload. No response
body or token is retained; stale page/CSRF state is an inference. The companion
now selects CSRF from its freshly fetched authorized shell page, with local
stale/fresh/fallback/no-replay regression cases. Native command TTY results are
recorded separately in REAL_DEVICE_TESTS.md; no new agent protocol is inferred.

Recorded 2026-10-19. A native Pion peer and interactive Go CLI have now reached the
unchanged Pi through official Connect infrastructure. These are observations of
one authorised account/device, not a public API stability guarantee.

## Evidence sources

- Authorised Chrome session inspected with Chrome DevTools MCP.
- Public web-client asset `application-e4a29160.js`, SHA-256
  `327c052ffee0c9fcd89b6f2ada062625c31bc94768bd07b23695de1b287444fc`.
  No upstream implementation is vendored or copied into the client.
- Successful native probe and interactive CLI checks described in
  [REAL_DEVICE_TESTS.md](REAL_DEVICE_TESTS.md).
- [Official security documentation](https://www.raspberrypi.com/documentation/services/connect.html#security).

## Authentication and discovery

The browser remains signed in through the official Raspberry Pi ID flow. Cookies
and CSRF tokens stay inside the browser. The Go companion does not read browser
profiles, cookie APIs, passwords or account tokens. It receives only selected
device identity, ICE configuration and the SDP answer in memory.

The companion uses exact device-name/ID matching against authorised Devices-page
links. When needed it fetches `/devices/<id>/remote-shell-session` and parses its
inert HTML for the `data-shell-device-value` attribute, without executing the web
shell controller or starting a second browser WebRTC peer. An already-open shell
page can also supply this device metadata. Ambiguous names fail.

This browser session is not an independently verified native CLI authentication
protocol. MFA, sign-in expiry and access checks remain the official service's.

On 2026-10-20, a previously signed-in Devices page remained accessible while
fetching the remote-shell page returned HTTP 200 HTML titled reauthentication,
with a form whose action was `/auth/raspberry_pi` and no shell controller. Thus
HTTP success alone does not establish permission to start a shell. The companion
now identifies that observed form and sends only a fixed `reauthentication_required`
code; Go prints recovery instructions. It does not submit hidden authentication
forms or bypass recent-authentication requirements. Opening the normal official
Remote shell flow refreshed authentication, after which the native CLI connected.

## Observed signaling sequence

1. Fetch `/devices/<id>/ice-configuration` with `Accept: application/json` in the
   authenticated browser. ICE server configuration is passed to Pion in memory.
2. Go creates the reliable ordered `shell` data channel and gathers a complete
   WebRTC offer. No private key leaves its native peer.
3. The browser POSTs the JSON session description to `/devices/<id>/connections`,
   using `Content-Type: application/octet-stream`, `Accept: application/json`,
   and the page's CSRF token in `X-CSRF-Token`. The observed service accepted it.
4. Follow the response's `Location` using same-origin authenticated GETs with
   `Accept: application/json`, polling roughly once a second until an `answer`
   session description is returned. Cross-origin locations/redirects are rejected.
5. Go applies the answer with Pion's default DTLS fingerprint verification intact.
   ICE/DTLS/SCTP and shell traffic are native; the browser does not relay shell I/O.

No cookies, CSRF values, TURN credentials, descriptions, addresses or concrete
connection URLs belong in fixtures or logs. The existing companion uses a
short-lived random-capability HTTP listener on 127.0.0.1 with exact Origin/Host
checks, solely to pass configuration and descriptions in memory. Browser local
network permission was granted by the user through Chrome's normal prompt.

## Shell and terminal behavior

- The initiator-created channel is named `shell`. Browser input uses WebRTC text
  messages; native text input, Unicode and Ctrl-C worked against the real agent.
- The native reader passes received message bytes unchanged to the local terminal.
  The web client configures binary array-buffer output. Further framing/maximum
  sizes, alternate encodings and arbitrary binary input remain unverified.
- The agent creates a `resize` data channel. Text JSON with `cols` and `rows` was
  accepted: the probe sent 100x32, and the remote `stty size` returned 32 100.
- The observed shell is a TTY and sets CONNECT_TTY. It behaves interactively,
  including echo, prompts and Ctrl-C. This does not prove no-PTY exec support.
- `exit` closes the channel. Neither the observed browser implementation nor the
  native sessions supplied a separate remote exit-status message. The CLI returns
  255 on remote close, rather than equating close with remote success.
- On 2026-10-20, Ctrl-D at an empty Bash prompt also logged out and closed the
  channel. Unicode/multiline text with standard bracketed-paste wrappers worked.
  A live local size change reached the remote PTY (111 columns, 37 rows). These
  are terminal bytes/resize observations, not new protocol requests or evidence
  of an exit-status or signal channel.

## Continued authorization and lifetime

The official web client checks `/devices/<id>.json` about every minute and expects
204 while access is valid. The companion follows this observed behavior and
reports successful checks to Go. Explicit failures terminate the client; absence
of browser confirmation for 90 seconds also fails closed. A real interactive
session stayed usable after its first periodic browser check.

The web client refreshes ICE configuration about every 85,800 seconds. Native ICE
renewal, reconnect, background browser throttling, long sessions and forced relay
operation are not verified/implemented yet. Do not claim those lifecycle cases.

## Unverified capabilities

Native exec requests, PTY opt-out, separate stdout/stderr, process exit/signal
status, environment requests, file-transfer/subsystem channels, socket forwarding,
agent/X11 forwarding, multiplexing and protocol/version negotiation remain
unverified. No channel or endpoint should be invented for them. A functioning
shell is evidence only for the shell and specific controls tested above.

The [agent-source investigation](AGENT_EXIT_STATUS.md) did not locate public source
for the observed 2.13.0 agent. Native exit-status support therefore remains unknown,
rather than proved absent. No internal process-wait implementation is claimed.
The explicit [command adapter](COMMAND_EXECUTION.md), verified 2026-10-20, obtains
its child's status by in-memory shell framing over the existing shell bytes.
Its READY/EXIT markers are pi-sh application data, not newly discovered Connect
protocol messages or agent support for exec/status.

Service terms remain a separate unresolved interpretation/publication question;
see [SERVICE_CONSTRAINTS.md](SERVICE_CONSTRAINTS.md). Private development resumption
does not amount to provider permission or a legal conclusion.

## Client-defined shell broker

`internal/shellbroker` transports PISH3 application records inside the existing
observed shell channel. This framing is independently authored pi-sh behavior,
not a newly discovered Connect protocol or native exec/subsystem/status channel.
The existing agent is unchanged. Source upload, binary hex stdio, acknowledgments
and explicit completion are described in SHELL_BROKER.md. A PTY carrier does not
mean that the broker child owns a PTY: child descriptors are anonymous pipes.

## Direct HTTPS login investigation — 2026-10-20

Normal form metadata was observed: Connect POST /auth/raspberry_pi leads to ID
sign-in; password POST /session uses email, password and a fresh authenticity_token.
After the owner manually entered credentials in an isolated browser, the ID page
at /session offered POST /session/verify with otp, autocomplete=one-time-code,
and another hidden authenticity_token. Only structure was retained. The Go
preflight independently reached the password stage without JavaScript or Chrome;
no full native credential login is verified. These are observed web behaviors,
not a supported third-party API contract. See DIRECT_LOGIN_PROBE.md.

## Direct HTTP continuation verified

The owner-run Go probe completed password/TOTP and the normal Connect callback.
A real `--direct-login` command then returned the expected marker and23. The
normal web session can therefore be used by this Go client without importing
browser cookies or automating a browser. Discovery/signaling use the observed
sequence above with this process's own CSRF/cookies, not a new OAuth API.
The live OTP page follows password POST /session with HTTP 401; only its exact
verified challenge is accepted. Authenticated signaling requests reject redirects
and unexpected device/polling identities. No additional Connect protocol channel
or installed agent change was introduced. See DIRECT_LOGIN_PROBE.md.
