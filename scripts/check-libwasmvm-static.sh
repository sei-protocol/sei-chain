#!/usr/bin/env bash
# Verifies that the static libwasmvm artefacts required for a static `seid`
# build are present. The .a files are produced by `make release-build-alpine`
# in sei-wasmvm and are checked into sei-wasmvm/internal/api/.
#
# Used by .goreleaser.yaml as a `before:` hook so a release tag fails fast
# when the artefacts are missing rather than producing a broken binary.

set -euo pipefail

repo_root="$(git rev-parse --show-toplevel)"

# A muslc static seid links the base libwasmvm (sei-wasmvm) plus the versioned
# CosmWasm VMs v152 + v155 (sei-wasmd); every archive must be present or the
# static link fails late instead of fast.
required=(
  "$repo_root/sei-wasmvm/internal/api/libwasmvm_muslc.a"
  "$repo_root/sei-wasmvm/internal/api/libwasmvm_muslc.aarch64.a"
  "$repo_root/sei-wasmd/x/wasm/artifacts/v152/api/libwasmvm152_muslc.a"
  "$repo_root/sei-wasmd/x/wasm/artifacts/v152/api/libwasmvm152_muslc.aarch64.a"
  "$repo_root/sei-wasmd/x/wasm/artifacts/v155/api/libwasmvm155_muslc.a"
  "$repo_root/sei-wasmd/x/wasm/artifacts/v155/api/libwasmvm155_muslc.aarch64.a"
)

missing=0
for f in "${required[@]}"; do
  if [[ ! -f "$f" ]]; then
    echo "::error::missing static libwasmvm artefact: $f"
    missing=1
  fi
done

if [[ $missing -eq 1 ]]; then
  echo "::error::regenerate the missing muslc archive(s) from their module (sei-wasmvm: 'make release-build-alpine'; sei-wasmd: x/wasm/artifacts)"
  exit 1
fi

echo "All required static libwasmvm artefacts present:"
ls -la "${required[@]}"
