package ss

import (
	"path/filepath"

	"github.com/sei-protocol/sei-db/config"
	"github.com/sei-protocol/sei-db/ss/pebbledb"
	"github.com/sei-protocol/sei-db/ss/types"
)

func init() {
	initializer := func(dir string, configs config.StateStoreConfig) (types.StateStore, error) {
		dbDirectory := dir
		if configs.DBDirectory != "" {
			dbDirectory = configs.DBDirectory
		}
		return pebbledb.New(filepath.Join(dbDirectory, "pebbledb"), configs)
	}
	RegisterBackend(PebbleDBBackend, initializer)
}
