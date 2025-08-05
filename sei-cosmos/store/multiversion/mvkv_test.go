package multiversion_test

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/store/cachekv"
	"github.com/cosmos/cosmos-sdk/store/dbadapter"
	"github.com/cosmos/cosmos-sdk/store/multiversion"
	"github.com/cosmos/cosmos-sdk/store/types"
	scheduler "github.com/cosmos/cosmos-sdk/types/occ"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"
)

func TestVersionIndexedStoreGetters(t *testing.T) {
	mem := dbadapter.Store{DB: dbm.NewMemDB()}
	parentKVStore := cachekv.NewStore(mem, types.NewKVStoreKey("mock"), 1000)
	mvs := multiversion.NewMultiVersionStore(parentKVStore)
	// initialize a new VersionIndexedStore
	vis := multiversion.NewVersionIndexedStore(parentKVStore, mvs, 1, 2, make(chan scheduler.Abort, 1))

	// mock a value in the parent store
	parentKVStore.Set([]byte("key1"), []byte("value1"))

	// read key that doesn't exist
	val := vis.Get([]byte("key2"))
	require.Nil(t, val)
	require.False(t, vis.Has([]byte("key2")))

	// read key that falls down to parent store
	val2 := vis.Get([]byte("key1"))
	require.Equal(t, []byte("value1"), val2)
	require.True(t, vis.Has([]byte("key1")))
	// verify value now in readset
	require.Equal(t, [][]byte{[]byte("value1")}, vis.GetReadset()["key1"])

	// read the same key that should now be served from the readset (can be verified by setting a different value for the key in the parent store)
	parentKVStore.Set([]byte("key1"), []byte("value2")) // realistically shouldn't happen, modifying to verify readset access
	val3 := vis.Get([]byte("key1"))
	require.True(t, vis.Has([]byte("key1")))
	require.Equal(t, []byte("value1"), val3)

	// test deleted value written to MVS but not parent store
	mvs.SetWriteset(0, 2, map[string][]byte{
		"delKey": nil,
	})
	parentKVStore.Set([]byte("delKey"), []byte("value4"))
	valDel := vis.Get([]byte("delKey"))
	require.Nil(t, valDel)
	require.False(t, vis.Has([]byte("delKey")))

	// set different key in MVS - for various indices
	mvs.SetWriteset(0, 2, map[string][]byte{
		"delKey": nil,
		"key3":   []byte("value3"),
	})
	mvs.SetWriteset(2, 1, map[string][]byte{
		"key3": []byte("value4"),
	})
	mvs.SetEstimatedWriteset(5, 0, map[string][]byte{
		"key3": nil,
	})

	// read the key that falls down to MVS
	val4 := vis.Get([]byte("key3"))
	// should equal value3 because value4 is later than the key in question
	require.Equal(t, []byte("value3"), val4)
	require.True(t, vis.Has([]byte("key3")))

	// try a read that falls through to MVS with a later tx index
	vis2 := multiversion.NewVersionIndexedStore(parentKVStore, mvs, 3, 2, make(chan scheduler.Abort, 1))
	val5 := vis2.Get([]byte("key3"))
	// should equal value3 because value4 is later than the key in question
	require.Equal(t, []byte("value4"), val5)
	require.True(t, vis2.Has([]byte("key3")))

	// test estimate values writing to abortChannel
	abortChannel := make(chan scheduler.Abort, 1)
	vis3 := multiversion.NewVersionIndexedStore(parentKVStore, mvs, 6, 2, abortChannel)
	require.Panics(t, func() {
		vis3.Get([]byte("key3"))
	})
	abort := <-abortChannel // read the abort from the channel
	require.Equal(t, 5, abort.DependentTxIdx)
	// require.Equal(t, scheduler.ErrReadEstimate, abort.Err)

	vis.Set([]byte("key4"), []byte("value4"))
	// verify proper response for GET
	val6 := vis.Get([]byte("key4"))
	require.True(t, vis.Has([]byte("key4")))
	require.Equal(t, []byte("value4"), val6)
	// verify that its in the writeset
	require.Equal(t, []byte("value4"), vis.GetWriteset()["key4"])
	// verify that its not in the readset
	require.Nil(t, vis.GetReadset()["key4"])
}

func TestVersionIndexedStoreSetters(t *testing.T) {
	mem := dbadapter.Store{DB: dbm.NewMemDB()}
	parentKVStore := cachekv.NewStore(mem, types.NewKVStoreKey("mock"), 1000)
	mvs := multiversion.NewMultiVersionStore(parentKVStore)
	// initialize a new VersionIndexedStore
	vis := multiversion.NewVersionIndexedStore(parentKVStore, mvs, 1, 2, make(chan scheduler.Abort, 1))

	// test simple set
	vis.Set([]byte("key1"), []byte("value1"))
	require.Equal(t, []byte("value1"), vis.GetWriteset()["key1"])

	mvs.SetWriteset(0, 1, map[string][]byte{
		"key2": []byte("value2"),
	})
	vis.Delete([]byte("key2"))
	require.Nil(t, vis.Get([]byte("key2")))
	// because the delete should be at the writeset level, we should not have populated the readset
	require.Zero(t, len(vis.GetReadset()))

	// try setting the value again, and then read
	vis.Set([]byte("key2"), []byte("value3"))
	require.Equal(t, []byte("value3"), vis.Get([]byte("key2")))
	require.Zero(t, len(vis.GetReadset()))
}

func TestVersionIndexedStoreBoilerplateFunctions(t *testing.T) {
	mem := dbadapter.Store{DB: dbm.NewMemDB()}
	parentKVStore := cachekv.NewStore(mem, types.NewKVStoreKey("mock"), 1000)
	mvs := multiversion.NewMultiVersionStore(parentKVStore)
	// initialize a new VersionIndexedStore
	vis := multiversion.NewVersionIndexedStore(parentKVStore, mvs, 1, 2, make(chan scheduler.Abort, 1))

	// asserts panics where appropriate
	require.Panics(t, func() { vis.CacheWrap(types.NewKVStoreKey("mock")) })
	require.Panics(t, func() { vis.CacheWrapWithListeners(types.NewKVStoreKey("mock"), nil) })
	require.Panics(t, func() { vis.CacheWrapWithTrace(types.NewKVStoreKey("mock"), nil, nil) })
	require.Panics(t, func() { vis.GetWorkingHash() })

	// assert properly returns store type
	require.Equal(t, types.StoreTypeDB, vis.GetStoreType())
}

func TestVersionIndexedStoreWrite(t *testing.T) {
	mem := dbadapter.Store{DB: dbm.NewMemDB()}
	parentKVStore := cachekv.NewStore(mem, types.NewKVStoreKey("mock"), 1000)
	mvs := multiversion.NewMultiVersionStore(parentKVStore)
	// initialize a new VersionIndexedStore
	vis := multiversion.NewVersionIndexedStore(parentKVStore, mvs, 1, 2, make(chan scheduler.Abort, 1))

	mvs.SetWriteset(0, 1, map[string][]byte{
		"key3": []byte("value3"),
	})

	require.False(t, mvs.Has(3, []byte("key1")))
	require.False(t, mvs.Has(3, []byte("key2")))
	require.True(t, mvs.Has(3, []byte("key3")))

	// write some keys
	vis.Set([]byte("key1"), []byte("value1"))
	vis.Set([]byte("key2"), []byte("value2"))
	vis.Delete([]byte("key3"))

	vis.WriteToMultiVersionStore()

	require.Equal(t, []byte("value1"), mvs.GetLatest([]byte("key1")).Value())
	require.Equal(t, []byte("value2"), mvs.GetLatest([]byte("key2")).Value())
	require.True(t, mvs.GetLatest([]byte("key3")).IsDeleted())
}

func TestVersionIndexedStoreWriteEstimates(t *testing.T) {
	mem := dbadapter.Store{DB: dbm.NewMemDB()}
	parentKVStore := cachekv.NewStore(mem, types.NewKVStoreKey("mock"), 1000)
	mvs := multiversion.NewMultiVersionStore(parentKVStore)
	// initialize a new VersionIndexedStore
	vis := multiversion.NewVersionIndexedStore(parentKVStore, mvs, 1, 2, make(chan scheduler.Abort, 1))

	mvs.SetWriteset(0, 1, map[string][]byte{
		"key3": []byte("value3"),
	})

	require.False(t, mvs.Has(3, []byte("key1")))
	require.False(t, mvs.Has(3, []byte("key2")))
	require.True(t, mvs.Has(3, []byte("key3")))

	// write some keys
	vis.Set([]byte("key1"), []byte("value1"))
	vis.Set([]byte("key2"), []byte("value2"))
	vis.Delete([]byte("key3"))

	vis.WriteEstimatesToMultiVersionStore()

	require.True(t, mvs.GetLatest([]byte("key1")).IsEstimate())
	require.True(t, mvs.GetLatest([]byte("key2")).IsEstimate())
	require.True(t, mvs.GetLatest([]byte("key3")).IsEstimate())
}

func TestVersionIndexedStoreValidation(t *testing.T) {
	mem := dbadapter.Store{DB: dbm.NewMemDB()}
	parentKVStore := cachekv.NewStore(mem, types.NewKVStoreKey("mock"), 1000)
	mvs := multiversion.NewMultiVersionStore(parentKVStore)
	// initialize a new VersionIndexedStore
	abortC := make(chan scheduler.Abort, 1)
	vis := multiversion.NewVersionIndexedStore(parentKVStore, mvs, 2, 2, abortC)
	// set some initial values
	parentKVStore.Set([]byte("key4"), []byte("value4"))
	parentKVStore.Set([]byte("key5"), []byte("value5"))
	parentKVStore.Set([]byte("deletedKey"), []byte("foo"))

	mvs.SetWriteset(0, 1, map[string][]byte{
		"key1":       []byte("value1"),
		"key2":       []byte("value2"),
		"deletedKey": nil,
	})

	// load those into readset
	vis.Get([]byte("key1"))
	vis.Get([]byte("key2"))
	vis.Get([]byte("key4"))
	vis.Get([]byte("key5"))
	vis.Get([]byte("keyDNE"))
	vis.Get([]byte("deletedKey"))

	// everything checks out, so we should be able to validate successfully
	require.True(t, vis.ValidateReadset())
	// modify underlying transaction key that is unrelated
	mvs.SetWriteset(1, 1, map[string][]byte{
		"key3": []byte("value3"),
	})
	// should still have valid readset
	require.True(t, vis.ValidateReadset())

	// modify underlying transaction key that is related
	mvs.SetWriteset(1, 1, map[string][]byte{
		"key3": []byte("value3"),
		"key1": []byte("value1_b"),
	})
	// should now have invalid readset
	require.False(t, vis.ValidateReadset())
	// reset so readset is valid again
	mvs.SetWriteset(1, 1, map[string][]byte{
		"key3": []byte("value3"),
		"key1": []byte("value1"),
	})
	require.True(t, vis.ValidateReadset())

	// mvs has a value that was initially read from parent
	mvs.SetWriteset(1, 1, map[string][]byte{
		"key3": []byte("value3"),
		"key1": []byte("value1"),
		"key4": []byte("value4_b"),
	})
	require.False(t, vis.ValidateReadset())
	// reset key
	mvs.SetWriteset(1, 1, map[string][]byte{
		"key3": []byte("value3"),
		"key1": []byte("value1"),
		"key4": []byte("value4"),
	})
	require.True(t, vis.ValidateReadset())

	// mvs has a value that was initially read from parent - BUT in a later tx index
	mvs.SetWriteset(4, 2, map[string][]byte{
		"key4": []byte("value4_c"),
	})
	// readset should remain valid
	require.True(t, vis.ValidateReadset())

	// mvs has an estimate
	mvs.SetEstimatedWriteset(1, 1, map[string][]byte{
		"key2": nil,
	})
	// readset should be invalid now - but via abort channel write
	go func() {
		vis.ValidateReadset()
	}()
	abort := <-abortC // read the abort from the channel
	require.Equal(t, 1, abort.DependentTxIdx)

	// test key deleted later
	mvs.SetWriteset(1, 1, map[string][]byte{
		"key3": []byte("value3"),
		"key1": []byte("value1"),
		"key4": []byte("value4"),
		"key2": nil,
	})
	require.False(t, vis.ValidateReadset())
	// reset key2
	mvs.SetWriteset(1, 1, map[string][]byte{
		"key3": []byte("value3"),
		"key1": []byte("value1"),
		"key4": []byte("value4"),
		"key2": []byte("value2"),
	})

	// lastly verify panic if parent kvstore has a conflict - this shouldn't happen but lets assert that it would panic
	parentKVStore.Set([]byte("keyDNE"), []byte("foobar"))
	require.Equal(t, []byte("foobar"), parentKVStore.Get([]byte("keyDNE")))
	require.Panics(t, func() {
		vis.ValidateReadset()
	})
}

func TestIterator(t *testing.T) {
	mem := dbadapter.Store{DB: dbm.NewMemDB()}
	parentKVStore := cachekv.NewStore(mem, types.NewKVStoreKey("mock"), 1000)
	mvs := multiversion.NewMultiVersionStore(parentKVStore)
	// initialize a new VersionIndexedStore
	abortC := make(chan scheduler.Abort, 1)
	vis := multiversion.NewVersionIndexedStore(parentKVStore, mvs, 2, 2, abortC)

	// set some initial values
	parentKVStore.Set([]byte("key4"), []byte("value4"))
	parentKVStore.Set([]byte("key5"), []byte("value5"))
	parentKVStore.Set([]byte("deletedKey"), []byte("foo"))
	mvs.SetWriteset(0, 1, map[string][]byte{
		"key1":       []byte("value1"),
		"key2":       []byte("value2"),
		"deletedKey": nil,
	})
	// add an estimate to MVS
	mvs.SetEstimatedWriteset(3, 1, map[string][]byte{
		"key3": []byte("value1_b"),
	})

	// iterate over the keys - exclusive on key5
	iter := vis.Iterator([]byte("000"), []byte("key5"))

	// verify domain is superset
	start, end := iter.Domain()
	require.Equal(t, []byte("000"), start)
	require.Equal(t, []byte("key5"), end)

	vals := []string{}
	defer iter.Close()
	for ; iter.Valid(); iter.Next() {
		vals = append(vals, string(iter.Value()))
	}
	require.Equal(t, []string{"value1", "value2", "value4"}, vals)
	iter.Close()

	// test reverse iteration
	vals2 := []string{}
	iter2 := vis.ReverseIterator([]byte("000"), []byte("key6"))
	defer iter2.Close()
	for ; iter2.Valid(); iter2.Next() {
		vals2 = append(vals2, string(iter2.Value()))
	}
	// has value5 because of end being key6
	require.Equal(t, []string{"value5", "value4", "value2", "value1"}, vals2)
	iter2.Close()

	// add items to writeset
	vis.Set([]byte("key3"), []byte("value3"))
	vis.Set([]byte("key4"), []byte("valueNew"))

	// iterate over the keys - exclusive on key5
	iter3 := vis.Iterator([]byte("000"), []byte("key5"))
	vals3 := []string{}
	defer iter3.Close()
	for ; iter3.Valid(); iter3.Next() {
		vals3 = append(vals3, string(iter3.Value()))
	}
	require.Equal(t, []string{"value1", "value2", "value3", "valueNew"}, vals3)
	iter3.Close()

	vis.Set([]byte("key6"), []byte("value6"))
	// iterate over the keys, writeset being the last of the iteration range
	iter4 := vis.Iterator([]byte("000"), []byte("key7"))
	vals4 := []string{}
	defer iter4.Close()
	for ; iter4.Valid(); iter4.Next() {
		vals4 = append(vals4, string(iter4.Value()))
	}
	require.Equal(t, []string{"value1", "value2", "value3", "valueNew", "value5", "value6"}, vals4)
	iter4.Close()

	// add an estimate to MVS
	mvs.SetEstimatedWriteset(1, 1, map[string][]byte{
		"key2": []byte("value1_b"),
	})
	// need to reset readset
	abortC2 := make(chan scheduler.Abort, 1)
	visNew := multiversion.NewVersionIndexedStore(parentKVStore, mvs, 2, 3, abortC2)
	require.Panics(t, func() {
		iter5 := visNew.Iterator([]byte("000"), []byte("key5"))
		defer iter5.Close()
		// new iter
		for ; iter5.Valid(); iter5.Next() {
		}
	})
	abort := <-abortC2 // read the abort from the channel
	require.Equal(t, 1, abort.DependentTxIdx)
}

func TestIteratorReadsetRace(t *testing.T) {
	mem := dbadapter.Store{DB: dbm.NewMemDB()}
	parentKVStore := cachekv.NewStore(mem, types.NewKVStoreKey("mock"), 1000)
	mvs := multiversion.NewMultiVersionStore(parentKVStore)
	// initialize a new VersionIndexedStore
	abortC := make(chan scheduler.Abort, 1)
	vis := multiversion.NewVersionIndexedStore(parentKVStore, mvs, 2, 2, abortC)

	// set some initial values
	parentKVStore.Set([]byte("key4"), []byte("value4"))
	parentKVStore.Set([]byte("key5"), []byte("value5"))
	parentKVStore.Set([]byte("deletedKey"), []byte("foo"))
	mvs.SetWriteset(0, 1, map[string][]byte{
		"key1":       []byte("value1"),
		"key2":       []byte("value2"),
		"deletedKey": nil,
	})

	// iterate over the keys - exclusive on key5
	iter := vis.Iterator([]byte("000"), []byte("key5"))

	// verify domain is superset
	start, end := iter.Domain()
	require.Equal(t, []byte("000"), start)
	require.Equal(t, []byte("key5"), end)

	// add new writeset for txIndex 1 - writes to key4
	mvs.SetWriteset(1, 1, map[string][]byte{
		"key4": []byte("value4NEW"),
	})

	// perform a read on key4 from VIS and assert it is the new value
	val := vis.Get([]byte("key4"))
	require.Equal(t, []byte("value4NEW"), val)

	vals := []string{}
	defer iter.Close()
	for ; iter.Valid(); iter.Next() {
		vals = append(vals, string(iter.Value()))
	}
	// we still read the OLD value because key4 wasn't known to be in the MVS thus wasnt served
	require.Equal(t, []string{"value1", "value2", "value4"}, vals)
	iter.Close()

	// verify that key4 has two elements in the readset
	readset := vis.GetReadset()
	require.Len(t, readset["key4"], 2)
}

func TestRemoveLastEntry(t *testing.T) {
	parentKVStore := dbadapter.Store{DB: dbm.NewMemDB()}
	mvs := multiversion.NewMultiVersionStore(parentKVStore)
	vis := multiversion.NewVersionIndexedStore(parentKVStore, mvs, 2, 1, make(chan scheduler.Abort, 1))

	parentKVStore.Set([]byte("key1"), []byte("value1"))
	parentKVStore.Set([]byte("key2"), []byte("value2"))
	parentKVStore.Set([]byte("key3"), []byte("value3"))

	i := 0
	iter := vis.Iterator([]byte("key1"), []byte("key5"))
	for iter.Valid() {
		i++
		iter.Next()
		// break after iterating 2 items
		if i == 2 {
			// this checks if a key exists after key2
			if iter.Valid() {
				//do something that affects consensus here (e.g. emit event)
			}
			break
		}
	}
	iter.Close()
	vis.WriteToMultiVersionStore()

	// removal of key3 by an earlier tx - should invalidate iterateset
	writeset := make(multiversion.WriteSet)
	writeset["key3"] = nil
	mvs.SetWriteset(1, 1, writeset)

	// should not be valid
	valid, conflicts := mvs.ValidateTransactionState(2)
	require.False(t, valid)
	require.Empty(t, conflicts)
}

func TestVersionIndexedStoreGetAllKeyStrsInRange(t *testing.T) {
	mem := dbadapter.Store{DB: dbm.NewMemDB()}
	parentKVStore := cachekv.NewStore(mem, types.NewKVStoreKey("mock"), 1000)
	mvs := multiversion.NewMultiVersionStore(parentKVStore)
	// initialize a new VersionIndexedStore
	vis := multiversion.NewVersionIndexedStore(parentKVStore, mvs, 1, 2, make(chan scheduler.Abort, 1))

	parentKVStore.Set([]byte("key1"), []byte("value1")) // unchanged
	parentKVStore.Set([]byte("key2"), []byte("value2")) // updated by index 0 incar 1
	parentKVStore.Set([]byte("key3"), []byte("value3")) // deleted by index 0 incar 1
	parentKVStore.Set([]byte("key4"), []byte("value4")) // deleted by index 2
	parentKVStore.Set([]byte("key5"), []byte("value5")) // updated by vis
	parentKVStore.Set([]byte("key6"), []byte("value6")) // deleted by vis
	mvs.SetWriteset(0, 1, map[string][]byte{
		"key2": []byte("value8"),
		"key3": nil,
	})
	mvs.SetWriteset(2, 1, map[string][]byte{
		"key4": nil,
	})
	vis.Set([]byte("key5"), []byte("value7"))
	vis.Delete([]byte("key6"))
	require.Equal(t, []string{"key1", "key2", "key4", "key5"}, vis.GetAllKeyStrsInRange(nil, nil))
}

func TestVersionIndexedStoreGetAllKeyStrsInRangeError(t *testing.T) {
	mem := dbadapter.Store{DB: dbm.NewMemDB()}
	parentKVStore := cachekv.NewStore(mem, types.NewKVStoreKey("mock"), 1000)
	mvs := multiversion.NewMultiVersionStore(parentKVStore)
	// initialize a new VersionIndexedStore
	vis2 := multiversion.NewVersionIndexedStore(parentKVStore, mvs, 2, 1, make(chan scheduler.Abort, 1))
	vis3 := multiversion.NewVersionIndexedStore(parentKVStore, mvs, 3, 1, make(chan scheduler.Abort, 1))

	parentKVStore.Set([]byte("key1"), []byte("value1"))
	parentKVStore.Set([]byte("key2"), []byte("value2"))

	mvs.SetWriteset(1, 1, map[string][]byte{
		"key1": []byte("value3"),
	})
	vis2.Set([]byte("key3"), []byte("value4"))
	vis3.DeleteAll(nil, nil)
	vis2.WriteToMultiVersionStore()
	vis3.WriteToMultiVersionStore()
	valid, _ := mvs.ValidateTransactionState(3)
	require.False(t, valid)
}
