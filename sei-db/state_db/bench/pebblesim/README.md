# pebblesim

Writes a synthetic mix of EVM storage-slot, balance, and nonce updates (60%/25%/15%) into a real
PebbleDB SS instance once per simulated block, so Pebble's own compaction/flush/disk metrics can
be watched over a sustained run. Storage and nonce keys use the real EVM layout (`0x03 ||
address(20) || slot(32)` and `0x0a || address(20)` respectively). Balances don't have a production
key format yet (they still live in the tendermint/IAVL store, not this SS layer) —
`randomBalanceKey` in `pebblesim.go` uses `evmss.StoreBalance`, the sub-DB type the codebase
already reserves for them, as the placeholder prefix. One code and one codehash write ride along
per block, just so those two sub-DBs see traffic at all.

## New keys versus new versions

What dominates this workload is not how many keys per second get written but whether those keys
are **new versions of keys already there** or **brand-new keys** — so that split is set directly,
by percentage, rather than emerging from how wide some pool happens to be:

| flag | what it sets |
| --- | --- |
| `-new-key-pct` | % of writes that mint a brand-new key. Its reciprocal is the average versions a key accumulates. |
| `-new-contract-pct` | % of *brand-new storage keys* that land on a contract that is also brand new. Its reciprocal is the average slots per contract. `0` holds the contract pool fixed. |
| `-contracts` | contracts that exist at t=0. An initial condition, not a rate: contract creation is far too rare to bootstrap within a run. |

Balances and nonces need no third knob — a brand-new balance or nonce key *is* a new account, so
`-new-key-pct` already governs them. Code and codehash address contracts and don't mint new ones;
at one write per block they'd take the pool nowhere.

This matters most for pruning. A prune pass (`pruneDescending` in `db_engine/pebbledb/mvcc`) is a
**full forward scan of every key**, and `-keep-recent` bounds *versions*, not *keys* — so the
cumulative distinct-key count only ever grows, and with it prune cost. A high `-new-key-pct` is
the pathological case: the scan gets longer and longer while finding almost nothing to delete.
`pebblesim_distinct_keys{kind="slot"|"account"|"contract"}` reports exactly that number, and is
the panel to put next to prune latency.

No key material is held in memory. Every key is named by an integer id and derived from it
(`slotForID`, `contractAddress`, `accountAddress`), so the simulation remembers only how many ids
it has minted and revisiting a key is redrawing its id.

Key/value generation runs on its own goroutine, feeding pre-built batches to the writer through a
channel (`-queue-depth` batches deep) so Pebble's write throughput isn't gated by generation cost.
`pebblesim_stall_duration_seconds` (and the `stall` figure in each log line) reports how long the
writer waited for a batch — non-zero means generation, not Pebble, is the bottleneck.

`-reads-per-second` (0 by default) runs `-read-workers` goroutines issuing random reads against
the store, combined at that rate through a single shared limiter. Reads don't re-derive a key the
same way writes do — at a high `-new-key-pct` the keyspace grows without bound, so a uniformly
random key would almost always miss. Instead every read samples from `readPool`, a bounded
reservoir of `-read-key-pool-capacity` keys (default 100,000) that the write path continuously
feeds with a small random subsample of each batch it commits — so reads are guaranteed to target
keys that actually exist. `pebblesim_read_duration_seconds` reports the read-latency distribution
seen by this benchmark, labeled by `kind` (slot/balance/nonce) and `hit`/`miss` — `hit` should read
as ~100% true, the direct proof reads are landing on real data. The underlying pebbledb wrapper
also emits `pebble_get_latency` for every `Get` it serves, split by physical sub-DB
(`storage`, `nonce`, ...); that one comes for free and isn't duplicated here.


create a machine in ec2: `c5.12xlarge`. disks: TODO.

```
sudo dnf update -y
sudo dnf install -y git golang docker
sudo systemctl enable --now docker
sudo usermod -aG docker ec2-user
newgrp docker
```

```
git clone https://github.com/sei-protocol/sei-chain
cd sei-chain
git checkout ss-write-benchmark
cd sei-db/state_db/bench/pebblesim/
```

in another window
```
ssh -i YOURKEY.pem -N -L 3000:localhost:3000 ec2-user@<EC2_PUBLIC_IP>
```

go to:


## Run

```
../../../../docker/monitornode/scripts/start-prometheus.sh
../../../../docker/monitornode/scripts/start-grafana.sh
../../../../docker/monitornode/scripts/start-node-exporter.sh   # Linux only; host CPU/disk/memory
```


Two configurations, identical except for the two split percentages. Both write the same 500,000
keys/sec; only whether those keys are new differs.

**A — mostly new versions.** A keyspace that stays small, so prune scans little and reclaims a
lot. This is the shape a real chain has.

```bash
go run ./cmd/pebblesim \
-dir ./pebblesim-data \
-batch-size 500000 -interval 1000ms -metrics-addr :9099 \
-contracts 10000 \
-new-key-pct 0.1 \
-new-contract-pct 0.01 \
-keep-recent 200 -prune-interval 60s \
-queue-depth 4 -presort \
-reads-per-second 5000 -read-workers 8
```

**B — mostly new keys.** A keyspace that grows without bound, so prune scans more and more while
finding less and less to delete. This is the pathological case.

```bash
go run ./cmd/pebblesim \
-dir ./pebblesim-data-new \
-batch-size 500000 -interval 1000ms -metrics-addr :9099 \
-contracts 10000 \
-new-key-pct 50 \
-new-contract-pct 5 \
-keep-recent 200 -prune-interval 60s \
-queue-depth 4 -presort \
-reads-per-second 5000 -read-workers 8
```

What each produces, at 500,000 writes/sec (300k slots, 125k balances, 75k nonces):

| | A (`0.1` / `0.01`) | B (`50` / `5`) |
| --- | --- | --- |
| new keys/sec | 500 | 250,000 |
| new slots/sec | 300 | 150,000 |
| new accounts/sec | 200 | 100,000 |
| new contracts/sec | 0.03 | 7,500 |
| versions per key | ~1,000 | ~2 |
| slots per contract | ~10,000 | ~20 |
| distinct keys after 1h | 1.8M | 900M |
| contracts after 1h | 10,108 | 27M |

## Inspecting the result

`seidb inspect` reports what each sub-store physically holds, in the style of `geth db inspect`.
PebbleDB takes a directory lock even read-only, so stop the writer first.

```bash
go run ../../../tools/cmd/seidb inspect -d ./pebblesim-data
go run ../../../tools/cmd/seidb inspect -d ./pebblesim-data --scan
```

The default report is metadata only and returns immediately. `--scan` adds the distinct logical
key count and a version-depth histogram, by making the same full forward scan a prune pass makes.

`ENTRIES` counts key **versions**, since the version is encoded into the on-disk key; `KEYS`
counts distinct logical keys. `ENTRIES/KEYS` is the measured version depth — compare it against
the `100/-new-key-pct` the knob predicts. Expect it to come out lower than the prediction on a
growing keyspace, for two reasons: keys minted late in the run have had less time to accumulate
versions, and a block that writes more slots than the keyspace holds writes some of them twice,
where only the last survives.

`-new-key-pct 100` is the absolute worst case: no key is ever revisited, so prune scans the whole
keyspace and deletes nothing. `-new-key-pct 0 -new-contract-pct 0` is the other end: one key per
space, rewritten forever, maximum version depth.

```
nohup go run ./cmd/pebblesim -dir ./pebblesim-data -batch-size 500000 -interval 1000ms -metrics-addr :9099 -contracts 10000 -new-key-pct 0.1 -new-contract-pct 0.01 -keep-recent 200 -prune-interval 60s -queue-depth 4 -presort -reads-per-second 5000 -read-workers 8 > logs.txt 2>&1 < /dev/null &
```