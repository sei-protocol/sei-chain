#!/usr/bin/env bash
# Runs the staticmigrate tool without leaving a compiled binary behind.
# Can be invoked from any directory; all arguments are forwarded to the program:
#   ./tools/cmd/staticmigrate/run.sh [-f] [--height N] \
#       <input-memiavl> <out-memiavl> <out-flatkv>
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Build to a temp binary that is removed on exit, so nothing is left behind.
BIN="$(mktemp)"
trap 'rm -f "$BIN"' EXIT

# Build from the package directory so the Go module is resolved correctly
# regardless of the caller's working directory.
( cd "$SCRIPT_DIR" && go build -o "$BIN" . )

# Run from the caller's current directory so relative path arguments resolve
# as the user expects. Not using `exec` so the EXIT trap still cleans up.
"$BIN" "$@"
