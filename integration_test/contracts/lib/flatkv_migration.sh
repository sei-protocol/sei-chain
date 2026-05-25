#!/bin/bash
#
# Shared helpers for docker-level FlatKV migration tests.

if [ -n "${FLATKV_MIGRATION_LIB_SOURCED:-}" ]; then
  return 0
fi
FLATKV_MIGRATION_LIB_SOURCED=1

NODE_COUNT=${MIGRATE_NODE_COUNT:-4}
FLATKV_DIR=${FLATKV_DIR:-/root/.sei/data/state_commit/flatkv}
APP_CONFIG=${APP_CONFIG:-/root/.sei/config/app.toml}
GO_BIN=${GO_BIN:-/usr/local/go/bin/go}

KEYS_TO_MIGRATE_PER_BLOCK=${MIGRATE_KEYS_PER_BLOCK:-400}
MIN_KEYS_MIGRATED=${MIGRATE_MIN_KEYS_MIGRATED:-0}
STOP_TIMEOUT=${MIGRATE_STOP_TIMEOUT:-30}
RESTART_PROBE_SECS=${MIGRATE_RESTART_PROBE_SECS:-60}
COMPLETION_TIMEOUT=${MIGRATE_COMPLETION_TIMEOUT:-180}
COMPARE_BUFFER=${MIGRATE_COMPARE_BUFFER:-2}
MIN_HEIGHT_AFTER=${MIGRATE_MIN_HEIGHT_AFTER:-5}
PRE_FLIP_SYNC_TIMEOUT=${MIGRATE_PREFLIP_SYNC_TIMEOUT:-120}
PRE_FLIP_SETTLE_BLOCKS=${MIGRATE_PREFLIP_SETTLE_BLOCKS:-2}
PRE_FLIP_STOP_ATTEMPTS=${MIGRATE_PREFLIP_STOP_ATTEMPTS:-5}
FIXTURE_HEIGHT_FILE=${MIGRATE_FIXTURE_HEIGHT_FILE:-integration_test/contracts/flatkv_evm_latest_fixture_block_height.txt}

dump_node_log() {
  local node=$1
  local node_id=${node#sei-node-}
  local logfile="/sei-protocol/sei-chain/build/generated/logs/seid-${node_id}.log"

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

dump_all_node_logs() {
  for i in $(seq 0 $((NODE_COUNT - 1))); do
    dump_node_log "sei-node-$i"
  done
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
    # node_logged_committed_height only sees in-session log lines. After a
    # docker pause (and especially right after a restart) the new log file
    # may not yet contain a "committed state" line even though the node has
    # already replayed/committed up to a real height. Fall back to the most
    # recent RPC sample captured before the pause (saved in PRE_PAUSE_RPC_HEIGHTS)
    # to avoid spurious zero readings being mistaken for a split.
    if [ "$h" = "0" ] && [ -n "${PRE_PAUSE_RPC_HEIGHTS:-}" ]; then
      local rpc_h
      rpc_h=$(echo "$PRE_PAUSE_RPC_HEIGHTS" | awk -v n="$node" '{for(i=1;i<=NF;i++){split($i,kv,"=");if(kv[1]==n){print kv[2];exit}}}')
      if [[ "$rpc_h" =~ ^[0-9]+$ ]] && [ "$rpc_h" -gt 0 ]; then
        h=$rpc_h
      fi
    fi
    stopped_heights="${stopped_heights} ${node}=${h}"
    if [ -z "$stopped_min" ] || [ "$h" -lt "$stopped_min" ]; then
      stopped_min=$h
    fi
    if [ -z "$stopped_max" ] || [ "$h" -gt "$stopped_max" ]; then
      stopped_max=$h
    fi
  done
}

sample_pre_pause_rpc_heights() {
  PRE_PAUSE_RPC_HEIGHTS=""
  for i in $(seq 0 $((NODE_COUNT - 1))); do
    local node="sei-node-$i"
    local h
    h=$(node_height "$node")
    if ! [[ "$h" =~ ^[0-9]+$ ]]; then
      h=0
    fi
    PRE_PAUSE_RPC_HEIGHTS="${PRE_PAUSE_RPC_HEIGHTS} ${node}=${h}"
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
  dump_all_node_logs
  exit 1
}

wait_for_height_progress() {
  local min_delta=${1:-1}
  local timeout=${2:-60}
  local start
  start=$(node_height "sei-node-0")
  local target=$((start + min_delta))
  local elapsed=0
  while [ "$elapsed" -lt "$timeout" ]; do
    local h
    h=$(node_height "sei-node-0")
    if [ "$h" -ge "$target" ]; then
      echo "Chain progressed from height $start to $h"
      return 0
    fi
    sleep 2
    elapsed=$((elapsed + 2))
  done
  echo "ERROR: chain did not progress by $min_delta blocks from $start within ${timeout}s" >&2
  dump_all_node_logs
  exit 1
}

ensure_seidb() {
  local node=$1
  if docker exec "$node" bash -lc "test -x /sei-protocol/sei-chain/build/seidb && /sei-protocol/sei-chain/build/seidb migrate-evm-status --help >/dev/null 2>&1" >/dev/null 2>&1; then
    return 0
  fi
  echo "Building seidb on $node..."
  docker exec -e GOPROXY="${GOPROXY:-https://proxy.golang.org,direct}" "$node" bash -lc \
    "cd /sei-protocol/sei-chain && $GO_BIN build -o build/seidb ./sei-db/tools/cmd/seidb"
}

ensure_all_seidb() {
  for i in $(seq 0 $((NODE_COUNT - 1))); do
    ensure_seidb "sei-node-$i"
  done
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

current_sc_write_mode() {
  local node=$1
  docker exec "$node" bash -lc "grep -E '^sc-write-mode' '$APP_CONFIG' | tail -1 | awk -F'\"' '{print \$2}'" 2>/dev/null || true
}

assert_all_nodes_in_mode() {
  local expected=$1
  for i in $(seq 0 $((NODE_COUNT - 1))); do
    local node="sei-node-$i"
    local current_mode
    current_mode=$(current_sc_write_mode "$node")
    if [ "$current_mode" != "$expected" ]; then
      if [ -z "$current_mode" ]; then
        echo "ERROR: $node has no sc-write-mode line in $APP_CONFIG" >&2
      else
        echo "ERROR: $node is in sc-write-mode='$current_mode'; expected '$expected'" >&2
      fi
      exit 1
    fi
  done
  echo "All $NODE_COUNT nodes confirmed in $expected mode"
}

fixture_height_floor() {
  local fixture_height=0
  if [ -f "$FIXTURE_HEIGHT_FILE" ]; then
    fixture_height=$(tail -1 "$FIXTURE_HEIGHT_FILE" | tr -d '[:space:]' || echo 0)
  fi
  if ! [[ "$fixture_height" =~ ^[0-9]+$ ]]; then
    echo "ERROR: fixture height file $FIXTURE_HEIGHT_FILE contains non-numeric value '$fixture_height'" >&2
    exit 1
  fi
  echo $((fixture_height + PRE_FLIP_SETTLE_BLOCKS))
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
  dump_all_node_logs
  exit 1
}

coordinated_stop_at_common_height() {
  local restart_mode_label=${1:-current mode}
  local min_height=${2:-$(fixture_height_floor)}

  PRE_FLIP_HEIGHT=$(wait_for_cluster_height_sync "$min_height" "$PRE_FLIP_SYNC_TIMEOUT" | tail -1)
  echo "Pre-flip height floor reached across all $NODE_COUNT validators: $PRE_FLIP_HEIGHT (min_height=$min_height)"

  stopped_heights=""
  stopped_min=""
  stopped_max=""
  stopped_consistent=false
  for attempt in $(seq 1 "$PRE_FLIP_STOP_ATTEMPTS"); do
    echo "Freezing validators before migration stop (attempt ${attempt}/${PRE_FLIP_STOP_ATTEMPTS})..."
    # Sample heights via RPC before pausing so capture_stopped_heights has a
    # fallback when the in-session log has not yet flushed a "committed state"
    # line for every node (typical right after a coordinated restart).
    sample_pre_pause_rpc_heights
    for i in $(seq 0 $((NODE_COUNT - 1))); do
      docker pause "sei-node-$i" >/dev/null 2>&1 || true &
    done
    wait

    capture_stopped_heights
    echo "Frozen validator committed heights:${stopped_heights}"

    if [ -n "$stopped_min" ] && [ "$stopped_min" = "$stopped_max" ]; then
      echo "Stopping all $NODE_COUNT validators from frozen height $stopped_min..."
      for i in $(seq 0 $((NODE_COUNT - 1))); do
        node="sei-node-$i"
        (
          docker unpause "$node" >/dev/null 2>&1 || true
          docker exec "$node" pkill -TERM -f "seid start" >/dev/null 2>&1 || true
        ) &
      done
      wait

      local elapsed=0
      local all_dead=false
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
        dump_all_node_logs
        exit 1
      fi
      echo "All $NODE_COUNT validators confirmed stopped"

      capture_stopped_heights
      echo "Stopped validator committed heights:${stopped_heights}"

      if [ -n "$stopped_min" ] && [ "$stopped_min" = "$stopped_max" ]; then
        stopped_consistent=true
        break
      fi

      echo "Validators committed an extra block during shutdown; restarting in ${restart_mode_label} before retry:${stopped_heights}" >&2
      if [ "$attempt" -eq "$PRE_FLIP_STOP_ATTEMPTS" ]; then
        break
      fi

      for i in $(seq 0 $((NODE_COUNT - 1))); do
        docker exec -d -e "ID=${i}" "sei-node-$i" /usr/bin/start_sei.sh
      done
      wait_for_all_seid_start "restarted in ${restart_mode_label}"

      PRE_FLIP_HEIGHT=$(wait_for_cluster_height_sync "$stopped_max" "$PRE_FLIP_SYNC_TIMEOUT" | tail -1)
      echo "Pre-flip height floor restored across all $NODE_COUNT validators after shutdown drift: $PRE_FLIP_HEIGHT"
      continue
    fi

    echo "Validators froze at split heights; unpausing to let laggards converge before retry:${stopped_heights}" >&2
    for i in $(seq 0 $((NODE_COUNT - 1))); do
      docker unpause "sei-node-$i" >/dev/null 2>&1 || true &
    done
    wait

    if [ "$attempt" -eq "$PRE_FLIP_STOP_ATTEMPTS" ]; then
      break
    fi
    PRE_FLIP_HEIGHT=$(wait_for_cluster_height_sync "$stopped_max" "$PRE_FLIP_SYNC_TIMEOUT" | tail -1)
    echo "Pre-flip height floor restored across all $NODE_COUNT validators: $PRE_FLIP_HEIGHT"
  done

  if ! $stopped_consistent; then
    echo "ERROR: validators could not be stopped at a common committed height; refusing to flip sc-write-mode" >&2
    echo "Split final stopped heights:${stopped_heights}" >&2
    dump_all_node_logs
    exit 1
  fi
}

flip_sc_write_mode() {
  local mode=$1
  local batch=${2:-$KEYS_TO_MIGRATE_PER_BLOCK}
  for i in $(seq 0 $((NODE_COUNT - 1))); do
    node="sei-node-$i"
    docker exec "$node" bash -c "
      sed -i 's/^sc-write-mode = .*/sc-write-mode = \"${mode}\"/' '$APP_CONFIG'
      if grep -q '^sc-keys-to-migrate-per-block' '$APP_CONFIG'; then
        sed -i 's/^sc-keys-to-migrate-per-block = .*/sc-keys-to-migrate-per-block = ${batch}/' '$APP_CONFIG'
      else
        sed -i '/^sc-write-mode/a sc-keys-to-migrate-per-block = ${batch}' '$APP_CONFIG'
      fi
    "
  done
  echo "Flipped sc-write-mode to ${mode} on all $NODE_COUNT nodes (batch=$batch)"

  local written_mode
  written_mode=$(current_sc_write_mode "sei-node-0")
  if [ "$written_mode" != "$mode" ]; then
    echo "ERROR: sei-node-0 app.toml has sc-write-mode='$written_mode' after rewrite; expected '$mode'" >&2
    exit 1
  fi
}

coordinated_restart_with_settle() {
  local mode=$1
  for i in $(seq 0 $((NODE_COUNT - 1))); do
    node="sei-node-$i"
    docker exec -d -e "ID=${i}" "$node" /usr/bin/start_sei.sh
  done
  wait_for_all_seid_start "restarted in ${mode}"

  sleep 5
  local failed=false
  for i in $(seq 0 $((NODE_COUNT - 1))); do
    node="sei-node-$i"
    if ! docker exec "$node" pgrep -f "seid start" >/dev/null 2>&1; then
      echo "ERROR: $node died within 5s of restart in ${mode}" >&2
      failed=true
    fi
  done
  if $failed; then
    dump_all_node_logs
    exit 1
  fi
  echo "All $NODE_COUNT validators restarted in ${mode} mode and survived a 5s settle"
}

wait_for_migrate_status_complete() {
  local timeout=${1:-$COMPLETION_TIMEOUT}
  ensure_all_seidb

  local elapsed=0
  local all_done=false
  while [ "$elapsed" -lt "$timeout" ]; do
    all_done=true
    status_summary=""
    for i in $(seq 0 $((NODE_COUNT - 1))); do
      node="sei-node-$i"
      status_err="/tmp/${node}-migrate-evm-status.err"
      raw_status=$(docker exec "$node" bash -lc \
        "build/seidb migrate-evm-status --db-dir '$FLATKV_DIR' 2>$status_err" || true)
      status_json=$(printf '%s\n' "$raw_status" | extract_status_json)
      if [ -z "$status_json" ]; then
        status_json='{}'
      fi
      complete=$(echo "$status_json" | jq -r '.migrate_evm_complete // false' 2>/dev/null || echo false)
      version_at=$(echo "$status_json" | jq -r '.version_at // 0' 2>/dev/null || echo 0)
      migration_version=$(echo "$status_json" | jq -r '.migration_version // 0' 2>/dev/null || echo 0)
      boundary_present=$(echo "$status_json" | jq -r '.boundary_present // false' 2>/dev/null || echo false)
      height=$(node_height "$node")
      status_summary="$status_summary ${node}=${complete}@mv${migration_version}/v${version_at}/h${height}/boundary=${boundary_present}"
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
      echo "All $NODE_COUNT validators completed EVM migration:$status_summary"
      return 0
    fi
    echo "Waiting for EVM migration completion (elapsed=${elapsed}s/${timeout}s):$status_summary"
    sleep 5
    elapsed=$((elapsed + 5))
  done

  echo "ERROR: EVM migration did not complete within ${timeout}s on all validators" >&2
  echo "Final migrate-evm-status diagnostics:"
  for i in $(seq 0 $((NODE_COUNT - 1))); do
    node="sei-node-$i"
    status_err="/tmp/${node}-migrate-evm-status-final.err"
    raw_status=$(docker exec "$node" bash -lc \
      "build/seidb migrate-evm-status --db-dir '$FLATKV_DIR' 2>$status_err" || true)
    status_json=$(printf '%s\n' "$raw_status" | extract_status_json)
    echo "final migrate-evm-status raw ${node}: ${raw_status}"
    echo "final migrate-evm-status json ${node}: ${status_json:-{}}"
    docker exec "$node" bash -lc "if [ -s '$status_err' ]; then echo 'final migrate-evm-status stderr ${node}:'; cat '$status_err'; fi" || true
  done
  dump_all_node_logs
  exit 1
}

wait_for_migration_in_progress() {
  local timeout=${1:-60}
  ensure_all_seidb
  local elapsed=0
  while [ "$elapsed" -lt "$timeout" ]; do
    raw_status=$(docker exec "sei-node-0" bash -lc \
      "build/seidb migrate-evm-status --db-dir '$FLATKV_DIR'" || true)
    status_json=$(printf '%s\n' "$raw_status" | extract_status_json)
    boundary_present=$(echo "${status_json:-{}}" | jq -r '.boundary_present // false' 2>/dev/null || echo false)
    complete=$(echo "${status_json:-{}}" | jq -r '.migrate_evm_complete // false' 2>/dev/null || echo false)
    if [ "$boundary_present" = "true" ] && [ "$complete" != "true" ]; then
      echo "EVM migration is in progress on sei-node-0"
      return 0
    fi
    sleep 2
    elapsed=$((elapsed + 2))
  done
  echo "ERROR: EVM migration did not enter in-progress state within ${timeout}s" >&2
  dump_all_node_logs
  exit 1
}

print_migration_summaries() {
  echo "==================== migration completion summaries ===================="
  local failed=false
  for i in $(seq 0 $((NODE_COUNT - 1))); do
    local node="sei-node-$i"
    local logfile="/sei-protocol/sei-chain/build/generated/logs/seid-${i}.log"
    local summary=""
    local keys_migrated=""
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
    dump_all_node_logs
    exit 1
  fi
}

wait_for_post_migration_progress() {
  local timeout=${1:-60}
  local elapsed=0
  while [ "$elapsed" -lt "$timeout" ]; do
    base=$(node_height "sei-node-0")
    if [ "$base" -ge "$MIN_HEIGHT_AFTER" ]; then
      return 0
    fi
    echo "Waiting for post-migration chain progress (h=$base, want >= $MIN_HEIGHT_AFTER)"
    sleep 2
    elapsed=$((elapsed + 2))
  done
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
  shift 2
  local buckets=("$@")
  if [ "${#buckets[@]}" -eq 0 ]; then
    buckets=(account code storage)
  fi
  local bucket_args=""
  local out_dir="/tmp/flatkv-migrate-${version}-${node}"
  for bucket in "${buckets[@]}"; do
    bucket_args="$bucket_args \"${out_dir}/${bucket}\""
  done

  docker exec "$node" bash -lc "
    set -euo pipefail
    out_dir=${out_dir}
    rm -rf \"\$out_dir\" && mkdir -p \"\$out_dir\"
    cd /sei-protocol/sei-chain
    build/seidb dump-flatkv \
      --db-dir $FLATKV_DIR \
      --output-dir \"\$out_dir\" \
      --height $version > /dev/null
    tail -q -n +2 $bucket_args 2>/dev/null | sha256sum | cut -d' ' -f1
  "
}

assert_cross_validator_flatkv_digest() {
  local compare_version=${1:-}
  shift || true
  local buckets=("$@")
  if [ -z "$compare_version" ]; then
    wait_for_post_migration_progress 60
    compare_version=$(pick_compare_height)
  fi

  echo "Comparing FlatKV digests across $NODE_COUNT validators at chain height $compare_version (buckets: ${buckets[*]:-account code storage})"
  local reference_digest=""
  local reference_node=""
  local mismatch=false
  for i in $(seq 0 $((NODE_COUNT - 1))); do
    node="sei-node-$i"
    digest=$(flatkv_dump_digest "$node" "$compare_version" "${buckets[@]}")
    echo "  ${node} sha256 = $digest"
    if [ -z "$reference_digest" ]; then
      reference_digest="$digest"
      reference_node="$node"
      continue
    fi
    if [ "$digest" != "$reference_digest" ]; then
      echo "FAIL: ${node} diverges from ${reference_node} at height $compare_version" >&2
      mismatch=true
    fi
  done

  if $mismatch; then
    dump_all_node_logs
    exit 1
  fi
}

perform_migration_flip() {
  local from_mode=$1
  local to_mode=$2
  local batch=${3:-$KEYS_TO_MIGRATE_PER_BLOCK}
  local min_height=${4:-$(fixture_height_floor)}

  assert_all_nodes_in_mode "$from_mode"
  coordinated_stop_at_common_height "$from_mode" "$min_height"
  flip_sc_write_mode "$to_mode" "$batch"
  coordinated_restart_with_settle "$to_mode"
  wait_for_migrate_status_complete
}

run_v0_to_v1_migration() {
  local batch=${1:-$KEYS_TO_MIGRATE_PER_BLOCK}
  perform_migration_flip "memiavl_only" "migrate_evm" "$batch"
}

kill_node() {
  local node=$1
  docker exec "$node" pkill -KILL -f "seid start" >/dev/null 2>&1 || true
}

restart_node() {
  local node=$1
  local id=${node#sei-node-}
  docker exec -d -e "ID=${id}" "$node" /usr/bin/start_sei.sh
}

assert_all_nodes_alive() {
  local failed=false
  for i in $(seq 0 $((NODE_COUNT - 1))); do
    node="sei-node-$i"
    if ! docker exec "$node" pgrep -f "seid start" >/dev/null 2>&1; then
      echo "ERROR: $node is not running" >&2
      failed=true
    fi
  done
  if $failed; then
    dump_all_node_logs
    exit 1
  fi
}

# block_id_hash_at_height returns the Tendermint block_id.hash for a given
# committed height on a single node, or empty if not available.
block_id_hash_at_height() {
  local node=$1
  local height=$2
  docker exec "$node" bash -lc \
    "curl -sf 'http://localhost:26657/block?height=${height}' 2>/dev/null | jq -r '.result.block_id.hash // .block_id.hash // empty'" 2>/dev/null \
    || echo ""
}

# app_hash_at_height returns the Tendermint header.app_hash for a given
# committed height on a single node, or empty if not available.
app_hash_at_height() {
  local node=$1
  local height=$2
  docker exec "$node" bash -lc \
    "curl -sf 'http://localhost:26657/block?height=${height}' 2>/dev/null | jq -r '.result.block.header.app_hash // .block.header.app_hash // empty'" 2>/dev/null \
    || echo ""
}

# assert_cross_validator_block_hashes verifies that every height in
# [from_height, to_height] is present on every validator and that all
# validators agree on block_id.hash for that height. Catches:
#   - silent block-loss on a validator (missing or null hash),
#   - transient consensus divergence that recovered post-mortem
#     (different block_id at the same committed height).
assert_cross_validator_block_hashes() {
  local from=$1
  local to=$2
  local mismatch=false
  local total=$(( to - from + 1 ))
  if [ "$total" -le 0 ]; then
    echo "assert_cross_validator_block_hashes: no heights in range $from..$to"
    return 0
  fi
  echo "Comparing block hashes across $NODE_COUNT validators for heights $from..$to ($total heights)"
  for h in $(seq "$from" "$to"); do
    local ref=""
    local ref_node=""
    for i in $(seq 0 $((NODE_COUNT - 1))); do
      node="sei-node-$i"
      hash=$(block_id_hash_at_height "$node" "$h")
      if [ -z "$hash" ] || [ "$hash" = "null" ]; then
        echo "FAIL: ${node} missing block at height $h" >&2
        mismatch=true
        continue
      fi
      if [ -z "$ref" ]; then
        ref="$hash"
        ref_node="$node"
        continue
      fi
      if [ "$hash" != "$ref" ]; then
        echo "FAIL: ${node} block_id $hash != ${ref_node} $ref at height $h" >&2
        mismatch=true
      fi
    done
  done
  if $mismatch; then
    dump_all_node_logs
    exit 1
  fi
  echo "PASS: 4-node block_id equal across heights $from..$to"
}

# assert_no_panics_in_logs scans validator logs for a real Go panic --
# either one Cosmos recovered at the ProcessBlock / FinalizeBlocker
# boundary (which would still halt consensus on that height) or an
# unrecovered runtime panic that crashes the process.
#
# Patterns:
#   - 'level=(ERROR|FATAL) msg="panic recovered'   -- the marker the
#     cosmos baseapp / sei app logs after a deferred recover() catches
#     a panic in block execution. This is the bug class that the
#     post-migration EVM iterator regression (Cody review issue 1)
#     surfaces as; it does NOT kill the validator process but does
#     halt consensus on that block, so assert_all_nodes_alive alone
#     would miss it.
#   - '^panic: '                                    -- the Go runtime's
#     own panic printer, only present for unrecovered fatal panics.
#
# Patterns are deliberately anchored / structured so they don't match
# the many INF lines that happen to contain the word "panic" inside a
# message payload (e.g. config dumps, error strings).
assert_no_panics_in_logs() {
  local label=${1:-"migration window"}
  local pattern='level=(ERROR|FATAL) msg="panic recovered|^panic: '
  local failed=false
  for i in $(seq 0 $((NODE_COUNT - 1))); do
    local node="sei-node-$i"
    local logfile="/sei-protocol/sei-chain/build/generated/logs/seid-${i}.log"
    if docker exec "$node" grep -nE "$pattern" "$logfile" >/dev/null 2>&1; then
      echo "FAIL: ${node} log shows panic during ${label}" >&2
      docker exec "$node" grep -nE "$pattern" "$logfile" 2>/dev/null | head -20 >&2 || true
      failed=true
    fi
  done
  if $failed; then
    dump_all_node_logs
    exit 1
  fi
  echo "PASS: no panic in any validator log during ${label}"
}

# graceful_stop_node sends SIGTERM and waits up to `timeout` seconds for
# the process to exit on its own. Unlike kill_node (SIGKILL), this lets
# the validator finish in-flight commits, flush pebble/memiavl, and
# release file locks cleanly. Errors loudly if the process does not exit
# within the window so a hang regression cannot silently degrade into
# the SIGKILL path.
graceful_stop_node() {
  local node=$1
  local timeout=${2:-30}
  docker exec "$node" pkill -TERM -f "seid start" >/dev/null 2>&1 || true
  local elapsed=0
  while [ "$elapsed" -lt "$timeout" ]; do
    if ! docker exec "$node" pgrep -f "seid start" >/dev/null 2>&1; then
      echo "${node} stopped gracefully after ${elapsed}s"
      return 0
    fi
    sleep 2
    elapsed=$((elapsed + 2))
  done
  echo "ERROR: ${node} did not exit on SIGTERM within ${timeout}s" >&2
  dump_node_log "$node"
  exit 1
}

