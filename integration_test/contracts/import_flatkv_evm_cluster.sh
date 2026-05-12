#!/bin/bash

set -euo pipefail

PROJECT_ROOT=$(git rev-parse --show-toplevel)
NODE_COUNT=${FLATKV_EVM_IMPORT_NODE_COUNT:-4}
IMPORT_HEIGHT_FILE=${FLATKV_IMPORT_HEIGHT_FILE:-$PROJECT_ROOT/integration_test/contracts/flatkv_import_height.txt}

dump_node_log() {
  local node=$1
  echo "==================== ${node} seid log (last 200 lines) ====================" >&2
  local node_id=${node#sei-node-}
  docker exec "$node" tail -200 "/sei-protocol/sei-chain/build/generated/logs/seid-${node_id}.log" >&2 || true
  echo "==================== ${node} end log ====================" >&2
}

wait_for_height() {
  local min_height=$1
  local timeout=${2:-180}
  local elapsed=0
  local height=0

  until [ "$elapsed" -ge "$timeout" ]; do
    height=$(docker exec sei-node-0 build/seid status 2>/dev/null | jq -r ".SyncInfo.latest_block_height // 0" || echo 0)
    if [ "$height" -gt "$min_height" ]; then
      echo "sei-node-0 reached height $height"
      return 0
    fi
    echo "Still waiting for sei-node-0 to advance past height $min_height (height=$height elapsed=${elapsed}s/${timeout}s)"
    sleep 5
    elapsed=$((elapsed + 5))
  done

  echo "Timed out waiting for sei-node-0 to advance past height $min_height (last height: $height)" >&2
  for i in $(seq 0 $((NODE_COUNT - 1))); do
    dump_node_log "sei-node-$i"
  done
  return 1
}

# wait_for_evm_rpc polls each node's EVM HTTP endpoint until it responds, so
# the post-restart flatkv_evm_test.yaml run can't race the seid restart and
# hit connection refused on http://localhost:8545. Tendermint typically
# advances a height or two before the in-process EVM RPC server finishes
# binding 8545, so wait_for_height alone is not a sufficient readiness gate
# for the next test phase.
wait_for_evm_rpc() {
  local timeout=${1:-120}
  for i in $(seq 0 $((NODE_COUNT - 1))); do
    local node="sei-node-$i"
    local elapsed=0
    until docker exec "$node" bash -lc 'curl -sf -o /dev/null -X POST -H "Content-Type: application/json" -d "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"eth_blockNumber\",\"params\":[]}" http://localhost:8545'; do
      if [ "$elapsed" -ge "$timeout" ]; then
        echo "EVM RPC on $node did not become ready within ${timeout}s after restart" >&2
        dump_node_log "$node"
        return 1
      fi
      echo "Waiting for EVM RPC on $node (elapsed=${elapsed}s/${timeout}s)"
      sleep 3
      elapsed=$((elapsed + 3))
    done
    echo "EVM RPC on $node is responding"
  done
}

echo "Building seidb import tool..."
# Go lives at /usr/local/go/bin/go in the container (see docker/localnode/Dockerfile)
# but is not on the default PATH for non-interactive shells, so call it absolutely.
GO_BIN=${GO_BIN:-/usr/local/go/bin/go}
docker exec -e GOPROXY="${GOPROXY:-https://proxy.golang.org,direct}" sei-node-0 bash -c "cd /sei-protocol/sei-chain && $GO_BIN build -o build/seidb ./sei-db/tools/cmd/seidb"

start_height=$(docker exec sei-node-0 build/seid status | jq -r ".SyncInfo.latest_block_height")
echo "Stopping seid processes at height $start_height..."
for i in $(seq 0 $((NODE_COUNT - 1))); do
  docker exec "sei-node-$i" pkill -f "seid start" >/dev/null 2>&1 || true
done

echo "Waiting for seid processes to stop..."
for i in $(seq 0 $((NODE_COUNT - 1))); do
  stopped=false
  for _ in $(seq 1 30); do
    if ! docker exec "sei-node-$i" pgrep -f "seid start" >/dev/null 2>&1; then
      stopped=true
      break
    fi
    sleep 1
  done
  if [ "$stopped" != "true" ]; then
    echo "sei-node-$i did not stop within 30s" >&2
    exit 1
  fi
done

echo "Importing evm module from memiavl into FlatKV on all validators..."
for i in $(seq 0 $((NODE_COUNT - 1))); do
  docker exec "sei-node-$i" bash -lc "cd /sei-protocol/sei-chain && build/seidb import-flatkv-from-memiavl --modules=evm --data-dir /root/.sei/data"
done
printf "%s\n" "$start_height" > "$IMPORT_HEIGHT_FILE"
echo "Recorded FlatKV import height $start_height in $IMPORT_HEIGHT_FILE"

echo "Applying GIGA_STORAGE config and restarting seid processes..."
for i in $(seq 0 $((NODE_COUNT - 1))); do
  docker exec -e GIGA_STORAGE=true "sei-node-$i" /usr/bin/config_override.sh
  # The import tool moves only SC-layer EVM data into FlatKV. SS history
  # for EVM stays in the existing combined cosmos pebbledb, so we must keep
  # evm-ss-split=false to avoid the rootmulti startup panic:
  #   "EVM SS directory ... does not exist but Cosmos SS already has history".
  # Switching the SS layer to split mode mid-life requires a separate state-sync
  # workflow which is out of scope for this SC import test.
  docker exec "sei-node-$i" sed -i 's/evm-ss-split = true/evm-ss-split = false/' /root/.sei/config/app.toml
  # Lattice hash must also stay off across the import boundary. Pre-import
  # the chain ran without FlatKV, so tendermint persisted AppHash = memiavl-only
  # for all blocks up to the import height. Turning sc-enable-lattice-hash
  # on now would fold the FlatKV LtHash into the AppHash and the replay check
  # at startup would fail with "state.AppHash does not match AppHash after replay".
  # dual_write does not require lattice hash (see sei-db/config/toml_test.go);
  # only split_write does. A real production rollout would coordinate this
  # transition via a chain upgrade at an agreed height.
  docker exec "sei-node-$i" sed -i 's/sc-enable-lattice-hash = true/sc-enable-lattice-hash = false/' /root/.sei/config/app.toml
done
# `docker exec -d` is required: start_sei.sh backgrounds seid then exits, and a
# non-detached docker exec session would close stdout/stderr, killing seid.
# See integration_test/autobahn/autobahn_test.go::restartNode for the precedent.
for i in $(seq 0 $((NODE_COUNT - 1))); do
  docker exec -d -e "ID=$i" "sei-node-$i" /usr/bin/start_sei.sh
done

# Confirm each seid actually came up before waiting on block production, so a
# crash on startup is reported promptly instead of after the 4 minute timeout.
sleep 5
for i in $(seq 0 $((NODE_COUNT - 1))); do
  if ! docker exec "sei-node-$i" pgrep -f "seid start" >/dev/null 2>&1; then
    echo "ERROR: sei-node-$i did not stay running after restart" >&2
    dump_node_log "sei-node-$i"
    exit 1
  fi
done

wait_for_height "$start_height" 240

# Tendermint advancing past start_height does NOT imply the in-process EVM
# RPC HTTP server has finished binding 8545. The downstream
# integration_test/seidb/flatkv_evm_test.yaml docker-execs `cast` against
# http://localhost:8545; gate on that endpoint explicitly so it can't race
# the seid restart.
wait_for_evm_rpc 120

echo "FlatKV EVM import completed for $NODE_COUNT validators in $PROJECT_ROOT"
