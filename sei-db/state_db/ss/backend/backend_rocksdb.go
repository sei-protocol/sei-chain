//go:build rocksdbBackend

package backend

import (
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/rocksdb/mvcc"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/types"
)

func openRocksDB(dbHome string, cfg config.StateStoreConfig) (types.MvccDB, error) {
	return mvcc.OpenDB(dbHome, cfg)
}
