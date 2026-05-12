#!/bin/bash
#
# verify_flatkv_total_loss_recovery.sh
#
# D3a: simulate total local state loss for one validator, recover it via
# state-sync, and require its FlatKV content to match the surviving validators.

set -euo pipefail

NODE_COUNT=${FLATKV_TOTAL_LOSS_NODE_COUNT:-4}
VICTIM_INDEX=${FLATKV_TOTAL_LOSS_VICTIM_INDEX:-3}
VICTIM_NODE="sei-node-${VICTIM_INDEX}"
DONOR_NODE=${FLATKV_TOTAL_LOSS_DONOR:-sei-node-0}
SECOND_RPC_NODE=${FLATKV_TOTAL_LOSS_SECOND_RPC:-sei-node-1}
FLATKV_DIR=${FLATKV_DIR:-/root/.sei/data/state_commit/flatkv}
GO_BIN=${GO_BIN:-/usr/local/go/bin/go}
MIN_DONOR_HEIGHT=${FLATKV_TOTAL_LOSS_MIN_DONOR_HEIGHT:-250}
TRUST_LAG=${FLATKV_TOTAL_LOSS_TRUST_LAG:-30}
CATCHUP_TIMEOUT=${FLATKV_TOTAL_LOSS_CATCHUP_TIMEOUT:-300}
COMPARE_BUFFER=${FLATKV_TOTAL_LOSS_COMPARE_BUFFER:-2}

echo "verify_flatkv_total_loss_recovery: victim=$VICTIM_NODE donor=$DONOR_NODE"

dump_node_log() {
  local node=$1
  local logfile node_id
  node_id=${node#sei-node-}
  if [ "$node_id" = "$node" ]; then
    logfile="/sei-protocol/sei-chain/build/generated/logs/rpc-node.log"
  else
    logfile="/sei-protocol/sei-chain/build/generated/logs/seid-${node_id}.log"
  fi
  echo "==================== ${node} seid log ${logfile} (last 200 lines) ====================" >&2
  docker exec "$node" tail -200 "$logfile" >&2 2>/dev/null \
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

ensure_seidb() {
  local node=$1
  if docker exec "$node" test -x /sei-protocol/sei-chain/build/seidb >/dev/null 2>&1; then
    return 0
  fi
  echo "Building seidb on $node..."
  docker exec -e GOPROXY="${GOPROXY:-https://proxy.golang.org,direct}" "$node" bash -lc \
    "cd /sei-protocol/sei-chain && $GO_BIN build -o build/seidb ./sei-db/tools/cmd/seidb"
}

block_hash() {
  local node=$1
  local height=$2
  docker exec "$node" bash -lc \
    "curl -sf 'http://localhost:26657/block?height=${height}' | jq -r '.result.block_id.hash // .block_id.hash'"
}

configure_statesync() {
  local victim=$1
  local trust_height=$2
  local trust_hash=$3
  docker exec "$victim" bash -lc "
    set -euo pipefail
    peers=\$(grep -v '^$' /sei-protocol/sei-chain/build/generated/persistent_peers.txt | paste -sd ',' -)
    sed -i.bak -e 's|^enable *=.*|enable = true|' /root/.sei/config/config.toml
    sed -i.bak -e 's|^rpc-servers *=.*|rpc-servers = \"${DONOR_NODE}:26657,${SECOND_RPC_NODE}:26657\"|' /root/.sei/config/config.toml
    sed -i.bak -e 's|^trust-height *=.*|trust-height = ${trust_height}|' /root/.sei/config/config.toml
    sed -i.bak -e 's|^trust-hash *=.*|trust-hash = \"${trust_hash}\"|' /root/.sei/config/config.toml
    sed -i.bak -e \"s|^persistent-peers *=.*|persistent-peers = \\\"\${peers}\\\"|\" /root/.sei/config/config.toml
  "
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
  local tolerance=${FLATKV_TOTAL_LOSS_CATCHUP_TOLERANCE:-10}
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
    out_dir=/tmp/flatkv-total-loss-${version}-${node}
    rm -rf \"\$out_dir\" && mkdir -p \"\$out_dir\"
    cd /sei-protocol/sei-chain
    build/seidb dump-flatkv \
      --db-dir $FLATKV_DIR \
      --output-dir \"\$out_dir\" \
      --height $version > /dev/null
    # Hash canonical EVM buckets only. The legacy bucket is a fallback path for
    # non-EVM module-prefixed rows and can contain validator-local dual-write
    # noise in post-import test clusters.
    tail -q -n +2 \"\$out_dir/account\" \"\$out_dir/code\" \"\$out_dir/storage\" \
      | sha256sum | cut -d' ' -f1
  "
}

assert_flatkv_digests_match() {
  for i in $(seq 0 $((NODE_COUNT - 1))); do
    ensure_seidb "sei-node-$i"
  done

  local version
  version=$(pick_compare_height)
  if [ -z "$version" ] || [ "$version" -lt 1 ]; then
    echo "ERROR: failed to pick a positive comparison height" >&2
    exit 1
  fi
  echo "Comparing FlatKV across $NODE_COUNT validators at chain height $version"

  local reference_digest="" reference_node="" mismatch=false
  for i in $(seq 0 $((NODE_COUNT - 1))); do
    local node digest
    node="sei-node-$i"
    digest=$(flatkv_dump_digest "$node" "$version")
    echo "  ${node} sha256 = $digest"
    if [ -z "$reference_digest" ]; then
      reference_digest="$digest"
      reference_node="$node"
      continue
    fi
    if [ "$digest" != "$reference_digest" ]; then
      echo "FAIL: ${node} diverges from ${reference_node} at height $version" >&2
      mismatch=true
    fi
  done

  if $mismatch; then
    for i in $(seq 0 $((NODE_COUNT - 1))); do
      dump_node_log "sei-node-$i"
    done
    exit 1
  fi
}

wait_for_height "$DONOR_NODE" "$MIN_DONOR_HEIGHT" 420
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
echo "Stopping $VICTIM_NODE at height $stop_height before total-loss state-sync test"
docker exec "$VICTIM_NODE" pkill -f "seid start" >/dev/null 2>&1 || true
sleep 2

echo "Wiping $VICTIM_NODE data and wasm directories while preserving priv_validator_state.json"
docker exec "$VICTIM_NODE" bash -lc "
  set -euo pipefail
  cp /root/.sei/data/priv_validator_state.json /tmp/flatkv-priv-validator-state.json
  rm -rf /root/.sei/data /root/.sei/wasm
  mkdir -p /root/.sei/data
  mv /tmp/flatkv-priv-validator-state.json /root/.sei/data/priv_validator_state.json
"
configure_statesync "$VICTIM_NODE" "$trust_height" "$trust_hash"

echo "Starting $VICTIM_NODE for total-loss state-sync recovery"
start_victim
wait_for_process "$VICTIM_NODE" 30
wait_for_catchup "$VICTIM_NODE" "$DONOR_NODE" "$CATCHUP_TIMEOUT"
assert_flatkv_digests_match

echo "PASS: $VICTIM_NODE recovered from total local state loss via state-sync and matches FlatKV digests"
