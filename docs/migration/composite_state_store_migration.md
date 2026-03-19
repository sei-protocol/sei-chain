# CompositeStateStore Migration Strategy

## Architecture Overview

Two independent layers, each with their own write/read mode configs:

| Layer | Purpose | Cosmos Backend | EVM Backend |
|-------|---------|----------------|-------------|
| **SC** (State Commit) | Consensus / app hash | memiavl | FlatKV |
| **SS** (State Store) | Historical queries | Single MVCC DB (Pebble/Rocks) | 5 sharded MVCC DBs under `data/evm_ss/` |

**SC modes affect consensus** (app hash computation). **SS modes are per-node** and query-only with no consensus impact.

## Config Reference

Both layers share the same `WriteMode`/`ReadMode` enum but are configured independently:

```toml
[state-commit]
sc-write-mode = "cosmos_only"   # cosmos_only | dual_write | split_write
sc-read-mode  = "cosmos_only"   # cosmos_only | evm_first  | split_read

[state-store]
evm-ss-write-mode = "cosmos_only"
evm-ss-read-mode  = "cosmos_only"
ss-backend        = "pebbledb"   # pebbledb | rocksdb
```

### Write Modes

| Mode | Behavior |
|------|----------|
| `cosmos_only` | All writes to cosmos backend only. EVM stores not opened. |
| `dual_write` | EVM data goes to **both** backends. Non-EVM to cosmos only. |
| `split_write` | EVM data **only** to EVM backend. Non-EVM **only** to cosmos. |

### Read Modes

| Mode | Behavior |
|------|----------|
| `cosmos_only` | All reads from cosmos. |
| `evm_first` | Try EVM backend first, fall back to cosmos if missing. |
| `split_read` | EVM reads only from EVM backend. No fallback. |

## Migration Paths

### Path A: State-Syncing Full Node (most operators)

State sync calls `CompositeStateStore.Import()` which routes data based on the **importing node's** write mode. No pre-existing data exists, so no intermediate phase is needed.

**Target config:**

```toml
[state-commit]
sc-write-mode = "dual_write"       # memiavl keeps EVM data for app hash
sc-read-mode  = "cosmos_only"

[state-store]
evm-ss-write-mode = "split_write"  # import routes EVM→EVM SS, non-EVM→cosmos SS
evm-ss-read-mode  = "evm_first"    # fallback safety
ss-backend        = "rocksdb"      # if switching backend
```

**Why this works:** The SS `Import` path routes based on write mode with `split_write`, EVM snapshot nodes go only to EVM SS, non-EVM only to cosmos SS. Both stores are fully populated at the snapshot height. No gap, no fallback needed for imported data.

**Why SC stays at `dual_write`:** SC `split_write` means memiavl stops receiving EVM data, which breaks app hash unless the network has upgraded to lattice hash. `dual_write` keeps memiavl authoritative for consensus while also populating FlatKV.

**Snapshot source compatibility:** The importing node doesn't care whether the source is `cosmos_only` or `split_write`. The SS `Import` normalizes `evm_flatkv` → `evm` before routing, so both snapshot formats work.

**Steps:**

1. Build binary with `-tags rocksdbBackend` (if switching to RocksDB).
2. Set config as above.
3. State sync.
4. Node runs in split mode immediately.

---

### Path B: Live Full Node (no re-sync)

For a running node switching config without state sync, the EVM SS store starts empty. WAL recovery cannot backfill because the changelog is pruned, it typically covers only `KeepRecent` blocks, not all history.

#### Phase 1: Dual write + fallback reads

```toml
[state-commit]
sc-write-mode = "dual_write"
sc-read-mode  = "cosmos_only"

[state-store]
evm-ss-write-mode = "dual_write"
evm-ss-read-mode  = "evm_first"
```

- New EVM data goes to **both** cosmos SS and EVM SS.
- Reads try EVM SS first, miss on old data, fall back to cosmos (which has it).
- Zero risk since cosmos remains the full source of truth.

#### Phase 2: After `KeepRecent` blocks (~100k default)

```toml
[state-store]
evm-ss-write-mode = "split_write"
evm-ss-read-mode  = "evm_first"
```

- All live EVM data now exists in EVM SS (populated during Phase 1).
- Data older than `KeepRecent` has been pruned from both stores.
- Stop redundant cosmos EVM writes.
- `evm_first` still provides fallback for edge cases.

#### Phase 3 (optional): Full separation

```toml
[state-store]
evm-ss-write-mode = "split_write"
evm-ss-read-mode  = "split_read"
```

- No cosmos fallback. EVM reads exclusively from EVM SS.
- Only after high confidence in EVM SS health.

SC layer stays at `dual_write` throughout.

---

### Path C: Archive Node

Archive nodes keep all history (`KeepRecent = 0`), so the live-node transition (Path B) doesn't naturally converge. Old EVM data never gets pruned from cosmos, and EVM SS never accumulates it.

#### Option 1: New archive from genesis

- Configure with target modes from block 0.
- Replay all blocks; `ApplyChangesetSync` routes correctly from the start.
- EVM SS is fully populated by the time it catches up to head.
- This is the cleanest path.

#### Option 2: Migrate existing archive (key-to-key)

No migration tool exists today. One needs to be built. The tool would:

1. Open the cosmos SS MVCC DB.
2. Iterate all keys with `storeKey == "evm"` via `RawIterate`.
3. For each key, parse via `commonevm.ParseEVMKey` to determine sub-DB type (nonce, codehash, code, storage, legacy).
4. Write to the corresponding EVM SS sub-DB at the same version.
5. After completion, switch config to `split_write + evm_first`.

---

## DB Backend Migration (PebbleDB → RocksDB)

### Full Nodes

State sync into a node built with `-tags rocksdbBackend` and `ss-backend = "rocksdb"`. Both the cosmos SS MVCC DB and EVM SS sub-DBs use the same backend config, so a single flag switches everything. No data migration needed.

### Archive Nodes

No built-in migration tool exists. Two options:

1. **Replay from genesis** with RocksDB backend (cleanest, combines with Path C Option 1).
2. **Build a key-to-key migration tool** that reads all versioned data from PebbleDB and writes to RocksDB. The `backend.ResolveBackend()` abstraction makes this straightforward. Open a source DB with `openPebbleDB` and a destination with `openRocksDB`, iterate and copy.

---

## Summary

| Scenario | SC Write | SC Read | SS Write | SS Read | SS Backend | Notes |
|----------|----------|---------|----------|---------|------------|-------|
| **State sync (target)** | dual_write | cosmos_only | split_write | evm_first | rocksdb | Single step |
| **Live node Phase 1** | dual_write | cosmos_only | dual_write | evm_first | existing | Wait KeepRecent blocks |
| **Live node Phase 2** | dual_write | cosmos_only | split_write | evm_first | existing | Safe to stay here permanently |
| **Live node Phase 3** | dual_write | cosmos_only | split_write | split_read | existing | Optional, full separation |
| **Archive from genesis** | dual_write | cosmos_only | split_write | evm_first | rocksdb | Replay all blocks |
| **Archive migration** | dual_write | cosmos_only | dual_write→split_write | evm_first | existing→rocksdb | Needs migration tool |

### Key Constraints

- **SC `split_write` requires network-wide lattice hash upgrade.** Without it, memiavl's `evm` tree diverges and breaks consensus. Keep SC at `dual_write` until then.
- **SS modes are per-node safe.** Any node can independently change SS write/read modes without affecting consensus.
- **WAL recovery cannot bootstrap a fresh EVM SS on a live node.** The changelog is pruned. Use `dual_write` intermediate phase (Path B) or state sync (Path A).
- **Iterators always use cosmos SS.** `Iterator`, `ReverseIterator`, and `RawIterate` on the composite store always delegate to cosmos. This is by design since the EVM SS doesn't support cross-type iteration.
