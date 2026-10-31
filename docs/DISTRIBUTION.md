# Building, testing and distributing pi-sh

These scripts operate on the local computer. None accesses Connect or the Pi.
The public programs are `pi-sh`, `pi-cp`, `pi-sftp`; there is no `pi-ftp` or FTP protocol.

## Build

Install the Go version required by go.mod and put it on PATH, then run:

```sh
./scripts/build.sh          # current OS/architecture: bin/
./scripts/build.sh all      # seven release archives and SHA256SUMS: dist/
```

The release matrix is macOS amd64/arm64, Linux 386/amd64/arm64 and Windows
amd64/arm64. All use CGO_ENABLED=0; Linux/386 uses GO386=softfloat for iSH.
These are supported build targets, not every target recognized by Go, and do
not certify runtime behavior. Each archive contains all three clients, README,
notices and the project documentation. A root LICENSE is included
when present; the maintainer's own licensing decision is still pending.
Windows archives contain .exe files; extract and place them on PATH manually.

## Test

Run from macOS/Linux or a suitable Bash development environment with Go and Node.
Local Unix shell fixtures require Bash >=4.3 on the development computer. Set
PI_SH_TEST_BASH to its path; otherwise the runner checks PATH and the standard
Homebrew locations. macOS /bin/bash 3.2 does not satisfy this test prerequisite.

```sh
./scripts/test.sh           # uncached module tests, vet, integrity, browser/platform checks
PI_SH_TEST_BASH=/path/to/modern/bash ./scripts/test.sh  # explicit local Bash
PI_SH_TEST_BASH_52=/path/to/bash-5.2.37 \
PI_SH_TEST_BASH_53=/path/to/bash-5.3 \
  ./scripts/test.sh full    # plus race, fallback and cross-build/test compilation checks
```

Both modes summarize failures with `RESULT fail=N` and exit nonzero on failures.
The runner rejects an unavailable/old explicitly selected Bash before starting
checks. Full mode validates both explicit Bash versions before beginning and
uses the selected 5.3 for its main run unless PI_SH_TEST_BASH is set. It implements
the regular checklist in [TESTING.md](TESTING.md), adding fresh test runs and
installer checks. Optional tool-dependent fixtures can skip; inspect test output
and the prerequisites in that document. Local ssh/sshd, sftp-server, Perl, PTY
and setsid fixtures do not imply remote Pi verification. On systems where race
instrumentation is unavailable the full run fails; cross-compilation is not a
substitute for runtime tests. The load/stress benchmarks are opt-in separate
campaigns, not automatically spawned CPU stress. Native scripts and transfer
plans are deliberately excluded: approved Pi writes cannot be replayed by a
routine test command.

## Installer and release layout

```sh
./install.sh --repo paologiua/pi-sh --version v0.1.0-alpha.1
./install.sh --from-dir ./dist --bin-dir "$HOME/.local/bin"
./install.sh --help
```

Download mode requires a real GitHub repository and published release with
`pi-sh_OS_ARCH.tar.gz` and `SHA256SUMS` assets generated together by build.sh.
The default is the selected alpha, v0.1.0-alpha.1. Pass --version with another
tag to select a release. Explicit --version latest resolves GitHub's latest stable
release once before fetching both files; it does not select prereleases.
The installer checks the archive's SHA-256 before extracting the three clients, uses no sudo and defaults to ~/.local/bin.
It prints a PATH hint without changing shell configuration. Existing client
files in that directory are replaced. --from-dir exercises the same archive and
checksum path without network access. SHA-256 verifies release-file integrity;
there is no release-signature verification implemented.

The repository is currently private, so these anonymous curl commands are future
public-installation examples. They require public access and a published release:

```sh
curl -fsSL https://raw.githubusercontent.com/paologiua/pi-sh/main/install.sh | bash
# Pin a release:
curl -fsSL https://raw.githubusercontent.com/paologiua/pi-sh/main/install.sh | bash -s -- --repo paologiua/pi-sh --version v0.1.0-alpha.1
```

v0.1.0-alpha.1 is the selected project version; its GitHub release has not been
created. This document does not assert that download assets already exist.
The installer defaults to paologiua/pi-sh. --repo or PI_SH_REPO selects another
repository, for example a fork. The source URL downloads the script; --repo
selects the repository hosting the binary release assets. With curl, script
arguments follow `bash -s --`, as the pinned-release example above shows.

While the repository remains private, use an authenticated Git checkout, then
`./scripts/build.sh all` and `./install.sh --from-dir ./dist`. Anonymous downloads
cannot access private source or release assets. No GitHub token or password is
requested, stored or embedded by the installer. This work does not change
repository visibility or publish any assets.
macOS/Linux/iSH use the installer; Windows uses manual archive extraction.
iSH still needs GOMAXPROCS=1 PI_SH_ISH_RELAY=1 for the experimentally verified path.

Before publishing, resolve original-code licensing, review third-party notices
and service/publication scope as recorded in [SERVICE_CONSTRAINTS.md](SERVICE_CONSTRAINTS.md).
No script publishes, uploads, creates a GitHub release or changes the Pi.
