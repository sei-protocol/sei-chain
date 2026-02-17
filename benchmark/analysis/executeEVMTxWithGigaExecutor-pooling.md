# Profiling Analysis: Object Pooling in executeEVMTxWithGigaExecutor

## Context

Target: `executeEVMTxWithGigaExecutor` — the per-transaction hot path in the
Giga executor with OCC scheduling.

Machine: 12 CPU cores, 32 GB RAM, Apple Silicon. Benchmark config:
`GIGA_EXECUTOR=true GIGA_OCC=true`, 1000 txs/batch, 120s duration.

## Baseline Profile (commit 67f428c98)

- **TPS:** Median 6600, Max ~8000
- **Total heap allocations:** 196 GB over 120s (1.64 GB/s allocation rate)

### CPU breakdown (30s sample)

| Component       | % of total CPU | Notes                           |
|-----------------|----------------|---------------------------------|
| mallocgc        | 24.66%         | Heap allocation overhead        |
| memclrNoHeapPointers | 19.65%    | Zeroing newly allocated memory  |
| GC (bgMarkWorker + scanobject) | 14.83% | Garbage collection   |
| lock2 + usleep  | 20.40%         | Lock contention (sync.Map etc.) |
| cgocall (EVM)   | ~2.37%         | Actual EVM execution            |

**Key insight:** Only ~11% of CPU goes to actual EVM execution. The remaining
~89% is memory management overhead and lock contention.

### Top allocators (alloc_space)

| Allocator                  | Bytes   | What it creates                             |
|----------------------------|---------|---------------------------------------------|
| sync.Map nodes             | 17.0 GB | Regular cachekv store reads/writes          |
| CMS (Snapshot)             | 21.0 GB | CacheMultiStore clone per Snapshot()        |
| vm.NewEVM                  | 6.5 GB  | New EVM struct + interpreter per tx         |
| protobuf Marshal           | 12.6 GB | Response serialization                      |
| VersionIndexedStore        | 5.4 GB  | OCC scheduler per-task stores               |
| DBImpl + TemporaryState    | ~5.0 GB | Per-tx state database + transient maps      |

### Allocation pattern per transaction

Each of the ~750K transactions in a 120s run allocates:
1. **DBImpl struct** with 3 transient maps, journal slice, snapshottedCtxs slice
2. **TemporaryState** with accessList maps
3. **vm.EVM** struct with jumpDests map, precompiles map, EVMInterpreter
4. **CacheMultiStore** (~50 store keys) from Snapshot() in Prepare()
5. **Response marshaling** (MsgEVMTransactionResponse + TxMsgData)

All of these are discarded at the end of each transaction.

## Optimization Strategy

### 1. EVM pooling via sync.Pool (saves ~6.5 GB)

`vm.EVM` struct is identical across transactions within a block — same block
context, chain config, chain rules, precompiles, interpreter. Only the
`StateDB` and `TxContext` change per transaction.

The go-ethereum fork has `evm.Reset(stateDB)` which clears per-tx fields
(jumpDests, TxContext, abort, depth) while preserving block-level state.

Implementation: `EVMPool` wraps `sync.Pool`. Each OCC worker goroutine tends
to reuse the same EVM instance across transactions it processes.

### 2. DBImpl + TemporaryState pooling via sync.Pool (saves ~5 GB)

`DBImpl` struct is created and discarded per tx. By pooling:
- Reuse pre-allocated slices (`journal`, `snapshottedCtxs`) via truncation
- Reuse `TemporaryState` maps via `clear()` instead of reallocation
- Eliminate per-tx `accessList` map creation

### 3. Block-constant caching in gigaBlockCache (saves per-tx recomputation)

Block-level values (`chainID`, `blockCtx`, `chainConfig`, `baseFee`) were
recomputed for every transaction. Now computed once per block and shared
read-only across all OCC workers.

### 4. FastStore: plain maps instead of sync.Map (saves ~17 GB + lock contention)

Within the giga executor, each OCC task owns its own stores — no concurrent
access. Replacing `sync.Map`-based `cachekv.Store` with plain `map`-based
`FastStore` eliminates:
- sync.Map node allocations (indirectNode, entryNode)
- Lock contention on store reads/writes

Two implementations: `giga/deps/store.FastStore` for giga-specific stores,
`cachekv.FastStore` for cosmos-level snapshot stores.

### 5. Lazy CMS in Snapshot (saves ~4 GB from unnecessary store materialization)

`Snapshot()` creates a child CacheMultiStore by cloning the parent. Previously,
this eagerly created cachekv wrappers for all ~50 store keys, even though a
typical EVM transaction only accesses 3-4 stores (evm, bank, auth).

Now the child CMS is lazy: store wrappers are created only when first accessed
via `getOrCreateStore()`.

### 6. Hollow CMS for OCC prepareTask (saves ~8 GB)

The OCC scheduler's `prepareTask` creates a CMS then immediately replaces all
stores with `VersionIndexedStore`. The intermediate stores are wasted.

`CacheMultiStoreForOCC` creates a "hollow" CMS where stores are populated
directly from the caller's handler function, skipping intermediate creation.

### 7. GOGC tuning (reduces GC CPU by ~10%)

With 1.64 GB/s allocation rate, the default GOGC=100 triggers GC very
frequently. Setting `GOGC=-1` (disable percentage trigger) with
`GOMEMLIMIT=16GB` lets the heap grow larger between collections, reducing
GC overhead from ~15% to ~5% of CPU.

## Results

| Metric               | Baseline (67f428c98) | After pooling | Change  |
|----------------------|---------------------|---------------|---------|
| TPS Median           | 6,600               | 9,000         | +36%    |
| TPS Max              | ~8,000              | 9,800         | +22%    |
| Total alloc (120s)   | 196 GB              | 135 GB        | -31%    |
| GC CPU %             | ~15%                | ~10%          | -33%    |
| mallocgc CPU %       | 24.66%              | ~13%          | -47%    |

## How to reproduce

```bash
# Baseline
git checkout 67f428c98
GIGA_EXECUTOR=true GIGA_OCC=true benchmark/benchmark.sh

# With pooling
git checkout perf/pool-evm-and-state-objects
GIGA_EXECUTOR=true GIGA_OCC=true benchmark/benchmark.sh

# Side-by-side comparison (recommended)
benchmark/benchmark-compare.sh baseline=67f428c98 candidate=<pooling-commit>
```

## Remaining bottlenecks

After pooling, the remaining CPU breakdown:
- Lock contention (lock2): ~14% — from mutex in OCC scheduler, bech32 caching
- GC: ~10% — further reducible with more pooling or arena allocation
- protobuf Marshal: ~4% — response serialization
- Actual EVM execution (cgocall): ~8% — now a larger share of total CPU
