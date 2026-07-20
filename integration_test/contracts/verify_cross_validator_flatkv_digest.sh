#!/bin/bash
#
# verify_cross_validator_flatkv_digest.sh
#
# Cross-validator physical consistency check: dump each of the 4 validators'
# FlatKV buckets at the same chain height and require all 4 digests to be
# byte-identical.
#
# Why chain height (not flatkv snapshot version): the FlatKV CommitStore
# only creates a non-genesis snapshot every SnapshotInterval blocks
# (default 10000, see sei-db/state_db/sc/flatkv/store_write.go:90). A CI
# devnet rarely reaches that interval, so every validator's flatkv dir
# contains only the genesis sentinel snapshot-0; intersecting snapshot
# versions across nodes degenerates to {0} and dump-flatkv --height 0
# silently falls back to "current" (flatkv_open.go:252) which is each
# node's wall-clock latest -- guaranteed to disagree even on a perfectly
# healthy chain.
#
# Picking a real chain height H instead sidesteps this entirely:
# dump-flatkv --height H walks snapshot-0 + WAL-replays to H, returning
# the state actually committed at H. Consensus guarantees that all
# validators executed the same blocks 1..H, so RawGlobalIterator output
# is byte-identical when H is committed everywhere.
#
# Rationale for the check itself: validator agreement is normally
# enforced implicitly via AppHash during consensus, but a silent drift
# (e.g. one validator's flatkv missing a bucket excluded from the LtHash
# input, or a write path bypassing the hash entirely) would not halt
# consensus. This script provides an independent physical-level check
# against that whole class of silent drift. It is intended for
# GIGA_STORAGE=true jobs, so legacy is intentionally included in the digest.

set -euo pipefail

NODE_COUNT=${FLATKV_DIGEST_NODE_COUNT:-4}
FLATKV_DIR=${FLATKV_DIR:-/root/.sei/data/state_commit/flatkv}
GO_BIN=${GO_BIN:-/usr/local/go/bin/go}
WAIT_TIMEOUT=${FLATKV_DIGEST_WAIT_TIMEOUT:-180}
MIN_HEIGHT=${FLATKV_DIGEST_MIN_HEIGHT:-10}
# Subtract this many blocks from min(chain heights) so any trailing
# validator still mid-commit at the smallest height has had a couple of
# tendermint timeouts to settle there before we read it.
COMPARE_BUFFER=${FLATKV_DIGEST_COMPARE_BUFFER:-2}

echo "verify_cross_validator_flatkv_digest: node_count=$NODE_COUNT flatkv_dir=$FLATKV_DIR"

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

ensure_seidb() {
  local node=$1
  if docker exec "$node" test -x /sei-protocol/sei-chain/build/seidb >/dev/null 2>&1; then
    return 0
  fi
  echo "Building seidb on $node..."
  docker exec -e GOPROXY="${GOPROXY:-https://proxy.golang.org,direct}" "$node" bash -lc \
    "cd /sei-protocol/sei-chain && $GO_BIN build -o build/seidb ./sei-db/tools/cmd/seidb"
}

node_height() {
  local node=$1
  docker exec "$node" build/seid status 2>/dev/null \
    | jq -r '.SyncInfo.latest_block_height // "0"' 2>/dev/null \
    || echo 0
}

# Wait until every validator reports chain height >= MIN_HEIGHT. We
# require a small absolute floor so the comparison height after
# subtracting COMPARE_BUFFER is still positive and meaningful.
wait_all_above_min_height() {
  local elapsed=0
  while [ "$elapsed" -lt "$WAIT_TIMEOUT" ]; do
    local all_ready=true
    local heights=""
    for i in $(seq 0 $((NODE_COUNT - 1))); do
      local h
      h=$(node_height "sei-node-$i")
      heights="$heights sei-node-$i=$h"
      if [ -z "$h" ] || [ "$h" -lt "$MIN_HEIGHT" ]; then
        all_ready=false
      fi
    done
    if $all_ready; then
      echo "All $NODE_COUNT validators above height $MIN_HEIGHT:$heights"
      return 0
    fi
    echo "Waiting for every validator to reach height $MIN_HEIGHT (elapsed=${elapsed}s/${WAIT_TIMEOUT}s):$heights"
    sleep 5
    elapsed=$((elapsed + 5))
  done
  echo "Timed out waiting for all $NODE_COUNT validators to reach height $MIN_HEIGHT" >&2
  for i in $(seq 0 $((NODE_COUNT - 1))); do
    dump_node_log "sei-node-$i"
  done
  return 1
}

# Return min(chain heights) - COMPARE_BUFFER, clamped at >= 1.
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
    out_dir=/tmp/flatkv-xvalid-${version}-${node}
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

for i in $(seq 0 $((NODE_COUNT - 1))); do
  ensure_seidb "sei-node-$i"
done

wait_all_above_min_height

COMPARE_VERSION=$(pick_compare_height)
if [ -z "$COMPARE_VERSION" ] || [ "$COMPARE_VERSION" -lt 1 ]; then
  echo "ERROR: failed to pick a positive comparison height" >&2
  exit 1
fi

echo "Comparing FlatKV across $NODE_COUNT validators at chain height $COMPARE_VERSION"

REFERENCE_DIGEST=""
REFERENCE_NODE=""
MISMATCH=false
for i in $(seq 0 $((NODE_COUNT - 1))); do
  node="sei-node-$i"
  digest=$(flatkv_dump_digest "$node" "$COMPARE_VERSION")
  echo "  ${node} sha256 = $digest"
  if [ -z "$REFERENCE_DIGEST" ]; then
    REFERENCE_DIGEST="$digest"
    REFERENCE_NODE="$node"
    continue
  fi
  if [ "$digest" != "$REFERENCE_DIGEST" ]; then
    echo "FAIL: ${node} diverges from ${REFERENCE_NODE} at height $COMPARE_VERSION" >&2
    MISMATCH=true
  fi
done

if $MISMATCH; then
  for i in $(seq 0 $((NODE_COUNT - 1))); do
    dump_node_log "sei-node-$i"
  done
  exit 1
fi

echo "PASS: all $NODE_COUNT validators agree on FlatKV at chain height $COMPARE_VERSION"
