#!/usr/bin/env bash
# Local tests only. Never starts a Connect/SSH session or writes to a real Pi.
set -euo pipefail
cd "$(dirname "$0")/.."
mode=${1:-local}
case "$mode" in
  -h|--help)
    echo 'usage: scripts/test.sh [local|full]'
    echo 'local: unit/integration tests, vet, module integrity, browser tests, platform checks.'
    echo 'full: also race tests, Bash 5.2.37/5.3 fallback checks, seven cross-build/test compilations.'
    echo 'full requires PI_SH_TEST_BASH_52 and PI_SH_TEST_BASH_53 executable paths.'
    echo 'macOS/Linux: Bash >=4.3 required; set PI_SH_TEST_BASH or use Bash on PATH/Homebrew.'
    echo 'Optional external-tool fixtures may skip; consult output and docs/TESTING.md.'
    exit 0 ;;
  local|full) ;;
  *) echo 'usage: scripts/test.sh [local|full]' >&2; exit 2 ;;
esac
for tool in go gofmt node; do command -v "$tool" >/dev/null || { echo "$tool is required on PATH." >&2; exit 1; }; done
if [[ $mode == full ]]; then
  for pair in "${PI_SH_TEST_BASH_52:-}|5.2.37" "${PI_SH_TEST_BASH_53:-}|5.3"; do
    path=${pair%|*}; required=${pair#*|}
    [[ -x $path ]] || { echo 'Set PI_SH_TEST_BASH_52 and PI_SH_TEST_BASH_53 to existing Bash executables.' >&2; exit 1; }
    found=$("$path" -c 'printf "%s" "$BASH_VERSION"')
    [[ $found == "$required"* ]] || { echo "Expected Bash $required, found $found" >&2; exit 1; }
  done
fi
# The simulated remote shell needs modern Bash on the development host. Select
# it once for every Go test process, including subprocess fixtures.
case $(go env GOHOSTOS) in
  darwin|linux)
    if [[ -n ${PI_SH_TEST_BASH:-} ]]; then
      bash_candidates=("$PI_SH_TEST_BASH")
    elif [[ $mode == full ]]; then
      bash_candidates=("$PI_SH_TEST_BASH_53")
    else
      bash_candidates=("$(command -v bash || true)" /opt/homebrew/bin/bash /usr/local/bin/bash)
    fi
    selected_bash=''
    for candidate in "${bash_candidates[@]}"; do
      if [[ -n $candidate ]] && "$candidate" -c '((BASH_VERSINFO[0]>4||(BASH_VERSINFO[0]==4&&BASH_VERSINFO[1]>=3)))' >/dev/null 2>&1; then
        selected_bash=$candidate
        break
      fi
    done
    if [[ -z $selected_bash ]]; then
      echo 'Bash >=4.3 is required for local integration tests.' >&2
      echo 'Set PI_SH_TEST_BASH=/path/to/modern/bash (macOS system Bash 3.2 is too old).' >&2
      exit 2
    fi
    export PI_SH_TEST_BASH="$selected_bash"
    printf 'Local shell fixtures use Bash: %s\n' "$PI_SH_TEST_BASH"
    ;;
esac
fail=0
run() {
  printf '\nChecking:'; printf ' %q' "$@"; printf '\n'
  if "$@"; then :; else fail=$((fail+1)); fi
}
formatted=$(gofmt -l cmd internal)
if [[ -n $formatted ]]; then printf 'Formatting required:\n%s\n' "$formatted"; fail=$((fail+1)); fi
run go mod verify
run go vet ./...
# Upstream stdmethods warnings are documented; do not suppress other checks.
run go vet -C third_party/sftp -stdmethods=false ./...
run go test -count=1 -timeout=300s ./...
run go test -C third_party/sftp -count=1 -timeout=120s ./...
run node --test internal/browserauth/companion_test.mjs
run go run ./cmd/platform-check
run bash scripts/test-install.sh
if [[ $mode == full ]]; then
  run go test -race -count=1 -timeout=300s ./...
  run go test -C third_party/sftp -race -count=1 -timeout=120s ./...
  run env PI_SH_TEST_BASH="$PI_SH_TEST_BASH_52" go test -count=1 -timeout=300s ./internal/fwdbroker ./internal/shellbroker ./cmd/pi-sh
  run env PI_SH_TEST_BASH="$PI_SH_TEST_BASH_53" PI_SH_FORWARD_ADAPTER=bash go test -count=1 -timeout=300s ./internal/fwdbroker ./cmd/pi-sh
  run env PI_SH_TEST_BASH="$PI_SH_TEST_BASH_53" PI_SH_BROKER_WORKERS=bash go test -count=1 -timeout=300s ./internal/shellbroker ./cmd/...
  for bash_path in "$PI_SH_TEST_BASH_52" "$PI_SH_TEST_BASH_53"; do
    run env PI_SH_TEST_BASH="$bash_path" PI_SH_REMOTE_IMPL=bash go test -count=1 -timeout=300s ./internal/shellbroker ./internal/fwdbroker ./cmd/...
  done
  stage=$(mktemp -d "${TMPDIR:-/tmp}/pi-sh-tests.XXXXXXXX")
  trap 'rm -rf "$stage"' EXIT
  for target in linux/386 linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64; do
    os=${target%/*}; arch=${target#*/}; mkdir -p "$stage/$os-$arch"
    run env CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" GO386=softfloat go build ./...
    run env CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" GO386=softfloat go test -c -o "$stage/$os-$arch/" ./...
    run env CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" GO386=softfloat go test -C third_party/sftp -c -o "$stage/$os-$arch/sftp-extension.test" .
  done
  run bash scripts/build.sh native
  run go build -o bin/transfer-probe ./cmd/transfer-probe
fi
printf '\nRESULT fail=%s\n' "$fail"
[[ $fail == 0 ]]
