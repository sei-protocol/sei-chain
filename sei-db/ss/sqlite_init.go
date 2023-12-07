//go:build sqliteBackend
// +build sqliteBackend

package ss

import (
	"path/filepath"

	"github.com/sei-protocol/sei-db/config"
	"github.com/sei-protocol/sei-db/ss/sqlite"
	"github.com/sei-protocol/sei-db/ss/types"
)

func init() {
	initializer := func(dir string, configs config.StateStoreConfig) (types.StateStore, error) {
		dbDirectory := dir
		if configs.DBDirectory != "" {
			dbDirectory = configs.DBDirectory
		}
		return sqlite.New(filepath.Join(dbDirectory, "sqlite"), configs)
	}
	RegisterBackend(SQLiteBackend, initializer)
}
