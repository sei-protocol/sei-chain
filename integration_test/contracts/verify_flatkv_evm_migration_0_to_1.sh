#!/bin/bash
#
# verify_flatkv_evm_migration_0_to_1.sh
#
# Drives a coordinated operator-style cutover of the 4-validator devnet
# from sc-write-mode=memiavl_only to sc-write-mode=migrate_evm and then
# verifies that:
#
#   1) every validator's MigrationManager runs to completion
#      (migration-version key in flatkv == 1, boundary key absent), AND
#   2) all 4 validators end up with byte-identical FlatKV state at a
#      shared post-migration chain height (cross-validator digest agreement).
#
# Why a coordinated stop is required: the 0->1 MigrateEVM cutover
# rewrites how `evm/` data contributes to CommitInfo (memiavl IAVL root
# in v0; flatkv LtHash via the lattice subtree in v1). If one validator
# is flipped while the others are still in v0, the very next block's
# AppHash differs between the flipped node and the rest, and consensus
# halts. The only safe way to flip across a quorum is: stop everyone,
# rewrite app.toml everywhere, restart everyone. This script enforces
# exactly that sequence.
#
# Workflow assumption: the cluster was booted with
# GIGA_MIGRATE_FROM_MEMIAVL=true (see docker/localnode/scripts/
# step4_config_override.sh), so app.toml currently has
# sc-write-mode = "memiavl_only". This script does NOT verify that
# starting state up front: a typo here would silently produce a
# successful "migration" that did nothing, masking real bugs. The
# explicit pre-flip grep below catches the mistake.

set -euo pipefail

NODE_COUNT=${MIGRATE_NODE_COUNT:-4}
FLATKV_DIR=${FLATKV_DIR:-/root/.sei/data/state_commit/flatkv}
APP_CONFIG=${APP_CONFIG:-/root/.sei/config/app.toml}
GO_BIN=${GO_BIN:-/usr/local/go/bin/go}

# Small batch keeps the migration spread across multiple blocks, which
# exercises the resume / hybrid-read path. Override to 1024+ for a
# production-equivalent one-shot drain when sanity-checking the script.
KEYS_TO_MIGRATE_PER_BLOCK=${MIGRATE_KEYS_PER_BLOCK:-256}

STOP_TIMEOUT=${MIGRATE_STOP_TIMEOUT:-30}
# 60s default leaves headroom for the slowest realistic restart path on
# a CI runner: pebble WAL replay (~5s) + memiavl load (~5s) + tendermint
# state load + p2p handshake. The original 20s window was tight enough
# that a transient fast-crash + restart race could be silently misread
# as "process not yet up". The 3-second settle below is what actually
# distinguishes "still starting" from "started and died".
RESTART_PROBE_SECS=${MIGRATE_RESTART_PROBE_SECS:-60}
COMPLETION_TIMEOUT=${MIGRATE_COMPLETION_TIMEOUT:-180}
COMPARE_BUFFER=${MIGRATE_COMPARE_BUFFER:-2}
MIN_HEIGHT_AFTER=${MIGRATE_MIN_HEIGHT_AFTER:-5}

echo "verify_flatkv_evm_migration_0_to_1: node_count=$NODE_COUNT"

# --- shared helpers ----------------------------------------------------

dump_node_log() {
  local node=$1
  local node_id=${node#sei-node-}
  local logfile="/sei-protocol/sei-chain/build/generated/logs/seid-${node_id}.log"
  # Cobra prints a full FLAGS help block on RunE error, which can bury the
  # real "Error: ..." line. Print a targeted error excerpt first, then a
  # large head/tail window for context.
  echo "==================== ${node} seid log error excerpt ====================" >&2
  docker exec "$node" grep -nE '(^Error:|panic:|failed to|failed |version mismatch|FlatKV|migrate)' "$logfile" >&2 2>/dev/null \
    || echo "(no error excerpt found)" >&2
  echo "==================== ${node} seid log (head 220 lines) ====================" >&2
  docker exec "$node" head -220 "$logfile" >&2 2>/dev/null \
    || echo "(could not read ${logfile})" >&2
  echo "==================== ${node} seid log (tail 400 lines) ====================" >&2
  docker exec "$node" tail -400 "$logfile" >&2 2>/dev/null \
    || echo "(could not read ${logfile})" >&2
  echo "==================== ${node} pgrep seid ====================" >&2
  docker exec "$node" pgrep -af "seid" >&2 2>/dev/null \
    || echo "(no seid processes)" >&2
  echo "==================== ${node} docker logs (last 200 lines) ====================" >&2
  docker logs --tail 200 "$node" >&2 || true
}

node_height() {
  docker exec "$1" build/seid status 2>/dev/null \
    | jq -r '.SyncInfo.latest_block_height // "0"' 2>/dev/null \
    || echo 0
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

# --- step 1: pre-flip sanity ------------------------------------------
#
# Refuse to proceed unless every node is currently running in memiavl_only.
# Without this the script can succeed against a cluster that was never set
# up for a 0->1 cutover (e.g. dual_write mode), and the post-flip "all
# nodes agree" claim degenerates to "all nodes were already in v1".

for i in $(seq 0 $((NODE_COUNT - 1))); do
  node="sei-node-$i"
  # `grep || true` is mandatory: under `set -euo pipefail` a no-match grep
  # would otherwise kill this script silently with exit 1, which is exactly
  # the failure mode that masked the GIGA_MIGRATE_FROM_MEMIAVL propagation
  # regression. Force the missing-line case down the explicit ERROR branch
  # so future regressions print which node and which mode we actually saw.
  raw=$(docker exec "$node" cat "$APP_CONFIG" 2>/dev/null || true)
  current_mode=$(echo "$raw" | grep -E '^sc-write-mode' | tail -1 \
    | awk -F'"' '{print $2}' || true)
  if [ "$current_mode" != "memiavl_only" ]; then
    if [ -z "$current_mode" ]; then
      echo "ERROR: $node has no sc-write-mode line in $APP_CONFIG (cluster boot env did not reach the container)" >&2
    else
      echo "ERROR: $node is in sc-write-mode='$current_mode'; expected 'memiavl_only'" >&2
    fi
    echo "Boot the cluster with GIGA_MIGRATE_FROM_MEMIAVL=true before running this test." >&2
    echo "Check: Makefile docker-cluster-start forwards GIGA_MIGRATE_FROM_MEMIAVL to docker compose, and docker/docker-compose.yml lists it in every node service's environment block." >&2
    exit 1
  fi
done
echo "All $NODE_COUNT nodes confirmed in memiavl_only mode"

# Snapshot baseline height for diagnostic output.
PRE_FLIP_HEIGHT=$(node_height "sei-node-0")
echo "Pre-flip height on sei-node-0: $PRE_FLIP_HEIGHT"

# --- step 2: coordinated stop -----------------------------------------
#
# Stop cleanly before changing sc-write-mode. Tendermint may have blocks in
# the blockstore that must be replayed against the app on restart; if we
# SIGKILL here and then flip to migrate_evm before replay, the old memiavl_only
# block replays under new AppHash semantics and startup fails with:
# "state.AppHash does not match AppHash after replay". Crash/recovery of the
# migration engine is covered by the composite/rootmulti Go tests; this docker
# scenario models the safe operator cutover: stop cleanly, edit config, restart.

echo "Stopping all $NODE_COUNT validators..."
for i in $(seq 0 $((NODE_COUNT - 1))); do
  node="sei-node-$i"
  docker exec "$node" pkill -TERM -f "seid start" >/dev/null 2>&1 || true
done

elapsed=0
while [ "$elapsed" -lt "$STOP_TIMEOUT" ]; do
  all_dead=true
  for i in $(seq 0 $((NODE_COUNT - 1))); do
    node="sei-node-$i"
    if docker exec "$node" pgrep -f "seid start" >/dev/null 2>&1; then
      all_dead=false
      break
    fi
  done
  if $all_dead; then break; fi
  sleep 2
  elapsed=$((elapsed + 2))
done
if ! $all_dead; then
  echo "ERROR: not all validators stopped within ${STOP_TIMEOUT}s" >&2
  for i in $(seq 0 $((NODE_COUNT - 1))); do
    dump_node_log "sei-node-$i"
  done
  exit 1
fi
echo "All $NODE_COUNT validators confirmed stopped"

# --- step 3: flip sc-write-mode on every node -------------------------
#
# memiavl_only -> migrate_evm, and inject keys-to-migrate-per-block so
# the test runner controls how aggressive the per-block batch copier is.
# Both edits are idempotent: running this script twice in a row is safe
# (second flip is a no-op).

for i in $(seq 0 $((NODE_COUNT - 1))); do
  node="sei-node-$i"
  # The TOML key must match the FlagSC* constant in app/seidb.go
  # (sc-keys-to-migrate-per-block, prefix matches sibling state-commit
  # keys like sc-write-mode / sc-async-commit-buffer). The earlier
  # un-prefixed name "keys-to-migrate-per-block" matched the mapstructure
  # tag but not the FlagSC* viper key, so parseSCConfigs silently never
  # read it -- the seid log showed KeysToMigratePerBlock:1024 (default)
  # regardless of what we wrote here.
  docker exec "$node" bash -c "
    sed -i 's/^sc-write-mode = .*/sc-write-mode = \"migrate_evm\"/' '$APP_CONFIG'
    if grep -q '^sc-keys-to-migrate-per-block' '$APP_CONFIG'; then
      sed -i 's/^sc-keys-to-migrate-per-block = .*/sc-keys-to-migrate-per-block = $KEYS_TO_MIGRATE_PER_BLOCK/' '$APP_CONFIG'
    else
      sed -i '/^sc-write-mode/a sc-keys-to-migrate-per-block = $KEYS_TO_MIGRATE_PER_BLOCK' '$APP_CONFIG'
    fi
  "
done
echo "Flipped sc-write-mode to migrate_evm on all $NODE_COUNT nodes (batch=$KEYS_TO_MIGRATE_PER_BLOCK)"

# Belt-and-suspenders: confirm the rewrite actually landed on node 0.
# If it didn't (e.g. unexpected app.toml format change), the migration
# would silently run at the 1024 default rather than the requested batch
# size, and the resume / hybrid-read coverage we want from this test
# would degrade to a one-shot drain.
written_batch=$(docker exec "sei-node-0" grep -E '^sc-keys-to-migrate-per-block' "$APP_CONFIG" \
  | tail -1 | awk -F'=' '{print $2}' | tr -d ' "' || true)
if [ -z "$written_batch" ] || [ "$written_batch" != "$KEYS_TO_MIGRATE_PER_BLOCK" ]; then
  echo "ERROR: sei-node-0 app.toml has sc-keys-to-migrate-per-block='$written_batch' after rewrite; expected '$KEYS_TO_MIGRATE_PER_BLOCK'" >&2
  exit 1
fi

# --- step 4: coordinated restart --------------------------------------

for i in $(seq 0 $((NODE_COUNT - 1))); do
  node="sei-node-$i"
  docker exec -d -e "ID=${i}" "$node" /usr/bin/start_sei.sh
done

elapsed=0
all_up=false
while [ "$elapsed" -lt "$RESTART_PROBE_SECS" ]; do
  all_up=true
  down_node=""
  for i in $(seq 0 $((NODE_COUNT - 1))); do
    node="sei-node-$i"
    if ! docker exec "$node" pgrep -f "seid start" >/dev/null 2>&1; then
      all_up=false
      down_node=$node
      break
    fi
  done
  if $all_up; then break; fi
  sleep 2
  elapsed=$((elapsed + 2))
done
if ! $all_up; then
  echo "ERROR: not all validators restarted within ${RESTART_PROBE_SECS}s (last down: ${down_node})" >&2
  for i in $(seq 0 $((NODE_COUNT - 1))); do
    dump_node_log "sei-node-$i"
  done
  exit 1
fi

# Settle check: catch fast post-init crashes (e.g. a panic in
# composite.LoadVersion when flatkv is allocated for the first time on
# top of an existing memiavl tree). Without this, a process that lives
# just long enough for the probe loop above to see it but dies during
# rootmulti load shows up downstream as a confusing "migration never
# completes" timeout instead of an honest "node died at startup".
# Mirrors the established pattern in verify_flatkv_crash_recovery.sh.
sleep 5
SETTLE_FAIL=false
for i in $(seq 0 $((NODE_COUNT - 1))); do
  node="sei-node-$i"
  if ! docker exec "$node" pgrep -f "seid start" >/dev/null 2>&1; then
    echo "ERROR: $node died within 5s of restart (probable panic during composite/rootmulti LoadVersion in migrate_evm mode)" >&2
    SETTLE_FAIL=true
  fi
done
if $SETTLE_FAIL; then
  for i in $(seq 0 $((NODE_COUNT - 1))); do
    dump_node_log "sei-node-$i"
  done
  exit 1
fi
echo "All $NODE_COUNT validators restarted in migrate_evm mode and survived a 5s settle"

# --- step 5: wait for migration completion on every node --------------
#
# Poll seidb migrate-evm-status against each node's flatkv dir. The tool
# clones the snapshot+WAL into a temp dir so it can read concurrently
# with the live node. We require every node to report
# migrate_evm_complete=true within COMPLETION_TIMEOUT.

for i in $(seq 0 $((NODE_COUNT - 1))); do
  ensure_seidb "sei-node-$i"
done

elapsed=0
all_done=false
while [ "$elapsed" -lt "$COMPLETION_TIMEOUT" ]; do
  all_done=true
  status_summary=""
  for i in $(seq 0 $((NODE_COUNT - 1))); do
    node="sei-node-$i"
    json=$(docker exec "$node" build/seidb migrate-evm-status \
      --db-dir "$FLATKV_DIR" 2>/dev/null || echo '{}')
    complete=$(echo "$json" | jq -r '.migrate_evm_complete // false' 2>/dev/null || echo false)
    version_at=$(echo "$json" | jq -r '.version_at // 0' 2>/dev/null || echo 0)
    height=$(node_height "$node")
    status_summary="$status_summary ${node}=${complete}@v${version_at}/h${height}"
    if [ "$complete" != "true" ]; then
      all_done=false
    fi
  done
  if $all_done; then
    echo "All $NODE_COUNT validators completed migration:$status_summary"
    break
  fi
  echo "Waiting for migration to complete (elapsed=${elapsed}s/${COMPLETION_TIMEOUT}s):$status_summary"
  sleep 5
  elapsed=$((elapsed + 5))
done

if ! $all_done; then
  echo "ERROR: migration did not complete within ${COMPLETION_TIMEOUT}s on all $NODE_COUNT validators" >&2
  for i in $(seq 0 $((NODE_COUNT - 1))); do
    dump_node_log "sei-node-$i"
  done
  exit 1
fi

# --- step 6: cross-validator FlatKV digest agreement ------------------
#
# Identical pattern to verify_cross_validator_flatkv_digest.sh: dump
# each validator's FlatKV at a shared chain height past the migration
# completion block, sha256 the canonical EVM buckets, require equality.
# If consensus is healthy AND migration is deterministic, all 4 digests
# must match. Either flavor of failure manifests as a mismatch here.

elapsed=0
while [ "$elapsed" -lt 60 ]; do
  base=$(node_height "sei-node-0")
  if [ "$base" -ge "$MIN_HEIGHT_AFTER" ]; then
    break
  fi
  echo "Waiting for post-migration chain progress (h=$base, want >= $MIN_HEIGHT_AFTER)"
  sleep 2
  elapsed=$((elapsed + 2))
done

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

COMPARE_VERSION=$(pick_compare_height)
echo "Comparing FlatKV digests across $NODE_COUNT validators at chain height $COMPARE_VERSION"

flatkv_dump_digest() {
  local node=$1
  local version=$2
  docker exec "$node" bash -lc "
    set -euo pipefail
    out_dir=/tmp/flatkv-migrate-${version}-${node}
    rm -rf \"\$out_dir\" && mkdir -p \"\$out_dir\"
    cd /sei-protocol/sei-chain
    build/seidb dump-flatkv \
      --db-dir $FLATKV_DIR \
      --output-dir \"\$out_dir\" \
      --height $version > /dev/null
    tail -q -n +2 \"\$out_dir/account\" \"\$out_dir/code\" \"\$out_dir/storage\" \
      | sha256sum | cut -d' ' -f1
  "
}

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

echo "PASS: 0->1 EVM migration completed on all $NODE_COUNT validators and FlatKV digests agree at height $COMPARE_VERSION"
