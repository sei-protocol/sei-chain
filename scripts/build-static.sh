#!/usr/bin/env bash
# Build a statically-linked `seid` in Alpine (musl-native) for one target architecture.
# Single source of truth for the static build: used by the goreleaser `before:` hooks,
# the cross-arch CI guard, and local runs (`goreleaser release --snapshot`). It also
# self-verifies (required libwasmvm archives present, the pinned libgcc matches its
# checksums, the output is actually static and carries no b-tree unwinder) so every
# entry point fails fast rather than producing or shipping a broken binary.
#
# Usage: build-static.sh [amd64|arm64]      (default amd64)
#
# Output is build/seid-<arch>, so the two architectures do not overwrite each other.
#
# Ubuntu's musl-gcc can't fully static-link on 24.04 (glibc libgcc needs _dl_find_object,
# absent in musl) and zig cc rejects the -z muldefs flag needed for the libwasmvm
# v152/v155 archives; Alpine's GNU ld + musl links cleanly. The pinned golang image
# digest is a multi-arch index, so the same pin serves both targets. Building a
# non-native architecture needs binfmt registered on the host.
#
# The link takes libgcc from third_party/alpine-gcc10-libgcc/<arch> instead of the build
# image's toolchain: gcc >= 12's unwind-frame registry (a lock-free b-tree) corrupts
# under wasmer's JIT frame registration and SIGSEGVs at the genesis wasm store on most
# boots, on both architectures. See that directory's README.md for the full story and
# provenance. The nm assertion below keeps a toolchain upgrade from silently
# reintroducing the b-tree.
set -euo pipefail

ARCH="${1:-amd64}"
case "$ARCH" in
  amd64) LIBGCC_ARCH=x86_64  ;;
  arm64) LIBGCC_ARCH=aarch64 ;;
  *) echo "build-static: unsupported architecture '$ARCH' (want amd64 or arm64)" >&2; exit 1 ;;
esac

# Fail fast if the required static libwasmvm archives are missing.
bash "$(dirname "$0")/check-libwasmvm-static.sh"

LIBGCC_DIR="third_party/alpine-gcc10-libgcc/$LIBGCC_ARCH"
OUT="build/seid-$ARCH"

# The checksums and the -L directory are both derived from $LIBGCC_ARCH above, so a
# build cannot verify one architecture's archives while linking another's.
case "$LIBGCC_ARCH" in
  x86_64)
    LIBGCC_SHA=d3e066fafde74d53a89d48f2ceb9ed9934249a5d450e281edd22947a829469d8
    LIBGCC_EH_SHA=d14c9973a735909e11a863b0c850300bfd3aa683ef4689cbe76a53139766ed79
    ;;
  aarch64)
    LIBGCC_SHA=119d1714e0a2b47e1d829d0e92dc3ab51a25e1778f67ed43d2e6c87469573cc2
    LIBGCC_EH_SHA=2963a26e62a46ee283c5463ebaad176ddf74b6a049850df833cee5f333ca57a9
    ;;
esac

echo "build-static: target linux/$ARCH, libgcc pin $LIBGCC_DIR"

docker run --rm --platform "linux/$ARCH" -v "$PWD":/src -w /src golang:1.25.6-alpine@sha256:98e6cffc31ccc44c7c15d83df1d69891efee8115a5bb7ede2bf30a38af3e3c92 sh -c '
  set -e
  apk add --no-cache build-base git
  git config --global --add safe.directory /src
  printf "%s  %s\n%s  %s\n" \
    '"$LIBGCC_SHA"' '"$LIBGCC_DIR"'/libgcc.a \
    '"$LIBGCC_EH_SHA"' '"$LIBGCC_DIR"'/libgcc_eh.a \
    | sha256sum -c -
  LINK_STATICALLY=true BUILD_TAGS=muslc LEDGER_ENABLED=false \
    STATIC_EXTRA_LDFLAGS="-L/src/'"$LIBGCC_DIR"'" make build
  mv build/seid '"$OUT"'
  # Materialise the symbol table as its own command so `set -e` aborts when nm fails.
  # Reading nm through a pipe into grep reports the grep status, so a broken nm would
  # otherwise read as "no b-tree symbols found" and pass.
  nm '"$OUT"' > /tmp/seid.syms
  if grep -q version_lock_lock_exclusive /tmp/seid.syms; then
    echo "build-static: ERROR: binary contains the gcc>=12 unwind b-tree (libgcc pin not applied)" >&2
    exit 1
  fi
  echo "build-static: pre-b-tree unwinder confirmed (no version_lock symbols)"'

# Assert the output really is statically linked and built for the requested architecture,
# so a regression fails here rather than shipping a mislabelled binary.
info="$(file "$OUT")"
echo "$info"
case "$info" in
  *"statically linked"*) ;;
  *) echo "build-static: ERROR: $OUT is not statically linked" >&2; exit 1 ;;
esac
case "$ARCH:$info" in
  amd64:*x86-64*|arm64:*aarch64*) ;;
  *) echo "build-static: ERROR: $OUT is not a linux/$ARCH binary" >&2; exit 1 ;;
esac
