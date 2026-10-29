# CLOB benchmark on Harbor

Reproduces the Autobahn EVM-only CLOB benchmark: 8 validators on dedicated c7i.16xlarge hosts, 8 sei-load
pods offering 100k tx/s of CLOB orders. This branch sustains about 67-70k TPS chain-wide.

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

`seinetwork.yaml` carries every app.toml key the run used:

| Key | Run value | Effect |
|---|---|---|
| `giga.execution.flatkv_storage_view_shards` | 64 | FlatKV storage view shards (default 8); fewer readers blocked behind one shard's writer |
| `giga.execution.flatkv_view_shards` | 64 | Same for the account database |
| `giga.execution.dummy_app_hash` | true | FinalizeBlock's app hash leaves out the state changes; the HashVault app hash replaces it |
| `giga.execution.min_proposal_lane_blocks`, `max_proposal_wait_ms` | 5, 50 | Superblock shape |
| `giga.execution.lane_capacity`, `max_execution_lag` | 1000, 80 | Lane depth and the execution lag at which producers refuse new transactions |
| `giga.execution.gc_percent`, `parse_workers` | 400, 8 | GC target and sender-recovery workers |
| `giga.storage.*_fsync_disabled_unsafe` | true | Load-test only: block store and HashVault skip fsync |

## What the branch contains

Latest main, merged with:

- Superblocks (#4648) and the throughput patches it was benchmarked with: prepare-ahead (#4612), parallel FlatKV
  changeset encoding (#4611), batched admission (#4608), producer admission and nonce-read changes, the
  proposal-shape, lane-capacity and execution-lag knobs, and partitioned FlatKV changeset conversion.
- The FlatKV storage view shard count knob.
- The pending overlay of the previous block's changes indexed off the main loop.
- FlatKV view version retirement in short lock holds, with shards retired in parallel.
- A cache-miss reader released before its value is installed in the read cache.
- The dummy app hash knob.

## Results on this setup

| Change | Chain TPS |
|---|---|
| Superblock integration, 8 FlatKV storage shards | 43k |
| + 64 storage shards | 60-64k |
| + overlay prebuild, retirement in short lock holds retired in parallel, early cache-miss release | 66-69k |
| + dummy app hash | 67-68k |

Where the main loop spends its time (about 12.6 us per transaction): speculation 6.1, validation 2.4, merge 1.1,
waiting for the previous block's FlatKV commit 1.0, pending overlay 0.7, encoding 0.5. The validator that
receives the least RPC traffic is usually the slowest, since it recovers every sender during block preparation
alongside speculation.
