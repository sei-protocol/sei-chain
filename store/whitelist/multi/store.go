package multi

import (
	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	"github.com/sei-protocol/sei-chain/store/whitelist/cachemulti"
	"github.com/sei-protocol/sei-chain/store/whitelist/kv"
)

type Store struct {
	storetypes.MultiStore

	storeKeyToWriteWhitelist map[storetypes.StoreKey][]string
}

func NewStore(parent storetypes.MultiStore, storeKeyToWriteWhitelist map[storetypes.StoreKey][]string) storetypes.MultiStore {
	return &Store{
		MultiStore:               parent,
		storeKeyToWriteWhitelist: storeKeyToWriteWhitelist,
	}
}

func (cms Store) CacheMultiStore() storetypes.CacheMultiStore {
	return cachemulti.NewStore(cms.MultiStore.CacheMultiStore(), cms.storeKeyToWriteWhitelist)
}

func (cms Store) GetKVStore(key storetypes.StoreKey) storetypes.KVStore {
	rawKVStore := cms.MultiStore.GetKVStore(key)
	if writeWhitelist, ok := cms.storeKeyToWriteWhitelist[key]; ok {
		return kv.NewStore(rawKVStore, writeWhitelist)
	}
	return rawKVStore
}
