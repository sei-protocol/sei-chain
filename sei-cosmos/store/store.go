package store

import (
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/log"
	dbm "github.com/tendermint/tm-db"

	"github.com/sei-protocol/sei-chain/sei-cosmos/store/cache"
	"github.com/sei-protocol/sei-chain/sei-cosmos/store/rootmulti"
	"github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
)

func NewCommitMultiStore(db dbm.DB) types.CommitMultiStore {
	return rootmulti.NewStore(db, log.NewNopLogger())
}

func NewCommitMultiStoreWithArchival(db dbm.DB, archivalDb dbm.DB, archivalVersion int64) types.CommitMultiStore {
	return rootmulti.NewStoreWithArchival(db, archivalDb, archivalVersion, log.NewNopLogger())
}

func NewCommitKVStoreCacheManager() types.MultiStorePersistentCache {
	return cache.NewCommitKVStoreCacheManager(cache.DefaultCommitKVStoreCacheSize, types.DefaultCacheSizeLimit)
}
