package backend

import (
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb/mvcc"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
)

func openPebbleDB(dbHome string, cfg config.StateStoreConfig) (types.StateStore, error) {
	return mvcc.OpenDB(dbHome, cfg)
}
