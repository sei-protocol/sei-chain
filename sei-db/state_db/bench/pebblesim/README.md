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
-queue-depth 4
```

