# pi-sh — Development Instructions

## Project

Build an unofficial, open-source CLI suite that reproduces OpenSSH behavior as closely as technically possible while using the official Raspberry Pi Connect infrastructure.

The project includes `pi-sh`, `pi-cp` and potentially `pi-sftp`.

The goal is SSH-like behavioral and CLI compatibility, not SSH wire-protocol compatibility.

## Non-negotiable Raspberry Pi restrictions

The Raspberry Pi is already configured and accessible through Raspberry Pi Connect and SSH.

**Never modify the Raspberry Pi to make the project work.**

- Do not install packages, daemons, agents, scripts or services.
- Do not change Raspberry Pi Connect, SSH or system configuration.
- Do not restart services or reboot the Raspberry Pi.
- Do not modify persistent files or directories.
- SSH access is for read-only diagnostics.
- Temporary files may be created only for authorized `pi-cp` testing, in an approved location, and must be removed afterward.
- Never introduce a requirement for additional remote software.

## Development environment

- Codex may install Go and required development tools on the Mac, subject to normal approval controls.
- Git is already installed.
- Chrome is already authenticated to Raspberry Pi Connect.
- Use Chrome DevTools MCP to inspect authorized browser sessions.
- Never export or commit passwords, cookies, tokens or sensitive session captures.

## iSH compatibility

Treat iSH on iOS as an architectural requirement from the first commit.

- Support building for Linux/386 with CGO disabled.
- Prefer pure-Go libraries and portable code.
- Avoid unnecessary operating-system-specific dependencies.
- Analyze potential incompatibilities with iSH system calls and networking.
- Include Linux/386 in automated build checks.
- Do not claim runtime compatibility until tested on iSH.

## OpenSSH compatibility

Maintain `docs/SSH_COMPATIBILITY.md` as a complete compatibility matrix.

For every relevant SSH, SCP and SFTP feature, document:

- Intended equivalent behavior.
- Implementation status.
- Automated tests.
- Real-device verification.
- Known differences.
- Technical blockers.

Do not silently omit features or claim unsupported behavior.

## Architecture

Keep authentication, device discovery, signaling, WebRTC transport, session protocol, terminal handling, command execution, file transfer and forwarding modular.

Prefer Go and Pion WebRTC where technically appropriate.

Use the existing Raspberry Pi Connect agent and infrastructure exclusively.

## Security and publication

- Respect authentication and authorization.
- Do not bypass access restrictions or disable cryptographic verification.
- Do not store credentials in plaintext.
- Do not commit secrets.
- Treat Connect endpoints and protocol details as unverified until observed.
- Verify relevant service terms and licensing before public release.
- Clearly identify the project as unofficial.

## Development approach

Start by verifying the current Raspberry Pi Connect protocol using an authorized session.

Establish a real end-to-end interactive shell before implementing advanced features.

Use reproducible tests and evidence to assess progress.

Continue toward maximum practical OpenSSH compatibility while maintaining the non-negotiable restrictions.

Report external blockers explicitly instead of bypassing them or claiming success.
