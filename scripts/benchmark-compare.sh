



#!/bin/bash
# benchmark-compare.sh — Run multiple git commits as benchmarks in parallel
#
# Usage:
#   scripts/benchmark-compare.sh <label1>=<commit1> <label2>=<commit2> [...]
#
# Example:
#   scripts/benchmark-compare.sh \
#     pre-opt=fd2e28d74 \
#     lazy-cms=82acf458d \
#     lazy-cms-fix=37a17fd02
#
# Each scenario gets its own seid binary, data dir, port set, and log file.
# Init is sequential (fast, uses ~/.sei then moves). Run is parallel.
# After DURATION seconds (default 600), all nodes stop and TPS stats are printed.
#
# Environment variables (same as benchmark.sh):
#   GIGA_EXECUTOR=true/false  (default: true)
#   GIGA_OCC=true/false       (default: true)
#   BENCHMARK_TXS_PER_BATCH   (default: 1000)
#   DURATION                   (default: 600 seconds)
#   BENCHMARK_CONFIG           (default: scripts/scenarios/evm.json)

set -e

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"

GIGA_EXECUTOR=${GIGA_EXECUTOR:-true}
GIGA_OCC=${GIGA_OCC:-true}
BENCHMARK_TXS_PER_BATCH=${BENCHMARK_TXS_PER_BATCH:-1000}
DURATION=${DURATION:-600}
BENCHMARK_CONFIG=${BENCHMARK_CONFIG:-"$REPO_ROOT/scripts/scenarios/evm.json"}
BASE_DIR="/tmp/sei-bench"

PYTHON_CMD=python3
if ! command -v $PYTHON_CMD &> /dev/null; then
    PYTHON_CMD=python
fi

if [ $# -lt 2 ]; then
  echo "Usage: $0 <label1>=<commit1> <label2>=<commit2> [...]"
  echo ""
  echo "Example:"
  echo "  DURATION=600 $0 pre-opt=fd2e28d74 lazy-cms=82acf458d fix=37a17fd02"
  exit 1
fi

# Parse arguments
LABELS=()
COMMITS=()
for arg in "$@"; do
  label="${arg%%=*}"
  commit="${arg##*=}"
  if [ "$label" = "$commit" ]; then
    echo "ERROR: Invalid argument '$arg'. Use format label=commit"
    exit 1
  fi
  LABELS=("${LABELS[@]}" "$label")
  COMMITS=("${COMMITS[@]}" "$commit")
done

NUM=${#LABELS[@]}

echo "=== Parallel Benchmark Comparison ==="
echo "  Scenarios:      $NUM"
echo "  Duration:       ${DURATION}s"
echo "  GIGA_EXECUTOR:  $GIGA_EXECUTOR"
echo "  GIGA_OCC:       $GIGA_OCC"
echo "  TXS_PER_BATCH:  $BENCHMARK_TXS_PER_BATCH"
echo ""
for i in $(seq 0 $((NUM-1))); do
  echo "  [$((i+1))] ${LABELS[$i]} = ${COMMITS[$i]}"
done
echo ""

# Save current ref to restore later
ORIGINAL_BRANCH=$(git symbolic-ref --short HEAD 2>/dev/null || echo "")
ORIGINAL_REF=$(git rev-parse HEAD)

cleanup() {
  echo ""
  echo "Cleaning up..."
  for i in $(seq 0 $((NUM-1))); do
    label="${LABELS[$i]}"
    pkill -f "seid-${label}" 2>/dev/null || true
  done
  sleep 2
  if [ -n "$ORIGINAL_BRANCH" ]; then
    git checkout "$ORIGINAL_BRANCH" 2>/dev/null || true
  else
    git checkout "$ORIGINAL_REF" --detach 2>/dev/null || true
  fi
}
trap cleanup EXIT

# ============================================================
# Phase 1: Build all binaries (sequential, Go cache helps)
# ============================================================
echo "=== Phase 1: Building binaries ==="
rm -rf "$BASE_DIR"
for i in $(seq 0 $((NUM-1))); do
  label="${LABELS[$i]}"
  commit="${COMMITS[$i]}"
  mkdir -p "$BASE_DIR/$label"

  echo -n "  Building [$label] from ${commit}... "
  git checkout "$commit" --detach 2>/dev/null
  make install-bench > /dev/null 2>&1
  cp ~/go/bin/seid "$BASE_DIR/$label/seid-${label}"
  echo "done"
done
echo ""

# ============================================================
# Phase 2: Initialize chains (sequential — python script uses ~/.sei)
# ============================================================
echo "=== Phase 2: Initializing chains ==="
cd "$REPO_ROOT"
keyname=admin

for i in $(seq 0 $((NUM-1))); do
  label="${LABELS[$i]}"
  seid="$BASE_DIR/$label/seid-${label}"
  home_dir="$BASE_DIR/$label/data"
  port_offset=$((i * 100))

  echo -n "  Initializing [$label]... "

  # Use ~/.sei for init (python script requires it), then move
  rm -rf ~/.sei
  "$seid" init demo --chain-id sei-chain > /dev/null 2>&1
  "$seid" keys add $keyname --keyring-backend test > /dev/null 2>&1
  ADDR=$("$seid" keys show $keyname -a --keyring-backend test)
  "$seid" add-genesis-account "$ADDR" \
    100000000000000000000usei,100000000000000000000uusdc,100000000000000000000uatom \
    --keyring-backend test > /dev/null 2>&1
  "$seid" gentx $keyname 7000000000000000usei \
    --chain-id sei-chain --keyring-backend test > /dev/null 2>&1

  # Validator setup
  KEY=$(jq '.pub_key' ~/.sei/config/priv_validator_key.json -c)
  jq '.validators = [{}]' ~/.sei/config/genesis.json > ~/.sei/config/tmp1.json
  jq '.validators[0] += {"power":"7000000000"}' ~/.sei/config/tmp1.json > ~/.sei/config/tmp2.json
  jq ".validators[0] += {\"pub_key\":$KEY}" ~/.sei/config/tmp2.json > ~/.sei/config/genesis.json
  rm -f ~/.sei/config/tmp1.json ~/.sei/config/tmp2.json

  # Create test accounts (requires ~/.sei)
  python3 loadtest/scripts/populate_genesis_accounts.py 20 loc 2>/dev/null

  "$seid" collect-gentxs > /dev/null 2>&1

  # Genesis params
  G=~/.sei/config/genesis.json
  START_DATE=$($PYTHON_CMD -c "from datetime import datetime; print(datetime.now().strftime('%Y-%m-%d'))")
  END_3=$($PYTHON_CMD -c "from datetime import datetime, timedelta; print((datetime.now() + timedelta(days=3)).strftime('%Y-%m-%d'))")
  END_5=$($PYTHON_CMD -c "from datetime import datetime, timedelta; print((datetime.now() + timedelta(days=5)).strftime('%Y-%m-%d'))")

  jq '.app_state.gov.deposit_params.max_deposit_period="60s"
    | .app_state.gov.voting_params.voting_period="30s"
    | .app_state.gov.voting_params.expedited_voting_period="10s"
    | .app_state.oracle.params.vote_period="2"
    | .app_state.evm.params.target_gas_used_per_block="1000000000000"
    | .app_state.oracle.params.whitelist=[{"name":"ueth"},{"name":"ubtc"},{"name":"uusdc"},{"name":"uusdt"},{"name":"uosmo"},{"name":"uatom"},{"name":"usei"}]
    | .app_state.distribution.params.community_tax="0.000000000000000000"
    | .consensus_params.block.max_gas="100000000"
    | .consensus_params.block.min_txs_in_block="2"
    | .consensus_params.block.max_gas_wanted="150000000"
    | .app_state.staking.params.max_voting_power_ratio="1.000000000000000000"
    | .app_state.bank.denom_metadata=[{"denom_units":[{"denom":"usei","exponent":0,"aliases":["USEI"]}],"base":"usei","display":"usei","name":"USEI","symbol":"USEI"}]' \
    "$G" > "${G}.tmp" && mv "${G}.tmp" "$G"

  jq --arg sd "$START_DATE" --arg ed "$END_3" \
    '.app_state.mint.params.token_release_schedule=[{"start_date":$sd,"end_date":$ed,"token_release_amount":"999999999999"}]' \
    "$G" > "${G}.tmp" && mv "${G}.tmp" "$G"
  jq --arg sd "$END_3" --arg ed "$END_5" \
    '.app_state.mint.params.token_release_schedule += [{"start_date":$sd,"end_date":$ed,"token_release_amount":"999999999999"}]' \
    "$G" > "${G}.tmp" && mv "${G}.tmp" "$G"

  # Configure app.toml
  A=~/.sei/config/app.toml
  sed -i '' -e 's/# concurrency-workers = .*/concurrency-workers = 500/' "$A"
  sed -i '' -e 's/occ-enabled = .*/occ-enabled = true/' "$A"
  sed -i '' -e 's/sc-enable = .*/sc-enable = true/' "$A"
  sed -i '' -e 's/ss-enable = .*/ss-enable = true/' "$A"

  if [ "$GIGA_EXECUTOR" = true ]; then
    if grep -q "\[giga_executor\]" "$A"; then
      sed -i '' '/\[giga_executor\]/,/^\[/ s/enabled = false/enabled = true/' "$A"
    else
      printf '\n[giga_executor]\nenabled = true\nocc_enabled = false\n' >> "$A"
    fi
    if [ "$GIGA_OCC" = true ]; then
      sed -i '' 's/occ_enabled = false/occ_enabled = true/' "$A"
    fi
  fi

  # Configure config.toml — mode, indexer, pprof, ports
  C=~/.sei/config/config.toml
  sed -i '' 's/mode = "full"/mode = "validator"/g' "$C"
  sed -i '' 's/indexer = \["kv"\]/indexer = \["null"\]/g' "$C"

  RPC=$((26657 + port_offset))
  P2P=$((26656 + port_offset))
  PPROF=$((6060 + port_offset))
  GRPC=$((9090 + port_offset))
  GRPCWEB=$((9091 + port_offset))
  API=$((1317 + port_offset))
  ROSETTA=$((8080 + port_offset))
  EVM_HTTP=$((8545 + port_offset))
  EVM_WS=$((8546 + port_offset))

  sed -i '' "s|pprof-laddr = .*|pprof-laddr = \"localhost:${PPROF}\"|g" "$C"
  sed -i '' "s|laddr = \"tcp://127.0.0.1:26657\"|laddr = \"tcp://127.0.0.1:${RPC}\"|g" "$C"
  sed -i '' "s|laddr = \"tcp://127.0.0.1:26656\"|laddr = \"tcp://127.0.0.1:${P2P}\"|g" "$C"
  sed -i '' "s|address = \"0.0.0.0:9090\"|address = \"0.0.0.0:${GRPC}\"|g" "$A"
  sed -i '' "s|address = \"0.0.0.0:9091\"|address = \"0.0.0.0:${GRPCWEB}\"|g" "$A"
  sed -i '' "s|address = \"tcp://0.0.0.0:1317\"|address = \"tcp://0.0.0.0:${API}\"|g" "$A"
  sed -i '' "s|address = \":8080\"|address = \":${ROSETTA}\"|g" "$A"
  sed -i '' "s|http_port = 8545|http_port = ${EVM_HTTP}|g" "$A"
  sed -i '' "s|ws_port = 8546|ws_port = ${EVM_WS}|g" "$A"

  "$seid" config keyring-backend test > /dev/null 2>&1

  # Move setup to scenario dir
  mv ~/.sei "$home_dir"
  echo "done (RPC=$RPC pprof=$PPROF)"
done
echo ""

# ============================================================
# Phase 3: Run all nodes in parallel
# ============================================================
echo "=== Phase 3: Starting $NUM nodes (duration: ${DURATION}s) ==="
NODE_PIDS=()
for i in $(seq 0 $((NUM-1))); do
  label="${LABELS[$i]}"
  seid="$BASE_DIR/$label/seid-${label}"
  home_dir="$BASE_DIR/$label/data"
  log_file="$BASE_DIR/$label/output.log"

  BENCHMARK_CONFIG="$BENCHMARK_CONFIG" \
  BENCHMARK_TXS_PER_BATCH="$BENCHMARK_TXS_PER_BATCH" \
  "$seid" start --chain-id sei-chain --home "$home_dir" \
    > "$log_file" 2>&1 &
  pid=$!
  NODE_PIDS=("${NODE_PIDS[@]}" $pid)
  echo "  [$label] PID=$pid log=$log_file"
done
echo ""
echo "Running for ${DURATION}s..."

# Wait until midway, then capture pprof profiles
PPROF_WAIT=$(( DURATION / 2 ))
echo "  Waiting ${PPROF_WAIT}s before capturing pprof profiles..."
sleep "$PPROF_WAIT" || true

echo ""
echo "=== Capturing pprof profiles (30s CPU + heap allocs) ==="
PPROF_PIDS=()
for i in $(seq 0 $((NUM-1))); do
  label="${LABELS[$i]}"
  port_offset=$((i * 100))
  PPROF_PORT=$((6060 + port_offset))
  profile_dir="$BASE_DIR/$label/pprof"
  mkdir -p "$profile_dir"

  # CPU profile (30s)
  curl -s "http://localhost:${PPROF_PORT}/debug/pprof/profile?seconds=30" \
    -o "$profile_dir/cpu.pb.gz" &
  PPROF_PIDS=("${PPROF_PIDS[@]}" $!)

  # Heap (alloc_objects — total allocations)
  curl -s "http://localhost:${PPROF_PORT}/debug/pprof/heap?debug=0" \
    -o "$profile_dir/heap.pb.gz" &
  PPROF_PIDS=("${PPROF_PIDS[@]}" $!)

  echo "  [$label] capturing from localhost:${PPROF_PORT} -> $profile_dir/"
done

# Wait for all pprof captures to finish
for pid in "${PPROF_PIDS[@]}"; do
  wait "$pid" 2>/dev/null || true
done
echo "  pprof capture complete."

# Verify profile sizes
for i in $(seq 0 $((NUM-1))); do
  label="${LABELS[$i]}"
  profile_dir="$BASE_DIR/$label/pprof"
  cpu_size=$(wc -c < "$profile_dir/cpu.pb.gz" 2>/dev/null || echo 0)
  heap_size=$(wc -c < "$profile_dir/heap.pb.gz" 2>/dev/null || echo 0)
  echo "  [$label] cpu=${cpu_size}B heap=${heap_size}B"
done

# Wait remaining time
REMAINING=$(( DURATION - PPROF_WAIT - 30 ))
if [ "$REMAINING" -gt 0 ]; then
  echo ""
  echo "  Continuing benchmark for ${REMAINING}s..."
  sleep "$REMAINING" || true
fi

echo ""
echo "Stopping all nodes..."
for pid in "${NODE_PIDS[@]}"; do
  kill "$pid" 2>/dev/null || true
done
sleep 3

# ============================================================
# Phase 4: Results
# ============================================================
echo ""
echo "======================================================="
echo "=== Benchmark Comparison Results (${DURATION}s run) ==="
echo "======================================================="
echo ""

for i in $(seq 0 $((NUM-1))); do
  label="${LABELS[$i]}"
  commit="${COMMITS[$i]}"
  log_file="$BASE_DIR/$label/output.log"
  tps_file="$BASE_DIR/$label/tps.txt"

  # Extract TPS (strip ANSI codes)
  sed 's/\x1b\[[0-9;]*m//g' "$log_file" \
    | sed -n 's/.*tps=\([0-9.]*\).*/\1/p' > "$tps_file"

  HEIGHT=$(sed 's/\x1b\[[0-9;]*m//g' "$log_file" \
    | sed -n 's/.*height=\([0-9]*\).*/\1/p' | tail -1)

  STATS=$(sort -n "$tps_file" | awk '
    NR>3 && $1>1000 {
      sum += $1; count++
      a[count] = $1
    }
    END {
      if (count == 0) { printf "NO DATA"; exit }
      median = (count % 2 == 1) ? a[int(count/2)+1] : (a[int(count/2)] + a[int(count/2)+1]) / 2
      printf "Readings: %d | Median: %.0f | Avg: %.0f | Min: %.0f | Max: %.0f", \
        count, median, sum/count, a[1], a[count]
    }')

  echo "[$label] (${commit})"
  echo "  Height: ${HEIGHT:-N/A}"
  echo "  $STATS"
  echo ""
done

echo "Raw data:  $BASE_DIR/<label>/tps.txt"
echo "Full logs: $BASE_DIR/<label>/output.log"
echo "Profiles:  $BASE_DIR/<label>/pprof/{cpu,heap}.pb.gz"
echo ""
echo "Compare allocs:"
echo "  go tool pprof -alloc_space -top $BASE_DIR/<label>/pprof/heap.pb.gz"
echo "Compare CPU:"
echo "  go tool pprof -top $BASE_DIR/<label>/pprof/cpu.pb.gz"
