# OCC Store Layer Optimizations

## Summary

A series of optimizations targeting the store layer in the OCC (Optimistic Concurrency Control) transaction execution path. The changes reduce allocation pressure and GC overhead, yielding a cumulative **+20% median TPS** improvement on EVM transfer workloads.

**Baseline**: `ec9e52e87` (pre-optimization)
**Optimized**: `79ed1e92b` (all changes applied)
**Workload**: EVM transfers, 1000 txs/batch, Giga Executor + OCC enabled

## Results

### TPS (single-node benchmark, same machine, sequential runs)

| Metric | Baseline | Optimized | Delta |
|--------|----------|-----------|-------|
| Median | 7,799 | 9,398 | **+20.5%** |
| Avg | 7,617 | 9,164 | **+20.3%** |
| Min | 5,000 | 6,400 | +28.0% |
| Max | 8,400 | 10,401 | +23.8% |

### CPU profile diff (30s sample, negative = improvement)

| Delta | Function | Category |
|-------|----------|----------|
| -13.38s cum | `runtime.scanobject` | GC scanning |
| -6.91s cum | `runtime.newobject` | Allocation |
| -6.44s flat | `runtime.mallocgcSmallScanNoHeader` | Allocation |
| -4.86s cum | `runtime.greyobject` | GC marking |
| -3.56s cum | `runtime.findObject` | GC |
| -1.99s flat | `sync.HashTrieMap.Swap` | sync.Map removal |
| -1.52s cum | `runtime.(*sweepLocked).sweep` | GC sweeping |
| -1.13s flat | `runtime.wbBufFlush1` | Write barrier |

### Allocation diff (pprof alloc_space, negative = fewer bytes)

| Delta | Function |
|-------|----------|
| -254 GB | `btree.NewFreeListG` |
| -107 GB | `cachekv.NewStore` |
| -83 GB | `sync.newIndirectNode` |
| -46 GB | `cachekv.(*Store).Write` |
| -42 GB | `btree.NewWithFreeListG` |
| -28 GB | `tm-db.NewMemDB` |
| -24 GB | `sync.newEntryNode` |
| -17 GB | `gaskv.NewStore` |

### Allocation object count diff (negative = fewer objects)

| Delta | Function |
|-------|----------|
| -1.87B | `cachekv.NewStore` |
| -1.85B | `btree.NewWithFreeListG` |
| -1.85B | `btree.NewFreeListG` |
| -1.05B | `cachekv.(*Store).Write` |
| -927M | `tm-db.NewMemDB` |
| -546M | `sync.newIndirectNode` |
| -531M | `sync.newEntryNode` |
| -484M | `multiversion.NewVersionIndexedStore` |

**Net: ~9.6 billion fewer object allocations, ~600 GB less allocation pressure.**

## Commit Breakdown

Listed oldest to newest. Each commit was validated with unit tests (cachekv, cachemulti, multiversion, giga integration).

### 1. `80e8099da` — Lazy-init sortedCache in cachekv.Store

Defer btree allocation until first iteration. Most store instances are never iterated, so the btree (3 allocations per store) is wasted.

**Files**: `sei-cosmos/store/cachekv/store.go`

### 2. `fd2e28d74` — Refactor: self-initializing getter for sortedCache

Extract lazy-init into `getSortedCache()` getter to reduce code duplication.

**Files**: `sei-cosmos/store/cachekv/store.go`

### 3. `82acf458d` — Lazy CacheMultiStore creation

Defer `cachekv.NewStore` allocation in `CacheMultiStore` until the store key is first accessed. In OCC mode, each transaction creates a CMS with ~40 store keys but typically accesses only 3-5.

**Files**: `sei-cosmos/store/cachemulti/store.go`

### 4. `37a17fd02` — Fix: materialize lazy parents before CMS branching

Ensure parent CMS stores are materialized before creating child CMS to prevent nil pointer dereference in nested branching.

**Files**: `sei-cosmos/store/cachemulti/store.go`

### 5. `d52d4a19e` — Fix: add RWMutex to lazy CacheMultiStore

Add thread-safe lazy initialization for concurrent OCC access to the same CMS.

**Files**: `sei-cosmos/store/cachemulti/store.go`

### 6. `13f343ee3` — Cache gaskv.Store wrappers in Context

Reuse `gaskv.Store` wrappers across repeated `KVStore()` calls within the same transaction, avoiding redundant allocations.

**Files**: `sei-cosmos/types/context.go`

### 7. `d35723748` — Fix: add RWMutex to gaskv cache in Context

Thread-safe access to the gaskv cache for concurrent store access.

**Files**: `sei-cosmos/types/context.go`

### 8. `de7b59d23` — Fix: invalidate gaskv cache when tracing state changes

Clear the gaskv cache when `WithIsCheckTx` or `WithGasMeter` is called, ensuring correctness when gas metering context changes.

**Files**: `sei-cosmos/types/context.go`

### 9. `ae6bbe828` — Skip redundant initial Snapshot in OCC executor

Remove unnecessary `Snapshot()` call at the start of OCC execution since the multiversion store already handles state isolation.

**Files**: `giga/deps/xevm/state/statedb.go`

### 10. `f2970afb1` — Remove dead sortedStore field from VersionIndexedStore

Remove unused `sortedStore` field that was allocated but never read.

**Files**: `sei-cosmos/store/multiversion/mvkv.go`

### 11. `6ab07c6f0` — Replace sync.Map with plain map in cachekv stores

Replace `sync.Map` (used for thread-safe iteration) with a plain `map` + `sync.RWMutex` in the giga cachekv store. `sync.Map` has high per-operation allocation overhead from internal indirection nodes.

**Files**: `giga/deps/store/cachekv.go`

### 12. `2b3cec470` — Hybrid sync.Map + RWMutex in multiversion Store

Replace `sync.Map` in the multiversion `Store` with a plain map guarded by `sync.RWMutex` for the write-heavy `txWritesetKeys` and `txReadSets` fields.

**Files**: `sei-cosmos/store/multiversion/store.go`

### 13. `b97b114f5` — Optimize UpdateReadSet allocation pattern

Use direct slice literal `[][]byte{value}` for new readset entries instead of `append([][]byte{}, value)`, avoiding an empty intermediate slice allocation.

**Files**: `sei-cosmos/store/multiversion/mvkv.go`

### 14. `8b30fb322` — Skip db CacheKV in child CacheMultiStore creation

When creating a child CMS from a parent CMS, skip wrapping the `db` field in a new cachekv store since the child's `db` is never used in the OCC path.

**Files**: `sei-cosmos/store/cachemulti/store.go`

### 15. `7ab465ad5` — Skip force-materialization in child CacheMultiStore

Remove the loop that materialized all lazy parent stores when creating a child CMS. Child stores are now materialized on demand.

**Files**: `sei-cosmos/store/cachemulti/store.go`

### 16. `bc30a1288` — sync.Pool for cachekv stores + clear() in Write

Add `sync.Pool` to recycle `cachekv.Store` instances (both sei-cosmos and giga variants). Use `clear()` (Go 1.21+) instead of `make()` in `Write()` to zero maps without freeing backing hash tables. Add `Release()` to `CacheMultiStore` to return child stores to the pool.

**Files**: `sei-cosmos/store/cachekv/store.go`, `giga/deps/store/cachekv.go`, `sei-cosmos/store/cachemulti/store.go`, `giga/deps/tasks/scheduler.go`

### 17. `79ed1e92b` — Set GOGC=200 to reduce GC frequency

Increase the GC target percentage from 100 (default) to 200, halving GC frequency. This trades ~2x peak heap memory for reduced GC CPU overhead. Validated via isolated benchmark showing +3.2% avg TPS from GOGC alone.

**Files**: `app/app.go`

## Remaining bottlenecks

After these optimizations, the CPU profile is dominated by:

1. **Lock contention** (19.9% flat `runtime.usleep`, 23% cum `runtime.lock2`) — goroutines spinning on mutexes
2. **Disk I/O** (11.4% `syscall.syscall`) — leveldb writes
3. **Epoll** (10.2% `runtime.kevent`) — network/scheduler overhead
4. **CGO** (6.7% `runtime.cgocall`) — evmone FFI calls
5. **GC** (8.1% combined madvise + greyobject + scanobject) — reduced from ~17% pre-GOGC

The next optimization frontier is lock contention and I/O, not allocation/GC.

## Methodology

- All benchmarks run on the same machine (Apple Silicon), single-node, EVM transfer workload
- Baseline and optimized runs executed sequentially in the same session to ensure comparability
- TPS sampled every 5 seconds over ~20 minutes per run
- pprof CPU profiles captured as 30-second samples during steady state (~5 min after start)
- pprof allocation profiles captured from `/debug/pprof/allocs` endpoint
- Diffs computed via `go tool pprof -diff_base`
