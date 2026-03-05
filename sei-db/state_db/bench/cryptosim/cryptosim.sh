#!/usr/bin/env bash

# Resolve script directory (handles symlinks and relative paths).
SCRIPT_SOURCE="${BASH_SOURCE[0]}"
[[ "$SCRIPT_SOURCE" != /* ]] && SCRIPT_SOURCE="$(pwd)/${SCRIPT_SOURCE#./}"
while [[ -L "$SCRIPT_SOURCE" ]]; do
  SCRIPT_DIR="$(cd "$(dirname "$SCRIPT_SOURCE")" && pwd)"
  SCRIPT_SOURCE="$(readlink "$SCRIPT_SOURCE")"
  [[ "$SCRIPT_SOURCE" != /* ]] && SCRIPT_SOURCE="${SCRIPT_DIR}/${SCRIPT_SOURCE}"
done
SCRIPT_DIR="$(cd "$(dirname "$SCRIPT_SOURCE")" && pwd)"
BINARY="${SCRIPT_DIR}/bin/cryptosim"

# Build binary if needed.
[[ ! -x "$BINARY" ]] && make -C "$SCRIPT_DIR" build

# Run the benchmark.
exec "$BINARY" "$@"
