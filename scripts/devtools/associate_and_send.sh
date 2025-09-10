#!/bin/bash

# Usage: ./associate_and_send.sh <CW20_SENDER> <CW20_RECEIVER> <FROM_WALLET> <AMOUNT> <BASE64_MSG>

CHAIN_ID="pacific-1"
NODE_URL="https://sei-rpc.pacific-1.seinetwork.io"

WASM_SENDER="$1"
WASM_RECEIVER="$2"
SEI_FROM="$3"
AMOUNT="$4"
BASE64_MSG="$5"

# Step 1: Associate receiver contract
echo "[1/2] Associating receiver with EVM..."
seid tx evm associate-contract-address "$WASM_RECEIVER" \
  --from "$SEI_FROM" \
  --fees 20000usei \
  --chain-id "$CHAIN_ID" \
  --node "$NODE_URL" \
  -b block

# Step 2: Execute CW20 send
echo "[2/2] Executing CW20 send..."
seid tx wasm execute "$WASM_SENDER" \
  "{\"send\":{\"contract\":\"$WASM_RECEIVER\",\"amount\":\"$AMOUNT\",\"msg\":\"$BASE64_MSG\"}}" \
  --from "$SEI_FROM" \
  --fees 500000usei \
  --gas 200000 \
  --chain-id "$CHAIN_ID" \
  --node "$NODE_URL" \
  -b block
