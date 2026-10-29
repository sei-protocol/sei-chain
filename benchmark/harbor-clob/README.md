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

## Exact setup

### Cluster and hosts

| Item | Value |
|---|---|
| Cluster | Harbor EKS, `eu-central-1` |
| Validators | 8, one per host (`scheduling.nodeIsolation: Dedicated`). 10 was the earlier setup; the account's EC2 vCPU quota ruled it out |
| Validator host | `c7i.16xlarge` on demand: 64 vCPU (32 cores with hyper-threading), 128 GiB, Karpenter node pool `sei-validator` (taint `sei.io/workload=sei-validator:NoSchedule`) |
| Host OS | Bottlerocket 1.66.0 (aws-k8s-1.34), kernel 6.12 |
| `seid` container | requests 60 CPU and 100 GiB, memory limit 100 GiB, no CPU limit, so `GOMAXPROCS` is 64; the controller sets `GOMEMLIMIT` to about 87 GiB |
| Data volume | 1000 GiB gp3 through the EBS CSI driver, `volumeAttributesClassName: sei-gp3-performance-10k-1000-v1` (10,000 IOPS, 1,000 MB/s) |
| Sidecar | `sei-sidecar:5fd7f46a`, `seictl` preset `genesis-chain` v0.0.72 |
| Load hosts | Karpenter node pool `default` (c, m or r family, 4 to 32 vCPU, amd64, on demand), spread one pod per host where possible |

### Chain

| Item | Value |
|---|---|
| Consensus | Autobahn, `evmOnly: true` |
| Chain IDs | Cosmos `CHAIN_ID`; EVM chain ID 713715 |
| Genesis balances | 1e21 usei, uusdc and uatom per genesis account; staking 10,000,000 usei per validator, equal voting power |
| Block gas | `consensusParams.block.max_gas` and `max_gas_wanted` 30,000,000,000 per superblock, far above what a superblock uses (about 0.65 billion) |
| Min gas price | 1 gwei (`giga.execution.min_gas_price`, default) |
| fsync | Off for the block store and HashVault (load test only) |

### Superblock and lane settings

A validator's producer seals a lane block every `block_interval` or once it holds `max_txs_per_block`
transactions. The leader proposes a superblock that covers the certified lane blocks; the proposal settings
decide how many it waits for.

| Setting | Where | Run value | Default |
|---|---|---|---|
| Lane block interval | `spec.consensus.autobahn.blockInterval` | 400ms | set in the manifest |
| Transactions per lane block | `spec.consensus.autobahn.maxTxsPerBlock` | 2000 | set in the manifest |
| Empty lane blocks | `spec.consensus.autobahn.allowEmptyBlocks` | true | set in the manifest |
| View timeout | autobahn.json `view_timeout`, written by seictl | 1.5s | seictl's value |
| Lane blocks a proposal waits for | `giga.execution.min_proposal_lane_blocks` | 5 | 1 |
| Longest a proposal waits for them | `giga.execution.max_proposal_wait_ms` | 50 | 0 |
| Lane capacity, in lane blocks | `giga.execution.lane_capacity` | 1000 | 300 |
| Execution lag, in superblocks, at which producers refuse new transactions | `giga.execution.max_execution_lag` | 80 | 0 (off) |
| Superblocks prepared ahead of execution | code constant `prepareAheadBlocks` | 3 | 3 |

With these values the run produced about 12 superblocks a second, each covering about 3 lane blocks and
5,000-5,600 transactions. Larger superblocks (10 lane blocks, 100ms) measured slower per transaction.

### Execution and storage knobs

| Setting | Run value | Default |
|---|---|---|
| `giga.execution.occ_workers` | unset (`GOMAXPROCS`, 64) | 0 |
| `giga.execution.parse_workers` | 8 | `GOMAXPROCS` |
| `giga.execution.gc_percent` | 400 | Go default |
| `giga.execution.flatkv_view_shards` (account database) | 64 | 8 |
| `giga.execution.flatkv_storage_view_shards` | 64 | 8 |
| `giga.execution.dummy_app_hash` | true | false |
| `giga.storage.hash_vault_fsync_disabled_unsafe`, `block_store_fsync_disabled_unsafe` | true | false |

### Load

| Setting | Value |
|---|---|
| Pods | 8 (Job, indexed), `--tps=12500` each, 100k tx/s offered |
| In flight | `--max-in-flight=24000` per pod |
| Accounts | 400,000, `disjoint: true`, routed to their owning validator by `shardRouting` |
| Scenario | `clob`, `markets: 50000`, `forceDeploy: true` |
| Gas limit | eth_estimateGas of one `placeNext` against the probe market, times `gasMargin` 1.20 (sei-load default): about 204k per transaction, of which about 117.6k is used |
| Gas price | fee cap is eth_gasPrice times `gasFeeCapMultiplier` 15 (sei-load default), or `gasFeeCapFallbackWei` 20 gwei when the node serves no gas price |
| Receipts and blocks | not tracked (`trackReceipts`, `trackBlocks` false) |
| Pod resources | requests 5 CPU and 2 GiB, memory limit 6 GiB, `GOMAXPROCS=14` |
| Duration | 30 minutes; the first 5 are warm-up and excluded from measurement |

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
