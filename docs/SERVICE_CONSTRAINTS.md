# Service and publication constraints

Research date: 19 October 2026. This is an engineering record of primary-source findings and unresolved permissions, not a legal opinion. pi-sh is unofficial and must not imply Raspberry Pi endorsement.

## Verified service terms

The [Connect Terms](https://connect.raspberrypi.com/terms), dated 23 July 2026, restrict reverse engineering in clause 2.8(a), except where applicable law cannot exclude it; clause 2.8 also restricts competing services, access outside the agreement and exceeding limits. Clause 11.5 identifies service details and performance-test results as confidential, subject to clause 11.1 exceptions. Clause 2.11 preserves applicable component open-source licences. These provisions do not establish permission for this CLI or publication of its observations. Clauses 3, 4 and 9 allow service/terms/price changes. Clauses 10.1–10.3 address ownership and trademark permission. **Status: written permission or a qualified determination of an applicable interoperability exception is unresolved.**

The [general terms](https://www.raspberrypi.com/terms-and-conditions/), dated 2 June 2026, incorporate Connect-specific terms in section 17 and give them precedence for the service. Section 5.8 restricts scraping and text/data mining, with a non-excludable-law caveat. Account security and unauthorized-access restrictions also apply. This record does not interpret an authenticated account as authorization to reverse engineer the service.

## Development resumption and publication review

The initial investigation treated these terms as a reason to stop detailed
protocol work. That was a prudential interpretation, not a Raspberry Pi refusal,
a demonstrated violation, a technical access restriction or evidence of a likely
ban or legal action. After discussing that distinction, the device owner
explicitly authorised continuing private development. Native shell work resumed
using the existing authenticated session and normal service access controls.

This user instruction does not establish provider permission or decide the
applicability of statutory interoperability rights. Do not describe those legal
questions as settled. No bypass of login, MFA, CSRF, authorization or cryptographic
verification is part of the implementation. Nothing has been published or sent
to Raspberry Pi.

Before public distribution, resolve the scope through written clarification or
qualified review as appropriate, particularly:

1. Authenticated protocol observation and independent client interoperability.
2. Distribution of an unofficial CLI and permitted authentication mechanisms.
3. Disclosure of independently authored protocol documentation and functional
   test results, including applicable confidentiality exceptions.
4. Client registration, usage limits, attribution and component licensing.

[PERMISSION_REQUEST.md](PERMISSION_REQUEST.md) is an unsent optional draft to
support that review. Lack of an explicit permission record is not itself proof
that this private development violates the agreement or will trigger enforcement.
No new terms were accepted on the user's behalf by the client.

## Licensing and attribution evidence

Raspberry Pi's [licensing page](https://www.raspberrypi.com/licensing/) describes several licences and warns that software licences differ. Its online documentation is generally CC BY-SA 4.0; other documents and software may have different terms. This does not establish the licence of Connect's web client. On 2026-10-20, the exact official agent 2.13.0 package copyright was checked separately: it declares Raspberry Pi Ltd BSD-3-clause and lists third-party notices. This does not establish a hosted-service or web-client licence; see [AGENT_EXIT_STATUS.md](AGENT_EXIT_STATUS.md). The guessed public repository `https://github.com/raspberrypi/rpi-connect` returned 404 during research, and an official archive directory listing returned 403; neither proves that source or a licence is unavailable elsewhere.

Project policy: do not copy agent/client source, minified bundles, screenshots, documentation prose or logos without establishing the exact applicable licence and preserving required notices. Observing behavior does not automatically authorize redistributing the observed implementation. Recheck exact versions and licences for all direct/transitive Go dependencies before release; maintain notices and an inventory rather than assuming all Go dependencies share a licence. Choose the repository's own licence only with the maintainer's authorization.

The [trademark guidelines](https://www.raspberrypi.com/trademark-rules/) constrain use of Raspberry Pi marks and prohibit misleading affiliation. Use original branding and an explicit unofficial statement. Trademark permission and a source-code licence are separate questions.

## Service capabilities and project restrictions

Official [Connect documentation](https://www.raspberrypi.com/documentation/services/connect.html) describes browser WebRTC connections, remote shell, relay networking, and remote device updates. It is a product description, not a stable third-party protocol/API contract or evidence that scp, SFTP or forwarding are exposed by the installed agent. Product changes must be verified against the actual authorized session; the native shell observations are recorded in PROTOCOL.md.

Regardless of product capabilities, AGENTS.md prohibits installation, service restarts, configuration changes and persistent writes on this Raspberry Pi. Do not use OTA/deployment features, enable debug logging, install a helper, change firewall settings or activate a different agent. Remote temporary writes are permitted only for separately authorized pi-cp tests in an approved location with cleanup.

## Publication readiness ledger

| Requirement | Status / required evidence |
| --- | --- |
| Independent CLI/service scope | Private development resumed by user instruction; provider/legal interpretation remains unresolved for publication review. |
| Remote-device restrictions | Mandatory throughout development; no remote mutations authorized by this document. |
| Project licence | MIT selected with maintainer authorisation on 2026-10-31 for original code and documentation; see [LICENSE](../LICENSE). Third-party licences and service/publication questions remain separate. |
| Dependency licences | Pion and imported module-root licences inventoried in [DEPENDENCIES.md](DEPENDENCIES.md); full notices retained. Source-file exceptions and release-linked code still need review. |
| Agent/web asset licences | Official agent 2.13.0 package declares BSD-3-clause with third-party notices; web assets/service rights remain unresolved. No upstream source/assets vendored. |
| Trademark/attribution | Use unofficial original branding; review final naming and notices. |
| Credentials and personal data | Never commit cookies, tokens, credentials, session captures, device identifiers or private addresses. |
| Runtime/performance claims | Publish only measured evidence within the permitted scope; iSH runtime remains untested. |
| Terms drift | Re-read linked terms before resumed service work and before release; record material changes. |
