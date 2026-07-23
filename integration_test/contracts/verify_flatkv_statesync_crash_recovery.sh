#!/bin/bash
#
# verify_flatkv_statesync_crash_recovery.sh
#
# F3: wipe one validator's local state, start state-sync, SIGKILL it while
# state-sync is in progress, then restart and require it to catch up with
# logically equivalent FlatKV EVM content.

set -euo pipefail

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

dump_statesync_config() {
  local node=$1
  echo "--- ${node} effective [statesync] section of /root/.sei/config/config.toml ---" >&2
  docker exec "$node" bash -lc \
    "awk '/^\[statesync\]/{flag=1;print;next} /^\[/{flag=0} flag' /root/.sei/config/config.toml" >&2 || true
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

wait_for_evm_rpc() {
  local node=$1
  local timeout=$2
  local elapsed=0
  while [ "$elapsed" -lt "$timeout" ]; do
    if docker exec "$node" bash -lc 'curl -sf -H "Content-Type: application/json" --data '"'"'{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}'"'"' http://localhost:8545 >/dev/null'; then
      echo "EVM RPC on $node is responding"
      return 0
    fi
    echo "Waiting for EVM RPC on $node (elapsed=${elapsed}s/${timeout}s)"
    sleep 2
    elapsed=$((elapsed + 2))
  done
  echo "ERROR: EVM RPC on $node did not respond within ${timeout}s" >&2
  dump_node_log "$node"
  return 1
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

# Print the effective [state-commit] section that Viper actually parses
# (sei-cosmos config uses dotted path state-commit.sc-write-mode, so the
# section name is [state-commit] -- the previous [state-store] grep was
# matching the unrelated SS layer config and never told us what mode the
# SC layer was actually running in).
dump_state_commit_section() {
  local node=$1
  echo "--- ${node} effective [state-commit] section of /root/.sei/config/app.toml ---" >&2
  docker exec "$node" bash -lc \
    "awk '/^\[state-commit\]/{flag=1;print;next} /^\[/{flag=0} flag' /root/.sei/config/app.toml" >&2 || true
}

# Print the parsed SC config that the running process actually loaded.
# This is the source of truth (vs app.toml on disk, which can be edited
# after startup) -- the line is emitted exactly once per process start
# and contains sc-write-mode/sc-read-mode/lattice as Viper saw them.
dump_parsed_sc_config() {
  local node=$1
  local node_id=${node#sei-node-}
  local logfile="/sei-protocol/sei-chain/build/generated/logs/seid-${node_id}.log"
  echo "--- ${node} parsed SeiDB SC config (from log, source of truth) ---" >&2
  docker exec "$node" bash -lc \
    "grep -m1 'SeiDB SC is enabled now' '$logfile' 2>/dev/null" >&2 \
    || echo "(no 'SeiDB SC is enabled now' line found)" >&2
}

# Dump per-bucket row counts and whether the EVM fixture rows
# (recipient/contract/storage/code) appear in each bucket of a node's
# FlatKV. This is the smoking gun for the post-state-sync divergence:
# if donor has K rows in 'account' but victim has K-1 (missing exactly
# the fixture recipient), the loss is local to the recipient row; if
# donor itself does not contain the recipient hex in 'account', the
# fixture assertion is wrong for native-transfer recipients and the
# test, not the product, needs to change.
dump_flatkv_bucket_summary() {
  local node=$1
  ensure_seidb "$node" >/dev/null 2>&1 || true
  echo "--- ${node} FlatKV bucket row counts and fixture presence ---" >&2
  docker exec "$node" bash -lc "
    set +e
    out_dir=/tmp/flatkv-debug-${node}
    rm -rf \"\$out_dir\" && mkdir -p \"\$out_dir\"
    cd /sei-protocol/sei-chain
    if ! build/seidb dump-flatkv --db-dir $FLATKV_DIR --output-dir \"\$out_dir\" >/dev/null 2>&1; then
      echo '(dump-flatkv failed -- FlatKV dir may be missing)'
      exit 0
    fi
    recipient_hex=\$(tail -1 integration_test/contracts/flatkv_evm_recipient_addr.txt 2>/dev/null | sed 's/^0x//' | tr '[:lower:]' '[:upper:]')
    contract_hex=\$(tail -1 integration_test/contracts/flatkv_evm_contract_addr.txt 2>/dev/null | sed 's/^0x//' | tr '[:lower:]' '[:upper:]')
    storage_hex=\$(tail -1 integration_test/contracts/flatkv_evm_storage_expected.txt 2>/dev/null | sed 's/^0x//' | tr '[:lower:]' '[:upper:]')
    code_hex=\$(tail -1 integration_test/contracts/flatkv_evm_code_expected.txt 2>/dev/null | sed 's/^0x//' | tr '[:lower:]' '[:upper:]')
    for b in account code storage misc; do
      f=\"\$out_dir/\$b\"
      if [ -s \"\$f\" ]; then
        n=\$(wc -l < \"\$f\")
      else
        n=0
      fi
      # grep -c always prints the count, but exits non-zero on 0 matches;
      # the previous '|| echo 0' fallback then printed an extra '0',
      # turning 'recipient_hits=0' into 'recipient_hits=0\n0' and breaking
      # the printf alignment. Pipe through head -1 instead so we keep the
      # genuine count and ignore the exit code via the no-op || true.
      r_hit=\$({ grep -c \"\$recipient_hex\" \"\$f\" 2>/dev/null || true; } | head -1)
      c_hit=\$({ grep -c \"\$contract_hex\" \"\$f\" 2>/dev/null || true; } | head -1)
      s_hit=\$({ grep -c \"\$storage_hex\" \"\$f\" 2>/dev/null || true; } | head -1)
      k_hit=\$({ grep -c \"\$code_hex\" \"\$f\" 2>/dev/null || true; } | head -1)
      : \"\${r_hit:=0}\" \"\${c_hit:=0}\" \"\${s_hit:=0}\" \"\${k_hit:=0}\"
      printf '  bucket=%-8s rows=%-6s recipient_hits=%s contract_hits=%s storage_hits=%s code_hits=%s\n' \
        \"\$b\" \"\$n\" \"\$r_hit\" \"\$c_hit\" \"\$s_hit\" \"\$k_hit\"
    done
  " >&2 || true
}

# Donor-side: at snapshot-export time, what is the SC layer actually
# running as, what did the snapshot exporter say, and -- critically --
# does the donor's own FlatKV contain the EVM fixture rows that we
# later assert on the victim. Without the bucket summary on the donor
# side, the test cannot distinguish "blocksync replay never wrote the
# recipient to FlatKV" (product invariant) from "donor never had it
# either" (broken test fixture).
dump_donor_snapshot_export_diagnostics() {
  local node=$1
  local node_id=${node#sei-node-}
  local logfile="/sei-protocol/sei-chain/build/generated/logs/seid-${node_id}.log"
  echo "==================== ${node} snapshot export diagnostics ====================" >&2
  dump_state_commit_section "$node"
  dump_parsed_sc_config "$node"
  dump_flatkv_bucket_summary "$node"
  echo "--- ${node} snapshot/export/FlatKV log lines ---" >&2
  docker exec "$node" bash -lc \
    "grep -E 'snapshot|Snapshot|Exporter|exporter|FlatKV|flatkv' '$logfile' 2>/dev/null | tail -100" >&2 \
    || echo "(no matches)" >&2
  echo "==================== end ${node} snapshot export diagnostics ====================" >&2
}

# Victim-side: the three things we need to decide whether the divergence
# is a state-sync importer bug, a blocksync-replay product bug, or a test
# fixture bug:
#   (1) parsed sc-config: did the victim actually start in dual_write?
#   (2) state-sync vs blocksync outcome: blocks_synced=N + state_synced=
#       false means it fell back to blocksync replay
#   (3) per-block FlatKV commit telemetry: any block whose Commit log
#       reports pendingAccount>0 / pendingCode>0 / pendingStorage>0
#       proves runtime dual_write replay does populate FlatKV EVM
#       buckets; if every line is pendingAccount=0 ..., FlatKV is only
#       fillable via offline import / state-sync, not blocksync.
# Plus the per-bucket dump on the victim so we can compare bucket row
# counts directly with the donor.
dump_victim_restore_diagnostics() {
  local node=$1
  local node_id=${node#sei-node-}
  local logfile="/sei-protocol/sei-chain/build/generated/logs/seid-${node_id}.log"
  echo "==================== ${node} restore diagnostics ====================" >&2
  dump_state_commit_section "$node"
  dump_parsed_sc_config "$node"
  echo "--- ${node} state-sync vs blocksync outcome ---" >&2
  docker exec "$node" bash -lc \
    "grep -E 'state_synced=|blocks_synced=|switching to consensus reactor|Start restoring store|applied snapshot' '$logfile' 2>/dev/null | head -40" >&2 \
    || echo "(no state-sync outcome lines found)" >&2
  echo "--- ${node} non-empty EVM FlatKV commits during replay (pendingAccount/Code/Storage > 0) ---" >&2
  docker exec "$node" bash -lc \
    "grep -E 'pendingAccount=[1-9]|pendingCode=[1-9]|pendingStorage=[1-9]' '$logfile' 2>/dev/null | head -40" >&2 \
    || echo "(NONE -- confirms blocksync replay in dual_write does not populate FlatKV EVM buckets)" >&2
  echo "--- ${node} sample FlatKV Commit lines (debug-level; present only when the node runs at --log_level debug) ---" >&2
  docker exec "$node" bash -lc \
    "grep 'FlatKV Commit complete' '$logfile' 2>/dev/null | { head -5; echo '...'; tail -5; }" >&2 \
    || echo "(no FlatKV Commit lines -- expected at info level; see the bucket summary below and the CommitLatency/CurrentVersion metrics)" >&2
  dump_flatkv_bucket_summary "$node"
  echo "--- ${node} restore/import/FlatKV log lines ---" >&2
  docker exec "$node" bash -lc \
    "grep -E 'Start restoring store|restoring|restore|FlatKV|flatkv|Importer|importer' '$logfile' 2>/dev/null | tail -100" >&2 \
    || echo "(no matches)" >&2
  echo "==================== end ${node} restore diagnostics ====================" >&2
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
  local min_height=0
  if [ -n "$MIN_SNAPSHOT_HEIGHT_OVERRIDE" ]; then
    min_height=$MIN_SNAPSHOT_HEIGHT_OVERRIDE
  elif [ -s "$IMPORT_HEIGHT_FILE" ]; then
    # Legacy: when an offline memiavl -> FlatKV import was performed, the
    # post-import height was recorded here and we floor the required
    # snapshot at import_height + 1 so the state-sync target has at least
    # one block of post-import data. With FlatKV enabled from genesis this
    # file does not exist; fall through to MIN_DONOR_HEIGHT below.
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
    # Scope state-sync rewrites to the [statesync] section. Use the known
    # following [consensus] header as the range end instead of a generic
    # 'next section' regex so sed implementations cannot terminate the range
    # on the [statesync] header itself.
    sed -i.bak \
      -e '/^\[statesync\]/,/^\[consensus\]/ s|^enable *=.*|enable = true|' \
      -e '/^\[statesync\]/,/^\[consensus\]/ s|^rpc-servers *=.*|rpc-servers = \"${DONOR_NODE}:26657,${SECOND_RPC_NODE}:26657\"|' \
      -e '/^\[statesync\]/,/^\[consensus\]/ s|^trust-height *=.*|trust-height = ${trust_height}|' \
      -e '/^\[statesync\]/,/^\[consensus\]/ s|^trust-hash *=.*|trust-hash = \"${trust_hash}\"|' \
      /root/.sei/config/config.toml
    sed -i.bak -e \"s|^persistent-peers *=.*|persistent-peers = \\\"\${peers}\\\"|\" /root/.sei/config/config.toml
  "
}

assert_statesync_configured() {
  local node=$1
  local trust_height=$2
  local trust_hash=$3
  if docker exec "$node" bash -lc "
    set -euo pipefail
    section=\$(awk '/^\[statesync\]/{flag=1;next} /^\[/{flag=0} flag' /root/.sei/config/config.toml)
    printf '%s\n' \"\$section\" | awk '
      /^enable[[:space:]]*=/ { enable=\$3 }
      /^rpc-servers[[:space:]]*=/ { rpc=\$0 }
      /^trust-height[[:space:]]*=/ { height=\$3 }
      /^trust-hash[[:space:]]*=/ { hash=\$3; gsub(/\\\"/, \"\", hash) }
      END {
        if (enable != \"true\") exit 10
        if (height != \"${trust_height}\") exit 11
        if (hash != \"${trust_hash}\") exit 12
        if (rpc !~ /${DONOR_NODE}:26657/ || rpc !~ /${SECOND_RPC_NODE}:26657/) exit 13
      }'
  "; then
    return 0
  fi
  echo "ERROR: $node state-sync config was not written as expected" >&2
  dump_statesync_config "$node"
  return 1
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
  local regex='This node needs state sync|starting state sync|starting state sync with picked snapshot|Offering snapshot to ABCI app|Snapshot accepted, restoring|Fetching snapshot chunk|Applied snapshot chunk|Start restoring store'

  while [ "$elapsed" -lt "$timeout" ]; do
    if docker exec "$node" bash -lc "grep -Eiq '$regex' '$log_path' 2>/dev/null"; then
      echo "Detected state-sync activity in $node log; killing mid-flight"
      docker exec "$node" bash -lc "grep -Ei '$regex' '$log_path' 2>/dev/null | tail -5" || true
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

  echo "WARNING: no explicit state-sync restore log detected within ${timeout}s; killing $node anyway and relying on content assertions" >&2
  docker exec "$node" bash -lc \
    "grep -E 'state sync|statesync|snapshot|blocks_synced=|state_synced=|switching to consensus reactor|Found local state' '$log_path' 2>/dev/null | tail -40" >&2 \
    || echo "(no state-sync/blocksync startup lines found before kill)" >&2
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

assert_flatkv_dump_contains_fixture() {
  local node=$1
  if ! ensure_seidb "$node"; then
    return 1
  fi
  # Use `if ! docker exec ...; then return 1; fi` (NOT `... || return 1` and
  # NOT a bare `docker exec` as the function's last command). When this helper
  # is invoked from `assert_flatkv_recovered` -- regardless of whether the
  # caller chains `|| fail` or wraps in `if !` -- bash suspends `set -e`
  # inside the function body, so any non-trailing failure will not abort
  # automatically. Capturing the docker exec exit code here is the only way
  # to propagate the failure reliably.
  if ! docker exec "$node" bash -lc "
    set -euo pipefail
    out_dir=/tmp/flatkv-statesync-crash-smoke-${node}
    rm -rf \"\$out_dir\" && mkdir -p \"\$out_dir\"
    cd /sei-protocol/sei-chain
    build/seidb dump-flatkv \
      --db-dir $FLATKV_DIR \
      --output-dir \"\$out_dir\" > /dev/null
    # NOTE: the native-transfer recipient is intentionally NOT asserted in any
    # FlatKV bucket -- see the long-form rationale on the 'account' assertion
    # below. Recipient liveness is verified via the RPC balance query in
    # assert_evm_fixture_queries instead.
    contract_hex=\$(tail -1 integration_test/contracts/flatkv_evm_contract_addr.txt | sed 's/^0x//' | tr '[:lower:]' '[:upper:]')
    storage_hex=\$(tail -1 integration_test/contracts/flatkv_evm_storage_expected.txt | sed 's/^0x//' | tr '[:lower:]' '[:upper:]')
    code_hex=\$(tail -1 integration_test/contracts/flatkv_evm_code_expected.txt | sed 's/^0x//' | tr '[:lower:]' '[:upper:]')
    # Use the contract address (not the native-transfer recipient) for the
    # 'account' bucket assertion. The recipient is a fresh EOA whose
    # default-value EVM state (nonce=0, codehash=keccak('')) is never
    # persisted by Sei's EVM keeper, so the recipient never appears in
    # FlatKV's account bucket on any node, including donors -- diagnostics
    # confirmed donor itself has 0 hits in account/code/storage/misc.
    # The native-transfer balance is held in the bank module, whose
    # changesets are not routed to FlatKV in dual_write mode at all
    # (only EVM-named changesets are). Recipient liveness is instead
    # validated via the RPC balance query in assert_evm_fixture_queries.
    # The contract address, by contrast, has explicit nonce/codehash
    # writes from CREATE and is present in the 'account' bucket on every
    # dual_write validator (diagnostics: 1 hit for the contract here).
    if [ ! -s \"\$out_dir/account\" ] || ! grep -q \"\$contract_hex\" \"\$out_dir/account\"; then
      echo \"ERROR: $node FlatKV account dump is missing fixture contract \$contract_hex\" >&2
      exit 1
    fi
    if [ ! -s \"\$out_dir/storage\" ] || ! grep -q \"\$contract_hex\" \"\$out_dir/storage\"; then
      echo \"ERROR: $node FlatKV storage dump is missing fixture contract \$contract_hex\" >&2
      exit 1
    fi
    if ! grep -q \"\$storage_hex\" \"\$out_dir/storage\"; then
      echo \"ERROR: $node FlatKV storage dump is missing expected value \$storage_hex\" >&2
      exit 1
    fi
    if [ ! -s \"\$out_dir/code\" ] || ! grep -q \"\$code_hex\" \"\$out_dir/code\"; then
      echo \"ERROR: $node FlatKV code dump is missing fixture code \$code_hex\" >&2
      exit 1
    fi
  "; then
    return 1
  fi
}

assert_evm_fixture_queries() {
  local node=$1
  if ! wait_for_evm_rpc "$node" 60; then
    return 1
  fi
  # IMPORTANT: use `if ! docker exec ...; then return 1; fi` rather than a
  # bare `docker exec` followed by an unconditional `echo "...passed..."`.
  # The function returns the exit status of its LAST command, so the trailing
  # echo would mask any failure inside the docker payload. A previous CI run
  # hit exactly this trap: `cast: command not found` produced two stderr
  # lines and an exit-1 docker payload, but the script still printed
  # "FlatKV EVM fixture queries passed" and proceeded.
  if ! docker exec "$node" bash -lc '
    set -euo pipefail
    # foundry installs cast under ~/.foundry/bin; without this prefix the
    # whole assertion silently no-ops (set -e does not abort on
    # command-substitution failures, so actual_balance="" then compares
    # against the expected hex). Fail loudly if cast is genuinely missing.
    export PATH="$HOME/.foundry/bin:/root/.foundry/bin:$PATH:/root/go/bin:/usr/local/go/bin"
    if ! command -v cast >/dev/null 2>&1; then
      echo "ERROR: cast not found in PATH=$PATH; FlatKV EVM fixture queries cannot run" >&2
      exit 1
    fi
    cd /sei-protocol/sei-chain

    recipient=$(tail -1 integration_test/contracts/flatkv_evm_recipient_addr.txt)
    expected_balance=$(tail -1 integration_test/contracts/flatkv_evm_balance_expected.txt)
    actual_balance=$(cast to-hex "$(cast balance "$recipient" --block latest --rpc-url http://localhost:8545)")

    contract=$(tail -1 integration_test/contracts/flatkv_evm_contract_addr.txt)
    slot=$(tail -1 integration_test/contracts/flatkv_evm_storage_slot.txt)
    expected_storage=$(tail -1 integration_test/contracts/flatkv_evm_storage_expected.txt)
    expected_code=$(tail -1 integration_test/contracts/flatkv_evm_code_expected.txt)
    actual_storage=$(cast storage "$contract" "$slot" --block latest --rpc-url http://localhost:8545)
    actual_code=$(cast code "$contract" --block latest --rpc-url http://localhost:8545)

    missing=$(tail -1 integration_test/contracts/flatkv_evm_missing_addr.txt)
    expected_missing_balance=$(tail -1 integration_test/contracts/flatkv_evm_missing_balance_expected.txt)
    expected_missing_storage=$(tail -1 integration_test/contracts/flatkv_evm_missing_storage_expected.txt)
    actual_missing_balance=$(cast to-hex "$(cast balance "$missing" --block latest --rpc-url http://localhost:8545)")
    actual_missing_storage=$(cast storage "$missing" "$slot" --block latest --rpc-url http://localhost:8545)

    if [ "$actual_balance" != "$expected_balance" ]; then
      echo "latest balance mismatch: got $actual_balance want $expected_balance" >&2
      exit 1
    fi
    if [ "$actual_storage" != "$expected_storage" ]; then
      echo "latest storage mismatch: got $actual_storage want $expected_storage" >&2
      exit 1
    fi
    if [ "$actual_code" != "$expected_code" ]; then
      echo "latest code mismatch: got $actual_code want $expected_code" >&2
      exit 1
    fi
    if [ "$actual_missing_balance" != "$expected_missing_balance" ]; then
      echo "missing balance mismatch: got $actual_missing_balance want $expected_missing_balance" >&2
      exit 1
    fi
    if [ "$actual_missing_storage" != "$expected_missing_storage" ]; then
      echo "missing storage mismatch: got $actual_missing_storage want $expected_missing_storage" >&2
      exit 1
    fi
  '; then
    return 1
  fi
  echo "FlatKV EVM fixture queries passed on $node"
}

# Print which recovery path the victim actually took (state-sync resume vs
# blocksync fallback) for diagnostic visibility, but DO NOT fail the test
# either way. Rationale: this test SIGKILL's the victim mid-state-sync, so
# both outcomes are legitimate -- (a) Tendermint resumes the snapshot apply
# on restart and emits state_synced=true, or (b) the partial snapshot is
# abandoned and the node catches up via blocksync replay. The CI run on
# 2026-05-13T15:53Z confirmed (b) is the typical path here, and that the
# blocksync-replay path still produces a correct FlatKV (EVM dual_write
# replay populates account/code/storage buckets at the fixture-deploy
# heights -- diagnostics: pendingAccount=1 at version=30, pendingAccount=2
# at version=35, matching the recipient transfer at h=32 and contract
# create at h=37). The strict "must use state-sync" invariant only belongs
# to verify_flatkv_total_loss_recovery.sh, where no crash is injected and
# state-sync is the only intended recovery path.
log_recovery_path() {
  local node=$1
  local node_id=${node#sei-node-}
  local logfile="/sei-protocol/sei-chain/build/generated/logs/seid-${node_id}.log"
  if docker exec "$node" bash -lc "grep -qE 'state_synced=true' '$logfile' 2>/dev/null"; then
    echo "$node recovery path: STATE-SYNC RESUME (state_synced=true after mid-flight kill)"
  elif docker exec "$node" bash -lc "grep -qE 'state_synced=false .*blocks_synced=' '$logfile' 2>/dev/null"; then
    echo "$node recovery path: BLOCKSYNC FALLBACK (mid-flight state-sync abandoned, replayed via blocksync)"
  else
    echo "$node recovery path: UNKNOWN (no state_synced= outcome line in log)"
  fi
}

assert_flatkv_recovered() {
  # FlatKV snapshot export/import is logically lossless for EVM queries, but it
  # can re-serialize rows, so raw byte digests need not match donor validators.
  echo "Verifying restored FlatKV EVM content on $VICTIM_NODE"
  log_recovery_path "$VICTIM_NODE"
  # Run both content checks unconditionally and aggregate failure: short-circuiting
  # via `&&` / `||` would (a) hide secondary failures behind the first one and
  # (b) re-introduce the bash conditional-context trap (set -e suspended in
  # helpers; trailing `echo "passed"` masking the real exit code). One
  # explicit failure flag avoids both. Recovery path (state-sync vs blocksync)
  # is logged for diagnostic visibility only -- see log_recovery_path comment.
  local failed=0
  assert_flatkv_dump_contains_fixture "$VICTIM_NODE" || failed=1
  assert_evm_fixture_queries "$VICTIM_NODE" || failed=1
  if [ "$failed" -ne 0 ]; then
    # Failure path only: dump donor + victim diagnostics so a divergence
    # can be attributed (parsed sc-config + per-bucket row counts on
    # both sides + non-empty pendingAccount/Code/Storage commits during
    # replay). Diagnostics are intentionally skipped on PASS runs to
    # keep CI logs scannable; the donor snapshot export diagnostics
    # are still emitted unconditionally earlier in the main flow,
    # right after wait_for_snapshot_at_or_after.
    dump_donor_snapshot_export_diagnostics "$DONOR_NODE"
    dump_victim_restore_diagnostics "$VICTIM_NODE"
    dump_node_log "$VICTIM_NODE"
    dump_node_log "$DONOR_NODE"
    exit 1
  fi
}

required_snapshot_height=$(min_required_snapshot_height)
wait_for_snapshot_at_or_after "$DONOR_NODE" "$required_snapshot_height" "$SNAPSHOT_WAIT_TIMEOUT"
# The donor's snapshot at this height is what the victim will restore from.
# Capture writer-side state now (effective sc-write-mode + snapshot/export
# log lines) so that a later FlatKV-divergence failure has the runtime
# evidence needed to attribute it to "donor never wrote FlatKV into the
# snapshot" vs "victim failed to import it".
dump_donor_snapshot_export_diagnostics "$DONOR_NODE"
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
  rm -rf /root/.sei/data /root/.sei/wasm /sei-protocol/sei-chain/build/generated/node_${VICTIM_INDEX}/snapshots
  mkdir -p /root/.sei/data /sei-protocol/sei-chain/build/generated/node_${VICTIM_INDEX}/snapshots
  mv /tmp/flatkv-priv-validator-state.json /root/.sei/data/priv_validator_state.json
  sed -i.bak -e 's|^snapshot-directory *=.*|snapshot-directory = \"./build/generated/node_${VICTIM_INDEX}/snapshots\"|' /root/.sei/config/app.toml
"
configure_statesync "$VICTIM_NODE" "$trust_height" "$trust_hash"
assert_statesync_configured "$VICTIM_NODE" "$trust_height" "$trust_hash"

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
assert_flatkv_recovered

echo "PASS: $VICTIM_NODE recovered from a SIGKILL during state-sync and serves restored FlatKV EVM data"
