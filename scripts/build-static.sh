#!/usr/bin/env bash
# Build the statically-linked linux/amd64 `seid` in Alpine (musl-native).
# Single source of truth for the static build: used by the goreleaser `before:` hook,
# the cross-arch CI guard, and local runs (`goreleaser release --snapshot`).
#
# Ubuntu's musl-gcc can't fully static-link on 24.04 (glibc libgcc -> _dl_find_object,
# absent in musl) and zig cc rejects the -z muldefs flag needed for the libwasmvm
# v152/v155 archives; Alpine's GNU ld + musl links cleanly. --platform linux/amd64 is a
# no-op on amd64 CI and forces the right arch on Apple Silicon for local runs.
set -euo pipefail
docker run --rm --platform linux/amd64 -v "$PWD":/src -w /src golang:1.25.6-alpine sh -c '
  apk add --no-cache build-base git &&
  git config --global --add safe.directory /src &&
  LINK_STATICALLY=true BUILD_TAGS=muslc LEDGER_ENABLED=false make build'
file build/seid
