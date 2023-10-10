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
	mvs := multiversion.NewMultiVersionStore()
	// initialize a new VersionIndexedStore
	vis := multiversion.NewVersionIndexedStore(parentKVStore, mvs, 1, 2, make(chan scheduler.Abort))

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
	require.Equal(t, []byte("value1"), vis.GetReadset()["key1"])

	// read the same key that should now be served from the readset (can be verified by setting a different value for the key in the parent store)
	parentKVStore.Set([]byte("key1"), []byte("value2")) // realistically shouldn't happen, modifying to verify readset access
	val3 := vis.Get([]byte("key1"))
	require.True(t, vis.Has([]byte("key1")))
	require.Equal(t, []byte("value1"), val3)

	// test deleted value written to MVS but not parent store
	mvs.Delete(0, 2, []byte("delKey"))
	parentKVStore.Set([]byte("delKey"), []byte("value4"))
	valDel := vis.Get([]byte("delKey"))
	require.Nil(t, valDel)
	require.False(t, vis.Has([]byte("delKey")))

	// set different key in MVS - for various indices
	mvs.Set(0, 2, []byte("key3"), []byte("value3"))
	mvs.Set(2, 1, []byte("key3"), []byte("value4"))
	mvs.SetEstimate(5, 0, []byte("key3"))

	// read the key that falls down to MVS
	val4 := vis.Get([]byte("key3"))
	// should equal value3 because value4 is later than the key in question
	require.Equal(t, []byte("value3"), val4)
	require.True(t, vis.Has([]byte("key3")))

	// try a read that falls through to MVS with a later tx index
	vis2 := multiversion.NewVersionIndexedStore(parentKVStore, mvs, 3, 2, make(chan scheduler.Abort))
	val5 := vis2.Get([]byte("key3"))
	// should equal value3 because value4 is later than the key in question
	require.Equal(t, []byte("value4"), val5)
	require.True(t, vis2.Has([]byte("key3")))

	// test estimate values writing to abortChannel
	abortChannel := make(chan scheduler.Abort)
	vis3 := multiversion.NewVersionIndexedStore(parentKVStore, mvs, 6, 2, abortChannel)
	go func() {
		vis3.Get([]byte("key3"))
	}()
	abort := <-abortChannel // read the abort from the channel
	require.Equal(t, 5, abort.DependentTxIdx)
	require.Equal(t, scheduler.ErrReadEstimate, abort.Err)

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
	mvs := multiversion.NewMultiVersionStore()
	// initialize a new VersionIndexedStore
	vis := multiversion.NewVersionIndexedStore(parentKVStore, mvs, 1, 2, make(chan scheduler.Abort))

	// test simple set
	vis.Set([]byte("key1"), []byte("value1"))
	require.Equal(t, []byte("value1"), vis.GetWriteset()["key1"])

	mvs.Set(0, 1, []byte("key2"), []byte("value2"))
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
	mvs := multiversion.NewMultiVersionStore()
	// initialize a new VersionIndexedStore
	vis := multiversion.NewVersionIndexedStore(parentKVStore, mvs, 1, 2, make(chan scheduler.Abort))

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
	mvs := multiversion.NewMultiVersionStore()
	// initialize a new VersionIndexedStore
	vis := multiversion.NewVersionIndexedStore(parentKVStore, mvs, 1, 2, make(chan scheduler.Abort))

	mvs.Set(0, 1, []byte("key3"), []byte("value3"))

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
	mvs := multiversion.NewMultiVersionStore()
	// initialize a new VersionIndexedStore
	vis := multiversion.NewVersionIndexedStore(parentKVStore, mvs, 1, 2, make(chan scheduler.Abort))

	mvs.Set(0, 1, []byte("key3"), []byte("value3"))

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
	mvs := multiversion.NewMultiVersionStore()
	// initialize a new VersionIndexedStore
	abortC := make(chan scheduler.Abort)
	vis := multiversion.NewVersionIndexedStore(parentKVStore, mvs, 2, 2, abortC)
	// set some initial values
	parentKVStore.Set([]byte("key4"), []byte("value4"))
	parentKVStore.Set([]byte("key5"), []byte("value5"))
	parentKVStore.Set([]byte("deletedKey"), []byte("foo"))
	mvs.Set(0, 1, []byte("key1"), []byte("value1"))
	mvs.Set(0, 1, []byte("key2"), []byte("value2"))
	mvs.Delete(0, 1, []byte("deletedKey"))

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
	mvs.Set(1, 1, []byte("key3"), []byte("value3"))
	// should still have valid readset
	require.True(t, vis.ValidateReadset())

	// modify underlying transaction key that is related
	mvs.Set(1, 1, []byte("key1"), []byte("value1_b"))
	// should now have invalid readset
	require.False(t, vis.ValidateReadset())
	// reset so readset is valid again
	mvs.Set(1, 1, []byte("key1"), []byte("value1"))
	require.True(t, vis.ValidateReadset())

	// mvs has a value that was initially read from parent
	mvs.Set(1, 2, []byte("key4"), []byte("value4_b"))
	require.False(t, vis.ValidateReadset())
	// reset key
	mvs.Set(1, 2, []byte("key4"), []byte("value4"))
	require.True(t, vis.ValidateReadset())

	// mvs has a value that was initially read from parent - BUT in a later tx index
	mvs.Set(4, 2, []byte("key4"), []byte("value4_c"))
	// readset should remain valid
	require.True(t, vis.ValidateReadset())

	// mvs has an estimate
	mvs.SetEstimate(1, 2, []byte("key2"))
	// readset should be invalid now - but via abort channel write
	go func() {
		vis.ValidateReadset()
	}()
	abort := <-abortC // read the abort from the channel
	require.Equal(t, 1, abort.DependentTxIdx)

	// test key deleted later
	mvs.Delete(1, 1, []byte("key2"))
	require.False(t, vis.ValidateReadset())
	// reset key2
	mvs.Set(1, 1, []byte("key2"), []byte("value2"))

	// lastly verify panic if parent kvstore has a conflict - this shouldn't happen but lets assert that it would panic
	parentKVStore.Set([]byte("keyDNE"), []byte("foobar"))
	require.Equal(t, []byte("foobar"), parentKVStore.Get([]byte("keyDNE")))
	require.Panics(t, func() {
		vis.ValidateReadset()
	})
}
