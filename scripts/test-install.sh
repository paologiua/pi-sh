#!/usr/bin/env bash
# Deterministic installer contract checks, with local fake binaries only.
set -euo pipefail
unset PI_SH_REPO PI_SH_VERSION PI_SH_BIN_DIR
cd "$(dirname "$0")/.."
stage=$(mktemp -d "${TMPDIR:-/tmp}/pi-sh-install-test.XXXXXXXX")
trap 'rm -rf "$stage"' EXIT
case $(uname -s) in Darwin) os=darwin;; Linux) os=linux;; *) echo 'Installer fixtures skipped on this OS.'; exit 0;; esac
case $(uname -m) in x86_64|amd64) arch=amd64;; arm64|aarch64) arch=arm64;; *) arch=386;; esac
archive="pi-sh_${os}_${arch}.tar.gz"
mkdir "$stage/files" "$stage/releases"
for client in pi-sh pi-cp pi-sftp; do printf '#!/bin/sh\nprintf "fixture-ok\\n"\n' > "$stage/files/$client"; done
tar -czf "$stage/releases/$archive" -C "$stage/files" .
(cd "$stage/releases"; if command -v sha256sum >/dev/null; then sha256sum "$archive"; else shasum -a 256 "$archive"; fi) > "$stage/releases/SHA256SUMS"
bash install.sh --from-dir "$stage/releases" --bin-dir "$stage/bin with spaces"
for client in pi-sh pi-cp pi-sftp; do [[ $("$stage/bin with spaces/$client") == fixture-ok ]]; done
# A valid checksum with a missing client must not partially replace an installation.
mkdir "$stage/missing" "$stage/missing-release"
cp "$stage/files/pi-sh" "$stage/missing/pi-sh"
tar -czf "$stage/missing-release/$archive" -C "$stage/missing" .
(cd "$stage/missing-release"; if command -v sha256sum >/dev/null; then sha256sum "$archive"; else shasum -a 256 "$archive"; fi) > "$stage/missing-release/SHA256SUMS"
if bash install.sh --from-dir "$stage/missing-release" --bin-dir "$stage/bin with spaces" > "$stage/error" 2>&1; then echo 'Incomplete archive accepted' >&2; exit 1; fi
for client in pi-sh pi-cp pi-sftp; do [[ $("$stage/bin with spaces/$client") == fixture-ok ]]; done
# Download path exercised with a local curl stand-in: no network or release is used.
mkdir "$stage/tools"
cat > "$stage/tools/curl" <<'CURL'
#!/usr/bin/env bash
set -euo pipefail
output=''; url=''; resolve=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    -o) output=$2; shift 2;;
    -w) resolve=1; shift 2;;
    --proto|--proto-redir) [[ $2 == '=https' ]]; shift 2;;
    https://*) url=$1; shift;;
    -fsSL) shift;;
    *) exit 9;;
  esac
done
if [[ $resolve == 1 ]]; then
  [[ $url == https://github.com/paologiua/pi-sh/releases/latest ]]
  printf 'https://github.com/paologiua/pi-sh/releases/tag/v0.1.0'
else
  [[ $url == https://github.com/paologiua/pi-sh/releases/download/v0.1.0-alpha.1/* || $url == https://github.com/paologiua/pi-sh/releases/download/v0.1.0/* ]]
  cp "$PI_SH_INSTALL_FIXTURES/${url##*/}" "$output"
fi
CURL
chmod +x "$stage/tools/curl"
PATH="$stage/tools:$PATH" PI_SH_INSTALL_FIXTURES="$stage/releases" bash install.sh --repo paologiua/pi-sh --bin-dir "$stage/download"
for client in pi-sh pi-cp pi-sftp; do [[ $("$stage/download/$client") == fixture-ok ]]; done
# Emulate the configured script delivered through curl | bash, without arguments.
cat install.sh | env -u PI_SH_REPO PATH="$stage/tools:$PATH" PI_SH_INSTALL_FIXTURES="$stage/releases" PI_SH_BIN_DIR="$stage/piped" bash
for client in pi-sh pi-cp pi-sftp; do [[ $("$stage/piped/$client") == fixture-ok ]]; done
# Stable-latest resolution remains available explicitly; alpha is the default.
PATH="$stage/tools:$PATH" PI_SH_INSTALL_FIXTURES="$stage/releases" bash install.sh --version latest --bin-dir "$stage/latest"
for client in pi-sh pi-cp pi-sftp; do [[ $("$stage/latest/$client") == fixture-ok ]]; done
printf 'corruption' >> "$stage/releases/$archive"
if bash install.sh --from-dir "$stage/releases" --bin-dir "$stage/should-not-exist" > "$stage/error" 2>&1; then echo 'Corrupt archive accepted' >&2; exit 1; fi
[[ ! -e $stage/should-not-exist ]]
if bash install.sh --repo invalid --bin-dir "$stage/invalid" > "$stage/error" 2>&1; then echo 'Invalid repository accepted' >&2; exit 1; fi
[[ ! -e $stage/invalid ]]
echo 'Installer checks passed (local fixtures; no published release exercised).'
