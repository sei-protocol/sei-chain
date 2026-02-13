# Profiling Analysis: executeEVMTxWithGigaExecutor

**Date:** 2026-02-13
**Branch:** pd/benchmark-profiling-improvements (commit dcbfd5a02)
**Scenario:** benchmark/scenarios/evm.json
**Config:** GIGA_EXECUTOR=true GIGA_OCC=true DURATION=120

## Baseline

| Metric | Value |
|--------|-------|
| Median TPS | 8600 |
| Avg TPS | 8495 |
| Min TPS | 7800 |
| Max TPS | 9000 |
| Readings | 21 |
| Block Height | 995 |

## Function Hot Path

```
executeEVMTxWithGigaExecutor (app/app.go:1714)
  ├─ msg.AsTransaction()
  ├─ RecoverSenderFromEthTx()          — ECDSA recovery
  ├─ GetEVMAddress() / AssociateAddresses()
  ├─ NewDBImpl()                       — allocates state DB + initial Snapshot
  │   └─ Snapshot()                    — clones entire CacheMultiStore
  ├─ GetVMBlockContext()
  ├─ GetParams() + ChainID() (x2)     — redundant: ChainID called at lines 1721 and 1764
  ├─ NewGethExecutor() → vm.NewEVM()  — new EVM per tx
  ├─ ExecuteTransaction()
  │   └─ ApplyMessage() → StateTransition.Execute()
  │       └─ EVM.Call() — may trigger additional Snapshot() calls
  ├─ stateDB.Finalize()               — flushCtxs → cachekv.Store.Write
  ├─ WriteReceipt()
  └─ Marshal response (protobuf)
```

## CPU Profile (30s sample, 120.21s total across 4 cores)

`executeEVMTxWithGigaExecutor`: **25.95s cumulative (21.6%)**

| Component | Cumulative | % of total | Notes |
|-----------|-----------|------------|-------|
| runtime.lock2 (spinlock) | 36.87s | 30.7% | OCC goroutines fighting for locks |
| runtime.usleep (lock backoff) | 30.00s | 25.0% | Spinning on contended locks |
| ExecuteTransaction → ApplyMessage | 15.31s | 12.7% | Actual EVM execution |
| GC (gcDrain + scanobject) | 17.15s + 11.49s | 23.8% | Driven by allocation pressure |
| mallocgc | 11.42s | 9.5% | Object allocation |
| runtime.kevent | 10.38s | 8.6% | I/O polling |

### CPU Focused on executeEVMTxWithGigaExecutor

Top flat contributors within the function's call tree:

| Function | Flat | Notes |
|----------|------|-------|
| runtime.usleep | 4.95s | Lock spinning inside store operations |
| runtime.cgocall | 3.09s | CGo boundary (likely crypto) |
| runtime.mallocgc | 6.31s | Allocation pressure from stores |
| runtime.newobject | 3.72s | Heap allocations |
| memiavl.MemNode.Get | 1.00s | IAVL tree reads |
| cachekv.Store.Write | 1.03s | Flushing cache stores |
| cachemulti.newCacheMultiStoreFromCMS | 1.22s | CMS creation during Snapshot |

## Heap Profile (alloc_space)

`executeEVMTxWithGigaExecutor`: **56.2 GB cumulative (54% of 104 GB total)**

| Hotspot | Allocated | % of function | What |
|---------|----------|--------------|------|
| DBImpl.Snapshot → CacheMultiStore | 15.1 GB | 27% | Full cache store clone per snapshot |
| cachekv.NewStore | 9.0 GB | 16% | Individual KV store objects (sync.Map x2-3) |
| sync.newIndirectNode (sync.Map internals) | 7.8 GB | 14% | sync.Map internal trie nodes |
| cachekv.Store.Write | 8.4 GB | 15% | Flushing stores (map iteration + write) |
| DBImpl.Finalize → flushCtxs | 9.7 GB | 17% | Writing state changes back through layers |
| GetBalance → LockedCoins | 9.0 GB | 16% | Balance lookups triggering deep store reads |
| AccountKeeper.GetAccount | 7.2 GB | 13% | Account deserialization (protobuf UnpackAny) |
| scheduler.prepareTask | 8.8 GB | 16% | OCC task preparation (VersionIndexedStore) |

### Allocation Objects

| Hotspot | Objects | % of total | Notes |
|---------|---------|------------|-------|
| cachekv.NewStore | 157M | 10.2% | Largest single flat allocator |
| cachekv.Store.Write | 83M | 5.4% | Map iteration during flush |
| codec/types.UnpackAny | 134M | 8.7% | Protobuf deserialization |
| DBImpl.Snapshot | 137M | 8.9% | CMS + maps + sync primitives |

## Mutex/Contention Profile (196s total)

| Source | Time | % | What |
|--------|------|---|------|
| runtime.unlock | 159s | 81% | Runtime-level lock contention |
| sync.Mutex.Unlock | 29s | 15% | Application mutex contention |
| sync.RWMutex.Unlock | 19s | 10% | Reader-writer lock contention |
| AccAddress.String | 10.6s | 5% | Bech32 encoding under lock |
| EventManager.EmitEvents | 9.9s | 5% | Event emission contention |
| sync.Map.Store (HashTrieMap.Swap) | 6.9s | 4% | sync.Map write contention |

## Block Profile (7.26 hrs total)

Dominated by `runtime.selectgo` (6.97 hrs / 96%) — the OCC scheduler's `select` loop waiting for tasks. Not actionable for this function.

## Key Findings

### 1. CacheMultiStore allocation is the #1 optimization target

Each `Snapshot()` call triggers `CacheMultiStore()` which:
- Materializes ALL lazy module stores (not just ones the tx touches)
- Creates `cachekv.NewStore` with 2-3 `sync.Map` objects per module store
- Creates `gigacachekv.NewStore` with 2 `sync.Map` objects per giga store
- Allocates map copies, sync.RWMutex, sync.Once per CMS

This happens at minimum once per tx (NewDBImpl's initial Snapshot), plus additional times for nested EVM calls. OCC re-executions create entirely fresh chains.

### 2. GC overhead is a direct consequence of allocation pressure

17s of CPU on `gcDrain` + 11.5s on `scanobject` = ~24% of CPU spent on GC. The 104 GB of total allocations over 30s creates enormous GC pressure. Reducing allocations in the Snapshot path would have a compounding effect.

### 3. Lock contention is high but may be secondary

`runtime.lock2` at 30.7% is the #1 CPU consumer. Much of this is runtime-internal (GC, scheduler) and would decrease naturally if allocation pressure drops. Some is from `sync.Map` operations and store-level mutexes.

### 4. ChainID() and DefaultChainConfig() called redundantly

`ChainID()` is called at lines 1721 and 1764. `DefaultChainConfig()` is called inside `RecoverSenderFromEthTx` and again implicitly at line 1764. Minor but free to fix.

## Candidate Optimizations

### A. Pool/reuse cachekv.Store objects (high impact)

Replace fresh `cachekv.NewStore` allocations with `sync.Pool` recycling. On return to pool, call `sync.Map.Clear()` (Go 1.23+) to reset state. Eliminates ~9 GB of allocations + reduces GC.

**Expected impact:** 10-20% TPS improvement
**Risk:** Low — mechanical change, clear lifecycle boundaries

### B. Lazy per-store creation in snapshots (high impact)

Currently `materializeOnce.Do` creates cachekv.Store for ALL module stores when any snapshot is taken. Instead, create wrappers only for stores actually accessed via GetKVStore.

**Expected impact:** 10-15% TPS improvement (depends on how many stores are unused per tx)
**Risk:** Medium — changes cachemulti.Store threading model

### C. Replace sync.Map with regular map in giga cachekv (medium impact)

The giga `cachekv.Store` uses `sync.Map` but within OCC, each store belongs to a single goroutine's execution. Regular maps are ~10x cheaper to allocate and access.

**Expected impact:** 5-10% TPS improvement
**Risk:** Low — need to verify no concurrent access in OCC path

### D. Cache block-level constants per-tx (low impact)

Cache `ChainID()`, `DefaultChainConfig()`, `GetParams()`, `EthereumConfigWithSstore()` at block level instead of computing per-tx.

**Expected impact:** 2-5% TPS improvement
**Risk:** Minimal
