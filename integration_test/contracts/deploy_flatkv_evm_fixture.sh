#!/bin/bash

set -euo pipefail

export PATH="$PATH:/root/.foundry/bin:/root/go/bin"

RPC_URL=${EVM_RPC_URL:-http://localhost:8545}
FROM=${FLATKV_EVM_FIXTURE_FROM:-admin}
PASSWORD=${FLATKV_EVM_FIXTURE_PASSWORD:-12345678}
CHAIN_ID=${FLATKV_EVM_FIXTURE_CHAIN_ID:-sei}
RECIPIENT_ADDR=${FLATKV_EVM_FIXTURE_RECIPIENT:-0x70997970C51812dc3A010C7d01b50e0d17dc79C8}
MISSING_ADDR=${FLATKV_EVM_FIXTURE_MISSING:-0xc1cadaffffffffffffffffffffffffffffffffff}
TRANSFER_VALUE_WEI=${FLATKV_EVM_FIXTURE_TRANSFER_VALUE_WEI:-1}
BULK_STORAGE_KEYS=${FLATKV_EVM_BULK_STORAGE_KEYS:-4000}
BULK_STORAGE_KEYS_PER_CONTRACT=${FLATKV_EVM_BULK_STORAGE_KEYS_PER_CONTRACT:-50}
KEYRING_ARGS=()
if [ -n "${FLATKV_EVM_FIXTURE_KEYRING_BACKEND:-}" ]; then
  KEYRING_ARGS+=(--keyring-backend "$FLATKV_EVM_FIXTURE_KEYRING_BACKEND")
fi

# Constructor:
#   sstore(0, 42)
#   return runtime bytecode that returns 42 for any call.
STORAGE_CONTRACT_INIT_CODE=0x602a600055600a600f600039600a6000f3602a60005260206000f3
STORAGE_SLOT_ZERO=0x0000000000000000000000000000000000000000000000000000000000000000

seihome=$(git rev-parse --show-toplevel)
out_dir="$seihome/integration_test/contracts"

write_fixture() {
  local name=$1
  local value=$2
  printf "%s\n" "$value" > "$out_dir/$name"
}

run_seid() {
  printf "%s\n" "$PASSWORD" | seid "$@"
}

wait_for_evm_rpc() {
  local timeout=120
  local elapsed=0
  until cast block-number --rpc-url "$RPC_URL" >/dev/null 2>&1; do
    if [ "$elapsed" -ge "$timeout" ]; then
      echo "EVM RPC did not become ready within ${timeout}s" >&2
      exit 1
    fi
    sleep 2
    elapsed=$((elapsed + 2))
  done
}

block_number() {
  cast block-number --rpc-url "$RPC_URL"
}

query_balance() {
  cast balance "$1" --block "$2" --rpc-url "$RPC_URL"
}

query_balance_hex() {
  cast to-hex "$(query_balance "$1" "$2")"
}

query_storage() {
  cast storage "$1" "$2" --block "$3" --rpc-url "$RPC_URL"
}

query_code() {
  cast code "$1" --block "$2" --rpc-url "$RPC_URL"
}

extract_tx_hash() {
  grep -oE '0x[a-fA-F0-9]{64}' | head -1
}

wait_for_receipt() {
  local tx_hash=$1
  local timeout=${2:-60}
  local elapsed=0
  local response

  until [ "$elapsed" -ge "$timeout" ]; do
    response=$(curl -s -X POST -H "Content-Type: application/json" \
      -d "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"eth_getTransactionReceipt\",\"params\":[\"$tx_hash\"]}" \
      "$RPC_URL" || true)
    if printf "%s\n" "$response" | jq -e '.result != null' >/dev/null 2>&1; then
      printf "%s\n" "$response"
      return 0
    fi
    sleep 1
    elapsed=$((elapsed + 1))
  done

  echo "Timed out waiting for EVM receipt $tx_hash" >&2
  return 1
}

require_success_receipt() {
  local name=$1
  local receipt=$2
  local status
  status=$(printf "%s\n" "$receipt" | jq -r '.result.status // empty')
  if [ "$status" != "0x1" ] && [ "$status" != "1" ]; then
    echo "FlatKV EVM $name failed:" >&2
    printf "%s\n" "$receipt" >&2
    exit 1
  fi
}

write_bulk_storage_contract() {
  local output=$1
  local start_slot=$2
  local count=$3

  python3 - "$output" "$start_slot" "$count" <<'PY'
import sys

output = sys.argv[1]
start_slot = int(sys.argv[2])
count = int(sys.argv[3])

if start_slot < 0 or count < 0 or start_slot + count > 65536:
    raise SystemExit(f"slot range [{start_slot}, {start_slot + count}) is outside PUSH2 range")

code = bytearray()
for slot in range(start_slot, start_slot + count):
    # sstore(slot, slot + 1). Use fixed-width PUSH32/PUSH2 so the emitted
    # constructor is deterministic and does not need an assembler dependency.
    code.append(0x7F)  # PUSH32
    code.extend((slot + 1).to_bytes(32, "big"))
    code.append(0x61)  # PUSH2
    code.extend(slot.to_bytes(2, "big"))
    code.append(0x55)  # SSTORE

# Return empty runtime; the test only needs persisted storage rows.
code.extend(bytes.fromhex("60006000f3"))

with open(output, "w", encoding="utf-8") as fh:
    fh.write(code.hex())
PY
}

echo "Generating FlatKV EVM historical fixture via $RPC_URL..."
wait_for_evm_rpc

initial_height=$(block_number)
write_fixture "flatkv_evm_initial_block_height.txt" "$initial_height"
write_fixture "flatkv_evm_recipient_addr.txt" "$RECIPIENT_ADDR"
write_fixture "flatkv_evm_missing_addr.txt" "$MISSING_ADDR"
write_fixture "flatkv_evm_storage_slot.txt" "$STORAGE_SLOT_ZERO"

run_seid tx evm associate-address \
  --from "$FROM" \
  "${KEYRING_ARGS[@]}" \
  --chain-id "$CHAIN_ID" \
  -b sync \
  -y >/tmp/flatkv_evm_associate.out 2>&1 || true

echo "Sending native EVM transfer to create/update recipient account..."
if ! transfer_out=$(run_seid tx evm send "$RECIPIENT_ADDR" "$TRANSFER_VALUE_WEI" \
  --from "$FROM" \
  "${KEYRING_ARGS[@]}" \
  --chain-id "$CHAIN_ID" \
  --evm-rpc "$RPC_URL" \
  -b sync \
  -y 2>&1); then
  echo "FlatKV EVM transfer command failed:" >&2
  printf "%s\n" "$transfer_out" >&2
  exit 1
fi
printf "%s\n" "$transfer_out" >/tmp/flatkv_evm_transfer.out
transfer_tx=$(printf "%s\n" "$transfer_out" | extract_tx_hash || true)
if [ -z "$transfer_tx" ]; then
  echo "Failed to extract FlatKV EVM transfer tx hash:" >&2
  printf "%s\n" "$transfer_out" >&2
  exit 1
fi
transfer_receipt=$(wait_for_receipt "$transfer_tx")
require_success_receipt "transfer" "$transfer_receipt"
printf "%s\n" "$transfer_receipt" >/tmp/flatkv_evm_transfer_receipt.json

balance_height=$(block_number)
balance_expected=$(query_balance_hex "$RECIPIENT_ADDR" "$balance_height")
write_fixture "flatkv_evm_balance_block_height.txt" "$balance_height"
write_fixture "flatkv_evm_balance_expected.txt" "$balance_expected"

echo "Deploying storage/code fixture contract..."
contract_hex_file=/tmp/flatkv_evm_storage_contract.hex
printf "%s" "${STORAGE_CONTRACT_INIT_CODE#0x}" > "$contract_hex_file"
if ! deploy_out=$(run_seid tx evm deploy "$contract_hex_file" \
  --from "$FROM" \
  "${KEYRING_ARGS[@]}" \
  --chain-id "$CHAIN_ID" \
  --evm-rpc "$RPC_URL" \
  -b sync \
  -y 2>&1); then
  echo "FlatKV EVM deploy command failed:" >&2
  printf "%s\n" "$deploy_out" >&2
  exit 1
fi
deploy_tx=$(printf "%s\n" "$deploy_out" | extract_tx_hash || true)
if [ -z "$deploy_tx" ]; then
  echo "Failed to extract FlatKV EVM deploy tx hash:" >&2
  printf "%s\n" "$deploy_out" >&2
  exit 1
fi
deploy_receipt=$(wait_for_receipt "$deploy_tx")
require_success_receipt "contract deployment" "$deploy_receipt"
printf "%s\n" "$deploy_receipt" > /tmp/flatkv_evm_deploy_receipt.json

contract_addr=$(printf "%s\n" "$deploy_receipt" | jq -r '.result.contractAddress // empty')
if [ -z "$contract_addr" ] || [ "$contract_addr" = "null" ]; then
  contract_addr=$(printf "%s\n" "$deploy_out" | sed -n 's/^Deployed to: //p' | tail -1)
fi
if [ -z "$contract_addr" ] || [ "$contract_addr" = "null" ]; then
  echo "Failed to extract contract address from deploy receipt:" >&2
  printf "%s\n" "$deploy_receipt" >&2
  exit 1
fi

contract_height=$(block_number)
storage_expected=$(query_storage "$contract_addr" "$STORAGE_SLOT_ZERO" "$contract_height")
code_expected=$(query_code "$contract_addr" "$contract_height")

write_fixture "flatkv_evm_contract_addr.txt" "$contract_addr"
write_fixture "flatkv_evm_contract_block_height.txt" "$contract_height"
write_fixture "flatkv_evm_storage_expected.txt" "$storage_expected"
write_fixture "flatkv_evm_code_expected.txt" "$code_expected"

missing_balance_expected=$(query_balance_hex "$MISSING_ADDR" "$contract_height")
missing_storage_expected=$(query_storage "$MISSING_ADDR" "$STORAGE_SLOT_ZERO" "$contract_height")
write_fixture "flatkv_evm_missing_balance_expected.txt" "$missing_balance_expected"
write_fixture "flatkv_evm_missing_storage_expected.txt" "$missing_storage_expected"

write_fixture "flatkv_evm_bulk_storage_keys.txt" "$BULK_STORAGE_KEYS"
if [ "$BULK_STORAGE_KEYS" -gt 0 ]; then
  if [ "$BULK_STORAGE_KEYS_PER_CONTRACT" -le 0 ]; then
    echo "FLATKV_EVM_BULK_STORAGE_KEYS_PER_CONTRACT must be positive" >&2
    exit 1
  fi

  echo "Deploying bulk storage fixture: total_slots=$BULK_STORAGE_KEYS slots_per_contract=$BULK_STORAGE_KEYS_PER_CONTRACT"
  deployed_bulk=0
  while [ "$deployed_bulk" -lt "$BULK_STORAGE_KEYS" ]; do
    remaining=$((BULK_STORAGE_KEYS - deployed_bulk))
    batch_size=$BULK_STORAGE_KEYS_PER_CONTRACT
    if [ "$remaining" -lt "$batch_size" ]; then
      batch_size=$remaining
    fi

    bulk_contract_hex_file="/tmp/flatkv_evm_bulk_storage_${deployed_bulk}.hex"
    write_bulk_storage_contract "$bulk_contract_hex_file" "$deployed_bulk" "$batch_size"

    if ! bulk_out=$(run_seid tx evm deploy "$bulk_contract_hex_file" \
      --from "$FROM" \
      "${KEYRING_ARGS[@]}" \
      --chain-id "$CHAIN_ID" \
      --evm-rpc "$RPC_URL" \
      -b sync \
      -y 2>&1); then
      echo "FlatKV EVM bulk storage deploy command failed at slot offset $deployed_bulk:" >&2
      printf "%s\n" "$bulk_out" >&2
      exit 1
    fi
    bulk_tx=$(printf "%s\n" "$bulk_out" | extract_tx_hash || true)
    if [ -z "$bulk_tx" ]; then
      echo "Failed to extract FlatKV EVM bulk storage tx hash at slot offset $deployed_bulk:" >&2
      printf "%s\n" "$bulk_out" >&2
      exit 1
    fi
    bulk_receipt=$(wait_for_receipt "$bulk_tx" 120)
    require_success_receipt "bulk storage deployment" "$bulk_receipt"

    deployed_bulk=$((deployed_bulk + batch_size))
    echo "  bulk storage slots committed: $deployed_bulk/$BULK_STORAGE_KEYS"
  done
fi

latest_height=$(block_number)
write_fixture "flatkv_evm_latest_fixture_block_height.txt" "$latest_height"

echo "FlatKV EVM fixture generated:"
echo "  recipient=$RECIPIENT_ADDR balance_height=$balance_height balance=$balance_expected"
echo "  contract=$contract_addr contract_height=$contract_height storage=$storage_expected"
echo "  bulk_storage_keys=$BULK_STORAGE_KEYS"
