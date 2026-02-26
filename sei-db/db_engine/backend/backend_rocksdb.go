//go:build rocksdbBackend

package backend

import (
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/rocksdb/mvcc"
)

func openRocksDB(dbHome string, cfg config.StateStoreConfig) (db_engine.MvccDB, error) {
	return mvcc.OpenDB(dbHome, cfg)
}
