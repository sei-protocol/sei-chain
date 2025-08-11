package store

import (
	"github.com/sei-protocol/sei-chain/cosmos-sdk/store/cachekv"
	"github.com/sei-protocol/sei-chain/cosmos-sdk/store/cachemulti"
	"github.com/sei-protocol/sei-chain/cosmos-sdk/store/dbadapter"
	storetypes "github.com/sei-protocol/sei-chain/cosmos-sdk/store/types"
	"github.com/sei-protocol/sei-chain/cosmos-sdk/types"
	dbm "github.com/tendermint/tm-db"
)

func NewTestKVStore() types.KVStore {
	mem := dbadapter.Store{DB: dbm.NewMemDB()}
	return cachekv.NewStore(mem, storetypes.NewKVStoreKey("test"), storetypes.DefaultCacheSizeLimit)
}

func NewTestCacheMultiStore(stores map[types.StoreKey]types.CacheWrapper) types.CacheMultiStore {
	return cachemulti.NewStore(dbm.NewMemDB(), stores, map[string]types.StoreKey{}, nil, nil, make(map[types.StoreKey][]storetypes.WriteListener))
}
