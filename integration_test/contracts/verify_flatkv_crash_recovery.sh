#!/bin/bash
#
# verify_flatkv_crash_recovery.sh
#
# Crash-recovery smoke test for the FlatKV write path: SIGKILL one validator
# at an arbitrary moment during normal block production, restart it, wait
# until it has caught up to the surviving quorum, then dump FlatKV on all
# four validators and require byte-identical content at a shared past
# snapshot version.
#
# Rationale: existing docker-level coverage only exercises a graceful
# stop -> start cycle (upgrade tests) and an in-process catch-up loop
# (sei-db/state_db/sc/flatkv/store_catchup_test.go). Neither reproduces the
# real-disk fsync timing of an OS-level kill mid block commit. Running this
# in CI lets the kill land at a uniformly random point in the FlatKV commit
# pipeline across runs; over the life of the workflow the kill will land
# during snapshot rotation, during pending-write flush, between key writes,
# etc. A FlatKV recovery regression that corrupts state on restart will
# eventually surface as a digest mismatch against the surviving validators.
#
# Voting-weight assumption: sei-node-3 is killed because the 4-validator
# devnet is configured so that any single validator falling below the 1/3
# threshold should leave the other three above 2/3. In practice, CI can kill
# the node after it has participated in the current height, and Tendermint may
# not commit the next block until the validator is restarted. That temporary
# liveness stall is orthogonal to the FlatKV crash-recovery invariant below.
# If the cluster topology changes, override CRASH_NODE_INDEX.

set -euo pipefail

NODE_COUNT=${FLATKV_CRASH_NODE_COUNT:-4}
CRASH_NODE_INDEX=${FLATKV_CRASH_NODE_INDEX:-3}
CRASH_NODE="sei-node-${CRASH_NODE_INDEX}"
SURVIVOR_NODE=${FLATKV_CRASH_SURVIVOR:-sei-node-0}
FLATKV_DIR=${FLATKV_DIR:-/root/.sei/data/state_commit/flatkv}
GO_BIN=${GO_BIN:-/usr/local/go/bin/go}
# Keep the crashed validator down long enough for a slow CI runner to rotate
# through several Tendermint proposer rounds. The survivor-progress check exits
# as soon as any block is produced, so this is only the failure budget.
KILL_DOWN_SECS=${FLATKV_CRASH_DOWN_SECS:-45}
CATCHUP_TIMEOUT=${FLATKV_CRASH_CATCHUP_TIMEOUT:-240}
SURVIVOR_PROGRESS_TIMEOUT=${FLATKV_CRASH_SURVIVOR_TIMEOUT:-120}

echo "verify_flatkv_crash_recovery: crash_node=$CRASH_NODE survivor=$SURVIVOR_NODE"

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
  echo "$node did not reach height $target within ${timeout}s" >&2
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

# Return min(chain heights across all NODE_COUNT validators) minus
# COMPARE_BUFFER, clamped at >= 1. We deliberately compare at a real
# chain height rather than a flatkv snapshot version because the default
# SnapshotInterval (10000) means a CI devnet never produces a
# non-genesis snapshot; intersecting snapshot dirs would degenerate to
# {0} and dump-flatkv --height 0 silently falls back to "current"
# (flatkv_open.go:252) which is each node's wall-clock latest -- masking
# real divergence. dump-flatkv --height H does WAL-replay from
# snapshot-0 to H instead, which all 4 validators can serve identically
# whenever H is committed everywhere.
COMPARE_BUFFER=${FLATKV_CRASH_COMPARE_BUFFER:-2}
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
    out_dir=/tmp/flatkv-crash-${version}-${node}
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

# Step 1: confirm baseline chain progress, then capture the pre-kill height.
PRE_KILL_HEIGHT=$(node_height "$SURVIVOR_NODE")
if [ "$PRE_KILL_HEIGHT" -lt 10 ]; then
  echo "Waiting for chain to advance past height 10 before injecting crash..."
  wait_for_height "$SURVIVOR_NODE" 10 120
  PRE_KILL_HEIGHT=$(node_height "$SURVIVOR_NODE")
fi
echo "Pre-kill survivor height: $PRE_KILL_HEIGHT"

# Step 2: SIGKILL the target validator. -9 skips the SIGTERM handler so the
# seid process can't gracefully flush flatkv pending writes; this is the
# essential difference vs the upgrade-test stop/start cycle.
echo "Killing $CRASH_NODE with SIGKILL..."
docker exec "$CRASH_NODE" pkill -9 -f "seid start" >/dev/null 2>&1 || true

# Step 3: confirm the kill landed and give the surviving validators a chance
# to advance while the victim is down. Lack of progress here is not a FlatKV
# recovery failure: the SIGKILL can land after the victim participates in the
# current Tendermint height, and the devnet may not commit again until that
# validator comes back.
sleep 2
if docker exec "$CRASH_NODE" pgrep -f "seid start" >/dev/null 2>&1; then
  echo "ERROR: $CRASH_NODE did not actually die after SIGKILL" >&2
  dump_node_log "$CRASH_NODE"
  exit 1
fi
echo "$CRASH_NODE confirmed dead; polling survivor progress for up to ${KILL_DOWN_SECS}s..."
# Poll instead of single-sample. The killed node may have been the
# current tendermint proposer when SIGKILL hit; on a slow CI runner the
# surviving quorum can spend several rounds prevoting nil before the next
# proposal lands. The poll exits as soon as the survivor has produced any
# block past PRE_KILL_HEIGHT and only fails if no block was produced during
# the entire window -- the same signal as before, with enough budget for
# proposer timeout/backoff.
SURVIVOR_DURING_KILL=$PRE_KILL_HEIGHT
elapsed=0
while [ "$elapsed" -lt "$KILL_DOWN_SECS" ]; do
  SURVIVOR_DURING_KILL=$(node_height "$SURVIVOR_NODE")
  if [ "$SURVIVOR_DURING_KILL" -gt "$PRE_KILL_HEIGHT" ]; then
    break
  fi
  sleep 2
  elapsed=$((elapsed + 2))
done

if [ "$SURVIVOR_DURING_KILL" -le "$PRE_KILL_HEIGHT" ]; then
  echo "WARN: surviving validator $SURVIVOR_NODE did not produce a block while $CRASH_NODE was down for ${KILL_DOWN_SECS}s" >&2
  echo "  pre_kill=$PRE_KILL_HEIGHT during_kill=$SURVIVOR_DURING_KILL" >&2
  echo "  continuing with restart; post-restart progress is checked before digest comparison" >&2
else
  echo "Survivor $SURVIVOR_NODE advanced $PRE_KILL_HEIGHT -> $SURVIVOR_DURING_KILL while $CRASH_NODE was down (within ${elapsed}s)"
fi

# Step 4: restart the killed validator. Use a detached docker exec so the
# exec session stays open after start_sei.sh backgrounds seid and returns;
# a non-detached docker exec closes stdout/stderr when start_sei.sh exits,
# which would kill the freshly-spawned seid process.
#
# step5_start_sei.sh truncates seid-${ID}.log on restart (`>` not `>>`),
# so by the time we dump_node_log below we are seeing the restart-attempt
# output, not the pre-SIGKILL run -- exactly what we want for diagnosing
# crash-recovery startup failures.
echo "Restarting $CRASH_NODE..."
docker exec -d -e "ID=${CRASH_NODE_INDEX}" "$CRASH_NODE" /usr/bin/start_sei.sh

# Probe for "seid is running" over a window rather than a single sleep so
# a slow startup (pebble WAL recovery after SIGKILL, tendermint state
# repair, etc.) is not misclassified as "stayed down". If seid is not
# present at any probe point AND the probe window has elapsed, the
# process either never started or started-then-crashed -- either way the
# dumped seid log will show why.
RESTART_PROBE_SECS=${FLATKV_CRASH_RESTART_PROBE_SECS:-15}
seid_alive=false
probe_elapsed=0
while [ "$probe_elapsed" -lt "$RESTART_PROBE_SECS" ]; do
  if docker exec "$CRASH_NODE" pgrep -f "seid start" >/dev/null 2>&1; then
    seid_alive=true
    break
  fi
  sleep 1
  probe_elapsed=$((probe_elapsed + 1))
done

# Second sample after a short grace period: catch fast crashes where the
# process appeared briefly during WAL recovery and then died (e.g. panic
# in flatkv LoadVersion). Without this, a "process briefly present"
# moment would let us proceed and then deadlock at the catch-up wait
# below.
if $seid_alive; then
  sleep 3
  if ! docker exec "$CRASH_NODE" pgrep -f "seid start" >/dev/null 2>&1; then
    seid_alive=false
  fi
fi

if ! $seid_alive; then
  echo "ERROR: $CRASH_NODE did not stay running after restart (probed for ${RESTART_PROBE_SECS}s + 3s settle)" >&2
  dump_node_log "$CRASH_NODE"
  dump_node_log "$SURVIVOR_NODE"
  exit 1
fi
echo "$CRASH_NODE seid process is running after restart"

# Step 5: wait until the restarted node has caught back up to within
# CATCHUP_TOLERANCE blocks of the surviving leader.
CATCHUP_TOLERANCE=2
elapsed=0
while [ "$elapsed" -lt "$CATCHUP_TIMEOUT" ]; do
  survivor_h=$(node_height "$SURVIVOR_NODE")
  crash_h=$(node_height "$CRASH_NODE")
  gap=$((survivor_h - crash_h))
  if [ "$gap" -le "$CATCHUP_TOLERANCE" ] && [ "$crash_h" -gt 0 ]; then
    echo "$CRASH_NODE caught up: survivor=$survivor_h restarted=$crash_h gap=$gap"
    break
  fi
  echo "Waiting for catch-up: survivor=$survivor_h restarted=$crash_h gap=$gap (elapsed=${elapsed}s/${CATCHUP_TIMEOUT}s)"
  sleep 5
  elapsed=$((elapsed + 5))
done

if [ "$elapsed" -ge "$CATCHUP_TIMEOUT" ]; then
  echo "ERROR: $CRASH_NODE failed to catch up within ${CATCHUP_TIMEOUT}s" >&2
  dump_node_log "$CRASH_NODE"
  dump_node_log "$SURVIVOR_NODE"
  exit 1
fi

# Ensure the post-restart chain commits far enough past the crash height that
# pick_compare_height (which subtracts COMPARE_BUFFER) selects a version that
# was produced after the SIGKILL/restart cycle.
POST_RESTART_COMPARE_FLOOR=$((PRE_KILL_HEIGHT + COMPARE_BUFFER + 1))
echo "Waiting for all validators to reach post-restart comparison floor $POST_RESTART_COMPARE_FLOOR"
for i in $(seq 0 $((NODE_COUNT - 1))); do
  wait_for_height "sei-node-$i" "$POST_RESTART_COMPARE_FLOOR" "$CATCHUP_TIMEOUT"
done

# Step 6: build seidb everywhere (the crashed node may have had its previous
# build wiped; the others may have never built it), pick a chain height
# every validator has committed, and digest-compare flatkv at that
# height. dump-flatkv --height H WAL-replays from snapshot-0 to H, so
# this works even when no non-genesis flatkv snapshot has been created
# yet (CI chain length << SnapshotInterval).
for i in $(seq 0 $((NODE_COUNT - 1))); do
  ensure_seidb "sei-node-$i"
done

COMPARE_VERSION=$(pick_compare_height)
if [ -z "$COMPARE_VERSION" ] || [ "$COMPARE_VERSION" -lt 1 ]; then
  echo "ERROR: failed to pick a positive comparison height after crash recovery" >&2
  for i in $(seq 0 $((NODE_COUNT - 1))); do
    echo "  sei-node-$i height = $(node_height "sei-node-$i")" >&2
  done
  exit 1
fi
echo "Comparing FlatKV across $NODE_COUNT validators at chain height $COMPARE_VERSION (post crash recovery)"

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

echo "PASS: all $NODE_COUNT validators agree on FlatKV at chain height $COMPARE_VERSION after $CRASH_NODE SIGKILL + restart"
