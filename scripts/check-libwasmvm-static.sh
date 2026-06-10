#!/usr/bin/env bash
# Verifies that the static libwasmvm artefacts required for a static `seid`
# build are present. The .a files are produced by `make release-build-alpine`
# in sei-wasmvm and are checked into sei-wasmvm/internal/api/.
#
# Used by .goreleaser.yaml as a `before:` hook so a release tag fails fast
# when the artefacts are missing rather than producing a broken binary.

set -euo pipefail

repo_root="$(git rev-parse --show-toplevel)"
api_dir="$repo_root/sei-wasmvm/internal/api"

required=(
  "$api_dir/libwasmvm_muslc.a"
  "$api_dir/libwasmvm_muslc.aarch64.a"
)

missing=0
for f in "${required[@]}"; do
  if [[ ! -f "$f" ]]; then
    echo "::error::missing static libwasmvm artefact: $f"
    missing=1
  fi
done

if [[ $missing -eq 1 ]]; then
  echo "::error::run 'make release-build-alpine' inside sei-wasmvm/ to (re)produce them"
  exit 1
fi

echo "All required static libwasmvm artefacts present:"
ls -la "${required[@]}"
