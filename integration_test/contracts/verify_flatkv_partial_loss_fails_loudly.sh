#!/bin/bash
#
# verify_flatkv_partial_loss_fails_loudly.sh
#
# D3b: delete only the FlatKV directory while leaving memiavl, SS, and
# tendermint data intact. The node must either fail loudly on restart or
# fully self-heal without diverging from the other validators.

set -euo pipefail

NODE_COUNT=${FLATKV_PARTIAL_LOSS_NODE_COUNT:-4}
VICTIM_INDEX=${FLATKV_PARTIAL_LOSS_VICTIM_INDEX:-3}
VICTIM_NODE="sei-node-${VICTIM_INDEX}"
FLATKV_DIR=${FLATKV_DIR:-/root/.sei/data/state_commit/flatkv}
GO_BIN=${GO_BIN:-/usr/local/go/bin/go}
RESTART_OBSERVE_SECS=${FLATKV_PARTIAL_LOSS_RESTART_OBSERVE_SECS:-20}
COMPARE_BUFFER=${FLATKV_PARTIAL_LOSS_COMPARE_BUFFER:-2}
# Cap on how long we wait for the victim to catch up to the other validators
# before comparing FlatKV digests. The dump-flatkv tool clones a snapshot +
# WAL into a temp dir and only retries a small number of times if a live
# writer rolls a new snapshot and truncates the WAL mid-clone; running the
# digest comparison while the victim is still actively blocksyncing
# reliably loses that race on busy CI runners.
CATCHUP_TIMEOUT=${FLATKV_PARTIAL_LOSS_CATCHUP_TIMEOUT:-240}
CATCHUP_TOLERANCE=${FLATKV_PARTIAL_LOSS_CATCHUP_TOLERANCE:-10}

echo "verify_flatkv_partial_loss_fails_loudly: victim=$VICTIM_NODE flatkv_dir=$FLATKV_DIR"

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

# wait_for_catchup polls until the victim's height is within $tolerance of the
# maximum height across the other (non-victim) validators, or until $timeout
# seconds elapse. This must run before dump-flatkv: while the victim is
# blocksyncing it rolls new FlatKV snapshots and truncates the WAL, which the
# dump tool can only ride out for a small fixed number of retries before
# panicking with "source kept churning".
wait_for_catchup() {
  local victim=$1
  local timeout=$2
  local tolerance=$3
  local elapsed=0
  while [ "$elapsed" -lt "$timeout" ]; do
    local max_other_h=0 victim_h gap
    for i in $(seq 0 $((NODE_COUNT - 1))); do
      if [ "$i" = "$VICTIM_INDEX" ]; then
        continue
      fi
      local h
      h=$(node_height "sei-node-$i")
      if [ "$h" -gt "$max_other_h" ]; then
        max_other_h=$h
      fi
    done
    victim_h=$(node_height "$victim")
    gap=$((max_other_h - victim_h))
    if [ "$victim_h" -gt 0 ] && [ "$gap" -le "$tolerance" ]; then
      echo "$victim caught up: others_max=$max_other_h victim=$victim_h gap=$gap"
      return 0
    fi
    echo "Waiting for $victim to catch up: others_max=$max_other_h victim=$victim_h gap=$gap (elapsed=${elapsed}s/${timeout}s)"
    sleep 5
    elapsed=$((elapsed + 5))
  done
  echo "ERROR: $victim failed to catch up within ${timeout}s" >&2
  echo "Height snapshot at timeout:" >&2
  for i in $(seq 0 $((NODE_COUNT - 1))); do
    local node h
    node="sei-node-$i"
    h=$(node_height "$node")
    if [ "$i" = "$VICTIM_INDEX" ]; then
      echo "  $node (victim): $h" >&2
    else
      echo "  $node: $h" >&2
    fi
  done
  dump_node_log "$victim"
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
    out_dir=/tmp/flatkv-partial-loss-${version}-${node}
    rm -rf \"\$out_dir\" && mkdir -p \"\$out_dir\"
    cd /sei-protocol/sei-chain
    build/seidb dump-flatkv \
      --db-dir $FLATKV_DIR \
      --output-dir \"\$out_dir\" \
      --height $version > /dev/null
    # Hash canonical EVM buckets only. The misc bucket is a fallback path for
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
    if ! digest=$(flatkv_dump_digest "$node" "$version"); then
      echo "FAIL: could not dump FlatKV from ${node} at height $version" >&2
      dump_node_log "$node"
      exit 1
    fi
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

echo "Stopping $VICTIM_NODE before deleting only FlatKV data"
docker exec "$VICTIM_NODE" pkill -f "seid start" >/dev/null 2>&1 || true
sleep 2

if docker exec "$VICTIM_NODE" pgrep -f "seid start" >/dev/null 2>&1; then
  echo "ERROR: $VICTIM_NODE did not stop before partial-loss injection" >&2
  dump_node_log "$VICTIM_NODE"
  exit 1
fi

echo "Deleting only $FLATKV_DIR on $VICTIM_NODE"
docker exec "$VICTIM_NODE" bash -lc "rm -rf '$FLATKV_DIR'"

echo "Restarting $VICTIM_NODE after FlatKV-only loss"
docker exec -d -e "ID=${VICTIM_INDEX}" "$VICTIM_NODE" /usr/bin/start_sei.sh
sleep "$RESTART_OBSERVE_SECS"

if ! docker exec "$VICTIM_NODE" pgrep -f "seid start" >/dev/null 2>&1; then
  echo "$VICTIM_NODE exited after FlatKV-only loss; checking for a clear startup error"
  if docker exec "$VICTIM_NODE" bash -lc \
    "grep -Eiq 'flatkv|version|missing|LoadVersion|reconcile|state_commit' /sei-protocol/sei-chain/build/generated/logs/seid-${VICTIM_INDEX}.log"; then
    echo "PASS: $VICTIM_NODE failed loudly after FlatKV-only loss"
    exit 0
  fi
  echo "FAIL: $VICTIM_NODE exited after FlatKV-only loss but log did not identify the storage failure" >&2
  dump_node_log "$VICTIM_NODE"
  exit 1
fi

echo "$VICTIM_NODE stayed running after FlatKV-only loss; verifying it did not silently diverge"
if ! docker exec "$VICTIM_NODE" build/seid status >/dev/null 2>&1; then
  echo "FAIL: $VICTIM_NODE process is alive but status is not healthy" >&2
  dump_node_log "$VICTIM_NODE"
  exit 1
fi

# Wait for the victim to catch up before dumping FlatKV; see CATCHUP_TIMEOUT
# comment at the top of this script for why running dump-flatkv against an
# actively-syncing node is unreliable.
wait_for_catchup "$VICTIM_NODE" "$CATCHUP_TIMEOUT" "$CATCHUP_TOLERANCE"

assert_flatkv_digests_match
echo "PASS: $VICTIM_NODE self-healed after FlatKV-only loss and matches FlatKV digests"
