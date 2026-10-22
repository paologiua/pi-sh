# Client authentication without a browser companion

Research date: 2026-10-20. This report concerns an **operator client** that opens sessions to an already enrolled Pi. It does not propose enrolling pi-sh as a device. No cookies, tokens or credentials were exported, and no Pi files or account settings were modified for this investigation. Public-source research below is distinguished from an isolated browser observation supplied by the coordinating agent.

## Result

A registered native-client login with browser approval is a sound design, but **no public Raspberry Pi registration path or grant authorizing third-party remote-session creation was found** in the sources reviewed. That is a missing integration contract, not proof that Raspberry Pi could never support it. A localhost callback alone cannot supply the missing server-side authorization.

## What the official Connect documentation establishes

- The documented operator path uses the Connect website and Raspberry Pi ID.
- The security section reserves offer creation to the Connect web application acting for a signed-in user; device credentials cannot create offers. Session initiation also requires a Raspberry Pi ID authentication within the previous hour.
- `rpi-connect signin` verification codes and auth keys link devices to accounts; they are not documented as operator credentials for opening sessions.

Source: [Connect documentation: sign-in, remote shell and security](https://www.raspberrypi.com/documentation/services/connect.html).

The organisation management API documents provisioning and device management, including auth-key and device-identity creation. It does not document a remote-shell/session-offer API or a user-delegated scope for that operation. Its administrative access tokens are highly privileged and are not an appropriate substitute for a narrowly delegated operator credential. [Connect for Organisations documentation](https://www.raspberrypi.com/documentation/services/connect-for-organisations.html).

The Raspberry Pi ID guide explains account creation, service sign-in and 2FA. It does not publish third-party client registration, client metadata, Connect scopes or native-client callbacks. [Raspberry Pi ID documentation](https://www.raspberrypi.com/documentation/services/id.html).

## A real device-code flow exists, but it serves enrollment

Official Raspberry Pi Imager source was read at commit `8e7e0b6f67ba9b659b612c82b566f1a6260c59d9`:

- `ConnectDeviceSignIn::start` sends a configured `client_id` to `/client/device`.
- Its response parser reads `device_code`, `user_code`, `verification_uri_complete`, `expires_in` and `interval`.
- Polling sends `client_id` and `device_code` to `/client/token`; success returns `device_id` and an `access_token` with the `rpdev_` prefix.
- Pending, slower polling and expiry are handled explicitly.

These are observations from enrollment source, **not a specification for a pi-sh operator login**. The request shape also must not be described as fully RFC 8628 compliant merely because the response resembles device authorization: the observed polling implementation does not include RFC 8628's `grant_type` field. No enrollment request was initiated and no upstream client ID was reused. [Official Imager device sign-in source](https://github.com/raspberrypi/rpi-imager/blob/8e7e0b6f67ba9b659b612c82b566f1a6260c59d9/src/connect_device_signin.cpp).

## Isolated browser observation

The coordinating agent used a clean, unauthenticated browser context and followed Connect's ordinary **Sign in with Raspberry Pi ID** button. Connect submitted `POST /auth/raspberry_pi`, then navigated through `https://id.raspberrypi.com/oauth/authorize` to the ID sign-in page. The observed authorization URL contained the parameter names `client_id`, `code_challenge`, `code_challenge_method` (value `S256`) and `redirect_uri`; the redirect pointed back into Connect's own `/auth/raspberry_pi…` route. The displayed network URL was truncated, so the complete callback path, response type and scopes were not established. No credentials were entered and sign-in was not completed.

This confirms that the **official web application uses an OAuth authorization endpoint with a PKCE challenge**. It does not register pi-sh, make a loopback callback acceptable, establish an operator token audience/scope, or show that the web application's flow can be reused by an independent client. Client ID, state, challenge values and authorization data were not exported or stored.

The existing authenticated Raspberry Pi ID profile was also inspected for visible
navigation entries only. It exposed profile editing, password, two-factor and
session management, with no visible application/client-registration link. No
profile values were captured and no settings were changed. This observation is
limited to the current account UI, not proof that provider-managed registration
does not exist.

## Public metadata check and limits of the search

An unauthenticated HTTPS GET, without cookies or authorization headers, to `https://id.raspberrypi.com/.well-known/openid-configuration` returned HTTP 404 on the research date. This establishes only that **this URL** did not expose discovery metadata; another issuer path, private registration arrangement or unpublished capability remains possible. The public-source researcher did not guess alternate endpoints or perform authorization, token or registration requests; the separate normal browser redirect is described above.

Separately, the coordinating agent's isolated browser checked the ID root `/.well-known/openid-configuration` and `/.well-known/oauth-authorization-server` using credentials-omitted GETs; both returned HTTP 404 with non-JSON responses. This is consistent with the limited root discovery result above, and does not contradict the observed official OAuth redirect.

Public first-party documentation, the official Imager repository and standards were reviewed. Search results concerning Pi Network and Raspberry Pi Foundation's separate authentication library were excluded: they do not establish Raspberry Pi Ltd Connect authorization.

## What a supported native flow would require

For an authorization-code flow, use an external browser and PKCE, with a provider-approved native public client and redirect URI. A temporary server bound only to loopback can receive the authorization response; it cannot read the browser's login cookies. Follow the native-app registration and redirect requirements rather than transplanting Connect's web client ID or callback. [RFC 8252, sections 6–8](https://www.rfc-editor.org/rfc/rfc8252).

For iSH, a supported device grant would be preferable architecturally because authorization can occur in Safari or on another device while the CLI polls over HTTPS. RFC 8628 defines that exchange, including expiry and polling limits; its existence does **not** imply Connect offers such a grant for operator access. Actual iSH execution and suspension behavior remain untested. [RFC 8628](https://www.rfc-editor.org/rfc/rfc8628).

An identity token or a successful ID login alone does not establish authorization for a Connect resource. Client onboarding and the accepted token type, audience and operation scope must be specified by the provider. Registration is normally arranged with the authorization server; the OAuth framework does not grant a client arbitrary access by implementing the protocol. [RFC 6749, sections 2, 3.3 and 7](https://www.rfc-editor.org/rfc/rfc6749).

## Exact blocker and next actions for an independent OAuth client

Before a real operator login can be implemented and tested, obtain an official integration contract answering:

1. Can an unofficial native public client be registered for Raspberry Pi ID / Connect operator access, including private personal use?
2. Is authorization code plus S256 PKCE supported, and which loopback redirects are permitted? Is an operator device grant available for iSH?
3. Which documented audience, scopes and API authorize listing existing devices, creating a shell offer, retrieving ICE/signaling data and renewing an operator session?
4. How must the one-hour recent-authentication requirement, revocation and any refresh tokens be handled?

A request to Raspberry Pi would be a separate external communication requiring user authorization; none was sent. The general official [contact page](https://www.raspberrypi.com/contact/) is a starting point, not evidence that an application-registration programme exists.

Standards-only callback/PKCE components can be tested locally without provider registration, but they cannot demonstrate Connect login. Do not advertise a working `pi-sh login`, invent a scope or issuer, reinterpret provisioning tokens, relax redirect validation, or extract browser session credentials to fill this gap. Existing browser-companion authentication remains the verified route until a legitimate operator flow is confirmed.

## Scope clarification: normal web login

The registration blocker above concerns an independent OAuth application. It does
not prove that a fresh normal Connect web session cannot be driven by an HTTP
user agent. The owner ruled out contacting Raspberry Pi. Investigation now
follows that second path, with no imported browser cookies and no fabricated
OAuth parameters. Real Go preflight reaches the password form; full credential
login is not verified. See DIRECT_LOGIN_PROBE.md and DIRECT_LOGIN_DESIGN.md.

The normal fresh web-session path was subsequently verified end to end by the
owner: password/TOTP, callback, direct Go signaling, real Pi command output and
status 23, without a browser companion. The registration blocker in this report
still applies to an independent native OAuth application, not this demonstrated
web-user-agent flow. No provider contact was made. See DIRECT_LOGIN_PROBE.md.
