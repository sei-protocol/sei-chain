package store

import (
	"github.com/cosmos/cosmos-sdk/store/concurrentcachekv"
	"github.com/cosmos/cosmos-sdk/store/concurrentcachemulti"
	"github.com/cosmos/cosmos-sdk/store/dbadapter"
	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	"github.com/cosmos/cosmos-sdk/types"
	dbm "github.com/tendermint/tm-db"
)

func NewTestKVStore() types.KVStore {
	mem := dbadapter.Store{DB: dbm.NewMemDB()}
	return concurrentcachekv.NewStore(mem, storetypes.NewKVStoreKey("test"))
}

func NewTestCacheMultiStore(stores map[types.StoreKey]types.CacheWrapper) types.CacheMultiStore {
	return concurrentcachemulti.NewStore(
		dbm.NewMemDB(),
		stores,
		map[string]types.StoreKey{},
		nil,
		nil,
		make(map[types.StoreKey][]storetypes.WriteListener),
	)
}
