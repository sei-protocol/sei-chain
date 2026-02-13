# Benchmark

## Single scenario

```bash
GIGA_EXECUTOR=true GIGA_OCC=true DEBUG=true benchmark/benchmark.sh
```

TPS is logged every 5s as `tps=<value>` (with ANSI color codes). To capture output and extract TPS:

```bash
LOG_FILE=/tmp/bench.log DEBUG=true benchmark/benchmark.sh

# Extract TPS values
sed 's/\x1b\[[0-9;]*m//g' /tmp/bench.log | sed -n 's/.*tps=\([0-9.]*\).*/\1/p'
```

Available scenarios in `benchmark/scenarios/`: `evm.json` (default), `erc20.json`, `mixed.json`, `default.json`.

```bash
# Use a different scenario
BENCHMARK_CONFIG=benchmark/scenarios/erc20.json benchmark/benchmark.sh
```

## Environment variables

### benchmark.sh

| Var | Default | Purpose |
|-----|---------|---------|
| `BENCHMARK_PHASE` | `all` | `init` (build+init+configure), `start` (run node), `all` (both) |
| `SEI_HOME` | `$HOME/.sei` | Final chain data dir. If != ~/.sei, init in ~/.sei then `mv` |
| `PORT_OFFSET` | `0` | Added to all ports (RPC, P2P, pprof, gRPC, etc.) |
| `SEID_BIN` | `""` | Pre-built binary path. If set, skip build + copy to ~/go/bin/seid |
| `LOG_FILE` | `""` | Redirect seid output to file |
| `BENCHMARK_CONFIG` | `$SCRIPT_DIR/scenarios/evm.json` | Scenario config file (absolute path resolved from script location) |
| `BENCHMARK_TXS_PER_BATCH` | `1000` | Transactions per batch |
| `GIGA_EXECUTOR` | `false` | Enable evmone-based EVM executor |
| `GIGA_OCC` | `false` | Enable OCC for Giga Executor |
| `DB_BACKEND` | `goleveldb` | Database backend (goleveldb, memdb, cleveldb, rocksdb) |
| `MOCK_BALANCES` | `true` | Use mock balances during benchmark |
| `DISABLE_INDEXER` | `true` | Disable indexer for benchmark (reduces I/O overhead) |
| `DEBUG` | `false` | Print all log output without filtering |

### benchmark-compare.sh

Inherits all benchmark.sh vars via delegation. Additionally:

| Var | Default | Purpose |
|-----|---------|---------|
| `DURATION` | `600` | How long (seconds) to run each node before stopping |
| `GIGA_EXECUTOR` | **`true`** | Overrides benchmark.sh default (false) |
| `GIGA_OCC` | **`true`** | Overrides benchmark.sh default (false) |
| `DB_BACKEND` | `goleveldb` | Forwarded to build and init phases |

**Note:** `GIGA_EXECUTOR` and `GIGA_OCC` default to `true` in the compare script but `false` in benchmark.sh. The compare script is designed for performance comparison where Giga Executor is typically enabled.

## Parallel multi-scenario comparison

Use `benchmark/benchmark-compare.sh` to run multiple git commits side-by-side (minimum 2 scenarios required):

```bash
benchmark/benchmark-compare.sh \
  pre-opt=fd2e28d74 \
  lazy-cms=82acf458d \
  lazy-cms-fix=37a17fd02
```

Each scenario gets its own binary, home dir, and port set (offset by 100). Results are printed at the end with median/avg/min/max TPS. Raw data in `/tmp/sei-bench/<label>/tps.txt`.

## Comparing results across sessions

**Important:** Cross-session benchmark numbers (TPS, total allocs) are not directly comparable. Only comparisons within the same `benchmark-compare.sh` run are valid, since all scenarios share identical conditions.

## Profiling

### Available profile types

`benchmark-compare.sh` automatically captures all profile types midway through the run. Profiles are saved to `/tmp/sei-bench/<label>/pprof/`.

| Profile | File | What it shows |
|---------|------|---------------|
| CPU | `cpu.pb.gz` | On-CPU time only (computation, hashing, EVM execution) |
| fgprof | `fgprof.pb.gz` | Wall-clock time: on-CPU + off-CPU (I/O, blocking, GC pauses) |
| Heap | `heap.pb.gz` | Memory allocations (analyzable with multiple metrics, see below) |
| Goroutine | `goroutine.pb.gz` | Goroutine stacks (find pileups and leaks) |
| Block | `block.pb.gz` | Time waiting on channel ops and mutex locks |
| Mutex | `mutex.pb.gz` | Mutex contention (where goroutines fight over locks) |

**CPU vs fgprof:** Go's built-in CPU profiler uses OS-level `SIGPROF` signals delivered only to running threads — goroutines waiting on I/O, channels, or locks are invisible. fgprof samples all goroutines via `runtime.GoroutineProfile` regardless of scheduling state, showing the full wall-clock picture. Use CPU when you suspect pure computation is the bottleneck; use fgprof when TPS is low but CPU utilization is also low (points to I/O or contention).

**Block and mutex profiles** require `runtime.SetBlockProfileRate` and `runtime.SetMutexProfileFraction` to be enabled. Both are automatically enabled when seid is built with the `benchmark` build tag (`make install-bench`). They are disabled in production builds.

### Capturing profiles during single-scenario runs

Single-scenario `benchmark.sh` runs enable the pprof HTTP endpoint but don't auto-capture profiles. Capture manually in another terminal:

```bash
PPROF_PORT=6060  # adjust with PORT_OFFSET if set

# 30-second CPU profile
go tool pprof -http=:8080 "http://localhost:$PPROF_PORT/debug/pprof/profile?seconds=30"

# 30-second fgprof (wall-clock)
go tool pprof -http=:8080 "http://localhost:$PPROF_PORT/debug/fgprof?seconds=30"

# Heap snapshot
go tool pprof -http=:8080 "http://localhost:$PPROF_PORT/debug/pprof/heap"

# Goroutine dump
go tool pprof -http=:8080 "http://localhost:$PPROF_PORT/debug/pprof/goroutine"

# Block profile
go tool pprof -http=:8080 "http://localhost:$PPROF_PORT/debug/pprof/block"

# Mutex contention
go tool pprof -http=:8080 "http://localhost:$PPROF_PORT/debug/pprof/mutex"
```

### Comparing profiles (diff)

Always use `pprof -diff_base` to compare profiles between benchmark runs. Never compare profiles side-by-side manually.

```bash
# CPU diff (positive = regression, negative = improvement)
go tool pprof -top -diff_base /tmp/sei-bench/<baseline>/pprof/cpu.pb.gz /tmp/sei-bench/<candidate>/pprof/cpu.pb.gz

# Wall-clock diff (fgprof)
go tool pprof -top -diff_base /tmp/sei-bench/<baseline>/pprof/fgprof.pb.gz /tmp/sei-bench/<candidate>/pprof/fgprof.pb.gz

# Allocation diff (total bytes allocated over time)
go tool pprof -alloc_space -top -diff_base /tmp/sei-bench/<baseline>/pprof/heap.pb.gz /tmp/sei-bench/<candidate>/pprof/heap.pb.gz

# Heap escape diff (objects that should be stack-allocated)
go tool pprof -alloc_objects -top -diff_base /tmp/sei-bench/<baseline>/pprof/heap.pb.gz /tmp/sei-bench/<candidate>/pprof/heap.pb.gz
```

### Heap profile metrics

The heap profile contains multiple metrics. Choose the right one for your question:

| Metric | Flag | Use when |
|--------|------|----------|
| Active memory | `-inuse_space` | Finding memory leaks or high RSS |
| Active objects | `-inuse_objects` | Finding what's holding memory right now |
| Total allocated bytes | `-alloc_space` | Finding hot allocation paths (GC pressure) |
| Total allocated objects | `-alloc_objects` | Finding heap escapes (objects that should live on the stack) |

### Interactive analysis and flamegraphs

Text output (`-top`) is useful for quick comparisons, but the web UI with flamegraphs is far more effective for navigating large profiles.

```bash
# Interactive web UI with flamegraphs
go tool pprof -http=:8080 /tmp/sei-bench/<label>/pprof/cpu.pb.gz

# Diff flamegraph (red = regression, blue = improvement)
go tool pprof -http=:8080 -diff_base /tmp/sei-bench/<baseline>/pprof/cpu.pb.gz /tmp/sei-bench/<candidate>/pprof/cpu.pb.gz
```

For drilling into specific functions, use the interactive CLI:

```bash
go tool pprof /tmp/sei-bench/<label>/pprof/cpu.pb.gz
(pprof) top20 --cum          # sort by cumulative time (default flat hides expensive callees)
(pprof) list DeliverTx       # line-by-line source attribution
(pprof) web DeliverTx        # SVG graph focused on one function's callers/callees
```

Run `go tool pprof` from the sei-chain repo root so that `list` and `web` commands can resolve source file paths.
