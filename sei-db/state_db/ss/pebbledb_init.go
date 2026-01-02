package ss

import (
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb/mvcc"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/types"
)

func init() {
	initializer := func(dir string, configs config.StateStoreConfig) (types.StateStore, error) {
		dbHome := utils.GetStateStorePath(dir, configs.Backend)
		if configs.DBDirectory != "" {
			dbHome = configs.DBDirectory
		}
		return mvcc.New(dbHome, configs)
	}
	RegisterBackend(PebbleDBBackend, initializer)
}
