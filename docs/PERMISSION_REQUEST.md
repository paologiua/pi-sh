# Draft interoperability permission request

Draft only. Not sent. The maintainer must select an appropriate official contact
and review this wording. No protocol captures, account/device identifiers or
credentials should accompany an initial request.

**Subject:** Permission scope for an unofficial open-source Connect CLI

We are investigating an independent, unofficial open-source Go command-line
client, provisionally called pi-sh, providing SSH-like user behavior through
Raspberry Pi Connect's official infrastructure and existing unmodified agent.
It would not implement the SSH wire protocol or claim Raspberry Pi endorsement.

Before continuing detailed protocol investigation or implementing a custom live
client, we would like written clarification or permission under the current
Connect terms, particularly clauses 2.8 and 11, for the following scope:

- Observe the authorized web client's authentication, device discovery,
  signaling and WebRTC shell behavior on accounts and devices we control.
- Independently implement a Go client using only officially permitted service
  access, with a supported authentication approach, unchanged remote agents,
  ordinary authorization checks and cryptographic verification intact.
- Distribute the independent client as open-source software, using original
  code and branding, without redistributing your client or agent implementation.
- Publish sanitized interoperability documentation, fixtures and functional
  compatibility results, identifying any information that you consider
  confidential or require us to withhold.

Please identify the approved authentication or client-registration mechanism,
relevant API documentation, usage/rate limits, attribution requirements and any
limits on account tiers, supported features or public distribution. We would
also appreciate clarification of applicable open-source licences for any
official components intended for reuse; no licence for Connect itself is assumed.

We will not install software or change configuration on the Raspberry Pi,
replace service infrastructure, bypass restrictions, scan infrastructure,
disable security verification, or export credentials. Advanced transfer or
forwarding features would be pursued only where supported within that scope.

Please confirm the permitted scope and any conditions in writing before we
resume the custom-client investigation.
