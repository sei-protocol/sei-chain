# Receipt Store (`littidx`)

The receipt store persists EVM transaction receipts and serves the RPC reads
that depend on them — `eth_getTransactionReceipt` (point lookup by tx hash) and
`eth_getLogs` (log filtering over a block range).

`littidx` is the receipt-store backend built for Giga throughput. It keeps
receipt **bodies** in [LittDB](../../db_engine/litt) (an append-only,
segment-based value store) and a small **tag index** in PebbleDB for `getLogs`.
It is an alternative to the default `pebbledb` backend; receipts are auxiliary,
non-consensus data, so the store is tuned for high write throughput and fast
recent reads rather than for consensus-grade durability.

## Key characteristics
- Sustains ~150k+ receipt writes/sec while serving reads concurrently.
- `eth_getTransactionReceipt` is a single LittDB point lookup — sub-millisecond
  on warm data.
- `eth_getLogs` over recent ranges is served from an in-Pebble tag index plus
  parallel LittDB point-reads.
- Receipt bytes live in LittDB's immutable segments, so large values never enter
  LSM compaction and expired data is reclaimed by dropping whole segments.

## Architecture

Two stores back one receipt store:

**1. Bodies — LittDB.** Each block's receipts are written as one or more
immutable "parts" (`block + partIndex -> the part's receipts, concatenated`).
Every tx hash is registered as a LittDB *secondary key* that aliases its
receipt's byte range within the part, so a receipt lookup is one keymap lookup
+ one segment read — no separate receipt index.

**2. Tag index — PebbleDB.** To answer `getLogs` without scanning whole blocks,
one empty-valued key is written per log tag:

```
't' + block (u64 BE) + kind (1B) + tag (20B addr / 32B topic)
    + txIndex (u32 BE) + firstLogIndex (u32 BE) + txHash (32B)  ->  nil
```

The `kind` byte keeps the address and each topic position in disjoint keyspaces
(criteria are positional). A `getLogs` query intersects the candidate
transactions across the criteria dimensions, then point-reads only the matching
receipts from LittDB by the tx hash carried in the key. `firstLogIndex` lets
reads number logs without decoding the preceding receipts. Version metadata
(`m:latest` / `m:earliest`) also lives in the Pebble index.

Per-block work in a range query (tag scan + body reads) is independent, so the
range is fanned across a bounded worker pool, which cuts wide-range tail latency
without adding write cost.

### Durability
A background flusher bounds LittDB durability lag to ~one block (`5ms`) **off
the commit path** — block commit never waits on an `fsync`. On a hard crash the
store can lose up to that interval of the most recent receipt bodies (the index
may still list them; reads return not-found for a missing body). This is
acceptable because receipts are auxiliary, non-consensus data. There is **no
WAL**; the tight flush interval is only affordable because LittDB flushes its
keymap asynchronously off the control loop, so frequent flushing overlaps with
writes instead of stalling them.

### Retention
`KeepRecent` (derived from the node's global `min-retain-blocks`) is the
authoritative retention window. It is enforced three ways that converge on the
same floor:
- Receipt bodies expire via LittDB's per-table **TTL** (time-based:
  `KeepRecent × ~2s`, set to over-retain relative to block time).
- Tag keys are pruned by **block range** (a single range tombstone per prune).
- Reads enforce the `KeepRecent` **floor**, so visible retention never exceeds
  `KeepRecent` regardless of GC timing.

## Limits & trade-offs
- **Auxiliary durability only.** A crash can drop up to ~one block of the most
  recent receipt *bodies*; those reads return not-found rather than erroring.
  Fine for RPC data, not a consensus-grade guarantee.
- **`getLogs` has no legacy fallback for ranges.** Point lookups
  (`GetReceipt`) fall back to the legacy in-state KV store (keyed by tx hash)
  for pre-`littidx` receipts; range `getLogs` cannot, because that legacy store
  has no block/tag index. So a node that **retains blocks below the legacy
  cutover height whose receipts have not yet been migrated into `littidx`** can
  silently under-return logs for that old range. This affects only
  archive/deep-retention nodes mid-migration — a node whose retained range sits
  above the cutover (typical state-synced / pruned node) is unaffected, since
  everything it serves is in `littidx`.
- **Cold/archive `getLogs` is slower.** Recent-range queries are served from
  warm caches (fast); wide or deep-historical ranges are disk-bound on the
  keymap lookups and have a heavier latency tail.

## Configuration

Set under `[receipt-store]` in `app.toml`:

| Key | Default | Applies to | Description |
|---|---|---|---|
| `rs-backend` | `pebbledb` | — | Receipt backend: `pebbledb` or `littidx`. |
| `db-directory` | `<home>/data/…/receipt` | both | Receipt store data directory. |
| `async-write-buffer` | `100` | `pebbledb` | Async commit queue length (`<= 0` = synchronous). |
| `enable-read-write-metrics` | `false` | `pebbledb` | Emit estimated read/write counters. |

`KeepRecent` is **not** a receipt-store key — it is always derived from the
global `min-retain-blocks` flag at the app layer.

Example:

```toml
[receipt-store]
rs-backend = "littidx"
```

## Rollout — no migration, state sync only

**There is no migration.** Adopting `littidx` is a config change plus a normal
(state) sync — the same posture as the SeiDB state store. Nothing to export,
import, or backfill by hand, and no downtime beyond the restart.

- **Enable it:** set `rs-backend = "littidx"` and restart.
- **How it populates:** the `littidx` data is a **local side store**
  (`data/…/receipt`) that is *not* part of any state-sync snapshot. It is always
  rebuilt from the blocks a node processes — a state-synced node brings committed
  state from the snapshot and builds `littidx` forward from its sync height.
  There is nothing to move between nodes.

Legacy pre-`littidx` receipts are handled automatically chain-side and are not
an operator concern. The only related caveat is the `getLogs`-over-legacy-range
limit noted above, which is confined to archive nodes retaining blocks below the
cutover; pruned / state-synced nodes are unaffected.
