#!/bin/bash

set -euo pipefail

CHAIN_ID=${CHAIN_ID:-"pacific-1"}
NODE=${NODE:-"http://localhost:26657"}

if ! command -v seid >/dev/null 2>&1; then
  echo "error: seid CLI not found in PATH" >&2
  exit 1
fi

echo "Querying Seinet vault balance..."

seid q seinet vault-balance \
  --chain-id "$CHAIN_ID" \
  --node "$NODE"

