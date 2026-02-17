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
# Init is sequential (fast, uses mktemp staging). Run is parallel.
# After DURATION seconds (default 600), all nodes stop and TPS stats are printed.
#
# Environment variables (same as benchmark.sh):
#   GIGA_EXECUTOR=true/false  (default: true)
#   GIGA_OCC=true/false       (default: true)
#   BENCHMARK_TXS_PER_BATCH   (default: 1000)
#   DURATION                   (default: 600 seconds)
#   BENCHMARK_CONFIG           (default: benchmark/scenarios/evm.json)
#   RUN_ID                     (default: $$ PID — namespaces BASE_DIR)
#   RUN_PORT_OFFSET            (default: auto-claimed slot — added to all port offsets)

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

GIGA_EXECUTOR=${GIGA_EXECUTOR:-true}
GIGA_OCC=${GIGA_OCC:-true}
BENCHMARK_TXS_PER_BATCH=${BENCHMARK_TXS_PER_BATCH:-1000}
DURATION=${DURATION:-120}
BENCHMARK_CONFIG=${BENCHMARK_CONFIG:-"$SCRIPT_DIR/scenarios/evm.json"}
DB_BACKEND=${DB_BACKEND:-goleveldb}
RUN_ID=${RUN_ID:-$$}
BASE_DIR="/tmp/sei-bench-${RUN_ID}"

# Auto-claim a port offset slot using atomic mkdir if not explicitly set.
# Slots start at offset 1000 (not 0) so compare runs never collide with
# standalone benchmark.sh which uses the default PORT_OFFSET=0 ports.
PORT_SLOT_DIR=""
if [ -z "${RUN_PORT_OFFSET+x}" ]; then
  for slot in $(seq 0 29); do
    if mkdir "/tmp/sei-bench-port-slot-${slot}" 2>/dev/null; then
      RUN_PORT_OFFSET=$((1000 + slot * 1000))
      PORT_SLOT_DIR="/tmp/sei-bench-port-slot-${slot}"
      break
    fi
  done
  if [ -z "$PORT_SLOT_DIR" ]; then
    echo "ERROR: Could not claim a port offset slot (all 30 slots in use)"
    exit 1
  fi
else
  RUN_PORT_OFFSET=${RUN_PORT_OFFSET}
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
echo "  Scenarios:        $NUM"
echo "  Duration:         ${DURATION}s"
echo "  GIGA_EXECUTOR:    $GIGA_EXECUTOR"
echo "  GIGA_OCC:         $GIGA_OCC"
echo "  TXS_PER_BATCH:    $BENCHMARK_TXS_PER_BATCH"
echo "  RUN_ID:           $RUN_ID"
echo "  RUN_PORT_OFFSET:  $RUN_PORT_OFFSET"
echo "  BASE_DIR:         $BASE_DIR"
echo ""
for i in $(seq 0 $((NUM-1))); do
  echo "  [$((i+1))] ${LABELS[$i]} = ${COMMITS[$i]}"
done
echo ""

WORKTREE_DIRS=()

cleanup() {
  echo ""
  echo "Cleaning up..."
  for i in $(seq 0 $((NUM-1))); do
    label="${LABELS[$i]}"
    pkill -f "seid-${label}" 2>/dev/null || true
  done
  sleep 2
  # Remove any leftover worktrees
  for wt in "${WORKTREE_DIRS[@]}"; do
    if [ -d "$wt" ]; then
      git -C "$REPO_ROOT" worktree remove --force "$wt" 2>/dev/null || true
    fi
  done
  # Release port slot lock
  if [ -n "$PORT_SLOT_DIR" ] && [ -d "$PORT_SLOT_DIR" ]; then
    rmdir "$PORT_SLOT_DIR" 2>/dev/null || true
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

  WORKTREE_DIR="$BASE_DIR/$label/worktree"
  echo -n "  Building [$label] from ${commit}... "
  git -C "$REPO_ROOT" worktree add --detach "$WORKTREE_DIR" "$commit" 2>/dev/null
  WORKTREE_DIRS=("${WORKTREE_DIRS[@]}" "$WORKTREE_DIR")

  # Forward DB_BACKEND build tags if needed
  BUILD_TAGS=""
  case "$DB_BACKEND" in
    cleveldb) BUILD_TAGS="cleveldb" ;;
    rocksdb)  BUILD_TAGS="rocksdb" ;;
  esac

  GOBIN_DIR="$BASE_DIR/$label/bin"
  mkdir -p "$GOBIN_DIR"
  if [ -n "$BUILD_TAGS" ]; then
    GOBIN="$GOBIN_DIR" COSMOS_BUILD_OPTIONS="$BUILD_TAGS" make -C "$WORKTREE_DIR" install-bench > /dev/null 2>&1
  else
    GOBIN="$GOBIN_DIR" make -C "$WORKTREE_DIR" install-bench > /dev/null 2>&1
  fi
  mv "$GOBIN_DIR/seid" "$BASE_DIR/$label/seid-${label}"
  rm -rf "$GOBIN_DIR"

  echo "done"
done
# Worktrees are kept alive — the seid binary's @rpath references libwasmvm.dylib
# inside the worktree. The EXIT trap cleans them up when the script finishes.
echo ""

# ============================================================
# Phase 2: Initialize chains (sequential — delegates to benchmark.sh)
# ============================================================
echo "=== Phase 2: Initializing chains ==="

for i in $(seq 0 $((NUM-1))); do
  label="${LABELS[$i]}"
  seid="$BASE_DIR/$label/seid-${label}"
  home_dir="$BASE_DIR/$label/data"
  port_offset=$((RUN_PORT_OFFSET + i * 100))

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
  port_offset=$((RUN_PORT_OFFSET + i * 100))

  BENCHMARK_PHASE=start \
  DURATION=0 \
  SEI_HOME="$home_dir" \
  BASE_DIR="$BASE_DIR/$label" \
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
echo "=== Capturing profiles (30s CPU + 30s fgprof + heap + goroutine + block + mutex) ==="
set +e
PROFILE_CAPTURE_START=$(date +%s)

# Capture CPU across all nodes first, then fgprof, then remaining profiles.
# CPU and fgprof cannot run together on the same process.
for i in $(seq 0 $((NUM-1))); do
  label="${LABELS[$i]}"
  port_offset=$((RUN_PORT_OFFSET + i * 100))
  PPROF_PORT=$((6060 + port_offset))
  profile_dir="$BASE_DIR/$label/pprof"
  mkdir -p "$profile_dir"

  # CPU profile (30s) — on-CPU time only
  echo "  [$label] capturing cpu"
  curl -s "http://localhost:${PPROF_PORT}/debug/pprof/profile?seconds=30" \
    -o "$profile_dir/cpu.pb.gz"
done

for i in $(seq 0 $((NUM-1))); do
  label="${LABELS[$i]}"
  port_offset=$((RUN_PORT_OFFSET + i * 100))
  PPROF_PORT=$((6060 + port_offset))
  profile_dir="$BASE_DIR/$label/pprof"

  # fgprof (30s) — wall-clock time (on-CPU + off-CPU/I/O/blocking)
  echo "  [$label] capturing fgprof"
  curl -s "http://localhost:${PPROF_PORT}/debug/fgprof?seconds=30" \
    -o "$profile_dir/fgprof.pb.gz"
done

for i in $(seq 0 $((NUM-1))); do
  label="${LABELS[$i]}"
  port_offset=$((RUN_PORT_OFFSET + i * 100))
  PPROF_PORT=$((6060 + port_offset))
  profile_dir="$BASE_DIR/$label/pprof"

  echo "  [$label] capturing heap/goroutine/block/mutex"

  # Heap snapshot
  curl -s "http://localhost:${PPROF_PORT}/debug/pprof/heap?debug=0" \
    -o "$profile_dir/heap.pb.gz"

  # Goroutine dump
  curl -s "http://localhost:${PPROF_PORT}/debug/pprof/goroutine?debug=0" \
    -o "$profile_dir/goroutine.pb.gz"

  # Block profile (time waiting on channels/mutexes)
  curl -s "http://localhost:${PPROF_PORT}/debug/pprof/block?debug=0" \
    -o "$profile_dir/block.pb.gz"

  # Mutex contention profile
  curl -s "http://localhost:${PPROF_PORT}/debug/pprof/mutex?debug=0" \
    -o "$profile_dir/mutex.pb.gz"

  echo "  [$label] capturing from localhost:${PPROF_PORT} -> $profile_dir/"
done

echo "  profile capture complete."

set -e

PROFILE_CAPTURE_END=$(date +%s)
PROFILE_CAPTURE_SECS=$(( PROFILE_CAPTURE_END - PROFILE_CAPTURE_START ))

# Verify profile sizes
for i in $(seq 0 $((NUM-1))); do
  label="${LABELS[$i]}"
  profile_dir="$BASE_DIR/$label/pprof"
  cpu_size=$(wc -c < "$profile_dir/cpu.pb.gz" 2>/dev/null | tr -d ' ' || echo 0)
  fgprof_size=$(wc -c < "$profile_dir/fgprof.pb.gz" 2>/dev/null | tr -d ' ' || echo 0)
  heap_size=$(wc -c < "$profile_dir/heap.pb.gz" 2>/dev/null | tr -d ' ' || echo 0)
  goroutine_size=$(wc -c < "$profile_dir/goroutine.pb.gz" 2>/dev/null | tr -d ' ' || echo 0)
  block_size=$(wc -c < "$profile_dir/block.pb.gz" 2>/dev/null | tr -d ' ' || echo 0)
  mutex_size=$(wc -c < "$profile_dir/mutex.pb.gz" 2>/dev/null | tr -d ' ' || echo 0)
  echo "  [$label] cpu=${cpu_size}B fgprof=${fgprof_size}B heap=${heap_size}B goroutine=${goroutine_size}B block=${block_size}B mutex=${mutex_size}B"
done

# Wait remaining time
REMAINING=$(( DURATION - PPROF_WAIT - PROFILE_CAPTURE_SECS ))
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
echo "Profiles:  $BASE_DIR/<label>/pprof/{cpu,fgprof,heap,goroutine,block,mutex}.pb.gz"
echo ""
echo "Compare CPU (on-CPU only):"
echo "  go tool pprof -top -diff_base $BASE_DIR/<baseline>/pprof/cpu.pb.gz $BASE_DIR/<candidate>/pprof/cpu.pb.gz"
echo "Compare wall-clock (on-CPU + off-CPU/I/O):"
echo "  go tool pprof -top -diff_base $BASE_DIR/<baseline>/pprof/fgprof.pb.gz $BASE_DIR/<candidate>/pprof/fgprof.pb.gz"
echo "Compare allocs:"
echo "  go tool pprof -alloc_space -top -diff_base $BASE_DIR/<baseline>/pprof/heap.pb.gz $BASE_DIR/<candidate>/pprof/heap.pb.gz"
echo "Interactive flamegraph (pick any profile):"
echo "  go tool pprof -http=:8080 -diff_base $BASE_DIR/<baseline>/pprof/cpu.pb.gz $BASE_DIR/<candidate>/pprof/cpu.pb.gz"
