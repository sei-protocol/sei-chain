# walrussim

A benchmark harness for the WALRUS historical state engine. It generates an EVM-shaped write
workload, feeds it to the engine, and issues historical reads at random block heights while
checking every answer.

It is shaped like `cryptosim`: one binary, one JSON config file, a launcher script, and OTel
metrics exported to Prometheus.

## Running

```bash
cd sei-db/db_engine/walrus/walrussim
./walrussim.sh ./config/basic-config.json
```

The script builds the binary into `bin/` and runs it. `Ctrl-C` stops a run cleanly.

Metrics are served at `http://localhost:9090/metrics` by default. The existing Prometheus and
Grafana setup under `docker/monitornode/scripts` scrapes that address.

## Configs

| File | What it is for |
|---|---|
| `config/basic-config.json` | The reference config. Every field appears in it. |
| `config/write-only.json` | No snapshots, no readers. Measures append throughput without a second database in the write path. |
| `config/deep-history.json` | Mostly cold keys and half the reads against keys nothing writes, which is the deepest walk the engine can be asked for. |

Unknown fields are rejected, so a typo fails the run rather than being silently ignored. Any
field left out of the file takes its default; the run prints the resolved config before it starts.

## What it measures

The headline number is **`walrus_pods_probed`** — how many pods a backwards walk had to test to
answer one query. That is the read amplification the engine pays for never compacting, and
whether it is acceptable is the question this benchmark exists to answer.

Read it alongside:

- `walrus_pods_searched` — the pods a bloom filter failed to rule out. The gap between this and
  the pods that really held the key is what false positives cost.
- `walrus_query_duration` — labeled by `ReadStatus`, which separates a key nothing ever wrote
  (always `absent`, always the deepest walk) from one a class writes.
- `walrus_pod_bytes` / `walrus_index_bytes` / `walrus_bloom_bytes` — the space the schema costs.
- `walrus_pod_build_duration` — the write-side cost of building a pod.
- `walrussim_mismatches` — **must stay at zero.** Anything else means the engine returned a value
  the workload says is wrong.

## The workload

Keys are grouped into classes by write period. A key in a class with period `p` is written at
every block where its id is congruent to the block number modulo `p`, so it is written once every
`p` blocks and a query at a random height finds its last write a uniform `[0, p)` blocks back.

That is the dial. `KeyClasses` sets how far a backwards walk has to travel, which is what the
benchmark is measuring, so sweep it rather than hoping a random workload produces an interesting
tail. `NeverWrittenKeyCount` reserves ids that no class writes; a read against one probes every
pod, reaches the floor snapshot, and returns absent, which is the worst case by construction.

The schedule is deliberately invertible. Because the last write of any key at any block is a
closed form rather than a search, the harness can compute the expected answer to every read in a
few integer operations and verify it — with nothing remembered, at any scale. The expected answer
and the key bytes are computed *before* the timer starts and compared *after* it stops, so
neither enters the measurement.

## Snapshots

With `EnableSnapshots` on, a `statestub` — a flat pebble store standing in for FlatKV — is written
alongside the engine and checkpointed every `SnapshotIntervalBlocks`. The checkpoint is handed to
the engine, which hard-links it and takes ownership; the stub's copy is deleted immediately.

Snapshots are what let retention delete anything. **With `EnableSnapshots` off, nothing is ever
deleted**, however small `RetentionBlocks` is, because a walk that reached the bottom of the
retained pods would have nothing to terminate against. That is also why `write-only.json` is the
mode to measure append throughput in: with snapshots on, the write path is dominated by the stub's
pebble writes rather than by the engine's appends.

## Sizing

The defaults are sized for a workstation, not for the production target. In particular
`TargetPodSize` is far below the 4 GiB the format allows: a pod is built entirely in memory and
`PodBuildConcurrency` of them are built at once, so the peak is roughly
`TargetPodSize * PodBuildConcurrency * 2`. Raise both only as far as the host's memory allows.
