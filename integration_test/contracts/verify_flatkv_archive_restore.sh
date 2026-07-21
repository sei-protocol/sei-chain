#!/bin/bash
#
# verify_flatkv_archive_restore.sh
#
# End-to-end FlatKV-only out-of-band checkpoint restore:
#   1) assert a docker-compose cluster is running in flatkv_only mode;
#   2) archive a donor's current FlatKV checkpoint and upload it to S3;
#   3) wipe one validator's local state;
#   4) restore the checkpoint archive from S3, bootstrap Tendermint state, and
#      require the victim to block-sync back to the donor.

set -euo pipefail

NODE_COUNT=${FLATKV_ARCHIVE_NODE_COUNT:-4}
VICTIM_INDEX=${FLATKV_ARCHIVE_VICTIM_INDEX:-3}
VICTIM_NODE="sei-node-${VICTIM_INDEX}"
DONOR_NODE=${FLATKV_ARCHIVE_DONOR:-sei-node-0}
SECOND_RPC_NODE=${FLATKV_ARCHIVE_SECOND_RPC:-sei-node-1}
FLATKV_DIR=${FLATKV_DIR:-/root/.sei/data/state_commit/flatkv}
APP_CONFIG=${APP_CONFIG:-/root/.sei/config/app.toml}
GO_BIN=${GO_BIN:-/usr/local/go/bin/go}
MIN_DONOR_HEIGHT=${FLATKV_ARCHIVE_MIN_DONOR_HEIGHT:-250}
TRUST_LAG=${FLATKV_ARCHIVE_TRUST_LAG:-30}
SMOKE_TIMEOUT=${FLATKV_ARCHIVE_SMOKE_TIMEOUT:-180}
CATCHUP_TIMEOUT=${FLATKV_ARCHIVE_CATCHUP_TIMEOUT:-300}
CATCHUP_TOLERANCE=${FLATKV_ARCHIVE_CATCHUP_TOLERANCE:-10}
S3_URI=${S3_URI:-}

if [ -z "$S3_URI" ]; then
  echo "ERROR: S3_URI=s3://bucket/key is required" >&2
  exit 1
fi

echo "verify_flatkv_archive_restore: victim=$VICTIM_NODE donor=$DONOR_NODE second_rpc=$SECOND_RPC_NODE s3=$S3_URI"

aws_env_args=()
for name in AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY AWS_SESSION_TOKEN AWS_REGION AWS_DEFAULT_REGION AWS_PROFILE AWS_SDK_LOAD_CONFIG; do
  if [ -n "${!name:-}" ]; then
    aws_env_args+=("-e" "${name}=${!name}")
  fi
done

dump_node_log() {
  local node=$1
  local node_id=${node#sei-node-}
  local logfile="/sei-protocol/sei-chain/build/generated/logs/seid-${node_id}.log"
  echo "==================== ${node} app.toml state-commit excerpt ====================" >&2
  docker exec "$node" bash -lc "grep -E '^(sc-write-mode|sc-write-mode-enable-auto|evm-ss-split)' '$APP_CONFIG' 2>/dev/null" >&2 || true
  echo "==================== ${node} seid log ${logfile} (last 240 lines) ====================" >&2
  docker exec "$node" tail -240 "$logfile" >&2 2>/dev/null || true
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

chain_id() {
  docker exec "$DONOR_NODE" build/seid status 2>/dev/null \
    | jq -r '.NodeInfo.network // .node_info.network'
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
  local elapsed=0
  while [ "$elapsed" -lt "$timeout" ]; do
    local donor_h victim_h gap
    donor_h=$(node_height "$donor")
    victim_h=$(node_height "$victim")
    gap=$((donor_h - victim_h))
    if [ "$victim_h" -gt 0 ] && [ "$gap" -le "$CATCHUP_TOLERANCE" ]; then
      echo "$victim caught up: donor=$donor_h victim=$victim_h gap=$gap"
      return 0
    fi
    echo "Waiting for archive-restore catch-up: donor=$donor_h victim=$victim_h gap=$gap (elapsed=${elapsed}s/${timeout}s)"
    sleep 5
    elapsed=$((elapsed + 5))
  done
  echo "ERROR: $victim failed to catch up within ${timeout}s" >&2
  dump_node_log "$victim"
  dump_node_log "$donor"
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
    sleep 2
    elapsed=$((elapsed + 2))
  done
  echo "ERROR: EVM RPC on $node did not respond within ${timeout}s" >&2
  dump_node_log "$node"
  return 1
}

ensure_seid_has_archive_cmd() {
  local node=$1
  if docker exec "$node" build/seid flatkv-archive --help >/dev/null 2>&1; then
    return 0
  fi
  echo "Building seid with flatkv-archive command on $node..."
  docker exec -e GOPROXY="${GOPROXY:-https://proxy.golang.org,direct}" "$node" bash -lc \
    "cd /sei-protocol/sei-chain && $GO_BIN build -o build/seid ./cmd/seid"
}

assert_flatkv_only_layout() {
  local failed=false
  for i in $(seq 0 $((NODE_COUNT - 1))); do
    local node="sei-node-$i"
    if ! docker exec "$node" bash -lc "
      set -euo pipefail
      mode=\$(grep -E '^sc-write-mode[[:space:]]*=' '$APP_CONFIG' | tail -1 | awk -F'\"' '{print \$2}' || true)
      auto=\$(grep -E '^sc-write-mode-enable-auto[[:space:]]*=' '$APP_CONFIG' | tail -1 | awk -F'= *' '{print \$2}' || true)
      if [ \"\$mode\" != 'flatkv_only' ]; then
        echo \"ERROR: $node has sc-write-mode='\$mode'; expected flatkv_only\" >&2
        exit 1
      fi
      if [ \"\$auto\" = 'true' ]; then
        echo \"ERROR: $node has sc-write-mode-enable-auto=true; archive requires pinned flatkv_only\" >&2
        exit 1
      fi
      test -d '$FLATKV_DIR'
    "; then
      failed=true
      dump_node_log "$node"
    fi
  done
  if $failed; then
    exit 1
  fi
  echo "All $NODE_COUNT validators are pinned flatkv_only"
}

start_victim() {
  docker exec -d -e "ID=${VICTIM_INDEX}" "$VICTIM_NODE" bash -lc "
    cd /sei-protocol/sei-chain
    mkdir -p build/generated/logs
    build/seid start --chain-id sei --inv-check-period 0 > build/generated/logs/seid-${VICTIM_INDEX}.log 2>&1
  "
}

assert_evm_fixture_queries() {
  local node=$1
  wait_for_evm_rpc "$node" 60
  docker exec "$node" bash -lc '
    set -euo pipefail
    export PATH="$HOME/.foundry/bin:/root/.foundry/bin:$PATH:/root/go/bin:/usr/local/go/bin"
    command -v cast >/dev/null 2>&1
    cd /sei-protocol/sei-chain
    recipient=$(tail -1 integration_test/contracts/flatkv_evm_recipient_addr.txt)
    expected_balance=$(tail -1 integration_test/contracts/flatkv_evm_balance_expected.txt)
    actual_balance=$(cast to-hex "$(cast balance "$recipient" --block latest --rpc-url http://localhost:8545)")
    if [ "$actual_balance" != "$expected_balance" ]; then
      echo "latest balance mismatch: got $actual_balance want $expected_balance" >&2
      exit 1
    fi
  '
  echo "FlatKV EVM fixture query passed on $node"
}

assert_block_hash_match() {
  local compare_height donor_hash victim_hash
  compare_height=$(node_height "$VICTIM_NODE")
  donor_hash=$(block_hash "$DONOR_NODE" "$compare_height")
  victim_hash=$(block_hash "$VICTIM_NODE" "$compare_height")
  echo "Comparing block hash at height $compare_height: donor=$donor_hash victim=$victim_hash"
  if [ -z "$donor_hash" ] || [ "$donor_hash" = "null" ] || [ "$donor_hash" != "$victim_hash" ]; then
    echo "ERROR: block hash mismatch after archive restore" >&2
    dump_node_log "$VICTIM_NODE"
    dump_node_log "$DONOR_NODE"
    exit 1
  fi
}

archive_start=$(date +%s)

assert_flatkv_only_layout
wait_for_height "$DONOR_NODE" "$MIN_DONOR_HEIGHT" "$SMOKE_TIMEOUT"
assert_evm_fixture_queries "$DONOR_NODE"
ensure_seid_has_archive_cmd "$DONOR_NODE"
ensure_seid_has_archive_cmd "$VICTIM_NODE"

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
cid=$(chain_id)
echo "Using archive restore trust_height=$trust_height trust_hash=$trust_hash chain_id=$cid"

echo "Creating and uploading FlatKV archive from $DONOR_NODE"
create_start=$(date +%s)
docker exec "${aws_env_args[@]}" "$DONOR_NODE" bash -lc "
  set -euo pipefail
  cd /sei-protocol/sei-chain
  rm -f /tmp/flatkv-archive.tar.zst
  build/seid flatkv-archive create \
    --home /root/.sei \
    --chain-id '$cid' \
    --archive-rpc http://localhost:26657 \
    --out /tmp/flatkv-archive.tar.zst \
    --upload '$S3_URI'
"
create_end=$(date +%s)
echo "Archive create+upload elapsed=$((create_end - create_start))s"

echo "Stopping and wiping $VICTIM_NODE before archive restore"
docker exec "$VICTIM_NODE" pkill -f "seid start" >/dev/null 2>&1 || true
sleep 2
docker exec "$VICTIM_NODE" bash -lc "
  set -euo pipefail
  cp /root/.sei/data/priv_validator_state.json /tmp/flatkv-archive-priv-validator-state.json
  rm -rf /root/.sei/data /root/.sei/wasm
  mkdir -p /root/.sei/data
  mv /tmp/flatkv-archive-priv-validator-state.json /root/.sei/data/priv_validator_state.json
  sed -i.bak -e '/^\[statesync\]/,/^\[/{s|^enable *=.*|enable = false|}' /root/.sei/config/config.toml
"

echo "Restoring FlatKV archive from S3 into $VICTIM_NODE"
restore_start=$(date +%s)
docker exec "${aws_env_args[@]}" "$VICTIM_NODE" bash -lc "
  set -euo pipefail
  cd /sei-protocol/sei-chain
  build/seid flatkv-archive restore \
    --home /root/.sei \
    --chain-id '$cid' \
    --from '$S3_URI' \
    --verification-rpc '${DONOR_NODE}:26657' \
    --verification-rpc '${SECOND_RPC_NODE}:26657' \
    --trust-height '$trust_height' \
    --trust-hash '$trust_hash' \
    --force
"
restore_end=$(date +%s)
echo "Archive download+restore+bootstrap elapsed=$((restore_end - restore_start))s"

echo "Starting $VICTIM_NODE after archive restore"
start_victim
wait_for_process "$VICTIM_NODE" 30
wait_for_catchup "$VICTIM_NODE" "$DONOR_NODE" "$CATCHUP_TIMEOUT"
assert_evm_fixture_queries "$VICTIM_NODE"
assert_block_hash_match

archive_end=$(date +%s)
echo "PASS: $VICTIM_NODE recovered via FlatKV archive. total_elapsed=$((archive_end - archive_start))s create_upload=$((create_end - create_start))s restore_bootstrap=$((restore_end - restore_start))s"

