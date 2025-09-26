#!/data/data/com.termux/files/usr/bin/bash
#
# Convenience wrapper around build_claim_tx.py for Termux users. It ensures
# Python is invoked from the Termux environment and forwards all CLI arguments.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
PYTHON_BIN="${PYTHON_BIN:-python3}"

CLI_PATH="$PROJECT_ROOT/tools/solo/build_claim_tx.py"
if [ ! -f "$CLI_PATH" ]; then
  echo "[termux-runner] Unable to locate build_claim_tx.py at $CLI_PATH" >&2
  exit 1
fi

exec "$PYTHON_BIN" "$CLI_PATH" "$@"
