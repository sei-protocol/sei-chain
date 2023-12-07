//go:build rocksdbBackend
// +build rocksdbBackend

package ss

import (
	"path/filepath"

	"github.com/sei-protocol/sei-db/config"
	"github.com/sei-protocol/sei-db/ss/rocksdb"
	"github.com/sei-protocol/sei-db/ss/types"
)

func init() {
	initializer := func(dir string, configs config.StateStoreConfig) (types.StateStore, error) {
		dbDirectory := dir
		if configs.DBDirectory != "" {
			dbDirectory = configs.DBDirectory
		}
		return rocksdb.New(filepath.Join(dbDirectory, "rocksdb"), configs)
	}
	RegisterBackend(RocksDBBackend, initializer)
}
