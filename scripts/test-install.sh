#!/usr/bin/env bash
# Deterministic installer contract checks, with local fake binaries only.
set -euo pipefail
unset PI_SH_REPO PI_SH_VERSION PI_SH_BIN_DIR PI_SH_INSTALL_ISH
export PI_SH_INSTALL_ISH=0
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
for client in pi-sh pi-cp pi-sftp; do [[ $("$stage/bin with spaces/$client") == fixture-ok ]] || exit 1; done
# A valid checksum with a missing client must not partially replace an installation.
mkdir "$stage/missing" "$stage/missing-release"
cp "$stage/files/pi-sh" "$stage/missing/pi-sh"
tar -czf "$stage/missing-release/$archive" -C "$stage/missing" .
(cd "$stage/missing-release"; if command -v sha256sum >/dev/null; then sha256sum "$archive"; else shasum -a 256 "$archive"; fi) > "$stage/missing-release/SHA256SUMS"
if bash install.sh --from-dir "$stage/missing-release" --bin-dir "$stage/bin with spaces" > "$stage/error" 2>&1; then echo 'Incomplete archive accepted' >&2; exit 1; fi
for client in pi-sh pi-cp pi-sftp; do [[ $("$stage/bin with spaces/$client") == fixture-ok ]] || exit 1; done
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
  [[ $url == https://github.com/paologiua/pi-sh/releases/latest ]] || exit 1
  printf 'https://github.com/paologiua/pi-sh/releases/tag/v0.1.0'
else
  [[ $url == https://github.com/paologiua/pi-sh/releases/download/v0.1.0-alpha.2/* || $url == https://github.com/paologiua/pi-sh/releases/download/v0.1.0/* ]] || exit 1
  cp "$PI_SH_INSTALL_FIXTURES/${url##*/}" "$output"
fi
CURL
chmod +x "$stage/tools/curl"
PATH="$stage/tools:$PATH" PI_SH_INSTALL_FIXTURES="$stage/releases" bash install.sh --repo paologiua/pi-sh --bin-dir "$stage/download"
for client in pi-sh pi-cp pi-sftp; do [[ $("$stage/download/$client") == fixture-ok ]] || exit 1; done
# Emulate the configured script delivered through curl | bash, without arguments.
cat install.sh | env -u PI_SH_REPO PATH="$stage/tools:$PATH" PI_SH_INSTALL_FIXTURES="$stage/releases" PI_SH_BIN_DIR="$stage/piped" bash
for client in pi-sh pi-cp pi-sftp; do [[ $("$stage/piped/$client") == fixture-ok ]] || exit 1; done
# Stable-latest resolution remains available explicitly; alpha is the default.
PATH="$stage/tools:$PATH" PI_SH_INSTALL_FIXTURES="$stage/releases" bash install.sh --version latest --bin-dir "$stage/latest"
for client in pi-sh pi-cp pi-sftp; do [[ $("$stage/latest/$client") == fixture-ok ]] || exit 1; done
# wget fallback: PATH has only the tools the installer needs, no curl.
mkdir "$stage/nocurl"
for tool in bash env uname tar gzip sha256sum shasum mktemp rm rmdir cp awk sed head mkdir install grep cat chmod dirname; do
  found=$(command -v "$tool" || true); [[ -z $found ]] || ln -s "$found" "$stage/nocurl/$tool"
done
cat > "$stage/nocurl/wget" <<'WGET'
#!/usr/bin/env bash
set -euo pipefail
if [[ ${1:-} == --help ]]; then [[ ${FAKE_GNU_WGET:-0} == 1 ]] && echo '  --https-only  only follow secure HTTPS links'; exit 0; fi
output=''; url=''; https_only=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    -q) shift;;
    -O) output=$2; shift 2;;
    --https-only) https_only=1; shift;;
    https://*) url=$1; shift;;
    *) exit 9;;
  esac
done
[[ $https_only == "${FAKE_GNU_WGET:-0}" ]] || exit 8
case $url in
  https://api.github.com/repos/paologiua/pi-sh/releases/latest)
    printf '{\n  "url": "x",\n  "tag_name": "v0.1.0",\n  "name": "x"\n}\n' > "$output" ;;
  https://github.com/paologiua/pi-sh/releases/download/v0.1.0-alpha.2/*|https://github.com/paologiua/pi-sh/releases/download/v0.1.0/*)
    cp "$PI_SH_INSTALL_FIXTURES/${url##*/}" "$output" ;;
  *) exit 7 ;;
esac
WGET
chmod +x "$stage/nocurl/wget"
for gnu in 0 1; do
  cat install.sh | env PATH="$stage/nocurl" FAKE_GNU_WGET=$gnu PI_SH_INSTALL_FIXTURES="$stage/releases" PI_SH_BIN_DIR="$stage/wget$gnu" bash > /dev/null
  for client in pi-sh pi-cp pi-sftp; do [[ $("$stage/wget$gnu/$client") == fixture-ok ]] || exit 1; done
done
env PATH="$stage/nocurl" PI_SH_INSTALL_FIXTURES="$stage/releases" bash install.sh --version latest --bin-dir "$stage/wget-latest" > /dev/null
for client in pi-sh pi-cp pi-sftp; do [[ $("$stage/wget-latest/$client") == fixture-ok ]] || exit 1; done
if env PATH="$stage/nocurl" PI_SH_INSTALL_FIXTURES="$stage/releases" bash install.sh --version v9.9.9 --bin-dir "$stage/wget-missing" > "$stage/error" 2>&1; then echo 'Missing release accepted' >&2; exit 1; fi
[[ ! -e $stage/wget-missing ]] && grep -q 'Download failed' "$stage/error" || exit 1
rm "$stage/nocurl/wget"
if env PATH="$stage/nocurl" bash install.sh --bin-dir "$stage/no-tool" > "$stage/error" 2>&1; then echo 'Missing downloader accepted' >&2; exit 1; fi
grep -q 'curl or wget is required' "$stage/error" || exit 1
# iSH: launchers on the bin dir set the workaround variables, binaries live in libexec.
mkdir "$stage/envfiles" "$stage/envrelease"
for client in pi-sh pi-cp pi-sftp; do
  printf '#!/bin/sh\nprintf "%%s %%s %%s|" "${0##*/}" "$GOMAXPROCS" "$PI_SH_ISH_RELAY"; printf "[%%s]" "$@"; printf "\\n"\n' > "$stage/envfiles/$client"
done
tar -czf "$stage/envrelease/$archive" -C "$stage/envfiles" .
(cd "$stage/envrelease"; if command -v sha256sum >/dev/null; then sha256sum "$archive"; else shasum -a 256 "$archive"; fi) > "$stage/envrelease/SHA256SUMS"
ish_bin="$stage/ish it's/bin"
# Earlier installs: project binaries (build-info marker) and launchers are removed;
# unrelated files and symlinks stay.
home="$stage/home"
mkdir -p "$home/.local/bin" "$home/.local/libexec/pi-sh" "$home/bin"
for client in pi-sh pi-cp pi-sftp; do
  printf 'ELF\0path\tpi-sh/cmd/%s\n' "$client" > "$home/.local/bin/$client"
  printf '#!/bin/sh\n# iSH launcher written by the pi-sh installer; old\n' > "$home/bin/$client"
  printf 'ELF\0pi-sh/cmd/%s' "$client" > "$home/.local/libexec/pi-sh/$client"
done
printf 'someone else\n' > "$home/bin/pi-sftp"
rm "$home/.local/bin/pi-cp"; ln -s /bin/sh "$home/.local/bin/pi-cp"
PI_SH_INSTALL_ISH=1 HOME="$home" bash install.sh --from-dir "$stage/envrelease" --bin-dir "$ish_bin" > "$stage/out"
[[ ! -e $home/.local/bin/pi-sh && ! -e $home/.local/bin/pi-sftp && -L $home/.local/bin/pi-cp ]] || exit 1
[[ ! -e $home/bin/pi-sh && ! -e $home/bin/pi-cp && $(cat "$home/bin/pi-sftp") == 'someone else' ]] || exit 1
[[ ! -e $home/.local/libexec/pi-sh ]] || exit 1
grep -q "Removed earlier install: $home/.local/bin/pi-sh" "$stage/out" || exit 1
# --keep-old leaves earlier copies alone.
printf 'ELF\0pi-sh/cmd/pi-sh' > "$home/.local/bin/pi-sh"
PI_SH_INSTALL_ISH=1 HOME="$home" bash install.sh --keep-old --from-dir "$stage/envrelease" --bin-dir "$ish_bin" > /dev/null
[[ -e $home/.local/bin/pi-sh ]] || exit 1
# Reinstalling into ~/.local/bin itself keeps the new launchers and binaries.
PI_SH_INSTALL_ISH=1 HOME="$home" bash install.sh --from-dir "$stage/envrelease" --bin-dir "$home/.local/bin" > /dev/null
for client in pi-sh pi-sftp; do [[ $(env -u GOMAXPROCS -u PI_SH_ISH_RELAY "$home/.local/bin/$client") == "$client 1 1|[]" ]] || exit 1; done
for client in pi-sh pi-cp pi-sftp; do
  [[ -x "$stage/ish it's/libexec/pi-sh/$client" ]] || exit 1
  [[ $(env -u GOMAXPROCS -u PI_SH_ISH_RELAY "$ish_bin/$client" 'a b' '$x') == "$client 1 1|[a b][\$x]" ]] || exit 1
  [[ $(GOMAXPROCS=2 PI_SH_ISH_RELAY=0 "$ish_bin/$client") == "$client 2 0|[]" ]] || exit 1
done
if PI_SH_INSTALL_ISH=yes HOME="$home" bash install.sh --from-dir "$stage/envrelease" --bin-dir "$stage/bad-ish" > "$stage/error" 2>&1; then echo 'Invalid PI_SH_INSTALL_ISH accepted' >&2; exit 1; fi
[[ ! -e $stage/bad-ish ]] || exit 1
printf 'corruption' >> "$stage/releases/$archive"
if bash install.sh --from-dir "$stage/releases" --bin-dir "$stage/should-not-exist" > "$stage/error" 2>&1; then echo 'Corrupt archive accepted' >&2; exit 1; fi
[[ ! -e $stage/should-not-exist ]] || exit 1
if bash install.sh --repo invalid --bin-dir "$stage/invalid" > "$stage/error" 2>&1; then echo 'Invalid repository accepted' >&2; exit 1; fi
[[ ! -e $stage/invalid ]] || exit 1
echo 'Installer checks passed (local fixtures; no published release exercised).'
