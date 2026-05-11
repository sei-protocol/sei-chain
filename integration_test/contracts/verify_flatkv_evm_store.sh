#!/bin/bash

set -euo pipefail

export PATH="$PATH:/root/go/bin:/usr/local/go/bin"

seihome=$(git rev-parse --show-toplevel)
flatkv_dir=${FLATKV_DIR:-/root/.sei/data/state_commit/flatkv}
dump_dir=${FLATKV_EVM_DUMP_DIR:-/tmp/flatkv-evm-dump}
storage_dump="$dump_dir/storage"
contract_addr_file="$seihome/integration_test/contracts/flatkv_evm_contract_addr.txt"

cd "$seihome"

if [ ! -x build/seidb ]; then
  echo "Building seidb for FlatKV smoke verification..."
  GOPROXY=${GOPROXY:-https://proxy.golang.org,direct} go build -o build/seidb ./sei-db/tools/cmd/seidb
fi

rm -rf "$dump_dir"
mkdir -p "$dump_dir"

echo "Dumping FlatKV storage bucket from $flatkv_dir..."
build/seidb dump-flatkv --db-dir "$flatkv_dir" --output-dir "$dump_dir" --bucket storage

if [ ! -s "$storage_dump" ]; then
  echo "FlatKV storage dump is missing or empty: $storage_dump" >&2
  exit 1
fi

if ! grep -q '^Key:' "$storage_dump"; then
  echo "FlatKV storage dump has no key/value rows: $storage_dump" >&2
  exit 1
fi

if [ ! -s "$contract_addr_file" ]; then
  echo "Missing FlatKV EVM fixture contract address: $contract_addr_file" >&2
  exit 1
fi

contract_hex=$(tail -1 "$contract_addr_file")
contract_hex=${contract_hex#0x}
contract_hex=$(printf "%s" "$contract_hex" | tr '[:lower:]' '[:upper:]')
if [ -z "$contract_hex" ]; then
  echo "FlatKV EVM fixture contract address is empty: $contract_addr_file" >&2
  exit 1
fi

if ! grep -q "$contract_hex" "$storage_dump"; then
  echo "FlatKV storage dump does not contain fixture contract address $contract_hex: $storage_dump" >&2
  exit 1
fi

if ! awk '
  /^Key:/ {
    split($0, parts, "Value: ")
    if (length(parts[2]) == 82) {
      found = 1
    }
  }
  END { exit found ? 0 : 1 }
' "$storage_dump"; then
  echo "FlatKV storage dump has no row with a 41-byte serialized storage value: $storage_dump" >&2
  exit 1
fi

echo "FlatKV storage bucket smoke verification passed: $storage_dump"
