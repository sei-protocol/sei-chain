package store

import (
	dbm "github.com/tendermint/tm-db"

	"github.com/sei-protocol/sei-chain/cosmos/store/cache"
	"github.com/sei-protocol/sei-chain/cosmos/store/rootmulti"
	"github.com/sei-protocol/sei-chain/cosmos/store/types"
)

func NewCommitMultiStore(db dbm.DB) types.CommitMultiStore {
	return rootmulti.NewStore(db)
}

func NewCommitMultiStoreWithArchival(db dbm.DB, archivalDb dbm.DB, archivalVersion int64) types.CommitMultiStore {
	return rootmulti.NewStoreWithArchival(db, archivalDb, archivalVersion)
}

func NewCommitKVStoreCacheManager() types.MultiStorePersistentCache {
	return cache.NewCommitKVStoreCacheManager(cache.DefaultCommitKVStoreCacheSize, types.DefaultCacheSizeLimit)
}
