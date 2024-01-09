package cachekv_test

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/store/cachekv"
	"github.com/cosmos/cosmos-sdk/store/dbadapter"
	"github.com/cosmos/cosmos-sdk/store/types"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"
)

func TestMangerIterator(t *testing.T) {
	// initiate mock kvstore
	mem := dbadapter.Store{DB: dbm.NewMemDB()}
	kvstore := cachekv.NewStore(mem, types.NewKVStoreKey("CacheKvTest"), types.DefaultCacheSizeLimit)
	value := randSlice(defaultValueSizeBz)
	startKey := randSlice(32)

	keys := generateSequentialKeys(startKey, 3)
	for _, k := range keys {
		kvstore.Set(k, value)
	}

	// initialize mock mergeIterator
	parent := kvstore.Iterator(keys[0], keys[2])
	cache := kvstore.Iterator(nil, nil)
	for ; cache.Valid(); cache.Next() {
	}
	iter := cachekv.NewCacheMergeIterator(parent, cache, true, types.NewKVStoreKey("CacheKvTest"))

	// get the next value and it should not be nil
	nextValue := iter.Value()
	require.NotNil(t, nextValue)

	// get the next value
	nextValue = iter.Value()
	require.NotNil(t, nextValue)
}
