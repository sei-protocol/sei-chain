package store

import (
	"github.com/cosmos/cosmos-sdk/store/cachekv"
	"github.com/cosmos/cosmos-sdk/store/cachemulti"
	"github.com/cosmos/cosmos-sdk/store/dbadapter"
	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	"github.com/cosmos/cosmos-sdk/types"
	dbm "github.com/tendermint/tm-db"
)

func NewTestKVStore() types.KVStore {
	mem := dbadapter.Store{DB: dbm.NewMemDB()}
	return cachekv.NewStore(mem, storetypes.NewKVStoreKey("testStoreKey"))
}

func NewTestCacheMultiStore(stores map[types.StoreKey]types.CacheWrapper) types.CacheMultiStore {
	return cachemulti.NewStore(dbm.NewMemDB(), stores, map[string]types.StoreKey{}, nil, nil, make(map[types.StoreKey][]storetypes.WriteListener))
}
