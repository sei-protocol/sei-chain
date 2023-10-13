package multiversion_test

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/store/dbadapter"
	"github.com/cosmos/cosmos-sdk/store/multiversion"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"
)

func TestMultiVersionStore(t *testing.T) {
	store := multiversion.NewMultiVersionStore(nil)

	// Test Set and GetLatest
	store.SetWriteset(1, 1, map[string][]byte{
		"key1": []byte("value1"),
	})
	store.SetWriteset(2, 1, map[string][]byte{
		"key1": []byte("value2"),
	})
	store.SetWriteset(3, 1, map[string][]byte{
		"key2": []byte("value3"),
	})

	require.Equal(t, []byte("value2"), store.GetLatest([]byte("key1")).Value())
	require.Equal(t, []byte("value3"), store.GetLatest([]byte("key2")).Value())

	// Test SetEstimate
	store.SetEstimatedWriteset(4, 1, map[string][]byte{
		"key1": nil,
	})
	require.True(t, store.GetLatest([]byte("key1")).IsEstimate())

	// Test Delete
	store.SetWriteset(5, 1, map[string][]byte{
		"key1": nil,
	})
	require.True(t, store.GetLatest([]byte("key1")).IsDeleted())

	// Test GetLatestBeforeIndex
	store.SetWriteset(6, 1, map[string][]byte{
		"key1": []byte("value4"),
	})
	require.True(t, store.GetLatestBeforeIndex(5, []byte("key1")).IsEstimate())
	require.Equal(t, []byte("value4"), store.GetLatestBeforeIndex(7, []byte("key1")).Value())

	// Test Has
	require.True(t, store.Has(2, []byte("key1")))
	require.False(t, store.Has(0, []byte("key1")))
	require.False(t, store.Has(5, []byte("key4")))
}

func TestMultiVersionStoreHasLaterValue(t *testing.T) {
	store := multiversion.NewMultiVersionStore(nil)

	store.SetWriteset(5, 1, map[string][]byte{
		"key1": []byte("value2"),
	})

	require.Nil(t, store.GetLatestBeforeIndex(4, []byte("key1")))
	require.Equal(t, []byte("value2"), store.GetLatestBeforeIndex(6, []byte("key1")).Value())
}

func TestMultiVersionStoreKeyDNE(t *testing.T) {
	store := multiversion.NewMultiVersionStore(nil)

	require.Nil(t, store.GetLatest([]byte("key1")))
	require.Nil(t, store.GetLatestBeforeIndex(0, []byte("key1")))
	require.False(t, store.Has(0, []byte("key1")))
}

func TestMultiVersionStoreWriteToParent(t *testing.T) {
	// initialize cachekv store
	parentKVStore := dbadapter.Store{DB: dbm.NewMemDB()}
	mvs := multiversion.NewMultiVersionStore(parentKVStore)

	parentKVStore.Set([]byte("key2"), []byte("value0"))
	parentKVStore.Set([]byte("key4"), []byte("value4"))

	mvs.SetWriteset(1, 1, map[string][]byte{
		"key1": []byte("value1"),
		"key3": nil,
		"key4": nil,
	})
	mvs.SetWriteset(2, 1, map[string][]byte{
		"key1": []byte("value2"),
	})
	mvs.SetWriteset(3, 1, map[string][]byte{
		"key2": []byte("value3"),
	})

	mvs.WriteLatestToStore()

	// assert state in parent store
	require.Equal(t, []byte("value2"), parentKVStore.Get([]byte("key1")))
	require.Equal(t, []byte("value3"), parentKVStore.Get([]byte("key2")))
	require.False(t, parentKVStore.Has([]byte("key3")))
	require.False(t, parentKVStore.Has([]byte("key4")))

	// verify no-op if mvs contains ESTIMATE
	mvs.SetEstimatedWriteset(1, 2, map[string][]byte{
		"key1": []byte("value1"),
		"key3": nil,
		"key4": nil,
		"key5": nil,
	})
	mvs.WriteLatestToStore()
	require.False(t, parentKVStore.Has([]byte("key5")))
}

func TestMultiVersionStoreWritesetSetAndInvalidate(t *testing.T) {
	mvs := multiversion.NewMultiVersionStore(nil)

	writeset := make(map[string][]byte)
	writeset["key1"] = []byte("value1")
	writeset["key2"] = []byte("value2")
	writeset["key3"] = nil

	mvs.SetWriteset(1, 2, writeset)
	require.Equal(t, []byte("value1"), mvs.GetLatest([]byte("key1")).Value())
	require.Equal(t, []byte("value2"), mvs.GetLatest([]byte("key2")).Value())
	require.True(t, mvs.GetLatest([]byte("key3")).IsDeleted())

	writeset2 := make(map[string][]byte)
	writeset2["key1"] = []byte("value3")

	mvs.SetWriteset(2, 1, writeset2)
	require.Equal(t, []byte("value3"), mvs.GetLatest([]byte("key1")).Value())

	// invalidate writeset1
	mvs.InvalidateWriteset(1, 2)

	// verify estimates
	require.True(t, mvs.GetLatestBeforeIndex(2, []byte("key1")).IsEstimate())
	require.True(t, mvs.GetLatestBeforeIndex(2, []byte("key2")).IsEstimate())
	require.True(t, mvs.GetLatestBeforeIndex(2, []byte("key3")).IsEstimate())

	// third writeset
	writeset3 := make(map[string][]byte)
	writeset3["key4"] = []byte("foo")
	writeset3["key5"] = nil

	// write the writeset directly as estimate
	mvs.SetEstimatedWriteset(3, 1, writeset3)

	require.True(t, mvs.GetLatest([]byte("key4")).IsEstimate())
	require.True(t, mvs.GetLatest([]byte("key5")).IsEstimate())

	// try replacing writeset1 to verify old keys removed
	writeset1_b := make(map[string][]byte)
	writeset1_b["key1"] = []byte("value4")

	mvs.SetWriteset(1, 2, writeset1_b)
	require.Equal(t, []byte("value4"), mvs.GetLatestBeforeIndex(2, []byte("key1")).Value())
	require.Nil(t, mvs.GetLatestBeforeIndex(2, []byte("key2")))
	// verify that GetLatest for key3 returns nil - because of removal from writeset
	require.Nil(t, mvs.GetLatest([]byte("key3")))

	// verify output for GetAllWritesetKeys
	writesetKeys := mvs.GetAllWritesetKeys()
	// we have 3 writesets
	require.Equal(t, 3, len(writesetKeys))
	require.Equal(t, []string{"key1"}, writesetKeys[1])
	require.Equal(t, []string{"key1"}, writesetKeys[2])
	require.Equal(t, []string{"key4", "key5"}, writesetKeys[3])

}

func TestMultiVersionStoreValidateState(t *testing.T) {
	parentKVStore := dbadapter.Store{DB: dbm.NewMemDB()}
	mvs := multiversion.NewMultiVersionStore(parentKVStore)

	parentKVStore.Set([]byte("key2"), []byte("value0"))
	parentKVStore.Set([]byte("key3"), []byte("value3"))
	parentKVStore.Set([]byte("key4"), []byte("value4"))
	parentKVStore.Set([]byte("key5"), []byte("value5"))

	writeset := make(multiversion.WriteSet)
	writeset["key1"] = []byte("value1")
	writeset["key2"] = []byte("value2")
	writeset["key3"] = nil
	mvs.SetWriteset(1, 2, writeset)

	readset := make(multiversion.ReadSet)
	readset["key1"] = []byte("value1")
	readset["key2"] = []byte("value2")
	readset["key3"] = nil
	readset["key4"] = []byte("value4")
	readset["key5"] = []byte("value5")
	mvs.SetReadset(5, readset)

	// assert no readset is valid
	conflicts := mvs.ValidateTransactionState(4)
	require.Empty(t, conflicts)

	// assert readset index 5 is valid
	conflicts = mvs.ValidateTransactionState(5)
	require.Empty(t, conflicts)

	// introduce conflict
	mvs.SetWriteset(2, 1, map[string][]byte{
		"key3": []byte("value6"),
	})

	// expect index 2 to be returned
	conflicts = mvs.ValidateTransactionState(5)
	require.Equal(t, []int{2}, conflicts)

	// add a conflict due to deletion
	mvs.SetWriteset(3, 1, map[string][]byte{
		"key1": nil,
	})

	// expect indices 2 and 3 to be returned
	conflicts = mvs.ValidateTransactionState(5)
	require.Equal(t, []int{2, 3}, conflicts)

	// add a conflict due to estimate
	mvs.SetEstimatedWriteset(4, 1, map[string][]byte{
		"key2": []byte("test"),
	})

	// expect indices 2, 3, and 4to be returned
	conflicts = mvs.ValidateTransactionState(5)
	require.Equal(t, []int{2, 3, 4}, conflicts)

	// assert panic for parent store mismatch
	parentKVStore.Set([]byte("key5"), []byte("value6"))
	require.Panics(t, func() { mvs.ValidateTransactionState(5) })
}
