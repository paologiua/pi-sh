#!/usr/bin/env bash
# Build only the public clients, with no CGO dependency.
set -euo pipefail
cd "$(dirname "$0")/.."
mode=${1:-native}
case "$mode" in
  -h|--help) echo 'usage: scripts/build.sh [native|all]'; exit 0 ;;
  native|all) ;;
  *) echo 'usage: scripts/build.sh [native|all]' >&2; exit 2 ;;
esac
command -v go >/dev/null || { echo 'Go from go.mod is required on PATH.' >&2; exit 1; }
if [[ $mode == native ]]; then
  mkdir -p bin
  suffix=''; [[ $(go env GOOS) != windows ]] || suffix=.exe
  for client in pi-sh pi-cp pi-sftp; do
    CGO_ENABLED=0 go build -trimpath -o "bin/$client$suffix" "./cmd/$client"
  done
  echo 'Clients built in bin/'
  exit 0
fi
# Supported release matrix, including iSH softfloat. Not a runtime certification.
mkdir -p dist
stage=$(mktemp -d "${TMPDIR:-/tmp}/pi-sh-build.XXXXXXXX")
trap 'rm -rf "$stage"' EXIT
for target in darwin/amd64 darwin/arm64 linux/386 linux/amd64 linux/arm64 windows/amd64 windows/arm64; do
  os=${target%/*}; arch=${target#*/}; name="pi-sh_${os}_${arch}"
  mkdir -p "$stage/$name"
  suffix=''; [[ $os != windows ]] || suffix=.exe
  for client in pi-sh pi-cp pi-sftp; do
    CGO_ENABLED=0 GOOS=$os GOARCH=$arch GO386=softfloat go build -trimpath -o "$stage/$name/$client$suffix" "./cmd/$client"
  done
  cp README.md THIRD_PARTY_NOTICES.md "$stage/$name/"
  cp -R docs "$stage/$name/docs"
  [[ ! -f LICENSE ]] || cp LICENSE "$stage/$name/"
  tar -czf "dist/$name.tar.gz" -C "$stage/$name" .
  echo "Built dist/$name.tar.gz"
done
(
  cd dist
  for target in darwin_amd64 darwin_arm64 linux_386 linux_amd64 linux_arm64 windows_amd64 windows_arm64; do
    archive="pi-sh_$target.tar.gz"
    if command -v sha256sum >/dev/null; then sha256sum "$archive"; else shasum -a 256 "$archive"; fi
  done
) > dist/SHA256SUMS
