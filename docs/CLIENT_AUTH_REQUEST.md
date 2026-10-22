# Draft request: native Connect user authentication

2026-10-20. Draft only, not sent. No user/device identifiers or credentials are
needed for this initial technical question.

**Subject:** Public-client authentication for an unofficial Connect CLI

We are developing pi-sh, an independent Go CLI using the existing, unmodified
Raspberry Pi Connect agent and official infrastructure. We would like to use the
normal Raspberry Pi ID browser login, with a supported authorization response to
a native CLI, rather than exporting browser cookies or requiring a browser
extension. Linux/386 and future iSH on iOS are target platforms.

Does Raspberry Pi support registering a third-party public/native client for
user-initiated remote Connect sessions? If so, please identify:

- The client registration procedure and applicable service scope/conditions.
- Supported authorization-code + PKCE (S256) and/or device authorization grants,
  official endpoint documentation, and any issuer/discovery metadata.
- Permitted loopback redirect URIs, ephemeral port rules, and alternatives when
  the login browser and CLI run on different devices.
- The audience/scopes and Connect API authentication method required for device
  discovery, ICE configuration, offer creation, answer retrieval and continued
  authorization checks. A Raspberry Pi ID identity token alone is not assumed
  to grant these capabilities.
- Refresh/revocation/expiry rules, including how the recent-user-authentication
  requirement for a new session applies to native clients.

The device enrollment flow and management API are documented, but we have not
found a supported native-user authorization contract for starting shell sessions.
We do not intend to reuse the official web application's client identity or its
registered callback, enroll a replacement device, change the Pi, or bypass login,
MFA, authorization or cryptographic verification.

If this facility is not available, please confirm whether a supported integration
route exists or is planned. This request does not assume permission to publish
service internals or provider endorsement of the project.
