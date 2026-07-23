#!/usr/bin/env bash
# Spin up a local seid node, flood it with EVM transfers from N accounts to one
# recipient, and print only branch-specific logs + block time.
#
# Usage: ./scripts/evm_stress.sh
# Run from the repo root.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SEID="$HOME/go/bin/seid"
LOG_FILE="/tmp/seid_stress.log"

# Genesis balance per sender account. 10^12 usei ≈ unlimited for any test run.
SENDER_GENESIS_FUNDS="1000000000000"

cd "$REPO_ROOT"

cleanup() {
  echo ""
  echo "==> shutting down..."
  [ -n "${SEID_PID:-}" ] && kill "$SEID_PID" 2>/dev/null || true
  # Kill the entire process group so tail and grep children are also terminated.
  [ -n "${LOG_PID:-}" ]  && kill -- -"$LOG_PID" 2>/dev/null || true
}
trap cleanup EXIT INT TERM

# Kill any tail processes from previous runs that are still watching this log
# file. tail -F re-opens the file by name when it is truncated, so a stale
# tail would re-read the new run's log and emit duplicate lines.
pkill -f "tail.*${LOG_FILE}" 2>/dev/null || true

# ---------------------------------------------------------------------------
# 1. Init chain (no start)
# ---------------------------------------------------------------------------
echo "==> initializing chain..."
NO_RUN=1 ./scripts/initialize_local_chain.sh

# ---------------------------------------------------------------------------
# 2. Bulk-add sender accounts to genesis via direct JSON patch.
#    go run -dump-sei-addrs generates all bech32 addresses; Python patches
#    genesis.json in one pass (same technique as populate_genesis_accounts.py).
# ---------------------------------------------------------------------------
cat > /tmp/evm_stress_patch_genesis.py << 'PYEOF'
import json, sys

genesis_path = sys.argv[1]
amount = sys.argv[2]
denom = "usei"

addrs = [l.strip() for l in sys.stdin if l.strip()]

with open(genesis_path) as f:
    g = json.load(f)

for addr in addrs:
    g["app_state"]["auth"]["accounts"].append({
        "@type": "/cosmos.auth.v1beta1.BaseAccount",
        "address": addr,
        "pub_key": None,
        "account_number": "0",
        "sequence": "0",
    })
    g["app_state"]["bank"]["balances"].append({
        "address": addr,
        "coins": [{"denom": denom, "amount": amount}],
    })

with open(genesis_path, "w") as f:
    json.dump(g, f, separators=(",", ":"))

print(f"Added {len(addrs)} accounts to genesis", file=sys.stderr)
PYEOF

echo "==> patching genesis with sender accounts..."
go run "$REPO_ROOT/scripts/evm_stress/main.go" -dump-sei-addrs \
  | python3 /tmp/evm_stress_patch_genesis.py \
      "$HOME/.sei/config/genesis.json" "$SENDER_GENESIS_FUNDS"
echo "==> genesis patched"

# ---------------------------------------------------------------------------
# 3. Start seid, capturing all output to log file
# ---------------------------------------------------------------------------
echo "==> starting seid (logs -> $LOG_FILE)..."
mkdir -p /tmp/race
GORACE="log_path=/tmp/race/seid_race" \
  "$SEID" start --trace --chain-id sei-chain > "$LOG_FILE" 2>&1 &
SEID_PID=$!
echo "==> seid PID: $SEID_PID"

# ---------------------------------------------------------------------------
# 4. Tail log file, printing only branch-specific messages
#    - "occ scheduler key conflicts" from sei-cosmos/tasks/scheduler.go
#    - "execution block time" from sei-cosmos/baseapp/abci.go (debug-level; shows only at --log_level debug)
# ---------------------------------------------------------------------------
(
  tail -F "$LOG_FILE" 2>/dev/null \
  | grep --line-buffered -E \
      '"occ scheduler key conflicts"|"execution block time"'
) &
LOG_PID=$!

# ---------------------------------------------------------------------------
# 5. Wait for the EVM RPC to accept connections
# ---------------------------------------------------------------------------
echo "==> waiting for EVM RPC at http://127.0.0.1:8545..."
for i in $(seq 1 60); do
  if curl -sf -X POST http://127.0.0.1:8545 \
      -H "Content-Type: application/json" \
      -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' \
      > /dev/null 2>&1; then
    echo "==> EVM RPC ready (after ${i}s)"
    break
  fi
  if [ "$i" -eq 60 ]; then
    echo "ERROR: EVM RPC not ready after 60s" >&2
    exit 1
  fi
  sleep 1
done

# ---------------------------------------------------------------------------
# 6. Run the Go load tester
# ---------------------------------------------------------------------------
echo "==> starting EVM transfer stress test (target 500 TPS, 50k unique senders)..."
go run "$REPO_ROOT/scripts/evm_stress/main.go"
