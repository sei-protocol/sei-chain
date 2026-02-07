# Code Review: PR #2762 - Support query latest state when SS disabled

**PR:** https://github.com/sei-protocol/sei-chain/pull/2762
**Author:** yzang2019 (Yiming Zang)
**Status:** MERGED
**Files Changed:** 2 (+110, -9)

---

## Summary

This PR fixes a regression where validator nodes (which have SS disabled) could not serve queries at the latest height. The previous change completely removed SC-based historical queries to prevent data inconsistency issues, but broke validator functionality.

The fix:
- When SS is enabled: serve all queries from SS stores
- When SS is disabled: only serve queries for the latest version from SC stores

---

## Detailed Analysis

### Changes in `sei-cosmos/storev2/rootmulti/store.go`

```go:241:269:sei-cosmos/storev2/rootmulti/store.go
// CacheMultiStoreWithVersion Implements interface MultiStore
// used to createQueryContext, abci_query or grpc query service.
func (rs *Store) CacheMultiStoreWithVersion(version int64) (types.CacheMultiStore, error) {
	rs.mtx.RLock()
	defer rs.mtx.RUnlock()
	stores := make(map[types.StoreKey]types.CacheWrapper)
	// Serve from SS stores for ALL historical queries
	if rs.ssStore != nil {
		if version <= 0 {
			version = rs.ssStore.GetLatestVersion()
		}
		// add the transient/mem stores registered in current app.
		for k, store := range rs.ckvStores {
			if store.GetStoreType() != types.StoreTypeIAVL {
				stores[k] = store
			}
		}
		for k, store := range rs.ckvStores {
			if store.GetStoreType() == types.StoreTypeIAVL {
				stores[k] = state.NewStore(rs.ssStore, k, version)
			}
		}
	} else if version <= 0 || (rs.lastCommitInfo != nil && version == rs.lastCommitInfo.Version) {
		// Only serve from SC when query latest version and SS not enabled
		return rs.CacheMultiStore(), nil
	}

	return cachemulti.NewStore(nil, stores, rs.storeKeys, nil, nil, nil), nil
}
```

---

## Issues Identified

### 1. **Potential Double Lock Acquisition** (Minor - Correctness)

**Location:** Line 265

When SS is disabled and the version matches the latest, the code calls `rs.CacheMultiStore()` while still holding `rs.mtx.RLock()`. Looking at `CacheMultiStore()`:

```go:225:238:sei-cosmos/storev2/rootmulti/store.go
// Implements interface MultiStore
func (rs *Store) CacheMultiStore() types.CacheMultiStore {
	rs.mtx.RLock()
	defer rs.mtx.RUnlock()
	stores := make(map[types.StoreKey]types.CacheWrapper)
	for k, v := range rs.ckvStores {
		store := types.KVStore(v)
		stores[k] = store
	}
	gigaStores := make(map[types.StoreKey]types.KVStore, len(rs.gigaKeys))
	for _, k := range rs.gigaKeys {
		key := rs.storeKeys[k]
		gigaStores[key] = rs.ckvStores[key]
	}
	return cachemulti.NewStore(nil, stores, rs.storeKeys, gigaStores, nil, nil)
}
```

This results in nested `RLock()` calls from the same goroutine. While this works in Go (multiple readers can hold the lock), it's inefficient and could become a maintenance hazard. 

**Recommendation:** Consider extracting the core logic into an internal method `cacheMultiStoreInternal()` that doesn't acquire the lock, then have both public methods call it.

---

### 2. **Silent Failure for Historical Queries with SS Disabled** (Medium - UX/Behavior)

**Location:** Lines 263-268

When SS is disabled and a historical (non-latest) version is requested, the function:
1. Skips the SS block (because `rs.ssStore == nil`)
2. Skips the SC fallback block (because `version > 0` AND `version != lastCommitInfo.Version`)
3. Returns `cachemulti.NewStore(nil, stores, rs.storeKeys, nil, nil, nil)` with an **empty `stores` map**

This causes runtime panics when the caller attempts to access stores, as demonstrated in the test (lines 187-188). While this behavior is tested and "expected," it would be more user-friendly to return an explicit error.

**Recommendation:**
```go
} else if version <= 0 || (rs.lastCommitInfo != nil && version == rs.lastCommitInfo.Version) {
	// Only serve from SC when query latest version and SS not enabled
	return rs.CacheMultiStore(), nil
} else {
	return nil, fmt.Errorf("historical queries at version %d not supported when SS is disabled", version)
}
```

---

### 3. **Comment Accuracy** (Minor - Documentation)

**Location:** Line 247

The comment says "Serve from SS stores for ALL historical queries" but the code also handles `version <= 0` (latest query). The comment should reflect that it handles both latest and historical queries from SS when enabled.

**Suggestion:** 
```go
// Serve from SS stores for ALL queries (latest and historical) when SS is enabled
```

---

### 4. **Import Ordering in Test File** (Minor - Style)

**Location:** `store_test.go` lines 3-13

```go
import (
	"github.com/cosmos/cosmos-sdk/storev2/state"
	"testing"  // <-- standard library should come first

	"time"
```

**Recommendation:** Follow standard Go import ordering:
```go
import (
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/store/types"
	"github.com/cosmos/cosmos-sdk/storev2/state"
	// ... rest of imports
)
```

---

### 5. **Missing Test for Explicit Historical Query Error** (Minor - Testing)

The test at line 184-189 verifies that historical queries panic when SS is disabled, but it uses `require.Panics()`. If the recommendation from Issue #2 is adopted, this test would need to be updated to check for an explicit error return instead.

---

## Positive Aspects

1. **Well-structured test coverage**: The new test `TestCacheMultiStoreWithVersion_OnlyUsesSSStores` is comprehensive, testing both SS-enabled and SS-disabled scenarios with multiple store types (IAVL, transient, memory).

2. **Clear separation of concerns**: The logic clearly differentiates between SS-enabled and SS-disabled modes.

3. **Proper async handling in tests**: The test properly uses `waitUntilSSVersion()` to handle async SS writes.

4. **Test for store type verification**: The test validates that the returned stores are of the correct type (SSStore vs IAVL).

---

## Security Considerations

No security concerns identified. The change properly restricts historical queries when SS is disabled, which aligns with the data consistency goals mentioned in the PR description.

---

## Summary of Recommendations

| Priority | Issue | Recommendation |
|----------|-------|----------------|
| Medium | Silent failure for unsupported queries | Return explicit error instead of empty store map |
| Minor | Double lock acquisition | Extract internal method without locking |
| Minor | Comment accuracy | Update comment to reflect actual behavior |
| Minor | Import ordering | Fix import order in test file |

---

## Verdict

**The PR achieves its goal** of fixing the validator node query issue while maintaining data consistency for RPC nodes. The changes are well-tested and the logic is sound.

The issues identified are minor/medium and do not block the merge. However, the "silent failure" behavior (Issue #2) could be addressed in a follow-up PR to improve developer experience.
