#!/bin/bash
#
# verify_flatkv_only_statesync.sh
#
# End-to-end post-migration state-sync coverage:
#   1) assert a 4-validator cluster booted in sc-write-mode=flatkv_only,
#      with no memiavl commit-store artifacts on disk;
#   2) kill and wipe one validator's local state;
#   3) recover it with state-sync configuration pointed at the other nodes;
#   4) require the restored FlatKV state to match the donors at a shared
#      committed height, and require the EVM fixture to remain queryable.

set -euo pipefail

NODE_COUNT=${FLATKV_ONLY_NODE_COUNT:-4}
VICTIM_INDEX=${FLATKV_ONLY_STATESYNC_VICTIM_INDEX:-3}
VICTIM_NODE="sei-node-${VICTIM_INDEX}"
DONOR_NODE=${FLATKV_ONLY_STATESYNC_DONOR:-sei-node-0}
SECOND_RPC_NODE=${FLATKV_ONLY_STATESYNC_SECOND_RPC:-sei-node-1}
THIRD_PEER_NODE=${FLATKV_ONLY_STATESYNC_THIRD_PEER:-sei-node-2}
FLATKV_DIR=${FLATKV_DIR:-/root/.sei/data/state_commit/flatkv}
APP_CONFIG=${APP_CONFIG:-/root/.sei/config/app.toml}
GO_BIN=${GO_BIN:-/usr/local/go/bin/go}
MIN_DONOR_HEIGHT=${FLATKV_ONLY_STATESYNC_MIN_DONOR_HEIGHT:-250}
TRUST_LAG=${FLATKV_ONLY_STATESYNC_TRUST_LAG:-30}
SNAPSHOT_WAIT_TIMEOUT=${FLATKV_ONLY_STATESYNC_SNAPSHOT_WAIT_TIMEOUT:-420}
CATCHUP_TIMEOUT=${FLATKV_ONLY_STATESYNC_CATCHUP_TIMEOUT:-300}
COMPARE_BUFFER=${FLATKV_ONLY_STATESYNC_COMPARE_BUFFER:-2}
SMOKE_TIMEOUT=${FLATKV_ONLY_STATESYNC_SMOKE_TIMEOUT:-180}

echo "verify_flatkv_only_statesync: victim=$VICTIM_NODE donor=$DONOR_NODE second_rpc=$SECOND_RPC_NODE third_peer=$THIRD_PEER_NODE"

dump_node_log() {
  local node=$1
  local logfile node_id
  node_id=${node#sei-node-}
  logfile="/sei-protocol/sei-chain/build/generated/logs/seid-${node_id}.log"
  echo "==================== ${node} app.toml state-commit excerpt ====================" >&2
  docker exec "$node" bash -lc "grep -E '^(sc-write-mode|evm-ss-split)' '$APP_CONFIG' 2>/dev/null" >&2 || true
  echo "==================== ${node} seid log ${logfile} (last 240 lines) ====================" >&2
  docker exec "$node" tail -240 "$logfile" >&2 2>/dev/null \
    || echo "(could not read ${logfile})" >&2
  echo "==================== ${node} docker logs (last 200 lines) ====================" >&2
  docker logs --tail 200 "$node" >&2 || true
  echo "==================== ${node} end log ====================" >&2
}

node_height() {
  local node=$1
  docker exec "$node" build/seid status 2>/dev/null \
    | jq -r '.SyncInfo.latest_block_height // "0"' 2>/dev/null \
    || echo 0
}

block_hash() {
  local node=$1
  local height=$2
  docker exec "$node" bash -lc \
    "curl -sf 'http://localhost:26657/block?height=${height}' | jq -r '.result.block_id.hash // .block_id.hash'"
}

ensure_seidb() {
  local node=$1
  if docker exec "$node" test -x /sei-protocol/sei-chain/build/seidb >/dev/null 2>&1; then
    return 0
  fi
  echo "Building seidb on $node..."
  docker exec -e GOPROXY="${GOPROXY:-https://proxy.golang.org,direct}" "$node" bash -lc \
    "cd /sei-protocol/sei-chain && $GO_BIN build -o build/seidb ./sei-db/tools/cmd/seidb"
}

wait_for_height() {
  local node=$1
  local target=$2
  local timeout=$3
  local elapsed=0
  while [ "$elapsed" -lt "$timeout" ]; do
    local h
    h=$(node_height "$node")
    if [ "$h" -ge "$target" ]; then
      echo "$node reached height $h (target $target)"
      return 0
    fi
    echo "Waiting for $node to reach height $target (current=$h elapsed=${elapsed}s/${timeout}s)"
    sleep 5
    elapsed=$((elapsed + 5))
  done
  echo "ERROR: $node did not reach height $target within ${timeout}s" >&2
  dump_node_log "$node"
  return 1
}

wait_for_all_heights() {
  local target=$1
  local timeout=$2
  local elapsed=0
  while [ "$elapsed" -lt "$timeout" ]; do
    local all_ready=true
    local summary=""
    for i in $(seq 0 $((NODE_COUNT - 1))); do
      local node h
      node="sei-node-$i"
      h=$(node_height "$node")
      summary="$summary ${node}=${h}"
      if [ -z "$h" ] || [ "$h" -lt "$target" ]; then
        all_ready=false
      fi
    done
    if $all_ready; then
      echo "All validators reached height >= $target:$summary"
      return 0
    fi
    echo "Waiting for all validators to reach height $target (elapsed=${elapsed}s/${timeout}s):$summary"
    sleep 5
    elapsed=$((elapsed + 5))
  done
  echo "ERROR: validators did not all reach height $target within ${timeout}s" >&2
  for i in $(seq 0 $((NODE_COUNT - 1))); do
    dump_node_log "sei-node-$i"
  done
  return 1
}

wait_for_evm_rpc() {
  local node=$1
  local timeout=$2
  local elapsed=0
  while [ "$elapsed" -lt "$timeout" ]; do
    if docker exec "$node" bash -lc 'curl -sf -H "Content-Type: application/json" --data '"'"'{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}'"'"' http://localhost:8545 >/dev/null'; then
      echo "EVM RPC on $node is responding"
      return 0
    fi
    echo "Waiting for EVM RPC on $node (elapsed=${elapsed}s/${timeout}s)"
    sleep 2
    elapsed=$((elapsed + 2))
  done
  echo "ERROR: EVM RPC on $node did not respond within ${timeout}s" >&2
  dump_node_log "$node"
  return 1
}

assert_flatkv_only_layout() {
  local failed=false
  for i in $(seq 0 $((NODE_COUNT - 1))); do
    local node="sei-node-$i"
    echo "Asserting flatkv_only layout on $node"
    if ! docker exec "$node" bash -lc "
      set -euo pipefail
      mode=\$(grep -E '^sc-write-mode[[:space:]]*=' '$APP_CONFIG' | tail -1 | awk -F'\"' '{print \$2}' || true)
      if [ \"\$mode\" != 'flatkv_only' ]; then
        echo \"ERROR: $node has sc-write-mode='\$mode'; expected flatkv_only\" >&2
        exit 1
      fi
      test -d '$FLATKV_DIR'
      for path in /root/.sei/data/state_commit/memiavl /root/.sei/data/committer.db; do
        if [ -d \"\$path\" ] && [ -n \"\$(ls -A \"\$path\" 2>/dev/null)\" ]; then
          echo \"ERROR: $node has non-empty memiavl commit-store artifact at \$path\" >&2
          ls -la \"\$path\" >&2 || true
          exit 1
        fi
      done
    "; then
      failed=true
      dump_node_log "$node"
    fi
  done
  if $failed; then
    exit 1
  fi
  echo "All $NODE_COUNT validators are in flatkv_only mode with no memiavl commit-store artifacts"
}

snapshot_heights() {
  local node=$1
  docker exec "$node" bash -lc '
    set -euo pipefail
    value=$(curl -sf --get --data-urlencode "path=\"/app/snapshots\"" http://localhost:26657/abci_query | jq -r ".result.response.value // .response.value // empty")
    if [ -z "$value" ]; then
      exit 0
    fi
    printf "%s" "$value" | base64 -d 2>/dev/null | jq -r ".Snapshots[]?.Height // empty"
  ' 2>/dev/null || true
}

dump_snapshot_diagnostics() {
  local node=$1
  local node_id=${node#sei-node-}
  local logfile="/sei-protocol/sei-chain/build/generated/logs/seid-${node_id}.log"
  echo "==================== ${node} snapshot diagnostics ====================" >&2
  echo "--- effective [state-sync] section of ~/.sei/config/app.toml ---" >&2
  docker exec "$node" bash -lc "awk '/^\[state-sync\]/{flag=1;print;next} /^\[/{flag=0} flag' /root/.sei/config/app.toml" >&2 || true
  echo "--- raw /app/snapshots abci_query response ---" >&2
  docker exec "$node" bash -lc 'curl -sf --get --data-urlencode "path=\"/app/snapshots\"" http://localhost:26657/abci_query' >&2 || true
  echo >&2
  echo "--- snapshot-related lines in ${logfile} ---" >&2
  docker exec "$node" bash -lc \
    "grep -E 'snapshot|Snapshot|pruned|exporter|flatkv' '$logfile' 2>/dev/null | tail -240" >&2 \
    || echo "(no matches)" >&2
  echo "==================== end ${node} snapshot diagnostics ====================" >&2
}

wait_for_snapshot_at_or_after() {
  local node=$1
  local min_height=$2
  local timeout=$3
  local elapsed=0
  local snapshot=""
  local heights=""
  while [ "$elapsed" -lt "$timeout" ]; do
    heights=$(snapshot_heights "$node" | sort -n | tr '\n' ' ')
    snapshot=$(printf "%s\n" "$heights" | tr ' ' '\n' | awk -v min="$min_height" '$1 >= min { best=$1 } END { if (best != "") print best }')
    if [ -n "$snapshot" ]; then
      echo "$node has state-sync snapshot $snapshot (required >= $min_height)"
      return 0
    fi
    echo "Waiting for $node state-sync snapshot >= $min_height (current snapshots: ${heights:-none}; elapsed=${elapsed}s/${timeout}s)"
    sleep 5
    elapsed=$((elapsed + 5))
  done
  echo "ERROR: $node did not advertise a state-sync snapshot >= $min_height within ${timeout}s" >&2
  echo "  last snapshots: ${heights:-none}" >&2
  dump_snapshot_diagnostics "$node"
  dump_node_log "$node"
  return 1
}

configure_statesync() {
  local victim=$1
  local trust_height=$2
  local trust_hash=$3
  local victim_ip="192.168.10.$((10 + VICTIM_INDEX))"
  docker exec "$victim" bash -lc "
    set -euo pipefail
    peers=\$(grep -v '^$' /sei-protocol/sei-chain/build/generated/persistent_peers.txt | grep -v '$victim_ip' | paste -sd ',' -)
    sed -i.bak \
      -e '/^\[statesync\]/,/^\[consensus\]/ s|^enable *=.*|enable = true|' \
      -e '/^\[statesync\]/,/^\[consensus\]/ s|^rpc-servers *=.*|rpc-servers = \"${DONOR_NODE}:26657,${SECOND_RPC_NODE}:26657\"|' \
      -e '/^\[statesync\]/,/^\[consensus\]/ s|^trust-height *=.*|trust-height = ${trust_height}|' \
      -e '/^\[statesync\]/,/^\[consensus\]/ s|^trust-hash *=.*|trust-hash = \"${trust_hash}\"|' \
      /root/.sei/config/config.toml
    sed -i.bak -e \"s|^persistent-peers *=.*|persistent-peers = \\\"\${peers}\\\"|\" /root/.sei/config/config.toml
  "
}

assert_statesync_configured() {
  local victim=$1
  local trust_height=$2
  local trust_hash=$3
  if ! docker exec "$victim" bash -lc "
    set -euo pipefail
    section=\$(awk '/^\[statesync\]/{flag=1;print;next} /^\[/{flag=0} flag' /root/.sei/config/config.toml)
    echo \"--- effective [statesync] for $victim ---\"
    printf '%s\n' \"\$section\"
    printf '%s\n' \"\$section\" | grep -qx 'enable = true'
    printf '%s\n' \"\$section\" | grep -qx 'rpc-servers = \"${DONOR_NODE}:26657,${SECOND_RPC_NODE}:26657\"'
    printf '%s\n' \"\$section\" | grep -qx 'trust-height = ${trust_height}'
    printf '%s\n' \"\$section\" | grep -qx 'trust-hash = \"${trust_hash}\"'
  "; then
    echo "ERROR: failed to configure state-sync for $victim" >&2
    dump_node_log "$victim"
    return 1
  fi
}

start_victim() {
  docker exec -d -e "ID=${VICTIM_INDEX}" "$VICTIM_NODE" /usr/bin/start_sei.sh
}

wait_for_process() {
  local node=$1
  local timeout=$2
  local elapsed=0
  while [ "$elapsed" -lt "$timeout" ]; do
    if docker exec "$node" pgrep -f "seid start" >/dev/null 2>&1; then
      echo "$node seid process is running"
      return 0
    fi
    sleep 1
    elapsed=$((elapsed + 1))
  done
  echo "ERROR: $node did not start within ${timeout}s" >&2
  dump_node_log "$node"
  return 1
}

wait_for_catchup() {
  local victim=$1
  local donor=$2
  local timeout=$3
  local tolerance=${FLATKV_ONLY_STATESYNC_CATCHUP_TOLERANCE:-10}
  local elapsed=0
  while [ "$elapsed" -lt "$timeout" ]; do
    local donor_h victim_h gap
    donor_h=$(node_height "$donor")
    victim_h=$(node_height "$victim")
    gap=$((donor_h - victim_h))
    if [ "$victim_h" -gt 0 ] && [ "$gap" -le "$tolerance" ]; then
      echo "$victim caught up: donor=$donor_h victim=$victim_h gap=$gap"
      return 0
    fi
    echo "Waiting for state-sync catch-up: donor=$donor_h victim=$victim_h gap=$gap (elapsed=${elapsed}s/${timeout}s)"
    sleep 5
    elapsed=$((elapsed + 5))
  done
  echo "ERROR: $victim failed to catch up within ${timeout}s" >&2
  dump_node_log "$victim"
  dump_node_log "$donor"
  return 1
}

log_recovery_path() {
  local node=$1
  local node_id=${node#sei-node-}
  local logfile="/sei-protocol/sei-chain/build/generated/logs/seid-${node_id}.log"
  if docker exec "$node" bash -lc "grep -qE 'state_synced=true' '$logfile' 2>/dev/null"; then
    echo "$node recovery path: STATE-SYNC (state_synced=true)"
  elif docker exec "$node" bash -lc "grep -qE 'state_synced=false .*blocks_synced=' '$logfile' 2>/dev/null"; then
    echo "$node recovery path: BLOCKSYNC FALLBACK (state-sync did not engage)"
  else
    echo "$node recovery path: UNKNOWN (no state_synced= outcome line in log)"
  fi
  echo "  state-sync startup attempt lines:" >&2
  docker exec "$node" bash -lc \
    "grep -E 'This node needs state sync|starting state sync|Offering snapshot to ABCI app|Snapshot accepted, restoring|Start restoring store|state sync failed|Found local state with non-zero height|state_synced=' '$logfile' 2>/dev/null | head -40" >&2 \
    || echo "  (no state-sync attempt lines found)" >&2
}

pick_compare_height() {
  local min=""
  for i in $(seq 0 $((NODE_COUNT - 1))); do
    local h
    h=$(node_height "sei-node-$i")
    if [ -z "$min" ] || [ "$h" -lt "$min" ]; then
      min=$h
    fi
  done
  if [ -z "$min" ] || [ "$min" -le "$COMPARE_BUFFER" ]; then
    echo 1
    return
  fi
  echo $((min - COMPARE_BUFFER))
}

flatkv_dump_digest() {
  local node=$1
  local version=$2
  docker exec "$node" bash -lc "
    set -euo pipefail
    out_dir=/tmp/flatkv-only-statesync-${version}-${node}
    rm -rf \"\$out_dir\" && mkdir -p \"\$out_dir\"
    cd /sei-protocol/sei-chain
    build/seidb dump-flatkv \
      --db-dir $FLATKV_DIR \
      --output-dir \"\$out_dir\" \
      --height $version > /dev/null
    tail -q -n +2 \"\$out_dir/account\" \"\$out_dir/code\" \"\$out_dir/storage\" \"\$out_dir/legacy\" \
      | sha256sum | cut -d' ' -f1
  "
}

assert_flatkv_digests_match() {
  local compare_version
  compare_version=$(pick_compare_height)
  if [ -z "$compare_version" ] || [ "$compare_version" -lt 1 ]; then
    echo "ERROR: failed to pick a positive comparison height" >&2
    exit 1
  fi

  echo "Comparing FlatKV across $NODE_COUNT validators at chain height $compare_version"
  local reference_digest=""
  local reference_node=""
  local mismatch=false
  for i in $(seq 0 $((NODE_COUNT - 1))); do
    local node digest
    node="sei-node-$i"
    digest=$(flatkv_dump_digest "$node" "$compare_version")
    echo "  ${node} sha256 = $digest"
    if [ -z "$reference_digest" ]; then
      reference_digest="$digest"
      reference_node="$node"
      continue
    fi
    if [ "$digest" != "$reference_digest" ]; then
      echo "FAIL: ${node} diverges from ${reference_node} at height $compare_version" >&2
      mismatch=true
    fi
  done

  if $mismatch; then
    for i in $(seq 0 $((NODE_COUNT - 1))); do
      dump_node_log "sei-node-$i"
    done
    exit 1
  fi
  echo "FlatKV digests match across all validators at height $compare_version"
}

assert_evm_fixture_queries() {
  local node=$1
  if ! wait_for_evm_rpc "$node" 60; then
    return 1
  fi
  if ! docker exec "$node" bash -lc '
    set -euo pipefail
    export PATH="$HOME/.foundry/bin:/root/.foundry/bin:$PATH:/root/go/bin:/usr/local/go/bin"
    if ! command -v cast >/dev/null 2>&1; then
      echo "ERROR: cast not found in PATH=$PATH; FlatKV EVM fixture queries cannot run" >&2
      exit 1
    fi
    cd /sei-protocol/sei-chain

    recipient=$(tail -1 integration_test/contracts/flatkv_evm_recipient_addr.txt)
    expected_balance=$(tail -1 integration_test/contracts/flatkv_evm_balance_expected.txt)
    actual_balance=$(cast to-hex "$(cast balance "$recipient" --block latest --rpc-url http://localhost:8545)")

    contract=$(tail -1 integration_test/contracts/flatkv_evm_contract_addr.txt)
    slot=$(tail -1 integration_test/contracts/flatkv_evm_storage_slot.txt)
    expected_storage=$(tail -1 integration_test/contracts/flatkv_evm_storage_expected.txt)
    expected_code=$(tail -1 integration_test/contracts/flatkv_evm_code_expected.txt)
    actual_storage=$(cast storage "$contract" "$slot" --block latest --rpc-url http://localhost:8545)
    actual_code=$(cast code "$contract" --block latest --rpc-url http://localhost:8545)

    missing=$(tail -1 integration_test/contracts/flatkv_evm_missing_addr.txt)
    expected_missing_balance=$(tail -1 integration_test/contracts/flatkv_evm_missing_balance_expected.txt)
    expected_missing_storage=$(tail -1 integration_test/contracts/flatkv_evm_missing_storage_expected.txt)
    actual_missing_balance=$(cast to-hex "$(cast balance "$missing" --block latest --rpc-url http://localhost:8545)")
    actual_missing_storage=$(cast storage "$missing" "$slot" --block latest --rpc-url http://localhost:8545)

    if [ "$actual_balance" != "$expected_balance" ]; then
      echo "latest balance mismatch: got $actual_balance want $expected_balance" >&2
      exit 1
    fi
    if [ "$actual_storage" != "$expected_storage" ]; then
      echo "latest storage mismatch: got $actual_storage want $expected_storage" >&2
      exit 1
    fi
    if [ "$actual_code" != "$expected_code" ]; then
      echo "latest code mismatch: got $actual_code want $expected_code" >&2
      exit 1
    fi
    if [ "$actual_missing_balance" != "$expected_missing_balance" ]; then
      echo "missing balance mismatch: got $actual_missing_balance want $expected_missing_balance" >&2
      exit 1
    fi
    if [ "$actual_missing_storage" != "$expected_missing_storage" ]; then
      echo "missing storage mismatch: got $actual_missing_storage want $expected_missing_storage" >&2
      exit 1
    fi
  '; then
    return 1
  fi
  echo "FlatKV EVM fixture queries passed on $node"
}

assert_flatkv_only_layout
wait_for_all_heights 10 "$SMOKE_TIMEOUT"
for i in $(seq 0 $((NODE_COUNT - 1))); do
  wait_for_evm_rpc "sei-node-$i" 60
done

for i in $(seq 0 $((NODE_COUNT - 1))); do
  ensure_seidb "sei-node-$i"
done

assert_evm_fixture_queries "$DONOR_NODE"
wait_for_height "$DONOR_NODE" "$MIN_DONOR_HEIGHT" "$SNAPSHOT_WAIT_TIMEOUT"
wait_for_snapshot_at_or_after "$DONOR_NODE" "$MIN_DONOR_HEIGHT" "$SNAPSHOT_WAIT_TIMEOUT"

latest=$(node_height "$DONOR_NODE")
trust_height=$((latest - TRUST_LAG))
if [ "$trust_height" -lt 1 ]; then
  trust_height=1
fi
trust_hash=$(block_hash "$DONOR_NODE" "$trust_height")
if [ -z "$trust_hash" ] || [ "$trust_hash" = "null" ]; then
  echo "ERROR: failed to fetch trust hash at height $trust_height from $DONOR_NODE" >&2
  dump_node_log "$DONOR_NODE"
  exit 1
fi
echo "Using state-sync trust_height=$trust_height trust_hash=$trust_hash"

stop_height=$(node_height "$VICTIM_NODE")
echo "Stopping $VICTIM_NODE at height $stop_height before flatkv_only state-sync recovery"
docker exec "$VICTIM_NODE" pkill -f "seid start" >/dev/null 2>&1 || true
sleep 2

echo "Wiping $VICTIM_NODE data and wasm directories while preserving priv_validator_state.json"
docker exec "$VICTIM_NODE" bash -lc "
  set -euo pipefail
  cp /root/.sei/data/priv_validator_state.json /tmp/flatkv-only-priv-validator-state.json
  rm -rf /root/.sei/data /root/.sei/wasm /sei-protocol/sei-chain/build/generated/node_${VICTIM_INDEX}/snapshots
  mkdir -p /root/.sei/data /sei-protocol/sei-chain/build/generated/node_${VICTIM_INDEX}/snapshots
  mv /tmp/flatkv-only-priv-validator-state.json /root/.sei/data/priv_validator_state.json
  sed -i.bak -e 's|^snapshot-directory *=.*|snapshot-directory = \"./build/generated/node_${VICTIM_INDEX}/snapshots\"|' /root/.sei/config/app.toml
"
configure_statesync "$VICTIM_NODE" "$trust_height" "$trust_hash"
assert_statesync_configured "$VICTIM_NODE" "$trust_height" "$trust_hash"

echo "Starting $VICTIM_NODE for flatkv_only state-sync recovery"
start_victim
wait_for_process "$VICTIM_NODE" 30
wait_for_catchup "$VICTIM_NODE" "$DONOR_NODE" "$CATCHUP_TIMEOUT"
log_recovery_path "$VICTIM_NODE"
assert_flatkv_only_layout
assert_evm_fixture_queries "$VICTIM_NODE"
assert_flatkv_digests_match

echo "PASS: $VICTIM_NODE recovered in flatkv_only mode and FlatKV state matches the donor validators"
