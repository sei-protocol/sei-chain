#!/usr/bin/env bash
set -e
# Run execution-apis .io/.iox tests against local Sei EVM RPC. Start cluster first: make docker-cluster-start
# Usage: from repo root: ./integration_test/evm_module/scripts/evm_rpc_tests.sh
cd "$(dirname "$0")/../../.."

CONTAINER="${SEI_EVM_IO_TX_CONTAINER:-sei-node-0}"
PASSWORD="${SEI_EVM_IO_TX_PASSWORD:-12345678}"
FROM="${SEI_EVM_IO_TX_FROM:-admin}"
RECIPIENT="${SEI_EVM_IO_TX_RECIPIENT:-0xF87A299e6bC7bEba58dbBe5a5Aa21d49bCD16D52}"
PROJECT_ROOT="${SEI_EVM_IO_PROJECT_ROOT:-/sei-protocol/sei-chain}"
CONTRACT_HEX="${PROJECT_ROOT}/integration_test/evm_module/scripts/contracts/minimal_contract.hex"
REVERTER_HEX="${PROJECT_ROOT}/integration_test/evm_module/scripts/contracts/reverter_contract.hex"
EVM_RPC_URL="${SEI_EVM_RPC_URL:-http://localhost:8545}"
KEYRING_ARGS=()
if [[ -n "${SEI_EVM_IO_KEYRING_BACKEND:-}" ]]; then
  KEYRING_ARGS+=(--keyring-backend "$SEI_EVM_IO_KEYRING_BACKEND")
fi

run() {
  docker exec --env SEI_EVM_IO_PASSWORD="$PASSWORD" "$CONTAINER" /bin/bash -c \
    'export PATH=$PATH:/root/go/bin && printf "%s\n" "$SEI_EVM_IO_PASSWORD" | "$@"' bash "$@"
}

get_latest_height() {
  local h
  h=$(run seid status 2>/dev/null | jq -r '.SyncInfo.latest_block_height // "0"' 2>/dev/null) || true
  echo "${h:-0}"
}

wait_until_height_exceeds() {
  local prev="$1"
  local deadline=$(($(date +%s) + 30))
  while [ "$(date +%s)" -lt "$deadline" ]; do
    local cur; cur=$(get_latest_height)
    if [[ "$cur" =~ ^[0-9]+$ ]] && [ "$cur" -gt "$prev" ]; then
      return 0
    fi
    sleep 0.5
  done
  echo "timed out waiting for height to exceed ${prev}" >&2
  return 1
}

wait_for_receipt_field() {
  local tx_hash="$1"
  local field="$2"
  local deadline=$(($(date +%s) + 30))
  while [ "$(date +%s)" -lt "$deadline" ]; do
    local resp
    resp=$(docker exec "$CONTAINER" curl -s -X POST -H "Content-Type: application/json" -d "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"eth_getTransactionReceipt\",\"params\":[\"$tx_hash\"]}" "$EVM_RPC_URL" 2>/dev/null) || true
    local value
    value=$(echo "$resp" | jq -r --arg field "$field" '.result[$field] // empty' 2>/dev/null) || true
    if [[ -n "$value" && "$value" != "null" ]]; then
      echo "$value"
      return 0
    fi
    sleep 1
  done
  echo "timed out waiting for executed receipt field ${field} for ${tx_hash}" >&2
  return 1
}

bump_chain_to_height() {
  local target="$1"
  local height; height=$(get_latest_height)
  while [[ "$height" =~ ^[0-9]+$ ]] && [ "$height" -lt "$target" ]; do
    # Progress-only EVM send: these fixtures need real historical blocks first.
    local tx_hash
    tx_hash=$(run seid tx evm send "$RECIPIENT" 1 --from "$FROM" "${KEYRING_ARGS[@]}" --chain-id sei --evm-rpc "$EVM_RPC_URL" -b sync -y | grep -oE '0x[a-fA-F0-9]{64}' | head -1)
    height=$(wait_for_receipt_field "$tx_hash" blockNumber)
    height=$((height))
  done
}

# Seed chain with block/tx/contract; export SEI_EVM_IO_SEED_BLOCK so .iox __SEED__ tag resolves to deploy block.
# Static .io fixtures contain hard-coded historical block numbers up to 0x2d.
# Under Autobahn without empty blocks, create enough real blocks first so those
# requests stay within the available history.
bump_chain_to_height 100

# CLI deploy expects hex file with no whitespace; write trimmed hex to a temp path in the container.
docker exec "$CONTAINER" /bin/bash -c "tr -d '[:space:]' < \"$CONTRACT_HEX\" > /tmp/minimal_contract.hex"
# Use -b sync (not -b block): under Autobahn the cosmos KV indexer that
# -b block polls isn't populated, so -b block hangs to its 60s timeout
# per call. The deploy's tx hash is in the -b sync JSON response, and
# downstream eth_getTransactionReceipt polling handles inclusion
# confirmation independently.
#
# These are EVM transactions, so receipt polling is the reliable
# inclusion signal. Cosmos account sequence waits are not appropriate here.
SEND_OUT=$(run seid tx evm send "$RECIPIENT" 1 --from "$FROM" "${KEYRING_ARGS[@]}" --chain-id sei --evm-rpc "$EVM_RPC_URL" -b sync -y 2>&1)
SEND_TX=$(echo "$SEND_OUT" | grep -oE '0x[a-fA-F0-9]{64}' | head -1)
if [[ -z "$SEND_TX" ]]; then
  echo "Failed to extract seed transfer tx hash:" >&2
  printf "%s\n" "$SEND_OUT" >&2
  exit 1
fi
wait_for_receipt_field "$SEND_TX" blockNumber >/dev/null
DEPLOY_OUT=$(run seid tx evm deploy /tmp/minimal_contract.hex --from "$FROM" "${KEYRING_ARGS[@]}" --chain-id sei --evm-rpc "$EVM_RPC_URL" -b sync -y 2>&1) || true
DEPLOY_TX=$(echo "$DEPLOY_OUT" | grep -oE '0x[a-fA-F0-9]{64}' | head -1)
if [[ -n "$DEPLOY_TX" ]]; then
  SEED=$(wait_for_receipt_field "$DEPLOY_TX" blockNumber) || true
  if [[ -n "$SEED" ]]; then
    export SEI_EVM_IO_SEED_BLOCK="$SEED"
    export SEI_EVM_IO_DEPLOY_TX_HASH="$DEPLOY_TX"
  fi
fi

# Deploy reverter contract (reverts with Error("user error")); export SEI_EVM_IO_REVERTER_ADDRESS for .iox __REVERTER__ tag.
docker exec "$CONTAINER" /bin/bash -c "tr -d '[:space:]' < \"$REVERTER_HEX\" > /tmp/reverter_contract.hex"
REVERTER_OUT=$(run seid tx evm deploy /tmp/reverter_contract.hex --from "$FROM" "${KEYRING_ARGS[@]}" --chain-id sei --evm-rpc "$EVM_RPC_URL" -b sync -y 2>&1) || true
REVERTER_TX=$(echo "$REVERTER_OUT" | grep -oE '0x[a-fA-F0-9]{64}' | head -1)
if [[ -n "$REVERTER_TX" ]]; then
  REVERTER_ADDR=$(wait_for_receipt_field "$REVERTER_TX" contractAddress) || true
  if [[ -n "$REVERTER_ADDR" ]]; then
    export SEI_EVM_IO_REVERTER_ADDRESS="$REVERTER_ADDR"
  fi
fi
if [[ -z "${SEI_EVM_IO_REVERTER_ADDRESS:-}" ]]; then
  echo "WARNING: Reverter contract not deployed (deploy or receipt lookup failed). Tests using __REVERTER__ will be skipped." >&2
fi

export SEI_EVM_IO_RUN_INTEGRATION=1
go test ./integration_test/evm_module/rpc_io_test/ -v -count=1

# WebSocket integration tests (eth_subscribe et al.). Lives in a sibling
# package because the .io/.iox framework cannot represent streaming
# methods. The test itself is consensus-mode agnostic, so the same
# invocation works under standard CometBFT and Autobahn clusters alike.
export SEI_EVM_WS_RUN_INTEGRATION=1
go test ./integration_test/evm_module/ws_test/ -v -count=1
