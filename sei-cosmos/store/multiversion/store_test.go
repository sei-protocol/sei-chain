package multiversion_test

import (
	"bytes"
	"testing"

	"github.com/cosmos/cosmos-sdk/store/dbadapter"
	"github.com/cosmos/cosmos-sdk/store/multiversion"
	"github.com/cosmos/cosmos-sdk/types/occ"
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
	readset["key1"] = [][]byte{[]byte("value1")}
	readset["key2"] = [][]byte{[]byte("value2")}
	readset["key3"] = [][]byte{nil}
	readset["key4"] = [][]byte{[]byte("value4")}
	readset["key5"] = [][]byte{[]byte("value5")}
	mvs.SetReadset(5, readset)

	// assert no readset is valid
	valid, conflicts := mvs.ValidateTransactionState(4)
	require.True(t, valid)
	require.Empty(t, conflicts)

	// assert readset index 5 is valid
	valid, conflicts = mvs.ValidateTransactionState(5)
	require.True(t, valid)
	require.Empty(t, conflicts)

	// introduce conflict
	mvs.SetWriteset(2, 1, map[string][]byte{
		"key3": []byte("value6"),
	})

	// expect failure with conflict of tx 2
	valid, conflicts = mvs.ValidateTransactionState(5)
	require.False(t, valid)
	require.Equal(t, []int{2}, conflicts)

	// add a conflict due to deletion
	mvs.SetWriteset(3, 1, map[string][]byte{
		"key1": nil,
	})

	// expect failure with conflict of tx 2 and 3
	valid, conflicts = mvs.ValidateTransactionState(5)
	require.False(t, valid)
	require.Equal(t, []int{2, 3}, conflicts)

	// add a conflict due to estimate
	mvs.SetEstimatedWriteset(4, 1, map[string][]byte{
		"key2": []byte("test"),
	})

	// expect index 4 to be returned
	valid, conflicts = mvs.ValidateTransactionState(5)
	require.False(t, valid)
	require.Equal(t, []int{2, 3, 4}, conflicts)
}

func TestMultiVersionStoreParentValidationMismatch(t *testing.T) {
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
	readset["key1"] = [][]byte{[]byte("value1")}
	readset["key2"] = [][]byte{[]byte("value2")}
	readset["key3"] = [][]byte{nil}
	readset["key4"] = [][]byte{[]byte("value4")}
	readset["key5"] = [][]byte{[]byte("value5")}
	mvs.SetReadset(5, readset)

	// assert no readset is valid
	valid, conflicts := mvs.ValidateTransactionState(4)
	require.True(t, valid)
	require.Empty(t, conflicts)

	// assert readset index 5 is valid
	valid, conflicts = mvs.ValidateTransactionState(5)
	require.True(t, valid)
	require.Empty(t, conflicts)

	// overwrite tx writeset for tx1 - no longer writes key1
	writeset2 := make(multiversion.WriteSet)
	writeset2["key2"] = []byte("value2")
	writeset2["key3"] = nil
	mvs.SetWriteset(1, 3, writeset2)

	// assert readset index 5 is invalid - because of mismatch with parent store
	valid, conflicts = mvs.ValidateTransactionState(5)
	require.False(t, valid)
	require.Empty(t, conflicts)
}

func TestMultiVersionStoreMultipleReadsetValueValidationFailure(t *testing.T) {
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
	readset["key1"] = [][]byte{[]byte("value1")}
	readset["key2"] = [][]byte{[]byte("value2")}
	readset["key3"] = [][]byte{nil}
	readset["key4"] = [][]byte{[]byte("value4")}
	readset["key5"] = [][]byte{[]byte("value5"), []byte("value5b")}
	mvs.SetReadset(5, readset)

	// assert readset index 5 is invalid due to multiple values in readset
	valid, conflicts := mvs.ValidateTransactionState(5)
	require.False(t, valid)
	require.Empty(t, conflicts)
}

func TestMVSValidationWithOnlyEstimate(t *testing.T) {
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
	readset["key1"] = [][]byte{[]byte("value1")}
	readset["key2"] = [][]byte{[]byte("value2")}
	readset["key3"] = [][]byte{nil}
	readset["key4"] = [][]byte{[]byte("value4")}
	readset["key5"] = [][]byte{[]byte("value5")}
	mvs.SetReadset(5, readset)

	// add a conflict due to estimate
	mvs.SetEstimatedWriteset(4, 1, map[string][]byte{
		"key2": []byte("test"),
	})

	valid, conflicts := mvs.ValidateTransactionState(5)
	require.True(t, valid)
	require.Equal(t, []int{4}, conflicts)

}

func TestMVSIteratorValidation(t *testing.T) {
	parentKVStore := dbadapter.Store{DB: dbm.NewMemDB()}
	mvs := multiversion.NewMultiVersionStore(parentKVStore)
	vis := multiversion.NewVersionIndexedStore(parentKVStore, mvs, 5, 1, make(chan occ.Abort, 1))

	parentKVStore.Set([]byte("key2"), []byte("value0"))
	parentKVStore.Set([]byte("key3"), []byte("value3"))
	parentKVStore.Set([]byte("key4"), []byte("value4"))
	parentKVStore.Set([]byte("key5"), []byte("value5"))

	writeset := make(multiversion.WriteSet)
	writeset["key1"] = []byte("value1")
	writeset["key2"] = []byte("value2")
	writeset["key3"] = nil
	mvs.SetWriteset(1, 2, writeset)

	// test basic iteration
	iter := vis.ReverseIterator([]byte("key1"), []byte("key6"))
	for ; iter.Valid(); iter.Next() {
		// read value
		iter.Value()
	}
	iter.Close()
	vis.WriteToMultiVersionStore()

	// should be valid
	valid, conflicts := mvs.ValidateTransactionState(5)
	require.True(t, valid)
	require.Empty(t, conflicts)
}

func TestMVSIteratorValidationWithEstimate(t *testing.T) {
	parentKVStore := dbadapter.Store{DB: dbm.NewMemDB()}
	mvs := multiversion.NewMultiVersionStore(parentKVStore)
	vis := multiversion.NewVersionIndexedStore(parentKVStore, mvs, 5, 1, make(chan occ.Abort, 1))

	parentKVStore.Set([]byte("key2"), []byte("value0"))
	parentKVStore.Set([]byte("key3"), []byte("value3"))
	parentKVStore.Set([]byte("key4"), []byte("value4"))
	parentKVStore.Set([]byte("key5"), []byte("value5"))

	writeset := make(multiversion.WriteSet)
	writeset["key1"] = []byte("value1")
	writeset["key2"] = []byte("value2")
	writeset["key3"] = nil
	mvs.SetWriteset(1, 2, writeset)

	iter := vis.Iterator([]byte("key1"), []byte("key6"))
	for ; iter.Valid(); iter.Next() {
		// read value
		iter.Value()
	}
	iter.Close()
	vis.WriteToMultiVersionStore()

	writeset2 := make(multiversion.WriteSet)
	writeset2["key2"] = []byte("value2")
	mvs.SetEstimatedWriteset(2, 2, writeset2)

	// should be invalid
	valid, conflicts := mvs.ValidateTransactionState(5)
	require.False(t, valid)
	require.Equal(t, []int{2}, conflicts)
}

func TestMVSIteratorValidationWithKeySwitch(t *testing.T) {
	parentKVStore := dbadapter.Store{DB: dbm.NewMemDB()}
	mvs := multiversion.NewMultiVersionStore(parentKVStore)
	vis := multiversion.NewVersionIndexedStore(parentKVStore, mvs, 5, 1, make(chan occ.Abort, 1))

	parentKVStore.Set([]byte("key2"), []byte("value0"))
	parentKVStore.Set([]byte("key3"), []byte("value3"))
	parentKVStore.Set([]byte("key4"), []byte("value4"))
	parentKVStore.Set([]byte("key5"), []byte("value5"))

	writeset := make(multiversion.WriteSet)
	writeset["key1"] = []byte("value1")
	writeset["key2"] = []byte("value2")
	writeset["key3"] = nil
	mvs.SetWriteset(1, 2, writeset)

	iter := vis.Iterator([]byte("key1"), []byte("key6"))
	for ; iter.Valid(); iter.Next() {
		// read value
		iter.Value()
	}
	iter.Close()
	vis.WriteToMultiVersionStore()

	// deletion of 2 and introduction of 3
	writeset2 := make(multiversion.WriteSet)
	writeset2["key2"] = nil
	writeset2["key3"] = []byte("valueX")
	mvs.SetWriteset(2, 2, writeset2)

	// should be invalid with conflict of 2
	valid, conflicts := mvs.ValidateTransactionState(5)
	require.False(t, valid)
	require.Equal(t, []int{2}, conflicts)
}

func TestMVSIteratorValidationWithKeyAdded(t *testing.T) {
	parentKVStore := dbadapter.Store{DB: dbm.NewMemDB()}
	mvs := multiversion.NewMultiVersionStore(parentKVStore)
	vis := multiversion.NewVersionIndexedStore(parentKVStore, mvs, 5, 1, make(chan occ.Abort, 1))

	parentKVStore.Set([]byte("key2"), []byte("value0"))
	parentKVStore.Set([]byte("key3"), []byte("value3"))
	parentKVStore.Set([]byte("key4"), []byte("value4"))
	parentKVStore.Set([]byte("key5"), []byte("value5"))

	writeset := make(multiversion.WriteSet)
	writeset["key1"] = []byte("value1")
	writeset["key2"] = []byte("value2")
	writeset["key3"] = nil
	mvs.SetWriteset(1, 2, writeset)

	iter := vis.Iterator([]byte("key1"), []byte("key7"))
	for ; iter.Valid(); iter.Next() {
		// read value
		iter.Value()
	}
	iter.Close()
	vis.WriteToMultiVersionStore()

	// addition of key6
	writeset2 := make(multiversion.WriteSet)
	writeset2["key6"] = []byte("value6")
	mvs.SetWriteset(2, 2, writeset2)

	// should be invalid
	valid, conflicts := mvs.ValidateTransactionState(5)
	require.False(t, valid)
	require.Empty(t, conflicts)
}

func TestMVSIteratorValidationWithWritesetValues(t *testing.T) {
	parentKVStore := dbadapter.Store{DB: dbm.NewMemDB()}
	mvs := multiversion.NewMultiVersionStore(parentKVStore)
	vis := multiversion.NewVersionIndexedStore(parentKVStore, mvs, 5, 1, make(chan occ.Abort, 1))

	parentKVStore.Set([]byte("key2"), []byte("value0"))
	parentKVStore.Set([]byte("key3"), []byte("value3"))
	parentKVStore.Set([]byte("key4"), []byte("value4"))
	parentKVStore.Set([]byte("key5"), []byte("value5"))

	writeset := make(multiversion.WriteSet)
	writeset["key1"] = []byte("value1")
	writeset["key2"] = []byte("value2")
	writeset["key3"] = nil
	mvs.SetWriteset(1, 2, writeset)

	// set a key BEFORE iteration occurred
	vis.Set([]byte("key6"), []byte("value6"))

	iter := vis.Iterator([]byte("key1"), []byte("key7"))
	for ; iter.Valid(); iter.Next() {
	}
	iter.Close()
	vis.WriteToMultiVersionStore()

	// should be valid
	valid, conflicts := mvs.ValidateTransactionState(5)
	require.True(t, valid)
	require.Empty(t, conflicts)
}

func TestMVSIteratorValidationWithWritesetValuesSetAfterIteration(t *testing.T) {
	parentKVStore := dbadapter.Store{DB: dbm.NewMemDB()}
	mvs := multiversion.NewMultiVersionStore(parentKVStore)
	vis := multiversion.NewVersionIndexedStore(parentKVStore, mvs, 5, 1, make(chan occ.Abort, 1))

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
	readset["key1"] = [][]byte{[]byte("value1")}
	readset["key2"] = [][]byte{[]byte("value2")}
	readset["key3"] = [][]byte{nil}
	readset["key4"] = [][]byte{[]byte("value4")}
	readset["key5"] = [][]byte{[]byte("value5")}
	mvs.SetReadset(5, readset)

	// no key6 because the iteration was performed BEFORE the write
	iter := vis.Iterator([]byte("key1"), []byte("key7"))
	for ; iter.Valid(); iter.Next() {
	}
	iter.Close()

	// write key 6 AFTER iterator went
	vis.Set([]byte("key6"), []byte("value6"))
	vis.WriteToMultiVersionStore()

	// should be valid
	valid, conflicts := mvs.ValidateTransactionState(5)
	require.True(t, valid)
	require.Empty(t, conflicts)
}

func TestMVSIteratorValidationReverse(t *testing.T) {
	parentKVStore := dbadapter.Store{DB: dbm.NewMemDB()}
	mvs := multiversion.NewMultiVersionStore(parentKVStore)
	vis := multiversion.NewVersionIndexedStore(parentKVStore, mvs, 5, 1, make(chan occ.Abort, 1))

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
	readset["key1"] = [][]byte{[]byte("value1")}
	readset["key2"] = [][]byte{[]byte("value2")}
	readset["key3"] = [][]byte{nil}
	readset["key4"] = [][]byte{[]byte("value4")}
	readset["key5"] = [][]byte{[]byte("value5")}
	mvs.SetReadset(5, readset)

	// set a key BEFORE iteration occurred
	vis.Set([]byte("key6"), []byte("value6"))

	iter := vis.ReverseIterator([]byte("key1"), []byte("key7"))
	for ; iter.Valid(); iter.Next() {
	}
	iter.Close()
	vis.WriteToMultiVersionStore()

	// should be valid
	valid, conflicts := mvs.ValidateTransactionState(5)
	require.True(t, valid)
	require.Empty(t, conflicts)
}

func TestMVSIteratorValidationEarlyStop(t *testing.T) {
	parentKVStore := dbadapter.Store{DB: dbm.NewMemDB()}
	mvs := multiversion.NewMultiVersionStore(parentKVStore)
	vis := multiversion.NewVersionIndexedStore(parentKVStore, mvs, 5, 1, make(chan occ.Abort, 1))

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
	readset["key1"] = [][]byte{[]byte("value1")}
	readset["key2"] = [][]byte{[]byte("value2")}
	readset["key3"] = [][]byte{nil}
	readset["key4"] = [][]byte{[]byte("value4")}
	mvs.SetReadset(5, readset)

	iter := vis.Iterator([]byte("key1"), []byte("key7"))
	for ; iter.Valid(); iter.Next() {
		// read the value and see if we want to break
		if bytes.Equal(iter.Key(), []byte("key4")) {
			break
		}
	}
	iter.Close()
	vis.WriteToMultiVersionStore()

	// removal of key5 - but irrelevant because of early stop
	writeset2 := make(multiversion.WriteSet)
	writeset2["key5"] = nil
	mvs.SetWriteset(2, 2, writeset2)

	// should be valid
	valid, conflicts := mvs.ValidateTransactionState(5)
	require.True(t, valid)
	require.Empty(t, conflicts)
}

func TestMVSIteratorValidationEarlyStopEarlierKeyRemoved(t *testing.T) {
	parentKVStore := dbadapter.Store{DB: dbm.NewMemDB()}
	mvs := multiversion.NewMultiVersionStore(parentKVStore)
	vis := multiversion.NewVersionIndexedStore(parentKVStore, mvs, 5, 1, make(chan occ.Abort, 1))

	parentKVStore.Set([]byte("key2"), []byte("value0"))
	parentKVStore.Set([]byte("key3"), []byte("value3"))
	parentKVStore.Set([]byte("key4"), []byte("value4"))
	parentKVStore.Set([]byte("key5"), []byte("value5"))

	writeset := make(multiversion.WriteSet)
	writeset["key1"] = []byte("value1")
	writeset["key3"] = nil
	mvs.SetWriteset(1, 2, writeset)

	readset := make(multiversion.ReadSet)
	readset["key1"] = [][]byte{[]byte("value1")}
	readset["key3"] = [][]byte{nil}
	readset["key4"] = [][]byte{[]byte("value4")}
	mvs.SetReadset(5, readset)

	i := 0
	iter := vis.Iterator([]byte("key1"), []byte("key7"))
	for ; iter.Valid(); iter.Next() {
		iter.Key()
		i++
		// break after iterating 3 items
		if i == 3 {
			break
		}
	}
	iter.Close()
	vis.WriteToMultiVersionStore()

	// removal of key2 by an earlier tx - should cause invalidation for iterateset validation
	writeset2 := make(multiversion.WriteSet)
	writeset2["key2"] = nil
	mvs.SetWriteset(2, 2, writeset2)

	// should be invalid
	valid, conflicts := mvs.ValidateTransactionState(5)
	require.False(t, valid)
	require.Empty(t, conflicts)
}

func TestMVSIteratorValidationEarlyStopEarlierKeyRemovedAndOtherReplaced(t *testing.T) {
	parentKVStore := dbadapter.Store{DB: dbm.NewMemDB()}
	mvs := multiversion.NewMultiVersionStore(parentKVStore)
	vis := multiversion.NewVersionIndexedStore(parentKVStore, mvs, 5, 1, make(chan occ.Abort, 1))

	parentKVStore.Set([]byte("key2"), []byte("value0"))
	parentKVStore.Set([]byte("key3"), []byte("value3"))
	parentKVStore.Set([]byte("key4"), []byte("value4"))
	parentKVStore.Set([]byte("key5"), []byte("value5"))

	writeset := make(multiversion.WriteSet)
	writeset["key1"] = []byte("value1")
	writeset["key3"] = nil
	mvs.SetWriteset(1, 2, writeset)

	readset := make(multiversion.ReadSet)
	readset["key1"] = [][]byte{[]byte("value1")}
	readset["key3"] = [][]byte{nil}
	readset["key4"] = [][]byte{[]byte("value4")}
	mvs.SetReadset(5, readset)

	i := 0
	iter := vis.Iterator([]byte("key1"), []byte("key7"))
	for ; iter.Valid(); iter.Next() {
		iter.Key()
		i++
		// break after iterating 3 items
		if i == 3 {
			break
		}
	}
	iter.Close()
	vis.WriteToMultiVersionStore()

	// removal of key2 by an earlier tx - should cause invalidation for iterateset validation
	writeset2 := make(multiversion.WriteSet)
	writeset2["key2"] = nil
	writeset2["key2a"] = []byte("value2a")
	mvs.SetWriteset(2, 2, writeset2)

	// should be invalid because key mismatch
	valid, conflicts := mvs.ValidateTransactionState(5)
	require.False(t, valid)
	require.Empty(t, conflicts)
}

// TODO: what about early stop with a new key added in the range? - especially if its the last key that we stopped at?
func TestMVSIteratorValidationEarlyStopAtEndOfRange(t *testing.T) {
	parentKVStore := dbadapter.Store{DB: dbm.NewMemDB()}
	mvs := multiversion.NewMultiVersionStore(parentKVStore)
	vis := multiversion.NewVersionIndexedStore(parentKVStore, mvs, 5, 1, make(chan occ.Abort, 1))

	parentKVStore.Set([]byte("key2"), []byte("value0"))
	parentKVStore.Set([]byte("key3"), []byte("value3"))
	parentKVStore.Set([]byte("key4"), []byte("value4"))
	parentKVStore.Set([]byte("key5"), []byte("value5"))

	writeset := make(multiversion.WriteSet)
	writeset["key1"] = []byte("value1")
	writeset["key2"] = []byte("value2")
	writeset["key3"] = nil
	mvs.SetWriteset(1, 2, writeset)

	// test basic iteration
	iter := vis.Iterator([]byte("key1"), []byte("key7"))
	for ; iter.Valid(); iter.Next() {
		// read the value and see if we want to break
		if bytes.Equal(iter.Key(), []byte("key5")) {
			break
		}
	}
	iter.Close()
	vis.WriteToMultiVersionStore()

	// add key6
	writeset2 := make(multiversion.WriteSet)
	writeset2["key6"] = []byte("value6")
	mvs.SetWriteset(2, 2, writeset2)

	// should be valid
	valid, conflicts := mvs.ValidateTransactionState(5)
	require.True(t, valid)
	require.Empty(t, conflicts)
}

func TestMVSIteratorValidationWithKeyAddedForgetToClose(t *testing.T) {
	parentKVStore := dbadapter.Store{DB: dbm.NewMemDB()}
	mvs := multiversion.NewMultiVersionStore(parentKVStore)
	vis := multiversion.NewVersionIndexedStore(parentKVStore, mvs, 5, 1, make(chan occ.Abort, 1))

	parentKVStore.Set([]byte("key2"), []byte("value0"))
	parentKVStore.Set([]byte("key3"), []byte("value3"))
	parentKVStore.Set([]byte("key4"), []byte("value4"))
	parentKVStore.Set([]byte("key5"), []byte("value5"))

	writeset := make(multiversion.WriteSet)
	writeset["key1"] = []byte("value1")
	writeset["key2"] = []byte("value2")
	writeset["key3"] = nil
	mvs.SetWriteset(1, 2, writeset)

	iter := vis.Iterator([]byte("key1"), []byte("key7"))
	for ; iter.Valid(); iter.Next() {
		// read value
		iter.Value()
	}
	// iterator is never closed
	vis.WriteToMultiVersionStore()

	// addition of key6
	writeset2 := make(multiversion.WriteSet)
	writeset2["key6"] = []byte("value6")
	mvs.SetWriteset(2, 2, writeset2)

	// should be invalid
	valid, conflicts := mvs.ValidateTransactionState(5)
	require.False(t, valid)
	require.Empty(t, conflicts)
}

func TestMVSIteratorValidationEarlyStopIncludedInIterateset(t *testing.T) {
	parentKVStore := dbadapter.Store{DB: dbm.NewMemDB()}
	mvs := multiversion.NewMultiVersionStore(parentKVStore)
	vis := multiversion.NewVersionIndexedStore(parentKVStore, mvs, 2, 1, make(chan occ.Abort, 1))

	parentKVStore.Set([]byte("key1"), []byte("value1"))
	parentKVStore.Set([]byte("key2"), []byte("value2"))
	parentKVStore.Set([]byte("key3"), []byte("value3"))
	parentKVStore.Set([]byte("key4"), []byte("value4"))

	i := 0
	iter := vis.Iterator([]byte("key1"), []byte("key5"))
	for ; iter.Valid(); iter.Next() {
		// break after iterating 2 items
		if i == 2 {
			break
		}
		i++
	}
	iter.Close()
	vis.WriteToMultiVersionStore()

	// should be valid
	valid, conflicts := mvs.ValidateTransactionState(2)
	require.True(t, valid)
	require.Empty(t, conflicts)
}
