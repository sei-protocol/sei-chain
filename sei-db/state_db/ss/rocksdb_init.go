//go:build rocksdbBackend

package ss

import (
	"github.com/sei-protocol/sei-db/common/utils"
	"github.com/sei-protocol/sei-db/config"
	"github.com/sei-protocol/sei-db/db_engine/rocksdb/mvcc"
	"github.com/sei-protocol/sei-db/state_db/ss/types"
)

func init() {
	initializer := func(dir string, configs config.StateStoreConfig) (types.StateStore, error) {
		dbHome := utils.GetStateStorePath(dir, configs.Backend)
		if configs.DBDirectory != "" {
			dbHome = configs.DBDirectory
		}
		return mvcc.OpenDB(dbHome, configs)
	}
	RegisterBackend(RocksDBBackend, initializer)
}
