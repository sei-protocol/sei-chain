# Benchmark

## Single scenario

```bash
GIGA_EXECUTOR=true GIGA_OCC=true DEBUG=true benchmark/benchmark.sh
```

TPS is logged every 5s as `tps=<value>` (with ANSI color codes). Extract with:

```bash
grep "tps=" /tmp/sei-benchmark.log | sed 's/\x1b\[[0-9;]*m//g' | sed -n 's/.*tps=\([0-9.]*\).*/\1/p'
```

## Environment variables

| Var | Default | Purpose |
|-----|---------|---------|
| `SEI_HOME` | `$HOME/.sei` | Final chain data dir. If != ~/.sei, init in ~/.sei then `mv` |
| `PORT_OFFSET` | `0` | Added to all ports (RPC, P2P, pprof, gRPC, etc.) |
| `SEID_BIN` | `""` | Pre-built binary path. If set, skip build + copy to ~/go/bin/seid |
| `BENCHMARK_PHASE` | `all` | `init` (build+init+configure), `start` (run node), `all` (both) |
| `LOG_FILE` | `""` | Redirect seid output to file |
| `BENCHMARK_CONFIG` | `benchmark/scenarios/evm.json` | Scenario config file |
| `BENCHMARK_TXS_PER_BATCH` | `1000` | Transactions per batch |
| `GIGA_EXECUTOR` | `false` | Enable evmone-based EVM executor |
| `GIGA_OCC` | `false` | Enable OCC for Giga Executor |
| `DB_BACKEND` | `goleveldb` | Database backend (goleveldb, memdb, cleveldb, rocksdb) |
| `DEBUG` | `false` | Print all log output without filtering |

## Parallel multi-scenario comparison

Use `benchmark/benchmark-compare.sh` to run multiple git commits side-by-side:

```bash
benchmark/benchmark-compare.sh \
  pre-opt=fd2e28d74 \
  lazy-cms=82acf458d \
  lazy-cms-fix=37a17fd02
```

Each scenario gets its own binary, home dir, and port set (offset by 100). Results are printed at the end with median/avg/min/max TPS. Raw data in `/tmp/sei-bench/<label>/tps.txt`.

Set `DURATION=600` (default) to control how long each run lasts.

## Comparing results across sessions

Cross-session benchmark numbers (TPS, total allocs) are not directly comparable. Only comparisons within the same `benchmark-compare.sh` run are valid, since all scenarios share identical conditions.

## Comparing pprof profiles

Always use `pprof -diff_base` to compare profiles between benchmark runs. Never compare profiles side-by-side manually.

```bash
# CPU diff (positive = regression, negative = improvement)
go tool pprof -top -diff_base /tmp/sei-bench/<baseline>/pprof/cpu.pb.gz /tmp/sei-bench/<candidate>/pprof/cpu.pb.gz

# Allocation diff
go tool pprof -alloc_space -top -diff_base /tmp/sei-bench/<baseline>/pprof/heap.pb.gz /tmp/sei-bench/<candidate>/pprof/heap.pb.gz
```
