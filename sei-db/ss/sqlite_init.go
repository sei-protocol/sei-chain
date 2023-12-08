//go:build sqliteBackend
// +build sqliteBackend

package ss

import (
	"github.com/sei-protocol/sei-db/common/utils"
	"github.com/sei-protocol/sei-db/config"
	"github.com/sei-protocol/sei-db/ss/sqlite"
	"github.com/sei-protocol/sei-db/ss/types"
)

func init() {
	initializer := func(dir string, configs config.StateStoreConfig) (types.StateStore, error) {
		dbHome := dir
		if configs.DBDirectory != "" {
			dbHome = configs.DBDirectory
		}
		return sqlite.New(utils.GetStateStorePath(dbHome, configs.Backend), configs)
	}
	RegisterBackend(SQLiteBackend, initializer)
}
