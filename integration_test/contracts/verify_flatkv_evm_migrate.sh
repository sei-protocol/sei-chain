#!/bin/bash
#
# verify_flatkv_evm_migrate.sh
#
# Drives a coordinated operator-style migration of the 4-validator devnet
# from sc-write-mode=memiavl_only to sc-write-mode=migrate_evm and then
# verifies that:
#
#   1) every validator's MigrationManager runs to completion
#      (migration-version key in flatkv == 1, boundary key absent), AND
#   2) all 4 validators end up with byte-identical FlatKV state at a
#      shared post-migration chain height (cross-validator digest agreement).
#
# Why a coordinated stop is required: the FlatKV EVM migrate
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
#
# The cluster must also be booted with LOG_LEVEL=debug: this test recovers
# each stopped validator's committed height from the "committed state" log
# line, which is emitted at debug level. capture_stopped_heights fails fast
# if that line is absent.

set -euo pipefail

NODE_COUNT=${MIGRATE_NODE_COUNT:-4}
FLATKV_DIR=${FLATKV_DIR:-/root/.sei/data/state_commit/flatkv}
APP_CONFIG=${APP_CONFIG:-/root/.sei/config/app.toml}
GO_BIN=${GO_BIN:-/usr/local/go/bin/go}

# Small batch keeps the migration spread across multiple blocks. With the
# default fixture (~4000 EVM keys), 400 keys/block gives roughly ten batches
# and exercises the resume / hybrid-read path. Override to 1024+ for a
# production-equivalent one-shot drain when sanity-checking the script.
#
# The rate is consensus-relevant (migration writes feed the AppHash), so it
# is NOT a node-local config: every validator reads the governance-controlled
# migration.NumKeysToMigratePerBlock param from chain state each BeginBlock.
# This script raises that param from its 0 (paused) default via a single
# param-change proposal (step 4b) so all nodes drain at the identical rate.
KEYS_TO_MIGRATE_PER_BLOCK=${MIGRATE_KEYS_PER_BLOCK:-400}
MIN_KEYS_MIGRATED=${MIGRATE_MIN_KEYS_MIGRATED:-3500}
# Upper bound on how long to wait for the migration param-change proposal to
# move from submission through the voting period to PROPOSAL_STATUS_PASSED.
GOV_PASS_TIMEOUT=${MIGRATE_GOV_PASS_TIMEOUT:-120}

STOP_TIMEOUT=${MIGRATE_STOP_TIMEOUT:-30}
HALT_TIMEOUT=${MIGRATE_HALT_TIMEOUT:-120}
HALT_RESTART_ATTEMPTS=${MIGRATE_HALT_RESTART_ATTEMPTS:-4}
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
PRE_FLIP_SYNC_TIMEOUT=${MIGRATE_PREFLIP_SYNC_TIMEOUT:-120}
PRE_FLIP_SETTLE_BLOCKS=${MIGRATE_PREFLIP_SETTLE_BLOCKS:-2}
# The halt target is loaded at process start, so it must be far enough ahead
# that a validator with a slower Tendermint/app startup cannot miss the halt
# block while the other three validators form quorum. The devnet can produce
# empty blocks quickly (unsafe commit timeout is 50ms), but observed CI restart
# and consensus startup skew is still comfortably below this default window.
PRE_FLIP_HALT_BLOCKS=${MIGRATE_PREFLIP_HALT_BLOCKS:-300}
FIXTURE_HEIGHT_FILE=${MIGRATE_FIXTURE_HEIGHT_FILE:-integration_test/contracts/flatkv_evm_latest_fixture_block_height.txt}

echo "verify_flatkv_evm_migrate_migration: node_count=$NODE_COUNT"

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

node_logged_committed_height() {
  local node=$1
  local node_id=${node#sei-node-}
  local logfile="/sei-protocol/sei-chain/build/generated/logs/seid-${node_id}.log"
  local host_logfile="build/generated/logs/seid-${node_id}.log"
  local height=""

  if [ -f "$host_logfile" ]; then
    height=$(grep 'msg="committed state"' "$host_logfile" 2>/dev/null \
      | tail -1 \
      | sed -n 's/.* height=\([0-9][0-9]*\) .*/\1/p' \
      || true)
  else
    height=$(docker exec "$node" grep 'msg="committed state"' "$logfile" 2>/dev/null \
      | tail -1 \
      | sed -n 's/.* height=\([0-9][0-9]*\) .*/\1/p' \
      || true)
  fi

  if [[ "$height" =~ ^[0-9]+$ ]]; then
    echo "$height"
  else
    echo 0
  fi
}

capture_stopped_heights() {
  stopped_heights=""
  stopped_min=""
  stopped_max=""
  for i in $(seq 0 $((NODE_COUNT - 1))); do
    node="sei-node-$i"
    h=$(node_logged_committed_height "$node")
    stopped_heights="${stopped_heights} ${node}=${h}"
    if [ -z "$stopped_min" ] || [ "$h" -lt "$stopped_min" ]; then
      stopped_min=$h
    fi
    if [ -z "$stopped_max" ] || [ "$h" -gt "$stopped_max" ]; then
      stopped_max=$h
    fi
  done
  if [ "${stopped_max:-0}" -eq 0 ]; then
    echo "ERROR: no 'committed state' log line found on any node; cannot recover stopped heights." >&2
    echo "That line is emitted at debug level. Boot the cluster with LOG_LEVEL=debug, e.g. LOG_LEVEL=debug GIGA_MIGRATE_FROM_MEMIAVL=true make docker-cluster-start." >&2
    exit 1
  fi
}

node_last_sign_height() {
  local node=$1
  local height=""

  height=$(docker exec "$node" jq -r '.height // "0"' /root/.sei/data/priv_validator_state.json 2>/dev/null \
    || echo 0)

  if [[ "$height" =~ ^[0-9]+$ ]]; then
    echo "$height"
  else
    echo 0
  fi
}

capture_priv_validator_heights() {
  signed_heights=""
  signed_min=""
  signed_max=""
  for i in $(seq 0 $((NODE_COUNT - 1))); do
    node="sei-node-$i"
    h=$(node_last_sign_height "$node")
    signed_heights="${signed_heights} ${node}=${h}"
    if [ -z "$signed_min" ] || [ "$h" -lt "$signed_min" ]; then
      signed_min=$h
    fi
    if [ -z "$signed_max" ] || [ "$h" -gt "$signed_max" ]; then
      signed_max=$h
    fi
  done
}

is_seid_running() {
  docker exec "$1" pgrep -f "seid start" >/dev/null 2>&1
}

capture_process_states() {
  process_states=""
  process_live_count=0
  process_stopped_count=0
  for i in $(seq 0 $((NODE_COUNT - 1))); do
    node="sei-node-$i"
    if is_seid_running "$node"; then
      process_states="${process_states} ${node}=running"
      process_live_count=$((process_live_count + 1))
    else
      process_states="${process_states} ${node}=stopped"
      process_stopped_count=$((process_stopped_count + 1))
    fi
  done
}

all_node_heights() {
  for i in $(seq 0 $((NODE_COUNT - 1))); do
    node_height "sei-node-$i"
  done
}

wait_for_cluster_height_sync() {
  local min_height=$1
  local timeout=$2
  local elapsed=0
  local heights min max h summary

  echo "Waiting for all $NODE_COUNT validators to reach height >= $min_height before migration..." >&2
  while [ "$elapsed" -lt "$timeout" ]; do
    heights=$(all_node_heights)
    min=""
    max=""
    summary=""
    for h in $heights; do
      summary="${summary} ${h}"
      if [ -z "$min" ] || [ "$h" -lt "$min" ]; then
        min=$h
      fi
      if [ -z "$max" ] || [ "$h" -gt "$max" ]; then
        max=$h
      fi
    done

    if [ -n "$min" ] && [ "$min" -ge "$min_height" ]; then
      echo "$min"
      return 0
    fi

    echo "Waiting for pre-flip height floor (elapsed=${elapsed}s/${timeout}s):${summary}" >&2
    sleep 1
    elapsed=$((elapsed + 1))
  done

  echo "ERROR: validators did not all reach pre-flip height >= $min_height within ${timeout}s" >&2
  echo "Final pre-flip heights:${summary}" >&2
  for i in $(seq 0 $((NODE_COUNT - 1))); do
    dump_node_log "sei-node-$i"
  done
  exit 1
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

extract_status_json() {
  awk '
    /^[[:space:]]*\{/ && !in_json {
      in_json = 1
      depth = 0
      buf = ""
    }
    in_json {
      buf = buf $0 ORS
      line = $0
      depth += gsub(/\{/, "{", line)
      depth -= gsub(/\}/, "}", line)
      if (depth <= 0) {
        last = buf
        in_json = 0
      }
    }
    END {
      if (last != "") {
        printf "%s", last
      }
    }
  '
}

print_migration_summaries() {
  echo "==================== migration completion summaries ===================="
  local failed=false
  for i in $(seq 0 $((NODE_COUNT - 1))); do
    local node="sei-node-$i"
    local logfile="/sei-protocol/sei-chain/build/generated/logs/seid-${i}.log"
    local summary=""
    local keys_migrated=""

    # The completion log is emitted by the validator process after the final
    # migration commit succeeds. Retry briefly so CI output is deterministic
    # even if the status poll races log flushing by a moment.
    for _ in $(seq 1 10); do
      summary=$(docker exec "$node" grep -n 'msg="migration complete"' "$logfile" 2>/dev/null | tail -1 || true)
      if [ -n "$summary" ]; then
        break
      fi
      sleep 1
    done

    echo "-------------------- ${node} migration summary --------------------"
    if [ -z "$summary" ]; then
      echo "ERROR: ${node} did not print migration complete summary in ${logfile}" >&2
      failed=true
    else
      echo "$summary"
      keys_migrated=$(printf "%s\n" "$summary" | sed -n 's/.*keysMigrated=\([0-9][0-9]*\).*/\1/p')
      if [ "$MIN_KEYS_MIGRATED" -gt 0 ]; then
        if [ -z "$keys_migrated" ]; then
          echo "ERROR: ${node} migration summary did not include keysMigrated" >&2
          failed=true
        elif [ "$keys_migrated" -lt "$MIN_KEYS_MIGRATED" ]; then
          echo "ERROR: ${node} migrated only ${keys_migrated} keys; expected at least ${MIN_KEYS_MIGRATED}" >&2
          failed=true
        fi
      fi
    fi
  done

  if $failed; then
    for i in $(seq 0 $((NODE_COUNT - 1))); do
      dump_node_log "sei-node-$i"
    done
    exit 1
  fi
}

wait_for_all_seid_start() {
  local label=$1
  local elapsed=0
  local all_up=false
  local down_node=""

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
    if $all_up; then
      return 0
    fi
    sleep 2
    elapsed=$((elapsed + 2))
  done

  echo "ERROR: not all validators ${label} within ${RESTART_PROBE_SECS}s (last down: ${down_node})" >&2
  for i in $(seq 0 $((NODE_COUNT - 1))); do
    dump_node_log "sei-node-$i"
  done
  exit 1
}

start_all_validators() {
  local label=$1
  local barrier_dir="build/generated/flatkv_migrate_start_barrier_$(date +%s%N)"

  mkdir -p "$barrier_dir/ready"
  for i in $(seq 0 $((NODE_COUNT - 1))); do
    node="sei-node-$i"
    docker exec -d \
      -e "ID=${i}" \
      -e "FLATKV_START_BARRIER=${barrier_dir}" \
      -e "FLATKV_START_NODE_COUNT=${NODE_COUNT}" \
      "$node" bash -lc '
        set -euo pipefail
        cd /sei-protocol/sei-chain
        mkdir -p "$FLATKV_START_BARRIER/ready"
        touch "$FLATKV_START_BARRIER/ready/$ID"
        while true; do
          ready_count=$(echo "$FLATKV_START_BARRIER"/ready/* | wc -w | tr -d " ")
          if [ "$ready_count" -ge "$FLATKV_START_NODE_COUNT" ]; then
            break
          fi
          sleep 0.2
        done
        /usr/bin/start_sei.sh
      '
  done

  wait_for_all_seid_start "$label"
}

wait_for_all_seid_stop_or_timeout() {
  local label=$1
  local timeout=$2
  local elapsed=0
  local all_dead=false
  local live_node=""

  while [ "$elapsed" -lt "$timeout" ]; do
    all_dead=true
    live_node=""
    for i in $(seq 0 $((NODE_COUNT - 1))); do
      node="sei-node-$i"
      if is_seid_running "$node"; then
        all_dead=false
        live_node=$node
        break
      fi
    done
    if $all_dead; then
      return 0
    fi
    sleep 2
    elapsed=$((elapsed + 2))
  done

  last_live_node=$live_node
  echo "WARN: not all validators ${label} within ${timeout}s (last live: ${live_node})" >&2
  return 1
}

wait_for_all_seid_stop() {
  local label=$1
  local timeout=$2

  if wait_for_all_seid_stop_or_timeout "$label" "$timeout"; then
    return 0
  fi

  echo "ERROR: not all validators ${label} within ${timeout}s (last live: ${last_live_node})" >&2
  for i in $(seq 0 $((NODE_COUNT - 1))); do
    dump_node_log "sei-node-$i"
  done
  exit 1
}

validators_halted_safely() {
  if [ "$process_live_count" -ne 0 ]; then
    return 1
  fi

  if [ -z "$stopped_min" ] || [ "$stopped_min" != "$stopped_max" ]; then
    return 1
  fi

  if [ -n "$signed_max" ] && [ "$signed_max" -gt "$stopped_min" ]; then
    return 1
  fi

  return 0
}

configure_next_halt_height() {
  local next_start_height=0

  if [ -n "$stopped_max" ] && [ "$stopped_max" -gt "$next_start_height" ]; then
    next_start_height=$stopped_max
  fi
  if [ -n "$signed_max" ] && [ "$signed_max" -gt "$next_start_height" ]; then
    next_start_height=$signed_max
  fi

  HALT_HEIGHT=$((next_start_height + PRE_FLIP_HALT_BLOCKS))
  configure_halt_height "$HALT_HEIGHT"
  echo "Configured retry halt-height=$HALT_HEIGHT after unsafe halt state"
}

wait_for_safe_halt_height() {
  local attempt=1

  while [ "$attempt" -le "$HALT_RESTART_ATTEMPTS" ]; do
    echo "Waiting for validators to halt at a safe block boundary (attempt ${attempt}/${HALT_RESTART_ATTEMPTS}, halt-height=$HALT_HEIGHT)"
    wait_for_all_seid_stop_or_timeout "halted at height $HALT_HEIGHT" "$HALT_TIMEOUT" || true

    capture_stopped_heights
    capture_priv_validator_heights
    capture_process_states
    echo "Halt attempt ${attempt} process states:${process_states}"
    echo "Halt attempt ${attempt} committed heights:${stopped_heights}"
    echo "Halt attempt ${attempt} last-sign heights:${signed_heights}"

    if validators_halted_safely; then
      echo "All $NODE_COUNT validators halted safely at committed height $stopped_min"
      return 0
    fi

    if [ "$attempt" -ge "$HALT_RESTART_ATTEMPTS" ]; then
      break
    fi

    if [ "$process_live_count" -eq 0 ]; then
      echo "Validators are stopped but not at a safe common boundary; scheduling a fresh same-mode halt"
    else
      echo "Some validators halted before their peers caught up; stopping remaining live validators before a fresh same-mode halt"
      stop_all_validators "stopped after partial halt attempt ${attempt}"
      capture_stopped_heights
      capture_priv_validator_heights
      capture_process_states
      echo "Post-stop retry process states:${process_states}"
      echo "Post-stop retry committed heights:${stopped_heights}"
      echo "Post-stop retry last-sign heights:${signed_heights}"
    fi

    configure_next_halt_height
    start_all_validators "restarted in memiavl_only for halt retry"

    attempt=$((attempt + 1))
  done

  echo "ERROR: validators did not halt at a safe common boundary after ${HALT_RESTART_ATTEMPTS} attempts; refusing to flip sc-write-mode" >&2
  echo "Final process states:${process_states}" >&2
  echo "Final committed heights:${stopped_heights}" >&2
  echo "Final last-sign heights:${signed_heights}" >&2
  for i in $(seq 0 $((NODE_COUNT - 1))); do
    dump_node_log "sei-node-$i"
  done
  exit 1
}

stop_all_validators() {
  local label=$1
  for i in $(seq 0 $((NODE_COUNT - 1))); do
    node="sei-node-$i"
    docker exec "$node" pkill -TERM -f "seid start" >/dev/null 2>&1 || true &
  done
  wait
  wait_for_all_seid_stop "$label" "$STOP_TIMEOUT"
}

configure_halt_height() {
  local height=$1
  local written_halt=""

  for i in $(seq 0 $((NODE_COUNT - 1))); do
    node="sei-node-$i"
    docker exec "$node" bash -c "sed -i 's/^halt-height = .*/halt-height = $height/' '$APP_CONFIG'"
  done

  written_halt=$(docker exec "sei-node-0" grep -E '^halt-height' "$APP_CONFIG" \
    | tail -1 | awk -F'=' '{print $2}' | tr -d ' "' || true)
  if [ -z "$written_halt" ] || [ "$written_halt" != "$height" ]; then
    echo "ERROR: sei-node-0 app.toml has halt-height='$written_halt' after rewrite; expected '$height'" >&2
    exit 1
  fi
}

# --- step 1: pre-flip sanity ------------------------------------------
#
# Refuse to proceed unless every node is currently running in memiavl_only.
# Without this the script can succeed against a cluster that was never set
# up for a FlatKV EVM migrate (e.g. dual_write mode), and the post-flip "all
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

# Snapshot baseline height for diagnostic output. The coordinated stop must
# happen after all validators have caught up to the fixture-writing blocks.
# Otherwise a validator that already committed height N in memiavl_only can
# disagree with a validator that replays/commits height N after the flip to
# migrate_evm.
fixture_height=0
if [ -f "$FIXTURE_HEIGHT_FILE" ]; then
  fixture_height=$(tail -1 "$FIXTURE_HEIGHT_FILE" | tr -d '[:space:]' || echo 0)
fi
if ! [[ "$fixture_height" =~ ^[0-9]+$ ]]; then
  echo "ERROR: fixture height file $FIXTURE_HEIGHT_FILE contains non-numeric value '$fixture_height'" >&2
  exit 1
fi
PRE_FLIP_MIN_HEIGHT=$((fixture_height + PRE_FLIP_SETTLE_BLOCKS))
PRE_FLIP_HEIGHT=$(wait_for_cluster_height_sync "$PRE_FLIP_MIN_HEIGHT" "$PRE_FLIP_SYNC_TIMEOUT" | tail -1)
echo "Pre-flip height floor reached across all $NODE_COUNT validators: $PRE_FLIP_HEIGHT (fixture_height=$fixture_height settle_blocks=$PRE_FLIP_SETTLE_BLOCKS)"

# --- step 2: coordinated stop -----------------------------------------
#
# Stop cleanly before changing sc-write-mode. Tendermint may have blocks in
# the blockstore that must be replayed against the app on restart; if we
# SIGKILL here and then flip to migrate_evm before replay, the old memiavl_only
# block replays under new AppHash semantics and startup fails with:
# "state.AppHash does not match AppHash after replay". Crash/recovery of the
# migration engine is covered by the composite/rootmulti Go tests; this docker
# scenario models the safe operator migration: stop cleanly, edit config, restart.
#
# A common committed height is not enough by itself. A validator can sign a vote
# for H+1 before H+1 commits; flipping storage modes at committed height H then
# makes consensus WAL replay ask the signer for a different H+1 vote and trips
# Tendermint's double-sign guard ("error signing vote: conflicting data"). To
# avoid sampling that unstable window, first do a same-mode restart with a
# temporary future halt-height. The app halts from Commit after writing that
# block, so every validator stops at a durable height boundary before the flip.

echo "Stopping all $NODE_COUNT validators in memiavl_only mode to configure a deterministic halt height..."
stop_all_validators "stopped before halt-height configuration"
echo "All $NODE_COUNT validators confirmed stopped before halt-height configuration"

capture_stopped_heights
capture_priv_validator_heights
echo "Pre-halt stopped validator committed heights:${stopped_heights}"
echo "Pre-halt validator last-sign heights:${signed_heights}"

halt_start_height=$stopped_max
if [ -n "$signed_max" ] && [ "$signed_max" -gt "$halt_start_height" ]; then
  halt_start_height=$signed_max
fi
if [ -n "$PRE_FLIP_HEIGHT" ] && [ "$PRE_FLIP_HEIGHT" -gt "$halt_start_height" ]; then
  halt_start_height=$PRE_FLIP_HEIGHT
fi
HALT_HEIGHT=$((halt_start_height + PRE_FLIP_HALT_BLOCKS))

configure_halt_height "$HALT_HEIGHT"
echo "Configured halt-height=$HALT_HEIGHT on all $NODE_COUNT validators; restarting in memiavl_only to halt at a block boundary"

start_all_validators "restarted in memiavl_only with halt-height=$HALT_HEIGHT"
wait_for_safe_halt_height

# --- step 3: flip sc-write-mode on every node -------------------------
#
# memiavl_only -> migrate_evm. The per-block migration rate is no longer a
# node-local config: it is driven entirely by the consensus-relevant
# migration.NumKeysToMigratePerBlock gov param, raised via a proposal in
# step 4b below. Until that param is positive the migration stays paused, so
# flipping the write mode alone is a safe no-op copy boundary. The edit is
# idempotent: running this script twice in a row is safe (second flip is a
# no-op).

for i in $(seq 0 $((NODE_COUNT - 1))); do
  node="sei-node-$i"
  docker exec "$node" bash -c "
    sed -i 's/^halt-height = .*/halt-height = 0/' '$APP_CONFIG'
    sed -i 's/^sc-write-mode = .*/sc-write-mode = \"migrate_evm\"/' '$APP_CONFIG'
  "
done
echo "Flipped sc-write-mode to migrate_evm on all $NODE_COUNT nodes (migration rate driven by gov param)"

# Belt-and-suspenders: confirm the write-mode rewrite actually landed on
# node 0. If it didn't (e.g. unexpected app.toml format change), the nodes
# would restart in memiavl_only and the "migration" would silently do
# nothing, masking real bugs.
written_mode=$(docker exec "sei-node-0" grep -E '^sc-write-mode' "$APP_CONFIG" \
  | tail -1 | awk -F'=' '{print $2}' | tr -d ' "' || true)
if [ "$written_mode" != "migrate_evm" ]; then
  echo "ERROR: sei-node-0 app.toml has sc-write-mode='$written_mode' after rewrite; expected 'migrate_evm'" >&2
  exit 1
fi

# --- step 4: coordinated restart --------------------------------------

start_all_validators "restarted in migrate_evm"

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

# --- step 4b: drive the migration via a governance param change -------
#
# The per-block migration rate is consensus-relevant (migration writes feed
# the AppHash), so a per-node config would diverge the chain. Instead every
# validator reads migration.NumKeysToMigratePerBlock from chain state in
# BeginBlock and applies the same value. We raise it from its 0 (paused)
# default to $KEYS_TO_MIGRATE_PER_BLOCK with a single param-change proposal
# voted through by a quorum; once it passes, all nodes begin draining at the
# identical rate on the same height. Uses the same gov helpers as
# integration_test/gov_module/gov_proposal_test.yaml.

drive_migration_via_gov() {
  local rate="$1"
  local title="FlatKV migrate: set NumKeysToMigratePerBlock=${rate}"

  # Generate the param-change proposal JSON inside node 0.
  docker exec "sei-node-0" bash -lc "cat > /tmp/migration_param_change_proposal.json <<EOF
{
  \"title\": \"${title}\",
  \"description\": \"Set migration.NumKeysToMigratePerBlock to ${rate} to drive the SC memiavl->flatkv migration\",
  \"changes\": [
    {
      \"subspace\": \"migration\",
      \"key\": \"NumKeysToMigratePerBlock\",
      \"value\": \"${rate}\"
    }
  ],
  \"deposit\": \"10000000usei\",
  \"is_expedited\": false
}
EOF"

  # Submit from node 0's admin key; capture the new proposal id.
  local proposal_id
  proposal_id=$(docker exec "sei-node-0" bash -lc "
    cd /sei-protocol/sei-chain
    seidbin=build/seid chainid=sei
    source integration_test/utils/_tx_helpers.sh
    submit_gov_proposal admin '${title}' gov submit-proposal param-change /tmp/migration_param_change_proposal.json --fees 2000usei
  ")
  if [ -z "$proposal_id" ]; then
    echo "ERROR: failed to submit migration param-change proposal" >&2
    for i in $(seq 0 $((NODE_COUNT - 1))); do dump_node_log "sei-node-$i"; done
    exit 1
  fi
  echo "Submitted migration param-change proposal id=$proposal_id (rate=$rate)"

  # Top up the deposit to clear the min-deposit threshold (mirrors the yaml).
  docker exec "sei-node-0" bash -lc "
    cd /sei-protocol/sei-chain
    seidbin=build/seid chainid=sei
    source integration_test/utils/_tx_helpers.sh
    submit_tx_and_wait admin gov deposit $proposal_id 10000000usei --fees 2000usei
  " >/dev/null

  # Vote yes from a quorum (2/4 with the devnet's 0.5 quorum).
  for i in 0 1; do
    docker exec "sei-node-$i" bash -lc "
      cd /sei-protocol/sei-chain
      seidbin=build/seid chainid=sei
      source integration_test/utils/_tx_helpers.sh
      submit_tx_and_wait node_admin gov vote $proposal_id yes --fees 2000usei
    " >/dev/null
  done
  echo "Voted yes on proposal $proposal_id from a quorum of validators; waiting for it to pass..."

  local status
	status=$(docker exec "sei-node-0" bash -lc "
	  cd /sei-protocol/sei-chain
	  seidbin=build/seid chainid=sei
	  source integration_test/utils/_tx_helpers.sh
	  wait_for_proposal_status '$proposal_id' PROPOSAL_STATUS_PASSED admin '$GOV_PASS_TIMEOUT'
	" 2>/dev/null || true)
  if [ "$status" != "PROPOSAL_STATUS_PASSED" ]; then
    echo "ERROR: migration param-change proposal $proposal_id did not pass within ${GOV_PASS_TIMEOUT}s (last status=$status)" >&2
    exit 1
  fi

  # Confirm the param actually took the requested value on chain.
  local on_chain
  on_chain=$(docker exec "sei-node-0" build/seid q params subspace migration NumKeysToMigratePerBlock --output json 2>/dev/null \
    | jq -r '.value' 2>/dev/null | tr -d '"' || true)
  if [ "$on_chain" != "$rate" ]; then
    echo "ERROR: migration.NumKeysToMigratePerBlock='$on_chain' after proposal $proposal_id passed; expected '$rate'" >&2
    exit 1
  fi
  echo "Governance raised migration.NumKeysToMigratePerBlock to $rate; migration now draining on all validators"
}

drive_migration_via_gov "$KEYS_TO_MIGRATE_PER_BLOCK"

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
    status_err="/tmp/${node}-migrate-evm-status.err"
    raw_status=$(docker exec "$node" bash -lc \
      "build/seidb migrate-evm-status --db-dir '$FLATKV_DIR' 2>$status_err" || true)
    # seidb opens FlatKV and its dependencies may write diagnostics to stdout
    # before or after the JSON payload. Keep diagnostics visible but reduce the
    # parser input to the status object so jq never returns multi-line fields.
    status_json=$(printf '%s\n' "$raw_status" | extract_status_json)
    if [ -z "$status_json" ]; then
      status_json='{}'
    fi
    complete=$(echo "$status_json" | jq -r '.migrate_evm_complete // false' 2>/dev/null || echo false)
    version_at=$(echo "$status_json" | jq -r '.version_at // 0' 2>/dev/null || echo 0)
    height=$(node_height "$node")
    status_summary="$status_summary ${node}=${complete}@v${version_at}/h${height}"
    if [ "$complete" != "true" ]; then
      all_done=false
    fi
    if [ "$i" -eq 0 ] && [ $((elapsed % 30)) -eq 0 ]; then
      echo "migrate-evm-status raw ${node}: ${raw_status}"
      echo "migrate-evm-status json ${node}: ${status_json}"
      docker exec "$node" bash -lc "if [ -s '$status_err' ]; then echo 'migrate-evm-status stderr ${node}:'; cat '$status_err'; fi" || true
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
  echo "Final migrate-evm-status diagnostics:"
  for i in $(seq 0 $((NODE_COUNT - 1))); do
    node="sei-node-$i"
    status_err="/tmp/${node}-migrate-evm-status-final.err"
    raw_status=$(docker exec "$node" bash -lc \
      "build/seidb migrate-evm-status --db-dir '$FLATKV_DIR' 2>$status_err" || true)
    status_json=$(printf '%s\n' "$raw_status" | extract_status_json)
    if [ -z "$status_json" ]; then
      status_json='{}'
    fi
    echo "final migrate-evm-status raw ${node}: ${raw_status}"
    echo "final migrate-evm-status json ${node}: ${status_json}"
    docker exec "$node" bash -lc "if [ -s '$status_err' ]; then echo 'final migrate-evm-status stderr ${node}:'; cat '$status_err'; fi" || true
  done
  for i in $(seq 0 $((NODE_COUNT - 1))); do
    dump_node_log "sei-node-$i"
  done
  exit 1
fi

print_migration_summaries

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

echo "PASS: FlatKV EVM migrate completed on all $NODE_COUNT validators and FlatKV digests agree at height $COMPARE_VERSION"
