# walrussim

A benchmark harness for the WALRUS historical state engine. It generates an EVM-shaped write
workload, feeds it to the engine, and issues historical reads at random block heights while
checking every answer.

It is shaped like `cryptosim`: one binary, one JSON config file, a launcher script, and OTel
metrics exported to Prometheus.

## Running

```bash
cd sei-db/db_engine/walrus/walrussim
./walrussim.sh ./config/basic-config.json   # a short local run
./walrussim.sh                              # the defaults, which run until interrupted
```

The script builds the binary into `bin/` and runs it. `Ctrl-C` stops a run cleanly.

Metrics are served at `http://localhost:9090/metrics` by default. The existing Prometheus and
Grafana setup under `docker/monitornode/scripts` scrapes that address, and
`docker/monitornode/dashboards/walrus-dashboard.json` is provisioned automatically:

```bash
docker/monitornode/scripts/start-prometheus.sh
docker/monitornode/scripts/start-grafana.sh
```

Instrument names in the code are exactly the names Prometheus scrapes — the suffixes are spelled
out rather than left to the exporter to append, so a panel expression greps back to the line that
records it.

## Configs

**A config file states only what it changes.** Anything it leaves out keeps the default from
`DefaultConfig()` in `walrussim_config.go`, which is the single place defaults live. There is no
file that restates them all — a copy of the defaults is a copy that goes stale.

| File | What it changes |
|---|---|
| `config/basic-config.json` | A smaller keyspace, smaller pods, frequent snapshots, a tight retention window, and a block count. Finishes in a couple of minutes on a workstation and exercises collection, which the defaults take far longer to reach. |
| `config/write-only.json` | Snapshots and readers off. The mode to measure append throughput in, with no second database in the write path. |
| `config/deep-history.json` | Smaller pods, a ten minute snapshot interval, a retention window sized to hold a terabyte of pods, and half the reads against keys nothing writes. The deepest walk the engine can be asked for. Wants a dedicated machine — see Sizing. |

Unknown fields are rejected, so a typo fails the run rather than being silently ignored. Every run
prints its fully resolved config before it starts, so the output records exactly what produced it.

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
alongside the engine and checkpointed every `SnapshotIntervalSeconds`. The checkpoint is handed to
the engine, which hard-links it and takes ownership; the stub's copy is deleted immediately.

Cadence is wall clock rather than a block count, because a block count only means something once
you know the block rate, and the block rate is what a run is measuring. This costs the engine
nothing: snapshots were always allowed to land at arbitrary heights rather than on pod boundaries,
so a walk already handles a floor that sits in the middle of a pod.

Snapshots are what let retention delete anything. **With `EnableSnapshots` off, nothing is ever
deleted**, however small `RetentionBlocks` is, because a walk that reached the bottom of the
retained pods would have nothing to terminate against. That is also why `write-only.json` is the
mode to measure append throughput in: with snapshots on, the write path is dominated by the stub's
pebble writes rather than by the engine's appends.

## Sizing

`TargetPodSize` is far below the 4 GiB the format allows because a pod is built entirely in memory
and `PodBuildConcurrency` of them are built at once, so the memory peak is roughly
`TargetPodSize * PodBuildConcurrency * 2`. Raise both only as far as the host allows.

**The snapshot interval has to sit well inside the retention window.** The floor only advances
onto a real snapshot at or below `head - RetentionBlocks`, so if snapshots are further apart than
the window, the floor lands one to two intervals below it and snapshot spacing — not
`RetentionBlocks` — is what sets retention. Aim for at least four snapshots inside the window.

Nothing validates this: the interval is in seconds and the window in blocks, and they are only
comparable once you know the block rate, which is the thing a run is measuring.

**Two footprints, sized separately.** `RetentionBlocks` sets what the engine holds: pods, their
indexes, and their bloom filters, at roughly `RetentionBlocks` times the per-block cost — the
entry bytes plus the index and filter built over them, about 1.4x the raw data at these key sizes.
That is the number the schema is accountable for.

The state stub is pebble and takes whatever pebble takes: one full state image per retained
snapshot, plus the live database. It is a stand-in for FlatKV, so its size measures nothing about
WALRUS, and it is not counted against the target below.

| | `basic-config` | `deep-history` |
|---|---|---|
| Snapshot interval | 10s | 10 min |
| Retention | 5,000 blocks | 700,000 blocks |
| Pods, indexes, filters | ~7 GB | **~1.0 TB** |
| Pebble on top | ~1 GB | ~36 GB |
| Time before retention collects | under a minute | **~2 hours** |
| Deepest walk | ~14 pods | ~414 pods |

`basic-config` is meant for a laptop: it reaches steady state in under a minute and exercises pod
building, collection, floor advance, reference counting, deletions, and never-written reads.
`deep-history` wants a dedicated machine with a little over a terabyte free, and runs for a couple
of hours before the retention window is full enough for collection to do anything.
