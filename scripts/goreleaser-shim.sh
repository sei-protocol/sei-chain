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
# The prebuilt binary for each architecture is build/seid-<goarch>, as produced by
# scripts/build-static.sh. Override with $PREBUILT_SEID_AMD64 / $PREBUILT_SEID_ARM64.
set -euo pipefail

if [ "${1:-}" = "build" ]; then
  # Resolve the prebuilt binary for the architecture goreleaser asked for. Anything else
  # fails loudly rather than mislabelling one architecture's binary as another's.
  case "${GOARCH:-}" in
    amd64) PREBUILT="${PREBUILT_SEID_AMD64:-build/seid-amd64}" ;;
    arm64) PREBUILT="${PREBUILT_SEID_ARM64:-build/seid-arm64}" ;;
    *) echo "goreleaser-shim: no prebuilt binary for GOARCH=${GOARCH:-unset}; refusing to package." >&2
       exit 1 ;;
  esac

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

  # A mislabelled archive is worse than a failed release, so check the ELF machine of the
  # resolved binary rather than trusting its filename.
  info="$(file -b "$PREBUILT")"
  case "$GOARCH:$info" in
    amd64:*x86-64*|arm64:*aarch64*) ;;
    *) echo "goreleaser-shim: $PREBUILT is not a $GOARCH binary ($info)." >&2; exit 1 ;;
  esac

  mkdir -p "$(dirname "$out")"
  cp "$PREBUILT" "$out"
  echo "goreleaser-shim: packaged prebuilt $PREBUILT into $out" >&2
  exit 0
fi

exec go "$@"
