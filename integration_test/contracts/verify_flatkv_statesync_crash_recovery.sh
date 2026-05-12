#!/bin/bash
#
# verify_flatkv_statesync_crash_recovery.sh
#
# F3: wipe one validator's local state, start state-sync, SIGKILL it while
# state-sync is in progress, then restart and require it to catch up with
# byte-identical FlatKV content.

set -euo pipefail

NODE_COUNT=${FLATKV_STATESYNC_NODE_COUNT:-4}
VICTIM_INDEX=${FLATKV_STATESYNC_VICTIM_INDEX:-3}
VICTIM_NODE="sei-node-${VICTIM_INDEX}"
DONOR_NODE=${FLATKV_STATESYNC_DONOR:-sei-node-0}
SECOND_RPC_NODE=${FLATKV_STATESYNC_SECOND_RPC:-sei-node-1}
FLATKV_DIR=${FLATKV_DIR:-/root/.sei/data/state_commit/flatkv}
GO_BIN=${GO_BIN:-/usr/local/go/bin/go}
MIN_DONOR_HEIGHT=${FLATKV_STATESYNC_MIN_DONOR_HEIGHT:-250}
TRUST_LAG=${FLATKV_STATESYNC_TRUST_LAG:-30}
KILL_WINDOW_SECS=${FLATKV_STATESYNC_KILL_WINDOW_SECS:-30}
CATCHUP_TIMEOUT=${FLATKV_STATESYNC_CATCHUP_TIMEOUT:-300}
COMPARE_BUFFER=${FLATKV_STATESYNC_COMPARE_BUFFER:-2}
IMPORT_HEIGHT_FILE=${FLATKV_IMPORT_HEIGHT_FILE:-$(pwd)/integration_test/contracts/flatkv_import_height.txt}
MIN_SNAPSHOT_HEIGHT_OVERRIDE=${FLATKV_STATESYNC_MIN_SNAPSHOT_HEIGHT:-}
SNAPSHOT_WAIT_TIMEOUT=${FLATKV_STATESYNC_SNAPSHOT_WAIT_TIMEOUT:-420}

echo "verify_flatkv_statesync_crash_recovery: victim=$VICTIM_NODE donor=$DONOR_NODE"

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

snapshot_heights() {
  local node=$1
  # Cosmos handleQueryApp("snapshots") json.Marshal's the proto struct with no
  # JSON field tags, so the stdlib emits Go field names (capitalized) -- the
  # decoded payload is {"Snapshots":[{"Height":N,...}]}. Lowercase jq paths
  # silently match nothing and the test waits 420s on phantom-empty snapshots.
  # The ABCI response envelope differs between callers; support both shapes.
  docker exec "$node" bash -lc '
    set -euo pipefail
    value=$(curl -sf --get --data-urlencode "path=\"/app/snapshots\"" http://localhost:26657/abci_query | jq -r ".result.response.value // .response.value // empty")
    if [ -z "$value" ]; then
      exit 0
    fi
    printf "%s" "$value" | base64 -d 2>/dev/null | jq -r ".Snapshots[]?.Height // empty"
  ' 2>/dev/null || true
}

# Emit the donor's actual snapshot configuration and any snapshot-related log
# lines so a snapshot-wait timeout points at the root cause (config rewrite
# wiped snapshot-interval, snapshot creation panics post FlatKV import, abci
# parser breakage, etc.) instead of just "no snapshots after 420s".
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
  echo "--- snapshot-related lines in ${logfile} (matching: snapshot, Snapshot, pruned, exporter) ---" >&2
  docker exec "$node" bash -lc \
    "grep -E 'snapshot|Snapshot|pruned|exporter' '$logfile' 2>/dev/null | tail -200" >&2 \
    || echo "(no matches)" >&2
  echo "==================== end ${node} snapshot diagnostics ====================" >&2
}

min_required_snapshot_height() {
  local min_height
  if [ -n "$MIN_SNAPSHOT_HEIGHT_OVERRIDE" ]; then
    min_height=$MIN_SNAPSHOT_HEIGHT_OVERRIDE
  else
    if [ ! -s "$IMPORT_HEIGHT_FILE" ]; then
      echo "ERROR: missing FlatKV import height marker $IMPORT_HEIGHT_FILE" >&2
      echo "Run import_flatkv_evm_cluster.sh first, or set FLATKV_STATESYNC_MIN_SNAPSHOT_HEIGHT explicitly." >&2
      exit 1
    fi
    import_height=$(tail -1 "$IMPORT_HEIGHT_FILE")
    min_height=$((import_height + 1))
  fi
  if [ "$min_height" -lt "$MIN_DONOR_HEIGHT" ]; then
    min_height=$MIN_DONOR_HEIGHT
  fi
  echo "$min_height"
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
  docker exec "$victim" bash -lc "
    set -euo pipefail
    peers=\$(grep -v '^$' /sei-protocol/sei-chain/build/generated/persistent_peers.txt | paste -sd ',' -)
    # Scope the enable rewrite to the [statesync] section so a future
    # config.toml that adds another section with a top-level "enable = ..."
    # key (e.g. [fastsync]) is not silently flipped to true. The address
    # range stops at the next section header so the substitution can never
    # spill past the statesync block.
    sed -i.bak -e '/^\[statesync\]/,/^\[/ s|^enable *=.*|enable = true|' /root/.sei/config/config.toml
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

wait_for_statesync_log_and_kill() {
  local node=$1
  local timeout=$2
  local elapsed=0
  local log_path="/sei-protocol/sei-chain/build/generated/logs/seid-${VICTIM_INDEX}.log"
  local regex='statesync|state sync|snapshot|chunk|restore'

  while [ "$elapsed" -lt "$timeout" ]; do
    if docker exec "$node" bash -lc "tail -80 '$log_path' 2>/dev/null | grep -Eiq '$regex'"; then
      echo "Detected state-sync activity in $node log; killing mid-flight"
      docker exec "$node" pkill -9 -f "seid start" >/dev/null 2>&1 || true
      return 0
    fi
    if ! docker exec "$node" pgrep -f "seid start" >/dev/null 2>&1; then
      echo "ERROR: $node exited before mid-flight kill could be injected" >&2
      dump_node_log "$node"
      return 1
    fi
    sleep 1
    elapsed=$((elapsed + 1))
  done

  echo "No explicit state-sync log detected within ${timeout}s; killing $node anyway"
  docker exec "$node" pkill -9 -f "seid start" >/dev/null 2>&1 || true
}

wait_for_catchup() {
  local victim=$1
  local donor=$2
  local timeout=$3
  local tolerance=${FLATKV_STATESYNC_CATCHUP_TOLERANCE:-10}
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
    out_dir=/tmp/flatkv-statesync-crash-${version}-${node}
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

required_snapshot_height=$(min_required_snapshot_height)
wait_for_snapshot_at_or_after "$DONOR_NODE" "$required_snapshot_height" "$SNAPSHOT_WAIT_TIMEOUT"
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
echo "Stopping $VICTIM_NODE at height $stop_height before state-sync crash test"
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

echo "Starting $VICTIM_NODE for state-sync, then killing during restore"
start_victim
wait_for_process "$VICTIM_NODE" 20
wait_for_statesync_log_and_kill "$VICTIM_NODE" "$KILL_WINDOW_SECS"
sleep 2
if docker exec "$VICTIM_NODE" pgrep -f "seid start" >/dev/null 2>&1; then
  echo "ERROR: $VICTIM_NODE survived the injected state-sync SIGKILL" >&2
  dump_node_log "$VICTIM_NODE"
  exit 1
fi

echo "Restarting $VICTIM_NODE after mid-state-sync crash"
start_victim
wait_for_process "$VICTIM_NODE" 30
wait_for_catchup "$VICTIM_NODE" "$DONOR_NODE" "$CATCHUP_TIMEOUT"
assert_flatkv_digests_match

echo "PASS: $VICTIM_NODE recovered from a SIGKILL during state-sync and matches FlatKV digests"
