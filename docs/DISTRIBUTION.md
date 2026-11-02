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
notices and the project documentation. The root MIT LICENSE is included
in every archive; third-party components retain their own licences.
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
./install.sh --repo paologiua/pi-sh --version v0.1.0-alpha.2
./install.sh --from-dir ./dist --bin-dir "$HOME/.local/bin"
./install.sh --help
```

Download mode requires a real GitHub repository and published release with
`pi-sh_OS_ARCH.tar.gz` and `SHA256SUMS` assets generated together by build.sh.
The default is the selected alpha, v0.1.0-alpha.3. Pass --version with another
tag to select a release. Explicit --version latest resolves GitHub's latest stable
release once before fetching both files; it does not select prereleases.
The installer checks the archive's SHA-256 before extracting the three clients, uses no sudo and defaults to ~/.local/bin.
It prints a PATH hint without changing shell configuration. Existing client
files in that directory are replaced. --from-dir exercises the same archive and
checksum path without network access. SHA-256 verifies release-file integrity;
there is no release-signature verification implemented.

Downloads use curl when it is on PATH (HTTPS only, including redirects) and
otherwise wget, so the `wget -qO- … | bash` route works where curl is missing,
as on a stock iSH/Alpine image with BusyBox wget. A piped script cannot tell
which tool fetched it, so `--downloader curl|wget` (or PI_SH_DOWNLOADER) selects
one explicitly; the README's wget command passes `--downloader wget`. GNU wget gets --https-only;
BusyBox wget has no equivalent, so redirect targets are not restricted there
(the starting URLs are HTTPS and the archive must still match SHA256SUMS). With
wget, `--version latest` reads tag_name from the GitHub releases/latest API, which,
like curl's redirect, selects only stable releases. Both paths are tested with
local stand-ins; real BusyBox wget has not been run against a release.

The repository is public; these commands need the selected release's assets on GitHub:

```sh
curl -fsSL https://raw.githubusercontent.com/paologiua/pi-sh/main/install.sh | bash
# Pin a release:
curl -fsSL https://raw.githubusercontent.com/paologiua/pi-sh/main/install.sh | bash -s -- --repo paologiua/pi-sh --version v0.1.0-alpha.3
```

v0.1.0-alpha.3 is the selected project version and installer default; it adds
the OpenSSH 10.3 directory-mode rule for recursive uploads. Its release is
created by release.yml from the v0.1.0-alpha.3 tag.
v0.1.0-alpha.1 and v0.1.0-alpha.2 are published as GitHub prereleases. alpha.2
was built and published by release.yml on 2 November 2026; the installer from
main then installed it on macOS/arm64 and the clients reported
`v0.1.0-alpha.2+g1ebf24bacdfe`.
The installer defaults to paologiua/pi-sh. --repo or PI_SH_REPO selects another
repository, for example a fork. The source URL downloads the script; --repo
selects the repository hosting the binary release assets. With curl, script
arguments follow `bash -s --`, as the pinned-release example above shows.

From a Git checkout, `./scripts/build.sh all` and `./install.sh --from-dir ./dist`
install without downloading release assets. No GitHub token or password is
requested, stored or embedded by the installer. This work does not change
repository visibility or publish any assets.
macOS/Linux/iSH use the installer; Windows uses manual archive extraction.
iSH still needs GOMAXPROCS=1 PI_SH_ISH_RELAY=1 for the experimentally verified path.
When the installer detects iSH (linux/386 with /proc/ish or an `-ish` kernel
release; force with PI_SH_INSTALL_ISH=1 or disable with 0), it defaults to
/usr/local/bin when writable, puts the binaries in `<bin-dir>/../libexec/pi-sh`
and writes POSIX sh launchers to the bin directory. Each launcher applies
`: "${GOMAXPROCS:=1}"` and `: "${PI_SH_ISH_RELAY:=1}"`, exports both and execs
the binary with the original arguments, so exported values still win. A launcher
is required because the Go runtime reads GOMAXPROCS before main runs.
On iSH it then removes earlier copies from ~/.local/bin and ~/bin (the old
installer default and the manual-copy location) and from their ../libexec/pi-sh,
unless --keep-old is passed. A file is removed only if it is a regular file
containing this project's Go build-info path (`pi-sh/cmd/<client>`) or the
launcher header; symlinks, unrelated files and the new install directory are
left alone, and each removal is printed. Run `hash -r` in an open shell afterwards.
scripts/test-install.sh checks the launchers with local fixtures (defaults,
overrides, argument preservation, paths with spaces and quotes); the detection
and launchers have not yet been run on physical iSH.

Before publishing, review third-party notices
and service/publication scope as recorded in [SERVICE_CONSTRAINTS.md](SERVICE_CONSTRAINTS.md).
No script publishes, uploads, creates a GitHub release or changes the Pi.

## Publishing a release

`.github/workflows/release.yml` publishes a release when a `v*` tag is pushed
(or from the Actions tab with an existing tag). Before tagging:

1. Set `Release` in `internal/buildinfo/version.go` and the `install.sh` default
   to the new tag, and update the version references in the docs.
2. Write the release notes in `docs/releases/<tag>.md`.
3. Commit, then `git tag -a <tag> -m "pi-sh <tag>" && git push origin main <tag>`.

The workflow refuses a tag that does not match the version, installer default or
notes file, or whose release already exists. It runs the installer checks and
`go vet` (the Go test suite runs in check.yml and does not gate releases), builds
with `scripts/build.sh all`, checks that the built clients report
the tag without `dirty` and that `SHA256SUMS` matches, then creates the release
with the seven archives and `SHA256SUMS`. Tags with a suffix (`-alpha.N`, `-rc.N`)
become prereleases. It uses the workflow's own token; no personal token is stored.
For a tag pushed before its notes existed, run the workflow from the Actions tab
on `main` with that tag: it builds the tag but reads `docs/releases/<tag>.md`
from `main`. v0.1.0-alpha.2 is published this way.
