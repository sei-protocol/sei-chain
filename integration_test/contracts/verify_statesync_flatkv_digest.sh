#!/bin/bash
#
# verify_statesync_flatkv_digest.sh
#
# Strong-correctness assertion for the state-sync path: dump the receiver's
# (sei-rpc-node) FlatKV and the donor's (sei-node-0) FlatKV at the same
# chain height and require the two digests to be byte-identical.
#
# Why chain height (not flatkv snapshot version): see the same-named
# comment block at the top of verify_cross_validator_flatkv_digest.sh.
# Short version: the default flatkv SnapshotInterval is 10000 blocks, so
# in CI neither donor nor receiver has any non-genesis snapshot;
# intersecting snapshot dirs degenerates to {0} which dump-flatkv then
# silently translates to "current" -- masking real divergence. Picking a
# real committed chain height H and letting dump-flatkv WAL-replay to H
# always works regardless of where snapshot boundaries fell.
#
# Rationale for this check: the existing statesync_operation.yaml only
# asserts the rpc node reports a non-zero height, which trivially passes
# even if the FlatKV Importer / FinalizeImport / WriteSnapshot chain
# silently drops keys. By diffing the dumped key/value rows at a height
# both nodes have committed, this script catches the entire class of
# "state-sync produced a structurally valid but content-wrong FlatKV"
# regressions that the height check misses. A silent state-sync bug that
# produces wrong content at H_sync also produces wrong content at every
# height > H_sync (replay is a pure function of state at H_sync), so
# comparing at any shared post-sync height is sufficient. This script is
# intended for GIGA_STORAGE=true jobs; all FlatKV buckets, including legacy,
# are included in the digest.

set -euo pipefail

DONOR=${FLATKV_DIGEST_DONOR:-sei-node-0}
RECEIVER=${FLATKV_DIGEST_RECEIVER:-sei-rpc-node}
FLATKV_DIR=${FLATKV_DIR:-/root/.sei/data/state_commit/flatkv}
GO_BIN=${GO_BIN:-/usr/local/go/bin/go}
WAIT_TIMEOUT=${FLATKV_DIGEST_WAIT_TIMEOUT:-240}
MIN_HEIGHT=${FLATKV_DIGEST_MIN_HEIGHT:-10}
COMPARE_BUFFER=${FLATKV_DIGEST_COMPARE_BUFFER:-2}

echo "verify_statesync_flatkv_digest: donor=$DONOR receiver=$RECEIVER flatkv_dir=$FLATKV_DIR"

dump_node_log() {
  local node=$1
  local logfile node_id
  node_id=${node#sei-node-}
  if [ "$node_id" = "$node" ]; then
    # sei-rpc-node (or any non sei-node-N container) writes to rpc-node.log
    # via docker/rpcnode/scripts/step2_start_sei.sh.
    logfile="/sei-protocol/sei-chain/build/generated/logs/rpc-node.log"
  else
    # Validator nodes write to seid-<ID>.log via
    # docker/localnode/scripts/step5_start_sei.sh.
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

# Wait until both donor and receiver report chain height >= MIN_HEIGHT.
wait_both_above_min_height() {
  local elapsed=0
  while [ "$elapsed" -lt "$WAIT_TIMEOUT" ]; do
    local d_h r_h
    d_h=$(node_height "$DONOR")
    r_h=$(node_height "$RECEIVER")
    if [ -n "$d_h" ] && [ -n "$r_h" ] && [ "$d_h" -ge "$MIN_HEIGHT" ] && [ "$r_h" -ge "$MIN_HEIGHT" ]; then
      echo "Both above height $MIN_HEIGHT (donor=$d_h receiver=$r_h)"
      return 0
    fi
    echo "Waiting for donor & receiver to reach height $MIN_HEIGHT (donor=$d_h receiver=$r_h elapsed=${elapsed}s/${WAIT_TIMEOUT}s)"
    sleep 5
    elapsed=$((elapsed + 5))
  done
  echo "Timed out waiting for donor & receiver to reach height $MIN_HEIGHT" >&2
  dump_node_log "$DONOR"
  dump_node_log "$RECEIVER"
  return 1
}

# Return min(donor_height, receiver_height) - COMPARE_BUFFER, clamped at >= 1.
pick_compare_height() {
  local d_h r_h min
  d_h=$(node_height "$DONOR")
  r_h=$(node_height "$RECEIVER")
  min=$d_h
  if [ "$r_h" -lt "$min" ]; then
    min=$r_h
  fi
  if [ "$min" -le "$COMPARE_BUFFER" ]; then
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
    out_dir=/tmp/flatkv-statesync-${version}-${node}
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

ensure_seidb "$RECEIVER"
ensure_seidb "$DONOR"

wait_both_above_min_height

COMPARE_VERSION=$(pick_compare_height)
if [ -z "$COMPARE_VERSION" ] || [ "$COMPARE_VERSION" -lt 1 ]; then
  echo "ERROR: failed to pick a positive comparison height" >&2
  exit 1
fi

echo "Comparing FlatKV donor vs receiver at chain height $COMPARE_VERSION"

DONOR_DIGEST=$(flatkv_dump_digest "$DONOR" "$COMPARE_VERSION")
RECEIVER_DIGEST=$(flatkv_dump_digest "$RECEIVER" "$COMPARE_VERSION")

echo "  donor    sha256 = $DONOR_DIGEST"
echo "  receiver sha256 = $RECEIVER_DIGEST"

if [ "$DONOR_DIGEST" != "$RECEIVER_DIGEST" ]; then
  echo "FAIL: FlatKV state-sync digest mismatch at chain height $COMPARE_VERSION" >&2
  dump_node_log "$DONOR"
  dump_node_log "$RECEIVER"
  exit 1
fi

echo "PASS: FlatKV state-sync digests match at chain height $COMPARE_VERSION"
