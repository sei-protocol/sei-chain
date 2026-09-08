# pebblesim

Writes a synthetic mix of EVM storage-slot, balance, and nonce updates (60%/25%/15%) into a real
PebbleDB SS instance once per simulated block, so Pebble's own compaction/flush/disk metrics can
be watched over a sustained run. Storage and nonce keys use the real EVM layout (`0x03 ||
address(20) || slot(32)` and `0x0a || address(20)` respectively) against a fixed pool of simulated
addresses, so keys get revisited and accumulate real version history rather than growing the
keyspace forever. Balances don't have a production key format yet (they still live in the
tendermint/IAVL store, not this SS layer) — `randomBalanceKey` in `pebblesim.go` uses
`evmss.StoreBalance`, the sub-DB type the codebase already reserves for them, as the placeholder
prefix.

Key/value generation runs on its own goroutine, feeding pre-built batches to the writer through a
channel (`-queue-depth` batches deep) so Pebble's write throughput isn't gated by generation cost.
`pebblesim_stall_duration_seconds` (and the `stall` figure in each log line) reports how long the
writer waited for a batch — non-zero means generation, not Pebble, is the bottleneck.

`-reads-per-second` (0 by default) runs `-read-workers` goroutines issuing random reads against
the store, combined at that rate through a single shared limiter. Reads don't re-derive a key the
same way writes do — `-slots-per-contract` can run into the hundreds of millions, so a uniformly
random slot index would almost always miss. Instead every read samples from `readPool`, a bounded
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


```bash
go run ./cmd/pebblesim \
-dir ./pebblesim-data \
-batch-size 500000 \
-interval 1000ms \
-metrics-addr :9099 \
-contracts 10000 \
-slots-per-contract 100000000 \
-queue-depth 4 -presort \
-reads-per-second 5000 -read-workers 8
```

nohup go run ./cmd/pebblesim -dir ./pebblesim-data -batch-size 500000 -interval 1000ms -metrics-addr :9099 -contracts 10000 -slots-per-contract 100000000 -queue-depth 4 -presort -reads-per-second 5000 -read-workers 8 > logs.txt 2>&1 < /dev/null &