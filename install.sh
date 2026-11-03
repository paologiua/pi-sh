#!/usr/bin/env bash
# Install published pi-sh release binaries. Never touches the remote Raspberry Pi.
set -euo pipefail
usage() {
  cat <<'HELP'
usage: install.sh [--repo owner/name] [--version tag|latest] [--bin-dir path]
                  [--from-dir directory] [--downloader curl|wget] [--keep-old] [--help]
Defaults: PI_SH_REPO or the configured repository, PI_SH_VERSION=v0.1.0-alpha.4,
          PI_SH_BIN_DIR=$HOME/.local/bin (on iSH: /usr/local/bin when writable)
--from-dir installs from a local directory containing release archives and SHA256SUMS.
--downloader (or PI_SH_DOWNLOADER) picks the download tool; by default curl is
used when present, otherwise wget.
On iSH the binaries go to <bin-dir>/../libexec/pi-sh and <bin-dir> receives
launchers that default GOMAXPROCS=1 and PI_SH_ISH_RELAY=1 (both overridable).
PI_SH_INSTALL_ISH=1|0 forces or disables iSH detection.
On iSH, earlier pi-sh/pi-cp/pi-sftp copies in ~/.local/bin and ~/bin (and their
../libexec/pi-sh) are removed when they are files from this project;
--keep-old leaves them in place. Symlinks and unrelated files are never touched.
No sudo, Go compiler, credentials or Raspberry Pi access is needed.
HELP
}
# Override with PI_SH_REPO or --repo when using a fork.
repo=${PI_SH_REPO:-paologiua/pi-sh}
version=${PI_SH_VERSION:-v0.1.0-alpha.4}
bin_dir=${PI_SH_BIN_DIR:-}
from_dir=''
keep_old=0
downloader=${PI_SH_DOWNLOADER:-}
while [[ $# -gt 0 ]]; do
  case "$1" in
    -h|--help) usage; exit 0 ;;
    --keep-old) keep_old=1; shift ;;
    --repo|--version|--bin-dir|--from-dir|--downloader)
      [[ $# -ge 2 && -n $2 ]] || { echo "Missing value for $1" >&2; exit 2; }
      case "$1" in --repo) repo=$2;; --version) version=$2;; --bin-dir) bin_dir=$2;; --from-dir) from_dir=$2;; --downloader) downloader=$2;; esac
      shift 2 ;;
    *) echo "Unknown option: $1" >&2; usage >&2; exit 2 ;;
  esac
done
case $downloader in ''|curl|wget) ;; *) echo 'The downloader must be curl or wget.' >&2; exit 2 ;; esac
case $(uname -s) in Darwin) os=darwin;; Linux) os=linux;; *) echo 'Use the Windows release archive on Windows; this installer supports macOS/Linux/iSH.' >&2; exit 1;; esac
case $(uname -m) in x86_64|amd64) arch=amd64;; arm64|aarch64) arch=arm64;; i386|i486|i586|i686|x86) arch=386;; *) echo 'Unsupported architecture.' >&2; exit 1;; esac
[[ $os != darwin || $arch != 386 ]] || { echo 'macOS/386 is unsupported.' >&2; exit 1; }
# iSH reports itself through /proc/ish and an "-ish" kernel release.
case ${PI_SH_INSTALL_ISH:-auto} in
  1) ish=1 ;;
  0) ish=0 ;;
  auto) ish=0; if [[ $os/$arch == linux/386 ]] && { [[ -e /proc/ish ]] || [[ $(uname -r) == *ish* ]]; }; then ish=1; fi ;;
  *) echo 'PI_SH_INSTALL_ISH must be 0 or 1.' >&2; exit 2 ;;
esac
if [[ -z $bin_dir ]]; then
  # iSH's default shell runs as root with /usr/local/bin on PATH; ~/.local/bin is not.
  if [[ $ish == 1 && -d /usr/local/bin && -w /usr/local/bin ]]; then bin_dir=/usr/local/bin
  else bin_dir=${HOME:?HOME is required}/.local/bin; fi
fi
archive="pi-sh_${os}_${arch}.tar.gz"
command -v tar >/dev/null || { echo 'tar is required.' >&2; exit 1; }
if command -v sha256sum >/dev/null; then hash_tool=sha256sum
elif command -v shasum >/dev/null; then hash_tool=shasum
else echo 'sha256sum or shasum is required.' >&2; exit 1; fi
stage=$(mktemp -d "${TMPDIR:-/tmp}/pi-sh-install.XXXXXXXX")
trap 'rm -rf "$stage"' EXIT
if [[ -n $from_dir ]]; then
  cp "$from_dir/$archive" "$stage/$archive"
  cp "$from_dir/SHA256SUMS" "$stage/SHA256SUMS"
else
  [[ $repo =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]] || { echo 'Set PI_SH_REPO=owner/name or pass --repo owner/name.' >&2; exit 2; }
  [[ $version =~ ^[A-Za-z0-9_.-]+$ && $version != . && $version != .. ]] || { echo 'Invalid release tag.' >&2; exit 2; }
  # curl when present, else wget (iSH/Alpine ship BusyBox wget, often without curl).
  case $downloader in
    curl|wget) command -v "$downloader" >/dev/null || { echo "$downloader is not installed." >&2; exit 1; } ;;
    '') if command -v curl >/dev/null; then downloader=curl
        elif command -v wget >/dev/null; then downloader=wget
        else echo 'curl or wget is required.' >&2; exit 1; fi ;;
  esac
  wget_opts=(-q)
  # GNU wget can refuse non-HTTPS redirects; BusyBox wget has no such option.
  if [[ $downloader == wget ]] && wget --help 2>&1 | grep -q -e '--https-only'; then wget_opts+=(--https-only); fi
  fetch() { # url output
    if [[ $downloader == curl ]]; then curl -fsSL --proto '=https' --proto-redir '=https' "$1" -o "$2"
    else wget "${wget_opts[@]}" -O "$2" "$1"; fi
  }
  base="https://github.com/$repo/releases"
  if [[ $version == latest ]]; then
    # Resolve once: checksum and archive must belong to the same release.
    if [[ $downloader == curl ]]; then
      resolved=$(curl -fsSL --proto '=https' --proto-redir '=https' -o /dev/null -w '%{url_effective}' "$base/latest")
      version=${resolved##*/}
      [[ $resolved == "$base/tag/$version" ]] || version=''
    else
      # wget cannot report the final URL portably; ask the API for the stable tag.
      fetch "https://api.github.com/repos/$repo/releases/latest" "$stage/latest.json" || true
      version=$(sed -n 's/^[[:space:]]*"tag_name":[[:space:]]*"\([^"]*\)".*/\1/p' "$stage/latest.json" 2>/dev/null | head -n 1)
    fi
    [[ $version =~ ^[A-Za-z0-9_.-]+$ && $version != . && $version != .. ]] || { echo 'Cannot resolve latest release.' >&2; exit 1; }
  fi
  for file in "$archive" SHA256SUMS; do
    fetch "$base/download/$version/$file" "$stage/$file" || { echo "Download failed: $file ($version); nothing installed." >&2; exit 1; }
  done
fi
expected=$(awk -v file="$archive" '$2 == file && length($1) == 64 {print $1}' "$stage/SHA256SUMS")
[[ $expected =~ ^[0-9a-fA-F]{64}$ ]] || { echo 'Missing or ambiguous release checksum.' >&2; exit 1; }
if [[ $hash_tool == sha256sum ]]; then actual=$(sha256sum "$stage/$archive"); else actual=$(shasum -a 256 "$stage/$archive"); fi
[[ ${actual%% *} == "$expected" ]] || { echo 'Release checksum mismatch; nothing installed.' >&2; exit 1; }
mkdir "$stage/unpacked"
# Extract only the three expected regular executable files, never arbitrary paths.
for client in pi-sh pi-cp pi-sftp; do
  tar -xOzf "$stage/$archive" "./$client" > "$stage/unpacked/$client"
  [[ -s $stage/unpacked/$client ]] || { echo "Empty binary: $client" >&2; exit 1; }
done
mkdir -p "$bin_dir"
if [[ $ish == 1 ]]; then
  # Go reads GOMAXPROCS before main runs, so a launcher must set it.
  lib_dir="$(cd "$bin_dir/.." && pwd -P)/libexec/pi-sh"
  mkdir -p "$lib_dir"
  escaped_quote="'\\''"
  quote() { printf "'%s'" "${1//\'/$escaped_quote}"; }
  for client in pi-sh pi-cp pi-sftp; do
    install -m 755 "$stage/unpacked/$client" "$lib_dir/$client"
    {
      printf '#!/bin/sh\n'
      printf '# iSH launcher written by the pi-sh installer; exported values take precedence.\n'
      printf ': "${GOMAXPROCS:=1}"\n: "${PI_SH_ISH_RELAY:=1}"\n'
      printf 'export GOMAXPROCS PI_SH_ISH_RELAY\n'
      printf 'exec %s "$@"\n' "$(quote "$lib_dir/$client")"
    } > "$stage/launcher"
    install -m 755 "$stage/launcher" "$bin_dir/$client"
  done
  echo "Installed pi-sh, pi-cp and pi-sftp in $bin_dir (iSH launchers; binaries in $lib_dir)"
  echo 'iSH: launchers set GOMAXPROCS=1 PI_SH_ISH_RELAY=1 by default; broader runtime support is unverified.'
else
  for client in pi-sh pi-cp pi-sftp; do
    install -m 755 "$stage/unpacked/$client" "$bin_dir/$client"
  done
  echo "Installed pi-sh, pi-cp and pi-sftp in $bin_dir"
  if [[ $os/$arch == linux/386 ]]; then echo 'iSH not detected; on iSH start with GOMAXPROCS=1 PI_SH_ISH_RELAY=1 or rerun with PI_SH_INSTALL_ISH=1.'; fi
fi
# Earlier installs used ~/.local/bin (installer default) or ~/bin (manual copies),
# which iSH's PATH may not search, or may search before the new launchers.
if [[ $ish == 1 && $keep_old == 0 && -n ${HOME:-} ]]; then
  ours() { [[ -f $1 && ! -L $1 ]] && LC_ALL=C grep -q -e "pi-sh/cmd/$2" -e '^# iSH launcher written by the pi-sh installer' "$1"; }
  new_real=$(cd "$bin_dir" && pwd -P)
  for old_dir in "$HOME/.local/bin" "$HOME/bin"; do
    [[ -d $old_dir && $(cd "$old_dir" && pwd -P) != "$new_real" ]] || continue
    old_lib="$(cd "$old_dir/.." && pwd -P)/libexec/pi-sh"
    for client in pi-sh pi-cp pi-sftp; do
      for old in "$old_dir/$client" "$old_lib/$client"; do
        [[ $old != "$lib_dir/$client" ]] || continue
        if ours "$old" "$client"; then rm -f "$old" && echo "Removed earlier install: $old"; fi
      done
    done
    [[ ! -d $old_lib || $old_lib == "$lib_dir" ]] || rmdir "$old_lib" 2>/dev/null || true
  done
fi
# Go verifies HTTPS against these system locations (or SSL_CERT_FILE/SSL_CERT_DIR);
# iSH's Alpine image may lack them, which makes every sign-in fail.
if [[ ( $os == linux || -n ${PI_SH_INSTALL_CA_ROOT:-} ) && -z ${SSL_CERT_FILE:-} && -z ${SSL_CERT_DIR:-} ]]; then
  ca_root=${PI_SH_INSTALL_CA_ROOT:-}
  ca_found=0
  for ca in /etc/ssl/certs/ca-certificates.crt /etc/pki/tls/certs/ca-bundle.crt /etc/ssl/ca-bundle.pem \
    /etc/pki/tls/cacert.pem /etc/pki/ca-trust/extracted/pem/tls-ca-bundle.pem /etc/ssl/cert.pem; do
    if [[ -s $ca_root$ca ]]; then ca_found=1; break; fi
  done
  if [[ $ca_found == 0 ]]; then
    for ca in "$ca_root/etc/ssl/certs" "$ca_root/etc/pki/tls/certs"; do
      if [[ -d $ca ]] && ls "$ca" 2>/dev/null | grep -q -e '\.pem$' -e '\.crt$'; then ca_found=1; break; fi
    done
  fi
  if [[ $ca_found == 0 ]]; then
    if command -v apk >/dev/null; then hint='apk add ca-certificates'; else hint="install your distribution's ca-certificates package"; fi
    echo "Warning: no system CA certificates found, so sign-in will fail TLS verification; $hint." >&2
  fi
fi
case ":${PATH:-}:" in
  *":$bin_dir:"*)
    found=$(command -v pi-sh || true)
    [[ -z $found || $found == "$bin_dir/pi-sh" ]] || echo "Note: $found shadows $bin_dir/pi-sh on PATH." ;;
  *) printf 'Add this directory to PATH: export PATH="%s:$PATH"\n' "$bin_dir";;
esac
