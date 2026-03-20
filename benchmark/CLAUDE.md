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

## Comparing pprof profiles

`benchmark-compare.sh` automatically captures pprof profiles (CPU and heap) midway through the run. Single-scenario runs only enable the pprof HTTP endpoint — capture profiles manually with `curl` or `go tool pprof`.

Always use `pprof -diff_base` to compare profiles between benchmark runs. Never compare profiles side-by-side manually.

```bash
# CPU diff (positive = regression, negative = improvement)
go tool pprof -top -diff_base /tmp/sei-bench/<baseline>/pprof/cpu.pb.gz /tmp/sei-bench/<candidate>/pprof/cpu.pb.gz

# Allocation diff
go tool pprof -alloc_space -top -diff_base /tmp/sei-bench/<baseline>/pprof/heap.pb.gz /tmp/sei-bench/<candidate>/pprof/heap.pb.gz
```
