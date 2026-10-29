# CLOB benchmark on Harbor

Reproduces the Autobahn EVM-only CLOB benchmark: 8 validators on dedicated c7i.16xlarge hosts, 8 sei-load
pods offering 100k tx/s of CLOB orders. This branch reached about 69k TPS chain-wide, with most validators
able to execute 75-90k.

## Workload

sei-load branch `yzang/clob-fixed-markets` (image `ghcr.io/sei-protocol/sei-load:sha-b2faf076...`) runs the
`clob` scenario from sei-load #161 against contract `CLOBv1`:

- 50,000 markets, two price levels, FIFO queues; each `placeNext` call is one step of a 6-step order cycle
  (four resting asks, a taker that walks both levels, a clearing taker).
- `markets` mode gives each sender a fixed market by pool position: 400k accounts, 4 traders per market,
  50k sends apart, so a block has no conflicts and the hot key set stays fixed.
- About 117k gas and 150 bytes per transaction. Every resting order writes two never-written slots, so a
  transaction averages about 1.5 storage cache misses.

## Running it

1. Build the image from this branch through the `ecr.yml` workflow, which tags it `mock-<sha>`:

   ```bash
   gh workflow run ecr.yml --repo sei-protocol/sei-chain --ref main -f ref=<sha> -f tag=<sha>
   ```

2. Fill `NAMESPACE`, `CHAIN_ID` and `IMAGE` in `seinetwork.yaml` and commit it to your Harbor engineer
   workspace. Re-spinning a chain with the same chain ID needs its genesis artifacts removed from
   `s3://harbor-sei-k8s-genesis-artifacts/<CHAIN_ID>/` first, and validators are torn down and re-spun rather
   than restarted in place.

3. Once the chain is advancing, build the bench from the live committee:

   ```bash
   kubectl -n NAMESPACE exec CHAIN_ID-0-0 -c seid -- cat /home/nonroot/.sei/config/autobahn.json > autobahn.json
   python3 add_shard_routing.py seiload-clob.yaml bench.yaml autobahn.json
   ```

   Fill `NAMESPACE`, `CHAIN_ID`, `RUN_ID` (lowercase) and `COMMIT` in `bench.yaml`, then commit it next to the
   chain. Each pod requests 5 CPU and peaks near 5 cores.

4. After at least 5 minutes of load, summarize a 5-minute window:

   ```bash
   kubectl -n monitoring port-forward svc/sei-harbor-prometheus 19090:9090 &
   NAMESPACE=... CHAIN_ID=... python3 measure.py "my run" 5m
   ```

   `chain TPS` is the executed rate; `capacity k` is each validator's executed rate divided by the share of
   time its main loop was busy, so the lowest capacity is what the chain can sustain.

## Configuration

`seinetwork.yaml` carries every app.toml key the run used. The knobs this branch adds, all off unless set:

| Key | Run value | Effect |
|---|---|---|
| `giga.execution.flatkv_storage_view_shards` | 256 | FlatKV storage view shards; fewer write-lock collisions between cache misses, commits and readers |
| `giga.execution.flatkv_view_shards` | 64 | Same for the account database |
| `giga.execution.flatkv_read_pool_workers_per_queue` | 8 | Splits the FlatKV read pool into several queues instead of one shared channel |
| `giga.execution.dummy_app_hash` | true | FinalizeBlock's app hash leaves out the state changes; the HashVault app hash replaces it |
| `giga.execution.min_proposal_lane_blocks`, `max_proposal_wait_ms` | 5, 50 | Superblock shape |
| `giga.execution.lane_capacity`, `max_execution_lag` | 1000, 80 | Lane depth and the execution lag at which producers refuse new transactions |
| `giga.storage.*_fsync_disabled_unsafe` | true | Load-test only: block store and HashVault skip fsync |
| `giga.execution.flatkv_inline_miss_reads` | off | Reads cache misses on the caller; measured neutral |
| `giga.execution.mutex_profile_fraction` | off | Enables the mutex profile on the pprof port |
| `giga.execution.occ_workers` | default (GOMAXPROCS) | 32 and 128 measured no better than 64 |

## What the branch contains

Latest main, merged with:

- Superblocks (#4648), and the open throughput PRs it was benchmarked with: prepare-ahead (#4612), parallel
  FlatKV changeset encoding (#4611), batched admission (#4608), plus producer admission and nonce-read changes.
- Executor: pending-overlay index built off the main loop, contract code hash from the account row, tx hash at
  decode, sender reuse from CheckTx, account warming, OCC storage split by slot range so one hot contract
  spreads over workers.
- FlatKV and its view: partitioned changeset conversion, parallel and chunked version retirement, heap merge of
  shard diffs with per-shard parallel sorting, capped per-call shard fan-out, early release of cache-miss
  readers, the sharded read pool, and the lattice hash built with `GOEXPERIMENT=simd` (AVX-512).
- Metrics: OCC speculation worker time and per-step longest-worker time, FlatKV flush stages and lifecycle
  phases, Go scheduler latency, mutex wait and CPU classes.

## Results on this setup

| Change | Chain TPS |
|---|---|
| Superblocks + #4665, 10 validators | 59k |
| + parallel FlatKV version retirement | 69k |
| 8 validators (the AWS vCPU quota ruled out 10) | 66-67k |
| + dummy app hash, sharded read pool, heap flush merge, 256 storage shards | 68-69k |

Where the main loop spends its time at 69k (about 12.5 us per transaction): speculation 5.7, validation 2.4,
waiting for the previous block's FlatKV commit 1.2-2.0, merge 0.8, pending overlay 0.7, encoding 0.5. The
validator that receives the least RPC traffic is usually the slowest, since it recovers every sender during
block preparation alongside speculation.
