#!/usr/bin/env bash
set -euo pipefail

GOOD_GAS_HEX=0x471ed3
RPC_HOST=${RPC_HOST:-}
RPC_PORT=8545

if [ -z "$RPC_HOST" ]; then
  echo "RPC_HOST must be set (IP address)" >&2
  exit 1
fi

RPC_URL="http://${RPC_HOST}:${RPC_PORT}"

response=$(curl -sS "$RPC_URL" \
  -X POST \
  -H "Content-Type: application/json" \
  --data '{"method":"debug_traceTransaction","params":["0x49df2e4ddc40c3d99c4157283604b5ceb3b1dafeefa322f8ae1e87ead5b76fda", {"tracer": "callTracer"}], "id":1,"jsonrpc":"2.0"}')

gas_used=$(printf '%s' "$response" | jq -r '.result.gasUsed // empty')

if [ -z "$gas_used" ] || [ "$gas_used" = "null" ]; then
  echo "gasUsed missing in response" >&2
  exit 1
fi

gas_used_lower=$(printf '%s' "$gas_used" | tr '[:upper:]' '[:lower:]')

if [ "$gas_used_lower" = "$GOOD_GAS_HEX" ]; then
  exit 0
fi

exit 1
