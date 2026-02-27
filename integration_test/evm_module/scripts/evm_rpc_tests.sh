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
CONTRACT_HEX="${PROJECT_ROOT}/integration_test/evm_module/scripts/minimal_contract.hex"
EVM_RPC_URL="${SEI_EVM_RPC_URL:-http://localhost:8545}"
KEYRING_ARGS=()
if [[ -n "${SEI_EVM_IO_KEYRING_BACKEND:-}" ]]; then
  KEYRING_ARGS+=(--keyring-backend "$SEI_EVM_IO_KEYRING_BACKEND")
fi

run() {
  docker exec --env SEI_EVM_IO_PASSWORD="$PASSWORD" "$CONTAINER" /bin/bash -c \
    'export PATH=$PATH:/root/go/bin && printf "%s\n" "$SEI_EVM_IO_PASSWORD" | "$@"' bash "$@"
}

# Seed chain with block/tx/contract; export SEI_EVM_IO_SEED_BLOCK so .iox __SEED__ tag resolves to deploy block.
# CLI deploy expects hex file with no whitespace; write trimmed hex to a temp path in the container.
docker exec "$CONTAINER" /bin/bash -c "tr -d '[:space:]' < \"$CONTRACT_HEX\" > /tmp/minimal_contract.hex"
run seid tx evm associate-address --from "$FROM" "${KEYRING_ARGS[@]}" --chain-id sei -b block -y 2>/dev/null || true
run seid tx evm send "$RECIPIENT" 1 --from "$FROM" "${KEYRING_ARGS[@]}" --chain-id sei --evm-rpc "$EVM_RPC_URL" -b sync -y
DEPLOY_OUT=$(run seid tx evm deploy /tmp/minimal_contract.hex --from "$FROM" "${KEYRING_ARGS[@]}" --chain-id sei --evm-rpc "$EVM_RPC_URL" -b block -y 2>&1) || true
DEPLOY_TX=$(echo "$DEPLOY_OUT" | grep -oE '0x[a-fA-F0-9]{64}' | head -1)
if [[ -n "$DEPLOY_TX" ]]; then
  sleep 2
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    RESP=$(docker exec "$CONTAINER" curl -s -X POST -H "Content-Type: application/json" -d "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"eth_getTransactionReceipt\",\"params\":[\"$DEPLOY_TX\"]}" "$EVM_RPC_URL" 2>/dev/null) || true
    SEED=$(echo "$RESP" | grep -o '"blockNumber":"[^"]*"' | head -1 | cut -d'"' -f4)
    [[ -n "$SEED" ]] && break
    sleep 1
  done
  if [[ -n "$SEED" ]]; then
    export SEI_EVM_IO_SEED_BLOCK="$SEED"
    export SEI_EVM_IO_DEPLOY_TX_HASH="$DEPLOY_TX"
  fi
fi

export SEI_EVM_IO_RUN_INTEGRATION=1
go test ./integration_test/evm_module/rpc_io_test/ -v -count=1
