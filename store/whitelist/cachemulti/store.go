package cachemulti

import (
	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	"github.com/sei-protocol/sei-chain/store/whitelist/kv"
)

// Since `CacheMultiStore` has a method with the same name, we have to
// type alias here or otherwise we won't be able to inherit or implement
// `CacheMultiStore` the method.
type sdkCacheMultiStore = storetypes.CacheMultiStore

type Store struct {
	sdkCacheMultiStore

	storeKeyToWriteWhitelist map[string][]string
}

func NewStore(parent storetypes.CacheMultiStore, storeKeyToWriteWhitelist map[string][]string) storetypes.CacheMultiStore {
	return &Store{
		sdkCacheMultiStore:       parent,
		storeKeyToWriteWhitelist: storeKeyToWriteWhitelist,
	}
}

func (cms Store) CacheMultiStore() storetypes.CacheMultiStore {
	return NewStore(cms.sdkCacheMultiStore.CacheMultiStore(), cms.storeKeyToWriteWhitelist)
}

func (cms Store) GetKVStore(key storetypes.StoreKey) storetypes.KVStore {
	rawKVStore := cms.sdkCacheMultiStore.GetKVStore(key)
	if writeWhitelist, ok := cms.storeKeyToWriteWhitelist[key.Name()]; ok {
		return kv.NewStore(rawKVStore, writeWhitelist)
	}
	// whitelist nothing
	return kv.NewStore(rawKVStore, []string{})
}
