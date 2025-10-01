#!/bin/bash

set -euo pipefail

NETWORK=${NETWORK:-"ethereum"}

if [ "$NETWORK" = "ethereum" ]; then
  VAULT=${VAULT:-"0xd973555aAaa8d50a84d93D15dAc02ABE5c4D00c1"}
  RPC=${RPC:-"https://ethereum.publicnode.com"}

  JSON_PAYLOAD=$(cat <<JSON
{
  "jsonrpc": "2.0",
  "method": "eth_getBalance",
  "params": ["$VAULT", "latest"],
  "id": 1
}
JSON
)

  echo "Checking Ethereum Vault Balance for $VAULT..."

  if command -v cast >/dev/null 2>&1; then
    echo "Using foundry's cast CLI"
    cast balance "$VAULT" --rpc-url "$RPC"
    exit $?
  fi

  echo "cast not found; falling back to curl + python"
  RESPONSE=$(curl -sS -X POST "$RPC" -H "Content-Type: application/json" --data "$JSON_PAYLOAD")
  BALANCE_HEX=$(python3 - <<'PY'
import json, sys
try:
    data = json.loads(sys.stdin.read())
    result = data.get("result")
    if not result:
        raise ValueError("no result in RPC response")
    print(result)
except Exception as exc:
    sys.stderr.write(f"Failed to parse balance: {exc}\n")
    sys.exit(1)
PY
<<< "$RESPONSE")

  python3 - <<PY "$BALANCE_HEX"
from decimal import Decimal, getcontext
import sys
balance_hex = sys.argv[1]
wei = int(balance_hex, 16)
getcontext().prec = 50
ether = Decimal(wei) / Decimal(10**18)
print(f"Balance (hex): {balance_hex}")
print(f"Balance (wei): {wei}")
print(f"Balance (ETH): {ether}")
PY

elif [ "$NETWORK" = "sei" ]; then
  CHAIN_ID=${CHAIN_ID:-"pacific-1"}
  NODE=${NODE:-"http://localhost:26657"}

  if ! command -v seid >/dev/null 2>&1; then
    echo "error: seid CLI not found in PATH" >&2
    exit 1
  fi

  echo "Querying Sei vault balance..."
  seid q seinet vault-balance \
    --chain-id "$CHAIN_ID" \
    --node "$NODE"
else
  echo "Unsupported NETWORK value: $NETWORK (must be ethereum or sei)" >&2
  exit 1
fi
