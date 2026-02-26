package backend

import (
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb/mvcc"
)

func openPebbleDB(dbHome string, cfg config.StateStoreConfig) (db_engine.StateStore, error) {
	return mvcc.OpenDB(dbHome, cfg)
}
