#!/usr/bin/env bash
# Builds and runs the lakesim benchmark. Usage: ./run.sh <config-file>

set -euo pipefail

# Resolve script directory (handles symlinks and relative paths).
SCRIPT_SOURCE="${BASH_SOURCE[0]}"
[[ "$SCRIPT_SOURCE" != /* ]] && SCRIPT_SOURCE="$(pwd)/${SCRIPT_SOURCE#./}"
while [[ -L "$SCRIPT_SOURCE" ]]; do
  SCRIPT_DIR="$(cd "$(dirname "$SCRIPT_SOURCE")" && pwd)"
  SCRIPT_SOURCE="$(readlink "$SCRIPT_SOURCE")"
  [[ "$SCRIPT_SOURCE" != /* ]] && SCRIPT_SOURCE="${SCRIPT_DIR}/${SCRIPT_SOURCE}"
done
SCRIPT_DIR="$(cd "$(dirname "$SCRIPT_SOURCE")" && pwd)"

# Build the binary (no-op if already up to date; Go's build cache handles staleness).
make -C "$SCRIPT_DIR" build

exec "${SCRIPT_DIR}/bin/lakesim" "$@"
