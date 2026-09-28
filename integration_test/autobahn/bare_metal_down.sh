#!/usr/bin/env bash
# Stops everything started by bare_metal_up.sh: the 4 validators (node*.pid)
# and the archival RPC fullnode (rpc0.pid). Pass --clean to also delete its
# chain state (build/generated/autobahn-bare-metal), forcing the next
# bare_metal_up.sh to start from a fresh genesis.
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
STATE="$REPO/build/generated/autobahn-bare-metal"

if [ -d "$STATE/logs" ]; then
  for pidfile in "$STATE"/logs/*.pid; do
    [ -f "$pidfile" ] || continue
    pid=$(cat "$pidfile")
    if kill -0 "$pid" 2>/dev/null; then
      kill "$pid"
      echo "stopped $(basename "$pidfile" .pid) (pid $pid)"
    fi
    rm -f "$pidfile"
  done
else
  echo "nothing running (no state at $STATE)"
fi

if [ "${1:-}" = "--clean" ]; then
  rm -rf "$STATE"
  echo "removed $STATE"
fi
