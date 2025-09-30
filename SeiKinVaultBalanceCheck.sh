#!/bin/bash

set -euo pipefail

VAULT="0xd973555aAaa8d50a84d93D15dAc02ABE5c4D00c1"
RPC="https://ethereum.publicnode.com"

JSON_PAYLOAD=$(cat <<'JSON'
{
  "jsonrpc": "2.0",
  "method": "eth_getBalance",
  "params": ["VAULT_PLACEHOLDER", "latest"],
  "id": 1
}
JSON
)

JSON_PAYLOAD=${JSON_PAYLOAD//VAULT_PLACEHOLDER/$VAULT}

echo "Checking Vault Balance for $VAULT..."

if command -v cast >/dev/null 2>&1; then
  echo "Using foundry's cast CLI"
  cast balance "$VAULT" --rpc-url "$RPC"
  exit $?
fi

echo "cast not found; falling back to curl + python"
if ! RESPONSE=$(curl -sS -X POST "$RPC" -H "Content-Type: application/json" --data "$JSON_PAYLOAD" 2>&1); then
  echo "Failed to query $RPC:" >&2
  echo "$RESPONSE" >&2
  exit 1
fi

BALANCE_HEX=$(python3 - <<'PY'
import json, sys
try:
    data = json.loads(sys.stdin.read())
    if "error" in data:
        raise ValueError(data["error"])
    result = data.get("result")
    if result is None:
        raise ValueError("missing result field")
    print(result)
except Exception as exc:
    sys.stderr.write(f"Failed to parse balance from RPC response: {exc}\n")
    sys.exit(1)
PY
<<< "$RESPONSE")

if [ -z "$BALANCE_HEX" ]; then
  echo "No balance information returned from RPC" >&2
  exit 1
fi

python3 - <<'PY'
from decimal import Decimal, getcontext
import sys

balance_hex = sys.argv[1]
if not balance_hex.startswith("0x"):
    raise SystemExit("unexpected balance format: " + balance_hex)

wei = int(balance_hex, 16)
getcontext().prec = 50
ether = Decimal(wei) / Decimal(10**18)

print(f"Balance (hex): {balance_hex}")
print(f"Balance (wei): {wei}")
print(f"Balance (ETH): {ether}")
PY
"$BALANCE_HEX"
