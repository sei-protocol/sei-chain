package cachemulti

import (
	"fmt"
	"sync"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-cosmos/store/dbadapter"
	"github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"
)

func TestStoreGetKVStore(t *testing.T) {
	require := require.New(t)

	s := Store{stores: map[types.StoreKey]types.CacheWrap{}, mu: &sync.RWMutex{}}
	key := types.NewKVStoreKey("abc")
	errMsg := fmt.Sprintf("kv store with key %v has not been registered in stores", key)

	require.PanicsWithValue(errMsg,
		func() { s.GetStore(key) })

	require.PanicsWithValue(errMsg,
		func() { s.GetKVStore(key) })
}

func TestNestedCacheMultiStorePreservesStoreKeys(t *testing.T) {
	db := dbm.NewMemDB()

	// Create store keys
	key1 := types.NewKVStoreKey("store1")
	key2 := types.NewKVStoreKey("store2")
	key3 := types.NewKVStoreKey("store3")

	// Create the keys map (string -> StoreKey)
	keys := map[string]types.StoreKey{
		key1.Name(): key1,
		key2.Name(): key2,
		key3.Name(): key3,
	}

	// Create stores map with dbadapter stores
	stores := map[types.StoreKey]types.CacheWrapper{
		key1: dbadapter.Store{DB: db},
		key2: dbadapter.Store{DB: db},
		key3: dbadapter.Store{DB: db},
	}

	// Create the initial CacheMultiStore
	cms := NewStore(db, stores, keys, nil, nil, nil)

	// Verify the original store returns the correct keys
	originalKeys := cms.StoreKeys()
	require.Len(t, originalKeys, 3, "Original CacheMultiStore should have 3 store keys")

	// Verify each key is present
	keySet := make(map[string]bool)
	for _, k := range originalKeys {
		keySet[k.Name()] = true
	}
	require.True(t, keySet["store1"], "Original should have store1")
	require.True(t, keySet["store2"], "Original should have store2")
	require.True(t, keySet["store3"], "Original should have store3")

	nestedCms := cms.CacheMultiStore()

	// The nested store should also return the same keys
	nestedKeys := nestedCms.StoreKeys()
	require.Len(t, nestedKeys, 3,
		"Nested CacheMultiStore should preserve store keys for OCC scheduler")

	// Verify each key is present in nested store
	nestedKeySet := make(map[string]bool)
	for _, k := range nestedKeys {
		nestedKeySet[k.Name()] = true
	}
	require.True(t, nestedKeySet["store1"], "Nested should have store1")
	require.True(t, nestedKeySet["store2"], "Nested should have store2")
	require.True(t, nestedKeySet["store3"], "Nested should have store3")
}

func TestDoubleNestedCacheMultiStorePreservesStoreKeys(t *testing.T) {
	db := dbm.NewMemDB()

	key1 := types.NewKVStoreKey("evm")
	key2 := types.NewKVStoreKey("bank")

	keys := map[string]types.StoreKey{
		key1.Name(): key1,
		key2.Name(): key2,
	}

	stores := map[types.StoreKey]types.CacheWrapper{
		key1: dbadapter.Store{DB: db},
		key2: dbadapter.Store{DB: db},
	}

	// Level 0: Original store
	cms0 := NewStore(db, stores, keys, nil, nil, nil)
	require.Len(t, cms0.StoreKeys(), 2, "Level 0 should have 2 keys")

	// Level 1: First nested store
	cms1 := cms0.CacheMultiStore()
	require.Len(t, cms1.StoreKeys(), 2, "Level 1 should preserve 2 keys")

	// Level 2: Double nested store
	cms2 := cms1.CacheMultiStore()
	require.Len(t, cms2.StoreKeys(), 2, "Level 2 should preserve 2 keys")
}
