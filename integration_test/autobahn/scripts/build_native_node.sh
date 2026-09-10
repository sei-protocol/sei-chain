#!/usr/bin/env bash
set -euo pipefail

repo_dir=${1:?repository directory is required}
cd "$repo_dir"

build_dir=$repo_dir/build
ready_marker=$build_dir/autobahn-native-build.ready
failed_marker=$build_dir/autobahn-native-build.failed
mkdir -p "$build_dir"
rm -f "$ready_marker" "$failed_marker"

record_build_failure() {
  local exit_code=$?
  if (( exit_code != 0 )); then
    printf '%s\n' "$exit_code" > "$failed_marker"
  fi
}
trap record_build_failure EXIT

export LEDGER_ENABLED=false
export PATH="/usr/local/go/bin:$PATH"
make build-linux

case "$(uname -m)" in
  aarch64|arm64) wasm_arch=aarch64 ;;
  x86_64|amd64) wasm_arch=x86_64 ;;
  *) echo "unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

sudo install -d -m 0755 /opt/seid/bin /opt/seid/lib
sudo install -m 0755 build/seid /opt/seid/bin/seid
sudo install -m 0755 "sei-wasmvm/internal/api/libwasmvm.${wasm_arch}.so" /opt/seid/lib/
sudo install -m 0755 "sei-wasmd/x/wasm/artifacts/v152/api/libwasmvm152.${wasm_arch}.so" /opt/seid/lib/
sudo install -m 0755 "sei-wasmd/x/wasm/artifacts/v155/api/libwasmvm155.${wasm_arch}.so" /opt/seid/lib/

touch "$ready_marker"
