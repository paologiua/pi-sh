# Direct HTTPS login design

Research date: 2026-10-20. Status: design requirements, not a working login or a claim of service support. No authenticated browser was read, credentials submitted, account changed or service contacted by this research subtask. The coordinating implementation begins with a credential-free preflight; its concrete observations and test results belong in its own evidence report.

## What this approach means

The proposed CLI starts a **new, independent web session** with the official Connect site. It receives its own cookies through normal HTTPS responses and follows forms and redirects returned to that session. It neither imports Chrome cookies nor registers an independent OAuth application. Connect's existing web callback remains on Connect; no invented localhost callback, OAuth client identifier, scope, token audience or PKCE verifier is needed or permitted in the prototype.

This is a hypothesis about reproducing web navigation, not evidence that the entire login is available to a non-browser client. The prior [client authentication research](CLIENT_AUTH_RESEARCH.md) established an observed server-driven Connect-to-ID authorization navigation but did not complete a fresh credential-based login. Its missing third-party registration finding concerns an independent native OAuth grant; it does not prove a normal web session cannot be driven by an HTTP client. No supported operator API or authorization for publication is established by this distinction.

Raspberry Pi documents service sign-in through Raspberry Pi ID and optional TOTP authentication. It does not document the form field names, action paths, CSRF mechanism or a supported direct CLI login API. Obtain these from a fresh live response before implementing a submission. [Raspberry Pi ID guide](https://www.raspberrypi.com/documentation/services/id.html).

## Separate preflight from credential submission

The first deliverable must stop at an identified sign-in form. It may GET public pages and submit the ordinary **credential-free** Connect sign-in initiation only after validating its form. It must not choose guessed account endpoints, submit empty credentials as a probe, replay authenticated browser state, create a device enrollment or begin a Pi session. Report normalized stages and booleans rather than raw captures.

A future local login command must prompt only when its state machine has identified the expected fresh form and destination. A user running that command locally can provide email, password and a TOTP if requested. Do not request those values in chat, CLI arguments, environment variables or pasted scripts. Use a controlling terminal with echo disabled for password and TOTP; fail clearly if secure interactive input is unavailable. Go provides `term.ReadPassword`; platform terminal behavior still needs testing. Never request the TOTP seed or a recovery code as a fallback. [Go terminal package](https://pkg.go.dev/golang.org/x/term#ReadPassword).

## Session boundary and cookies

Use one owned `http.Client` and an in-memory `cookiejar.Jar` for a single login attempt and its subsequent Connect requests. Configure `publicsuffix.List`; the package explicitly warns that a nil suffix list is insecure. Do not serialize the jar, log cookies, import browser profiles or pass session values through the existing companion URL. [Go cookiejar documentation](https://pkg.go.dev/net/http/cookiejar).

The jar supplies domain, path and Secure matching; it is not a browser security sandbox. Apply the application origin policy before consulting it or making any request. Never assume Go enforces every browser navigation, SameSite, document-origin or script rule. Cookie authentication is bearer-sensitive: accepting a cookie does not establish that the current user may open a particular device. Preserve server-side authorization on every operation. [HTTP cookie model, RFC 6265](https://www.rfc-editor.org/rfc/rfc6265).

Session memory lasts only for the running process in the initial design. Consequently a separate `login` process that exits cannot silently authorize a later `pi-sh` process. Either login and connect in one process, or explicitly design a later broker/persistence feature. No daemon or credential persistence is implied here. Memory-only storage reduces persistence; it does not guarantee that the OS cannot swap memory or that Go can erase every transient string copy. Minimize secret lifetimes and avoid promises of perfect zeroization.

## Request and redirect policy

The following are project requirements, intentionally stricter than general HTTP defaults:

- Allow only exact, reviewed HTTPS origins: initially Connect and ID origins already observed. Compare parsed scheme, hostname and effective port; reject userinfo, unexpected ports, host suffix tricks and HTTPS downgrades. A redirect to another origin is a blocker to review, not a reason to expand the list automatically.
- Validate initial URLs, resolved form actions and **every** redirect before making requests. Limit transitions and response sizes and use context cancellation and bounded timeouts. Do not fetch third-party scripts, images or styles.
- Default to same-origin form submission. Permit a credential-free cross-origin transition only when specifically observed and represented in the state machine. Password and TOTP bodies must target the exact ID origin and known stage. Never send credentials to Connect or to a caller-supplied URL.
- Prevent 307/308 redirects from replaying credential bodies to another origin. Do not rely solely on Go's sensitive-header forwarding: a form body is not an Authorization header. Check and reject unsafe transitions before dispatch.
- Leave TLS certificate and hostname verification enabled. Do not install a private trust bypass, ignore TLS errors or copy browser certificates. Proxy/TLS interception behavior must be explicit rather than a hidden exception.

Go's `CheckRedirect` runs before the next redirect request; its defaults are broader than this project policy. Redirect codes can change the method or replay the body, so transition decisions must cover both. A normal non-2xx HTTP response is not automatically a Go client error. [Go HTTP client behavior](https://pkg.go.dev/net/http#Client).

## Forms, CSRF and explicit states

Parse HTML structurally, resolve relative actions against the fetched page URL and preserve the observed successful controls for the selected form. Reject ambiguous forms, duplicate security fields, unsupported encodings, unsupported submitter overrides and unknown credential controls. Do not infer the password form from a CSS label or hardcode a guessed token value. Form submission depends on method, encoding, controls and submitter; implement only the verified subset and stop when it changes. [WHATWG form submission standard](https://html.spec.whatwg.org/multipage/form-control-infrastructure.html#form-submission).

Carry hidden CSRF controls from the fresh form belonging to the current cookie jar. Never generate fake tokens, strip failed checks, reuse another browser's token or treat a public meta token as universally valid. If the service requires a matching Origin/Referer, reproduce only the legitimate page-to-action navigation actually performed; do not forge a different caller origin to evade a rejected check. CSRF protections bind requests to expected authenticated context and must remain effective. [OWASP CSRF explanation](https://owasp.org/www-community/attacks/csrf).

Recommended internal states are conceptual names, not asserted service routes: fresh Connect page; validated sign-in initiation; validated ID credential form; optional validated TOTP challenge; server-controlled callback; authenticated Connect resource. A successful ID page or HTTP 200 is insufficient: Connect's final authenticated resource and authorization checks must succeed before signaling can start. Do not parse a device list until that boundary is proven.

Unknown JavaScript requirements, CAPTCHA, WebAuthn/passkeys, consent, account lock, email verification, recovery, risk checks, changed forms or terms acceptance must stop with a specific safe error. Do not click through account changes or implement challenge bypasses. Do not retry invalid passwords/TOTP automatically; respect Retry-After and rate limits. TOTP support is documented, but its live form sequence remains unverified. [Raspberry Pi ID guide](https://www.raspberrypi.com/documentation/services/id.html).

## Diagnostics and terminal hygiene

Emit fixed stage/error identifiers and bounded counts. Never print response bodies, request/response headers, hidden values, cookies, CSRF, OAuth codes, redirect queries, URLs with fragments/userinfo, passwords, TOTP or account profile values. Wrap network errors in a sanitizing boundary: a standard `url.Error` can contain the full URL. Do not use HTTP dumps or test snapshots containing live responses. Synthetic fixtures must contain invented values only. Tests should deliberately plant canary secrets in all those locations and assert none reaches stdout, stderr or returned public errors.

CLI prompts go to the controlling terminal, leaving remote command stdin/stdout/stderr available for OpenSSH-like semantics. Restore terminal state on all ordinary error, cancellation and signal paths. An unsupported Windows terminal backend should fail explicitly rather than echo the password or read it from piped command input.

## Portability and iSH

Pure-Go HTTPS, cookie handling and HTML parsing can be built for Linux/386 without CGO; that is an architectural fit, not runtime proof. iSH implements Linux through user-mode x86 emulation and syscall translation. Its project description does not certify this CLI's TLS, DNS, entropy, terminal or background execution behavior. [iSH project](https://github.com/ish-app/ish).

Required later physical tests: certificate roots and hostname validation; DNS and IPv4/IPv6; secure random generation; password echo suppression and terminal restoration; TOTP prompt timing; iOS suspension/cancellation; cookie/session expiry and reauthentication. Authentication working over HTTPS would not establish WebRTC/UDP/TURN compatibility. Existing iSH transport research and real-device tests remain necessary. No browser automation, extension or localhost callback is assumed by this design.

## Meaningful validation before claiming success

Use local TLS test servers and a transport abstraction; production origin/TLS checks must remain fixed and must not gain an insecure test flag. Test the following independently:

1. Fresh cookie continuity and isolation between attempts; Secure, path/domain and public-suffix rejection; no cookie export.
2. Every redirect hop checked, including HTTPS downgrade, forbidden origin, misleading hostnames, userinfo, port changes, loop and cross-origin 307/308 credential replay.
3. Form variants with hidden-token rotation, HTML entity decoding, relative actions, duplicated fields, multiple forms, wrong method, unsupported encoding and oversized bodies. Reject unsupported ambiguity before prompting or sending any credential.
4. Password failure, optional TOTP, bad/expired TOTP, rate limit, unknown JavaScript/challenge and cancellation. Assert no automatic credential retries and no secrets in diagnostics.
5. HTTP 200 login page masquerading as success, ID success without Connect session, expired Connect session and unauthorized device. Do not advance to signaling.
6. A fully synthetic end-to-end sequence through callback into an authenticated Connect fixture. This establishes implementation behavior only; it does not establish live service compatibility.
7. Linux/386 CGO-disabled build plus normal platform builds. Record skips and untested runtime platforms precisely.

A real success claim then needs an operator-run, fresh local credential login (including 2FA where enabled), final authorized Connect response, real shell session and expiry/reauthentication evidence without captures containing secrets. Until that happens, describe this as an investigation/prototype and retain the verified browser companion route. If the ordinary service demands an unsupported challenge, record the exact stage and next action instead of bypassing it.
