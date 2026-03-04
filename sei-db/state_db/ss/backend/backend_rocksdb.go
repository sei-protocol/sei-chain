//go:build rocksdbBackend

package backend

import (
	"os"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/rocksdb/mvcc"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
)

func openRocksDB(dbHome string, cfg config.StateStoreConfig) (types.StateStore, error) {
	// pebble.Open internally calls MkdirAll to create the full directory tree,
	// but RocksDB's CreateIfMissing only creates the leaf directory. Normalize
	// the behaviour here so both backends work with arbitrary nested paths.
	if err := os.MkdirAll(dbHome, 0o750); err != nil {
		return nil, err
	}
	return mvcc.OpenDB(dbHome, cfg)
}
