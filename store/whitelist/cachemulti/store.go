package cachemulti

import (
	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	"github.com/sei-protocol/sei-chain/store/whitelist/kv"
)

// Since `CacheMultiStore` has a method with the same name, we have to
// type alias here or otherwise we won't be able to inherit or implement
// `CacheMultiStore` the method.
type sdkConcurrentCacheMultiStore = storetypes.ConcurrentCacheMultiStore

type Store struct {
	sdkConcurrentCacheMultiStore

	storeKeyToWriteWhitelist map[string][]string
}

func NewStore(
	parent storetypes.ConcurrentCacheMultiStore,
	storeKeyToWriteWhitelist map[string][]string,
) storetypes.ConcurrentCacheMultiStore {
	return &Store{
		sdkConcurrentCacheMultiStore: parent,
		storeKeyToWriteWhitelist: storeKeyToWriteWhitelist,
	}
}

func (cms Store) ConcurrentCacheMultiStore() storetypes.ConcurrentCacheMultiStore {
	return NewStore(cms.sdkConcurrentCacheMultiStore.CacheMultiStore(), cms.storeKeyToWriteWhitelist)
}

func (cms Store) GetKVStore(key storetypes.StoreKey) storetypes.KVStore {
	rawKVStore := cms.sdkConcurrentCacheMultiStore.GetKVStore(key)
	if writeWhitelist, ok := cms.storeKeyToWriteWhitelist[key.Name()]; ok {
		return kv.NewStore(rawKVStore, writeWhitelist)
	}
	// whitelist nothing
	return kv.NewStore(rawKVStore, []string{})
}
