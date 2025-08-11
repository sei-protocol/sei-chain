//go:build sqliteBackend
// +build sqliteBackend

package ss

import (
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/ss/sqlite"
	"github.com/sei-protocol/sei-chain/sei-db/ss/types"
)

func init() {
	initializer := func(dir string, configs config.StateStoreConfig) (types.StateStore, error) {
		dbHome := utils.GetStateStorePath(dir, configs.Backend)
		if configs.DBDirectory != "" {
			dbHome = configs.DBDirectory
		}
		return sqlite.New(dbHome, configs)
	}
	RegisterBackend(SQLiteBackend, initializer)
}
