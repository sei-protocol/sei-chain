# Brainstorm: GigaWrite State Visibility Issue Analysis

## Problem Summary
When running giga state tests, querying giga state via giga keeper after `WriteGiga()` was invoked showed empty state.

## Deep Root Cause Analysis

### The Store Hierarchy

**Non-OCC (Sequential) Mode:**
```
GigaCacheKV Store (giga/deps/store/cachekv.go)
       │
       │ Write() - flushes dirty entries to parent
       ▼
Commitment Store (sei-cosmos/storev2/commitment/store.go)
       │
       │ Set() writes to changeSet buffer
       │ Get() reads from tree (NOT changeSet)  <-- THE BUG
       ▼
IAVL Tree (actual persistent state)
```

**OCC (Parallel) Mode:**
```
VersionIndexedStore (wraps underlying store)
       │
       │ WriteToMultiVersionStore() - after each tx
       ▼
MultiVersionStore (tracks versioned writes)
       │
       │ WriteLatestToStore() - at end of scheduler
       ▼
GigaCacheKV Store → Commitment Store → IAVL Tree
```

### The Core Architectural Issue

**The problem isn't just about the commitment store** - it's about **when and why WriteGiga() is called**.

Looking at the code flow:

```
app/app.go:1634 (non-OCC) and :1900 (OCC):
    ctx.GigaMultiStore().WriteGiga()  // Called AFTER all txs execute

app/app.go:1640 and :1906:
    app.GigaBankKeeper.WriteDeferredBalances(ctx)  // NEEDS to read state
```

**The Critical Dependency**:
`WriteDeferredBalances()` reads from the store to accumulate deferred balances, then writes final balances. It MUST see all the state written by the EVM transactions.

### Why This is Really Two Different Issues

**Issue 1: Non-OCC Mode (Sequential Execution)**
- GigaCacheKV wraps commitment.Store directly
- WriteGiga() flushes cache to commitment.Store.changeSet
- commitment.Store.Get() doesn't read from changeSet
- **Fix**: Make commitment.Store.Get() read from changeSet (PR #2776)

**Issue 2: OCC Mode (Parallel Execution)**
- Giga stores are wrapped with VersionIndexedStore (scheduler.go:507-509)
- All writes go through multiversion stores
- WriteLatestToStore() is called at scheduler end (scheduler.go:334)
- **BUT**: WriteGiga() is called AFTER scheduler returns (app.go:1900)
- At this point, data should already be in the parent stores!

### The Real Question: Is WriteGiga() Even Necessary in OCC Mode?

Looking at the OCC scheduler flow:
```go
// scheduler.go:333-335
for _, mv := range s.multiVersionStores {
    mv.WriteLatestToStore()  // Writes to PARENT store (CacheKVStore or commitment.Store)
}
```

Then in app.go:
```go
// app.go:1900 - AFTER scheduler.ProcessAll() returns
ctx.GigaMultiStore().WriteGiga()  // What does this flush?
```

**In OCC mode, WriteGiga() is flushing stores that were REPLACED with VersionIndexedStores!**

The giga stores in the context's GigaMultiStore are the ORIGINAL stores, not the versioned wrappers. The scheduler creates fresh VersionIndexedStores for each task and those writes go to the multiversion store, which then writes to the parent.

### The Actual Root Cause

**The stores are being substituted but not tracked correctly:**

1. Scheduler creates `VersionIndexedStore` wrappers for each store key
2. These wrappers' parents are the ORIGINAL stores (from `cms.GetKVStore(k)`)
3. `WriteLatestToStore()` writes to these parent stores
4. But the GigaMultiStore's `gigaStores` map still points to the original stores
5. When `WriteGiga()` is called, it calls `Write()` on stores that may have:
   - Been modified through the multiversion path (writes already flushed)
   - OR have leftover cache state that wasn't part of the multiversion flow

---

## The Two Existing Fixes

### PR #2772 - Cache Workaround (commit `e82ed24ea`)
**Approach**: Keep the cache populated after `Write()` by marking entries as clean instead of clearing them.

**Analysis**: This works for the NON-OCC case where GigaCacheKV directly wraps commitment.Store. By keeping the cache populated, reads hit the cache instead of falling through to the parent.

**Cons**:
- Cache grows unbounded during block processing
- Fix is in wrong layer - masks the real issue
- Doesn't address the OCC case at all

---

### PR #2776 - Commitment Store Fix (commit `5529153d6`)
**Approach**: Modify `commitment.Store.Get()` and `Has()` to check the pending `changeSet` first.

**Analysis**: This fixes the NON-OCC case properly. After WriteGiga() flushes the cache, reads that fall through to commitment.Store will now find the data in changeSet.

**Cons**:
- O(n) lookup in changeSet for each Get()
- Changes a core assumption (documented as "Set only in Commit")
- Still doesn't fully explain the OCC path

---

## The Real Architectural Questions

### Question 1: Is WriteGiga() Being Called Correctly?

**Current Call Sites:**
- `app.go:1634` (non-OCC): After sequential tx execution, before WriteDeferredBalances
- `app.go:1900` (OCC): After OCC scheduler returns, before WriteDeferredBalances

**The Problem in OCC Mode:**
```
OCC Scheduler runs:
    ├─ Each tx gets VersionIndexedStore wrappers (scheduler.go:504-509)
    ├─ Tx writes go to VersionIndexedStore.writeset
    ├─ After tx: WriteToMultiVersionStore() (scheduler.go:566)
    └─ After all txs validated: WriteLatestToStore() (scheduler.go:334)
        └─ This writes from multiversion store → parent stores

THEN app.go:1900 calls:
    ctx.GigaMultiStore().WriteGiga()
        └─ This calls Write() on the ORIGINAL giga stores
        └─ But those stores weren't modified! The scheduler used wrappers!
```

**Key Insight**: In OCC mode, WriteGiga() is calling Write() on stores that weren't directly modified. The writes went through VersionIndexedStore → MultiVersionStore → parent stores. The GigaCacheKV's cache is likely empty or stale.

### Question 2: What is the Contract of commitment.Store?

**Original Design** (documented in code):
> "we assume Set is only called in `Commit`, so the written state is only visible after commit."

**The Assumption's Implications:**
- changeSet is a write buffer, not a queryable state
- Reads always go to the committed tree
- This is an OPTIMIZATION - no need to check changeSet on reads

**Giga's Violation of This Contract:**
- Giga calls Set() during block processing (via WriteGiga)
- Giga expects those writes to be readable before Commit
- This breaks the documented contract

**Should We Fix the Contract or the Usage?**

### Question 3: Is the Fix in the Right Place?

**Option A: Fix commitment.Store (PR #2776)**
- Makes changeSet queryable on reads
- Any code that calls Set() will now have immediately readable writes
- Wider impact, but consistent behavior

**Option B: Don't Call WriteGiga() Until Commit**
- Keep writes in GigaCacheKV until final commit
- Reads stay in cache, no need for commitment.Store to change
- But: Memory pressure from large cache during block processing

**Option C: Use a Different Store for Giga**
- Don't use commitment.Store as the parent for giga stores
- Use a store designed for immediate read-after-write semantics
- More complexity, but cleaner separation

**Option D: Make WriteGiga() a No-Op in OCC Mode**
- In OCC mode, WriteLatestToStore() already flushed data
- WriteGiga() is redundant and potentially harmful
- Just skip it when using OCC scheduler

---

## Correctness Analysis

### Is PR #2776 Correct?

**What it changes:**
```go
func (st *Store) Get(key []byte) []byte {
    // NEW: Check changeSet first
    if value, found := st.getFromChangeSet(key); found {
        return value
    }
    return st.tree.Get(key)
}
```

**Potential Issues:**

1. **PopChangeSet() Interaction**:
   ```go
   // rootmulti/store.go:143
   cs := commitStore.PopChangeSet()  // Clears changeSet!
   ```
   After `PopChangeSet()`, data in changeSet disappears from Get() results.
   - If something reads AFTER PopChangeSet but BEFORE tree commit → data is gone
   - Is this timing possible in practice?

2. **Iterator Inconsistency**:
   ```go
   func (st *Store) Iterator(start, end []byte) types.Iterator {
       return st.tree.Iterator(start, end, true)  // Only iterates tree!
   }
   ```
   Iterator doesn't include changeSet data! Get() and Iterator() are now inconsistent.

3. **Concurrent Access**:
   - changeSet is a slice, not protected by mutex
   - If Set() and Get() are called concurrently, race condition possible
   - In practice, probably serialized by caller, but no guarantee

### Is PR #2772 Correct?

**What it changes:**
- After Write(), marks cache entries as clean instead of clearing them
- Reads continue to hit cache

**Potential Issues:**

1. **Stale Data After External Modification**:
   - If parent store is modified externally, cache doesn't know
   - Reads return stale cached values
   - In practice, probably not an issue (single writer)

2. **Memory Growth**:
   - Cache never shrinks during block processing
   - For blocks with many unique keys, memory can grow significantly

3. **Double Writes**:
   - If same key is modified after WriteGiga(), it's in cache (dirty) AND in parent
   - Next WriteGiga() will write to parent again
   - Not incorrect, but potentially inefficient

---

## Recommendation Matrix

| Concern | PR #2772 (Cache) | PR #2776 (Store) | No WriteGiga in OCC |
|---------|------------------|------------------|---------------------|
| Correctness | ⚠️ Stale data risk | ⚠️ Iterator inconsistency | ✅ Clean |
| Architecture | ❌ Wrong layer | ✅ Right layer | ✅ Removes problem |
| Performance | ❌ Memory growth | ⚠️ O(n) lookups | ✅ No overhead |
| Complexity | ✅ Simple | ⚠️ Medium | ✅ Simple |
| OCC Mode | ❌ Doesn't help | ⚠️ Partial | ✅ Complete |

---

## Verified Facts

### Test Setup
- Tests use `NewGigaTestWrapper` with `UseSc: true` (StateCommitment store)
- This means commitment.Store IS being used in tests
- Tests use `useOcc: false` by default, so they're testing NON-OCC path

### PopChangeSet Timing
- `PopChangeSet()` is called from `flush()` which is called from `Commit()`
- `Commit()` happens AFTER block processing completes
- `WriteDeferredBalances()` is called BEFORE `Commit()`
- So there's NO race - the timing is:
  1. WriteGiga() → flushes to changeSet
  2. WriteDeferredBalances() → reads (needs changeSet to be queryable)
  3. EndBlock
  4. Commit() → PopChangeSet() + apply to tree

---

## Conclusions

### The Root Cause is Clear
In NON-OCC mode with commitment.Store:
1. GigaCacheKV caches writes
2. WriteGiga() flushes cache to commitment.Store.changeSet
3. GigaCacheKV clears its cache
4. Reads fall through to commitment.Store.Get()
5. **BUG**: Get() reads from tree, not changeSet → returns empty

### PR #2776 is the Correct Fix
- Fixes the issue at the correct architectural layer
- Makes commitment.Store.Get() check changeSet first
- This is semantically correct - writes should be readable before commit
- The original "Set only in Commit" was an optimization assumption, not a requirement

### PR #2772 is a Valid Workaround
- Works by keeping cache populated
- Masks the underlying issue
- Has memory growth concern but probably not severe in practice

### OCC Mode is a Separate Concern
- In OCC mode, stores are wrapped with VersionIndexedStore
- WriteLatestToStore() handles flushing to parent stores
- WriteGiga() may be redundant in OCC mode, but this is separate from the bug

---

## Final Assessment

**Recommended Fix: PR #2776 (commitment.Store modification)**

Reasons:
1. **Correctness**: Fixes the actual bug - store should return uncommitted writes
2. **Architecture**: Fix is at the right layer - the store that has the limitation
3. **Root Cause**: Addresses the underlying issue, not the symptom
4. **Maintainability**: GigaCacheKV can use standard Write() semantics

The original assumption that "Set is only called in Commit" was an implementation detail, not a fundamental contract. Giga legitimately needs read-after-write semantics, and the store should support that.

### Performance Consideration
The O(n) changeSet lookup in PR #2776 is likely acceptable because:
- changeSet is cleared on each commit (bounded by block size)
- Most reads in hot paths hit the cache layer first
- Can add a map index if profiling shows issues

---

## Files Involved
- `giga/deps/store/cachekv.go` - GigaCacheKV implementation
- `sei-cosmos/storev2/commitment/store.go` - Commitment store (FIX HERE)
- `sei-cosmos/storev2/rootmulti/store.go` - PopChangeSet() called during Commit
- `giga/deps/tasks/scheduler.go` - OCC scheduler (separate concern)
- `app/app.go` - WriteGiga() calls at lines 1634 and 1900
