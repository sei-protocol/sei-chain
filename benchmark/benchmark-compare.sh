#!/bin/bash
# benchmark-compare.sh — Run multiple git commits as benchmarks in parallel
#
# Usage:
#   benchmark/benchmark-compare.sh <label1>=<commit1> <label2>=<commit2> [...]
#
# Example:
#   benchmark/benchmark-compare.sh \
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
#   BENCHMARK_CONFIG           (default: benchmark/scenarios/evm.json)

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

GIGA_EXECUTOR=${GIGA_EXECUTOR:-true}
GIGA_OCC=${GIGA_OCC:-true}
BENCHMARK_TXS_PER_BATCH=${BENCHMARK_TXS_PER_BATCH:-1000}
DURATION=${DURATION:-600}
BENCHMARK_CONFIG=${BENCHMARK_CONFIG:-"$SCRIPT_DIR/scenarios/evm.json"}
DB_BACKEND=${DB_BACKEND:-goleveldb}
BASE_DIR="/tmp/sei-bench"

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
ORIGINAL_BRANCH=$(git -C "$REPO_ROOT" symbolic-ref --short HEAD 2>/dev/null || echo "")
ORIGINAL_REF=$(git -C "$REPO_ROOT" rev-parse HEAD)

cleanup() {
  echo ""
  echo "Cleaning up..."
  for i in $(seq 0 $((NUM-1))); do
    label="${LABELS[$i]}"
    pkill -f "seid-${label}" 2>/dev/null || true
  done
  sleep 2
  if [ -n "$ORIGINAL_BRANCH" ]; then
    git -C "$REPO_ROOT" checkout "$ORIGINAL_BRANCH" 2>/dev/null || true
  else
    git -C "$REPO_ROOT" checkout "$ORIGINAL_REF" --detach 2>/dev/null || true
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
  git -C "$REPO_ROOT" checkout "$commit" --detach 2>/dev/null
  # Forward DB_BACKEND build tags if needed
  BUILD_TAGS=""
  case "$DB_BACKEND" in
    cleveldb) BUILD_TAGS="cleveldb" ;;
    rocksdb)  BUILD_TAGS="rocksdb" ;;
  esac
  if [ -n "$BUILD_TAGS" ]; then
    COSMOS_BUILD_OPTIONS="$BUILD_TAGS" make -C "$REPO_ROOT" install-bench > /dev/null 2>&1
  else
    make -C "$REPO_ROOT" install-bench > /dev/null 2>&1
  fi
  cp ~/go/bin/seid "$BASE_DIR/$label/seid-${label}"
  echo "done"
done
echo ""

# Restore to original ref so benchmark.sh can find scenario files
if [ -n "$ORIGINAL_BRANCH" ]; then
  git -C "$REPO_ROOT" checkout "$ORIGINAL_BRANCH" 2>/dev/null
else
  git -C "$REPO_ROOT" checkout "$ORIGINAL_REF" --detach 2>/dev/null
fi

# ============================================================
# Phase 2: Initialize chains (sequential — delegates to benchmark.sh)
# ============================================================
echo "=== Phase 2: Initializing chains ==="

for i in $(seq 0 $((NUM-1))); do
  label="${LABELS[$i]}"
  seid="$BASE_DIR/$label/seid-${label}"
  home_dir="$BASE_DIR/$label/data"
  port_offset=$((i * 100))

  echo -n "  Initializing [$label]... "

  BENCHMARK_PHASE=init \
  SEI_HOME="$home_dir" \
  SEID_BIN="$seid" \
  PORT_OFFSET="$port_offset" \
  GIGA_EXECUTOR="$GIGA_EXECUTOR" \
  GIGA_OCC="$GIGA_OCC" \
  DB_BACKEND="$DB_BACKEND" \
  BENCHMARK_CONFIG="$BENCHMARK_CONFIG" \
  BENCHMARK_TXS_PER_BATCH="$BENCHMARK_TXS_PER_BATCH" \
  DISABLE_INDEXER=true \
    "$SCRIPT_DIR/benchmark.sh" > /dev/null 2>&1

  RPC=$((26657 + port_offset))
  PPROF=$((6060 + port_offset))
  echo "done (RPC=$RPC pprof=$PPROF)"
done
echo ""

# ============================================================
# Phase 3: Run all nodes in parallel (delegates to benchmark.sh)
# ============================================================
echo "=== Phase 3: Starting $NUM nodes (duration: ${DURATION}s) ==="
NODE_PIDS=()
for i in $(seq 0 $((NUM-1))); do
  label="${LABELS[$i]}"
  seid="$BASE_DIR/$label/seid-${label}"
  home_dir="$BASE_DIR/$label/data"
  log_file="$BASE_DIR/$label/output.log"
  port_offset=$((i * 100))

  BENCHMARK_PHASE=start \
  SEI_HOME="$home_dir" \
  SEID_BIN="$seid" \
  PORT_OFFSET="$port_offset" \
  LOG_FILE="$log_file" \
  DEBUG=true \
  BENCHMARK_CONFIG="$BENCHMARK_CONFIG" \
  BENCHMARK_TXS_PER_BATCH="$BENCHMARK_TXS_PER_BATCH" \
    "$SCRIPT_DIR/benchmark.sh" &
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

PYTHON_CMD=python3
if ! command -v $PYTHON_CMD &> /dev/null; then
    PYTHON_CMD=python
fi

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
echo "  go tool pprof -alloc_space -top -diff_base $BASE_DIR/<baseline>/pprof/heap.pb.gz $BASE_DIR/<candidate>/pprof/heap.pb.gz"
echo "Compare CPU:"
echo "  go tool pprof -top -diff_base $BASE_DIR/<baseline>/pprof/cpu.pb.gz $BASE_DIR/<candidate>/pprof/cpu.pb.gz"
