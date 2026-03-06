# debug_trace benchmark script (minimal path)

This script provides a lightweight way to measure `debug_trace` performance over time with varying block transaction counts.

Script: `scripts/debug_trace_bench.sh`

## What it does

1. Scans recent blocks (`eth_getBlockByNumber`) and picks candidate blocks by tx-count bins.
2. Repeatedly runs:
   - `debug_traceBlockByNumber`, or
   - `debug_traceBlockByHash`
3. Writes one CSV row per trace request, including:
   - timestamp
   - selected bin
   - block height/hash
   - tx count
   - latency
   - response size
   - success/error details

## Requirements

- `curl`
- `jq`

## Examples

Quick test (vary tx-density and run fixed request count):

```bash
./scripts/debug_trace_bench.sh \
  --rpc-url http://127.0.0.1:8545 \
  --bins "1-25,26-100,101-1000000" \
  --samples-per-bin 1 \
  --scan-back 2000 \
  --iterations 120 \
  --output ./trace_quick.csv
```

Long soak (degradation check over time):

```bash
./scripts/debug_trace_bench.sh \
  --rpc-url http://127.0.0.1:8545 \
  --bins "1-25,26-100,101-1000000" \
  --samples-per-bin 2 \
  --scan-back 10000 \
  --duration-sec 3600 \
  --sleep-ms 250 \
  --output ./trace_soak.csv
```

Scan-only (inspect candidate blocks without tracing):

```bash
./scripts/debug_trace_bench.sh --scan-only --scan-back 5000
```

## Suggested workflow

1. Start benchmark load generation (optional) to shape block tx counts:
   - `scripts/benchmark.sh` with desired `BENCHMARK_TXS_PER_BATCH`.
2. Run this script for one or more phases (different bins / durations).
3. Compare `latency_sec` by:
   - `target_bin` (tx density impact)
   - timestamp progression (time-based degradation)
