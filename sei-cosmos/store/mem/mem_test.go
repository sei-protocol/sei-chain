package mem_test

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-cosmos/store/cachekv"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-cosmos/store/mem"
	"github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
)

func TestStore(t *testing.T) {
	db := mem.NewStore()
	key, value := []byte("key"), []byte("value")

	require.Equal(t, types.StoreTypeMemory, db.GetStoreType())

	require.Nil(t, db.Get(key))
	db.Set(key, value)
	require.Equal(t, value, db.Get(key))

	newValue := []byte("newValue")
	db.Set(key, newValue)
	require.Equal(t, newValue, db.Get(key))

	db.Delete(key)
	require.Nil(t, db.Get(key))

	cacheWrapper := db.CacheWrap(nil)
	require.IsType(t, &cachekv.Store{}, cacheWrapper)

	cacheWrappedWithTrace := db.CacheWrapWithTrace(nil, nil, nil)
	require.IsType(t, &cachekv.Store{}, cacheWrappedWithTrace)
}

func TestCommit(t *testing.T) {
	db := mem.NewStore()
	key, value := []byte("key"), []byte("value")

	db.Set(key, value)
	id := db.Commit(true)
	require.True(t, id.IsZero())
	require.True(t, db.LastCommitID().IsZero())
	require.Equal(t, value, db.Get(key))
}
