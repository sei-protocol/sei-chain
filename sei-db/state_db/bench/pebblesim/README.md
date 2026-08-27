# pebblesim

Writes synthetic EVM storage-slot updates into a real PebbleDB SS instance once per simulated
block, so Pebble's own compaction/flush/disk metrics can be watched over a sustained run. Storage
keys use the real EVM layout (`0x03 || address(20) || slot(32)`) against a fixed pool of simulated
contracts, so slots get revisited and accumulate real version history rather than growing the
keyspace forever.

## Run

```bash
go run ./sei-db/state_db/bench/pebblesim/cmd/pebblesim \
  -dir ./pebblesim-data \
  -batch-size 1000 \
  -interval 500ms \
  -metrics-addr :9099
```

Defaults already match the above: one batch of 1,000 storage slots per tick, ticking every 500ms
— i.e. one simulated block. `-dir` resumes from whatever version is already there, so stopping and
restarting is safe. `Ctrl-C` stops cleanly and prints the final version, total slots written, and
total deadline misses.

Run `-h` for the full flag list (`-contracts`, `-slots-per-contract`, `-seed`).

## Are you missing the block deadline?

Each batch is timed end to end (build keys, apply the changeset, advance the version) against
`-interval` — the block time. But that total also includes this benchmark's own random
key/value generation, which isn't Pebble's cost to answer for, so the timing is split in two:
`Total` (everything) and `Write` (just `ApplyChangesetSync`/`SetLatestVersion`). Deadline misses
are judged on `Total`, since that's the real budget a block has to fit in — but every console line
prints `write Xms, generate Yms` so you can immediately tell which one is actually eating the
budget. `CannedRandom` (the key/value generator) is designed to be near-zero-cost, but "designed
to be" isn't the same as "measured to be" — this is why both numbers are there instead of just
trusting that.

On the console, a miss prints `MISSED DEADLINE` instead of the normal per-version line, with how
far over budget it ran and a running total. The same signal is in Prometheus/Netdata as
`pebblesim_deadline_misses_total` (a counter — `rate()` it to see how often you're missing) and
`pebblesim_batch_duration_seconds` / `pebblesim_write_duration_seconds` (histograms — compare
their distributions against your `-interval` budget and against each other). Since the ticker's
channel only ever buffers one pending tick, falling behind doesn't queue up a burst of catch-up
writes — it just shows up directly as slower, over-budget batches.

## Wiring up Netdata

Metrics are served at `http://localhost:9099/metrics` in Prometheus text format — this is just an
HTTP handler inside the process above, not a Prometheus server. Point Netdata's generic Prometheus
collector at it:

```yaml
# go.d/prometheus.conf
jobs:
  - name: pebblesim
    url: http://127.0.0.1:9099/metrics
```

Nothing else to install if you got Netdata via the official kickstart script, the Docker image, or
Homebrew — `go.d.plugin` ships bundled. If you installed via a Linux distro's native `apt`/`yum`
repo, that plugin was split into a separate `netdata-plugin-go` package in recent Netdata versions
— check it's present (or just reinstall `netdata` with recommended packages enabled).

## What you get

- `pebble_compaction_*`, `pebble_flush_*`, `pebble_sstable_*` (per level), `pebble_memtable_*`,
  `pebble_wal_size`, cache hit/miss, and `pebble_apply_changeset_latency` — emitted automatically
  by the SS engine every 10s, no extra code.
- `pebblesim_data_dir_size_bytes` / `pebblesim_data_dir_available_bytes` — your literal disk-growth
  number, polled every 5s.
- `pebblesim_process_write_bytes_total` (Linux only) — actual physical bytes this process has
  written to disk. Compare against `pebble_flush_bytes_written + pebble_compaction_bytes_written`
  (the logical bytes Pebble itself accounts for) to see write amplification directly.
- `pebblesim_batch_duration_seconds`, `pebblesim_write_duration_seconds`, and
  `pebblesim_deadline_misses_total` — whether each simulated block is finishing inside its
  `-interval` budget, and whether Pebble or key/value generation is to blame if not. See "Are you
  missing the block deadline?" above.

## Insertion throughput

`pebblesim_slots_written_total` is a counter of individual storage-slot writes (not batches).
Throughput in inserts/sec is a query, not a metric the app precomputes and exposes itself —
that's the idiomatic Prometheus way, since the app's own window would be arbitrary and Prometheus
already does correctly-windowed rates at query time:

```promql
rate(pebblesim_slots_written_total[1m])   # inserts/sec
rate(pebblesim_batches_written_total[1m]) # batches/sec — should track ~1/(-interval) unless missing deadlines
```

## Pebble's on-disk size

Three places to look, from most to least aggregated:

- `pebblesim_data_dir_size_bytes` — actual filesystem usage of the whole data directory
  (SSTables + WAL + manifest, everything). Simplest single number for "how big is it."
- `sum(pebble_sstable_total_size)` — just the SSTable data Pebble itself accounts for, summed
  across all 7 levels (each series carries a `level` label — drop `sum()` to see the shape across
  levels, which tells you whether growth is landing in the bottom level as expected).
- `pebble_wal_size` and `pebble_memtable_total_size` — the not-yet-flushed portion; small and
  bounded in steady state, so a sustained upward trend here (rather than sawtoothing back down on
  each flush) is itself a sign flushes are falling behind.

## Watching for degradation as the store grows

Since `pebblesim_write_duration_seconds` and `pebblesim_data_dir_size_bytes` are both plain time
series, "does it get worse as data grows" is a question you answer by graphing them over the same
long time range, not a metric by itself. Plot p50 and p99 write latency together with data-dir
size:

```promql
histogram_quantile(0.50, rate(pebblesim_write_duration_seconds_bucket[5m]))
histogram_quantile(0.99, rate(pebblesim_write_duration_seconds_bucket[5m]))
pebblesim_data_dir_size_bytes
```

If p99 climbs alongside the size (rather than staying flat while size grows), that's compaction
falling behind — cross-check against `pebble_compaction_bytes_written`/`pebble_compaction_count`
rate and `pebble_sstable_count{level="0"}` (rising L0 file count is the earliest warning sign,
since it's what triggers write stalls) to see whether compaction is the actual cause.
