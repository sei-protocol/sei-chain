#!/usr/bin/env bash
# GoReleaser build-tool shim: an OSS-friendly stand-in for the Pro-only `prebuilt` builder.
#
# GoReleaser invokes this wherever it would invoke `go`. Every subcommand is passed
# straight through to the real `go` EXCEPT `build`, which we intercept: instead of
# recompiling, we hand back the already-built static `seid` (produced in Alpine by the
# goreleaser `before:` hook, scripts/build-static.sh). This keeps the Makefile the single
# source of truth for the build and makes the released binary byte-identical to the static
# muslc `make build` (LINK_STATICALLY/muslc, ledger off), i.e. exactly what GoReleaser
# Pro's `prebuilt` builder would do, but on the OSS distribution. Wired via `builds[].tool`
# in .goreleaser.yaml.
#
# The prebuilt binary path can be overridden with $PREBUILT_SEID (defaults to build/seid).
set -euo pipefail

PREBUILT="${PREBUILT_SEID:-build/seid}"

if [ "${1:-}" = "build" ]; then
  # The prebuilt binary is linux/amd64 only. If goreleaser ever requests another arch
  # (e.g. once arm64 lands, PLT-757), fail loudly rather than mislabel the amd64 binary.
  if [ -n "${GOARCH:-}" ] && [ "$GOARCH" != "amd64" ]; then
    echo "goreleaser-shim: only linux/amd64 is prebuilt; refusing to package for GOARCH=$GOARCH." >&2
    exit 1
  fi

  # Extract the output path GoReleaser asked us to write the binary to. Handle both
  # `-o <path>` (what goreleaser emits) and `-o=<path>`, so the shim is robust either way.
  out=""
  prev=""
  for a in "$@"; do
    case "$a" in -o=*) out="${a#-o=}" ;; esac
    [ "$prev" = "-o" ] && out="$a"
    prev="$a"
  done
  : "${out:?goreleaser-shim: no -o output path found in build args}"

  if [ ! -x "$PREBUILT" ]; then
    echo "goreleaser-shim: prebuilt binary '$PREBUILT' is missing or not executable." >&2
    echo "                 Run scripts/build-static.sh (the goreleaser before: hook) first." >&2
    exit 1
  fi

  mkdir -p "$(dirname "$out")"
  cp "$PREBUILT" "$out"
  echo "goreleaser-shim: packaged prebuilt $PREBUILT into $out" >&2
  exit 0
fi

exec go "$@"
