package cachekv_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"

	"github.com/sei-protocol/sei-chain/sei-cosmos/store/cachekv"
	"github.com/sei-protocol/sei-chain/sei-cosmos/store/dbadapter"
	"github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
)

// stack builds n nested cachekv layers on top of base and returns them
// bottom-first (result[0] is the deepest layer over base, result[n-1] is the top).
func stack(base types.KVStore, n int) []*cachekv.Store {
	layers := make([]*cachekv.Store, n)
	parent := base
	for i := 0; i < n; i++ {
		s := cachekv.NewStore(parent, types.NewKVStoreKey("CacheKvTest"), types.DefaultCacheSizeLimit)
		layers[i] = s
		parent = s
	}
	return layers
}

// freezeAllButTop freezes every layer except the topmost, mirroring how the EVM
// snapshot stack freezes a layer once a newer one is stacked on top of it.
func freezeAllButTop(layers []*cachekv.Store) {
	for i := 0; i < len(layers)-1; i++ {
		layers[i].Freeze()
	}
}

// TestFrozenEmptyLayerSkipEquivalence verifies that reads through a deep stack of
// frozen, empty cache layers return exactly what a read against the base returns.
func TestFrozenEmptyLayerSkipEquivalence(t *testing.T) {
	mem := dbadapter.Store{DB: dbm.NewMemDB()}
	for i := 0; i < 20; i++ {
		mem.Set(keyFmt(i), valFmt(i))
	}

	layers := stack(mem, 64)
	freezeAllButTop(layers)
	top := layers[len(layers)-1]

	// Point reads through the empty frozen stack must match the base exactly.
	for i := 0; i < 20; i++ {
		require.Equal(t, valFmt(i), top.Get(keyFmt(i)), "key %d", i)
	}
	require.Nil(t, top.Get(bz("missing")))
	require.True(t, top.Has(keyFmt(0)))
	require.False(t, top.Has(bz("missing")))

	// Full iteration through the empty frozen stack must yield every base key.
	itr := top.Iterator(nil, nil)
	got := 0
	for ; itr.Valid(); itr.Next() {
		require.Equal(t, keyFmt(got), itr.Key())
		require.Equal(t, valFmt(got), itr.Value())
		got++
	}
	require.NoError(t, itr.Close())
	require.Equal(t, 20, got)

	// Reverse iteration too.
	ritr := top.ReverseIterator(nil, nil)
	rgot := 0
	for ; ritr.Valid(); ritr.Next() {
		exp := 19 - rgot
		require.Equal(t, keyFmt(exp), ritr.Key())
		require.Equal(t, valFmt(exp), ritr.Value())
		rgot++
	}
	require.NoError(t, ritr.Close())
	require.Equal(t, 20, rgot)
}

// TestFrozenLayerWithWritesIsNotSkipped verifies that a frozen layer holding a
// write (a set or a delete) still shadows the base — it must never be skipped.
func TestFrozenLayerWithWritesIsNotSkipped(t *testing.T) {
	mem := dbadapter.Store{DB: dbm.NewMemDB()}
	mem.Set(keyFmt(1), valFmt(1))
	mem.Set(keyFmt(2), valFmt(2))
	mem.Set(keyFmt(3), valFmt(3))

	layers := stack(mem, 8)

	// A middle layer overrides key 1 and deletes key 2, then is frozen (dirty).
	mid := layers[3]
	mid.Set(keyFmt(1), valFmt(100))
	mid.Delete(keyFmt(2))
	freezeAllButTop(layers)

	top := layers[len(layers)-1]

	// Point reads must reflect the middle layer, not the base.
	require.Equal(t, valFmt(100), top.Get(keyFmt(1)), "shadowed set must win")
	require.Nil(t, top.Get(keyFmt(2)), "delete in a frozen layer must hide the base key")
	require.Equal(t, valFmt(3), top.Get(keyFmt(3)))

	// Iteration must reflect the delete and the shadowing set.
	itr := top.Iterator(nil, nil)
	seen := map[string]string{}
	for ; itr.Valid(); itr.Next() {
		seen[string(itr.Key())] = string(itr.Value())
	}
	require.NoError(t, itr.Close())
	require.Equal(t, map[string]string{
		string(keyFmt(1)): string(valFmt(100)),
		string(keyFmt(3)): string(valFmt(3)),
	}, seen)
}

// TestSkipRecomputedAfterFrozenLayerBecomesDirty covers the RevertToSnapshot
// re-exposure case at the store level: a layer that was frozen-empty (and thus
// skippable) later receives writes, and any child created afterwards must consult
// it. In the real EVM flow, children created before the write are discarded by the
// revert, so no already-memoized skip can go stale.
func TestSkipRecomputedAfterFrozenLayerBecomesDirty(t *testing.T) {
	mem := dbadapter.Store{DB: dbm.NewMemDB()}
	mem.Set(keyFmt(1), valFmt(1))

	frozenEmpty := cachekv.NewStore(mem, types.NewKVStoreKey("CacheKvTest"), types.DefaultCacheSizeLimit)
	frozenEmpty.Freeze()

	// Child created while the layer is frozen+empty skips it and reads the base.
	early := cachekv.NewStore(frozenEmpty, types.NewKVStoreKey("CacheKvTest"), types.DefaultCacheSizeLimit)
	require.Equal(t, valFmt(1), early.Get(keyFmt(1)))

	// The layer is re-exposed (as after a revert) and written to.
	frozenEmpty.Set(keyFmt(1), valFmt(2))

	// A child created after the write must observe it (skip must not apply).
	late := cachekv.NewStore(frozenEmpty, types.NewKVStoreKey("CacheKvTest"), types.DefaultCacheSizeLimit)
	require.Equal(t, valFmt(2), late.Get(keyFmt(1)))
}

// TestRevertWriteSnapshotSequence models the exact DBImpl flow that motivated the
// Unfreeze-on-revert fix: RevertToSnapshot re-exposes a frozen layer, execution
// writes to it, then Snapshot re-freezes it and stacks a new layer. A read from
// the new top must observe the post-revert write for the written key while still
// short-circuiting reads of keys the layer does not hold.
func TestRevertWriteSnapshotSequence(t *testing.T) {
	mem := dbadapter.Store{DB: dbm.NewMemDB()}
	mem.Set(keyFmt(1), valFmt(1))
	mem.Set(keyFmt(2), valFmt(2))

	// L is a snapshot layer that was frozen when it was superseded.
	L := cachekv.NewStore(mem, types.NewKVStoreKey("CacheKvTest"), types.DefaultCacheSizeLimit)
	L.Freeze()

	// (1) RevertToSnapshot re-exposes L as the writable top: unfreeze it.
	L.Unfreeze()
	// (2) Execution writes key 1 into the re-exposed layer.
	L.Set(keyFmt(1), valFmt(100))
	// (3) Snapshot stacks a new layer and re-freezes L.
	L.Freeze()
	C := cachekv.NewStore(L, types.NewKVStoreKey("CacheKvTest"), types.DefaultCacheSizeLimit)

	// The written key reflects the post-revert write (L is dirty, not skipped);
	// the untouched key still resolves through to the base.
	require.Equal(t, valFmt(100), C.Get(keyFmt(1)))
	require.Equal(t, valFmt(2), C.Get(keyFmt(2)))
}

// TestUnfreezeBypassesStaleSkipMemo covers the concurrent-copy hazard: a store
// memoizes a skip over a frozen-empty parent, then that parent is re-exposed
// (unfrozen) and written. Because reads gate on the parent's live frozen bit, the
// same store must observe the write despite its stale memo.
func TestUnfreezeBypassesStaleSkipMemo(t *testing.T) {
	mem := dbadapter.Store{DB: dbm.NewMemDB()}
	mem.Set(keyFmt(1), valFmt(1))

	parent := cachekv.NewStore(mem, types.NewKVStoreKey("CacheKvTest"), types.DefaultCacheSizeLimit)
	parent.Freeze()

	child := cachekv.NewStore(parent, types.NewKVStoreKey("CacheKvTest"), types.DefaultCacheSizeLimit)
	// Read while parent is frozen+empty: child memoizes a skip over parent.
	require.Equal(t, valFmt(1), child.Get(keyFmt(1)))

	// Parent is re-exposed and written (the case the single-DBImpl revert would
	// otherwise discard the child for, but a live Copy would not).
	parent.Unfreeze()
	parent.Set(keyFmt(1), valFmt(2))

	// The child must now see the write even though it memoized a skip earlier.
	require.Equal(t, valFmt(2), child.Get(keyFmt(1)))
}

// TestWriteClearsFrozenSkipState verifies Write() resets emptiness bookkeeping so
// a flushed layer is treated as empty again.
func TestWriteClearsFrozenSkipState(t *testing.T) {
	mem := dbadapter.Store{DB: dbm.NewMemDB()}
	mem.Set(keyFmt(1), valFmt(1))

	a := cachekv.NewStore(mem, types.NewKVStoreKey("CacheKvTest"), types.DefaultCacheSizeLimit)
	a.Set(keyFmt(2), valFmt(2)) // a is dirty
	a.Freeze()
	a.Write() // flushes into mem and clears dirty

	b := cachekv.NewStore(a, types.NewKVStoreKey("CacheKvTest"), types.DefaultCacheSizeLimit)
	// a is frozen and empty again after Write; b must still read through correctly.
	require.Equal(t, valFmt(1), b.Get(keyFmt(1)))
	require.Equal(t, valFmt(2), b.Get(keyFmt(2)))
}

// buildEmptyStackOverBase is a benchmark helper.
func buildEmptyStackOverBase(depth int, freeze bool) *cachekv.Store {
	mem := dbadapter.Store{DB: dbm.NewMemDB()}
	for i := 0; i < 50; i++ {
		mem.Set(keyFmt(i), valFmt(i))
	}
	layers := stack(mem, depth)
	if freeze {
		freezeAllButTop(layers)
	}
	return layers[len(layers)-1]
}

// BenchmarkDeepStackGet demonstrates that freezing empty layers keeps a read O(1)
// in stack depth instead of O(depth).
func BenchmarkDeepStackGet(b *testing.B) {
	for _, depth := range []int{16, 256, 1024} {
		b.Run(fmt.Sprintf("depth=%d/frozen", depth), func(b *testing.B) {
			top := buildEmptyStackOverBase(depth, true)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = top.Get(keyFmt(i % 50))
			}
		})
		b.Run(fmt.Sprintf("depth=%d/unfrozen", depth), func(b *testing.B) {
			top := buildEmptyStackOverBase(depth, false)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = top.Get(keyFmt(i % 50))
			}
		})
	}
}
