#!/usr/bin/env bash
# Install published pi-sh release binaries. Never touches the remote Raspberry Pi.
set -euo pipefail
usage() {
  cat <<'HELP'
usage: install.sh [--repo owner/name] [--version tag|latest] [--bin-dir path]
                  [--from-dir directory] [--help]
Defaults: PI_SH_REPO or the configured repository, PI_SH_VERSION=v0.1.0-alpha.1,
          PI_SH_BIN_DIR=$HOME/.local/bin
--from-dir installs from a local directory containing release archives and SHA256SUMS.
No sudo, Go compiler, credentials or Raspberry Pi access is needed.
HELP
}
# Override with PI_SH_REPO or --repo when using a fork.
repo=${PI_SH_REPO:-paologiua/pi-sh}
version=${PI_SH_VERSION:-v0.1.0-alpha.1}
bin_dir=${PI_SH_BIN_DIR:-${HOME:?HOME is required}/.local/bin}
from_dir=''
while [[ $# -gt 0 ]]; do
  case "$1" in
    -h|--help) usage; exit 0 ;;
    --repo|--version|--bin-dir|--from-dir)
      [[ $# -ge 2 && -n $2 ]] || { echo "Missing value for $1" >&2; exit 2; }
      case "$1" in --repo) repo=$2;; --version) version=$2;; --bin-dir) bin_dir=$2;; --from-dir) from_dir=$2;; esac
      shift 2 ;;
    *) echo "Unknown option: $1" >&2; usage >&2; exit 2 ;;
  esac
done
case $(uname -s) in Darwin) os=darwin;; Linux) os=linux;; *) echo 'Use the Windows release archive on Windows; this installer supports macOS/Linux/iSH.' >&2; exit 1;; esac
case $(uname -m) in x86_64|amd64) arch=amd64;; arm64|aarch64) arch=arm64;; i386|i486|i586|i686|x86) arch=386;; *) echo 'Unsupported architecture.' >&2; exit 1;; esac
[[ $os != darwin || $arch != 386 ]] || { echo 'macOS/386 is unsupported.' >&2; exit 1; }
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
  command -v curl >/dev/null || { echo 'curl is required.' >&2; exit 1; }
  base="https://github.com/$repo/releases"
  if [[ $version == latest ]]; then
    # Resolve once: checksum and archive must belong to the same release.
    resolved=$(curl -fsSL --proto '=https' --proto-redir '=https' -o /dev/null -w '%{url_effective}' "$base/latest")
    version=${resolved##*/}
    [[ $resolved == "$base/tag/$version" && $version =~ ^[A-Za-z0-9_.-]+$ ]] || { echo 'Cannot resolve latest release.' >&2; exit 1; }
  fi
  for file in "$archive" SHA256SUMS; do
    curl -fsSL --proto '=https' --proto-redir '=https' "$base/download/$version/$file" -o "$stage/$file"
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
for client in pi-sh pi-cp pi-sftp; do
  install -m 755 "$stage/unpacked/$client" "$bin_dir/$client"
done
echo "Installed pi-sh, pi-cp and pi-sftp in $bin_dir"
case ":${PATH:-}:" in *":$bin_dir:"*) ;; *) printf 'Add this directory to PATH: export PATH="%s:$PATH"\n' "$bin_dir";; esac
if [[ $os/$arch == linux/386 ]]; then echo 'iSH: start with GOMAXPROCS=1 PI_SH_ISH_RELAY=1; broader runtime support is unverified.'; fi
