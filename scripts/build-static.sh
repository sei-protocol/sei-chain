#!/usr/bin/env bash
# Build the statically-linked linux/amd64 `seid` in Alpine (musl-native).
# Single source of truth for the static build: used by the goreleaser `before:` hook,
# the cross-arch CI guard, and local runs (`goreleaser release --snapshot`). It also
# self-verifies (required libwasmvm archives present, and the output is actually static)
# so every entry point fails fast rather than producing or shipping a broken binary.
#
# Ubuntu's musl-gcc can't fully static-link on 24.04 (glibc libgcc needs _dl_find_object,
# absent in musl) and zig cc rejects the -z muldefs flag needed for the libwasmvm
# v152/v155 archives; Alpine's GNU ld + musl links cleanly. --platform linux/amd64 is a
# no-op on amd64 CI and forces the right arch on Apple Silicon for local runs.
#
# The link takes libgcc from third_party/alpine-gcc10-libgcc instead of the build
# image's toolchain: gcc >= 12's unwind-frame registry (a lock-free b-tree) corrupts
# under wasmer's JIT frame registration and SIGSEGVs at the genesis wasm store on most
# boots, so the static binary must carry the pre-b-tree registry. See that directory's
# README.md for the full story and provenance. The nm assertion below keeps a toolchain
# upgrade from silently reintroducing the b-tree.
set -euo pipefail

# Fail fast if the required static libwasmvm archives are missing.
bash "$(dirname "$0")/check-libwasmvm-static.sh"

LIBGCC_DIR="third_party/alpine-gcc10-libgcc"

docker run --rm --platform linux/amd64 -v "$PWD":/src -w /src golang:1.25.6-alpine sh -c '
  set -e
  apk add --no-cache build-base git
  git config --global --add safe.directory /src
  printf "%s  %s\n%s  %s\n" \
    d3e066fafde74d53a89d48f2ceb9ed9934249a5d450e281edd22947a829469d8 '"$LIBGCC_DIR"'/libgcc.a \
    d14c9973a735909e11a863b0c850300bfd3aa683ef4689cbe76a53139766ed79 '"$LIBGCC_DIR"'/libgcc_eh.a \
    | sha256sum -c -
  LINK_STATICALLY=true BUILD_TAGS=muslc LEDGER_ENABLED=false \
    STATIC_EXTRA_LDFLAGS="-L/src/'"$LIBGCC_DIR"'" make build
  if nm build/seid | grep -q version_lock_lock_exclusive; then
    echo "build-static: ERROR: binary contains the gcc>=12 unwind b-tree (libgcc pin not applied)" >&2
    exit 1
  fi
  echo "build-static: pre-b-tree unwinder confirmed (no version_lock symbols)"'

# Assert the output really is statically linked, so a regression fails here rather than
# shipping a dynamically-linked binary advertised as static.
info="$(file build/seid)"
echo "$info"
case "$info" in
  *"statically linked"*) ;;
  *) echo "build-static: ERROR: build/seid is not statically linked" >&2; exit 1 ;;
esac
