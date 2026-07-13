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

# Resolve sender's cosmos address once. On any failure (transient
# docker hiccup, container start race, missing key) FROM_ADDR stays
# empty and the wait helper below becomes a no-op — the script falls
# back to the original unprotected behavior rather than crashing
# under set -e.
FROM_ADDR=$(run seid keys show "$FROM" "${KEYRING_ARGS[@]}" -a 2>/dev/null) || FROM_ADDR=

# Cosmos account sequence for $FROM_ADDR (or empty on any error).
# Always exits 0 so the calling `local cur=$(get_from_seq)` doesn't
# trip set -e in command substitution.
get_from_seq() {
  if [ -z "$FROM_ADDR" ]; then echo; return 0; fi
  local s
  s=$(run seid q account "$FROM_ADDR" -o json 2>/dev/null | jq -r '.sequence // ""' 2>/dev/null) || true
  echo "$s"
}

get_latest_height() {
  local h
  h=$(run seid status 2>/dev/null | jq -r '.SyncInfo.latest_block_height // "0"' 2>/dev/null) || true
  echo "${h:-0}"
}

# Wait until $FROM_ADDR's sequence advances past $1.
# Direct causal "previous tx committed" signal: the sender's sequence
# advances atomically when its tx is included in a block, so by the
# time this returns the next CLI's pre-flight `q account` will read
# the post-tx sequence. No-op if FROM_ADDR resolution failed.
wait_from_seq_advance() {
  local prev="$1"
  if [ -z "$FROM_ADDR" ] || [ -z "$prev" ]; then return 0; fi
  local deadline=$(($(date +%s) + 30))
  while [ "$(date +%s)" -lt "$deadline" ]; do
    local cur; cur=$(get_from_seq)
    if [[ "$cur" =~ ^[0-9]+$ ]] && [ "$cur" -gt "$prev" ]; then
      return 0
    fi
    sleep 0.5
  done
  echo "timed out waiting for sequence to advance past ${prev}" >&2
  return 1
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
# Poll the sender's sequence between each pair of consecutive -b sync
# submissions so the next CLI's pre-flight `q account` doesn't race
# the mempool's still-pending prior tx (otherwise both sign with the
# same sequence and the second's CheckTx rejects with "incorrect
# account sequence"). For the send line that's required because it
# has no `|| true` and a rejection would crash the script under set -e;
# for the deploy line it's required because a silent CheckTx rejection
# leaves SEI_EVM_IO_SEED_BLOCK unset and skips the __SEED__ fixtures.
# The reverter deploy further down only needs protection if the
# minimal deploy's receipt-poll loop didn't already wait (which it
# only skips when DEPLOY_TX is empty — i.e. minimal already failed,
# in which case reverter racing is no worse).
SEQ_BEFORE_ASSOC=$(get_from_seq)
run seid tx evm associate-address --from "$FROM" "${KEYRING_ARGS[@]}" --chain-id sei -b sync -y 2>/dev/null || true
wait_from_seq_advance "$SEQ_BEFORE_ASSOC"
SEQ_BEFORE_SEND=$(get_from_seq)
run seid tx evm send "$RECIPIENT" 1 --from "$FROM" "${KEYRING_ARGS[@]}" --chain-id sei --evm-rpc "$EVM_RPC_URL" -b sync -y
wait_from_seq_advance "$SEQ_BEFORE_SEND"
SEQ_BEFORE_MINIMAL=$(get_from_seq)
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
# wait_from_seq_advance even though minimal's receipt-poll loop above
# usually does the same job — when the grep fails to extract DEPLOY_TX
# from a successful CheckTx response (CLI format quirk, partial output),
# the receipt poll is skipped entirely and reverter would otherwise
# fire with a stale sequence.
docker exec "$CONTAINER" /bin/bash -c "tr -d '[:space:]' < \"$REVERTER_HEX\" > /tmp/reverter_contract.hex"
wait_from_seq_advance "$SEQ_BEFORE_MINIMAL"
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
