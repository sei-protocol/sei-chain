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

dump_flatkv_layout() {
  # Snapshot the on-disk state of $flatkv_dir for triage. dump-flatkv fails
  # opaquely ("clone aborted after 3 retries...") when, for example, the
  # live node never wrote a snapshot (=> no `current` symlink) or wrote it
  # to a different path than $flatkv_dir. Printing the layout removes that
  # ambiguity from the next CI run.
  echo "==================== app.toml FlatKV-related settings ====================" >&2
  grep -E '^(sc-write-mode|evm-ss-split)' \
    /root/.sei/config/app.toml >&2 2>/dev/null || true
  for candidate in "$flatkv_dir" /root/.sei/data/flatkv; do
    echo "==================== FlatKV directory state at $candidate ====================" >&2
    if [ ! -d "$candidate" ]; then
      echo "(directory $candidate does not exist)" >&2
    else
      ls -la "$candidate" >&2 || true
      for snap in "$candidate"/snapshot-*; do
        [ -d "$snap" ] || continue
        echo "---- $snap ----" >&2
        ls -la "$snap" >&2 || true
      done
    fi
  done
  local seid_log="/sei-protocol/sei-chain/build/generated/logs/seid-0.log"
  echo "==================== seid-0.log (head 60) ====================" >&2
  head -60 "$seid_log" >&2 2>/dev/null || echo "(no seid-0.log)" >&2
}
trap 'rc=$?; if [ "$rc" -ne 0 ]; then dump_flatkv_layout; fi; exit $rc' EXIT

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

# A serialized FlatKV StorageData row is 41 raw bytes:
#   1B tag (vtype.TagStorage) + 8B block height (big-endian) + 32B EVM slot value
# dump-flatkv prints values as uppercase hex, so the on-disk 41 bytes become
# 82 hex chars. If vtype.StorageData.Serialize() ever changes (varint
# height, dropping the tag for the empty value, etc.) this assertion will
# start failing -- update the breakdown here AND the literal length below.
expected_storage_hex_len=82
if ! awk -v want="$expected_storage_hex_len" '
  /^Key:/ {
    split($0, parts, "Value: ")
    if (length(parts[2]) == want) {
      found = 1
    }
  }
  END { exit found ? 0 : 1 }
' "$storage_dump"; then
  echo "FlatKV storage dump has no row whose Value field is ${expected_storage_hex_len} hex chars (= 41B = 1B tag + 8B height + 32B EVM slot value): $storage_dump" >&2
  exit 1
fi

echo "FlatKV storage bucket smoke verification passed: $storage_dump"
