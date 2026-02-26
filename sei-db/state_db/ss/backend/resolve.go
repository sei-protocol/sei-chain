package backend

import (
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine"
)

// OpenFunc creates a StateStore from a data directory and config.
type OpenFunc func(dbHome string, cfg config.StateStoreConfig) (db_engine.StateStore, error)

// ResolveBackend returns the OpenFunc for the given backend name.
// Defaults to PebbleDB. RocksDB is available only when built with -tags=rocksdbBackend.
func ResolveBackend(backendName string) OpenFunc {
	switch backendName {
	case config.RocksDBBackend:
		return openRocksDB
	default:
		return openPebbleDB
	}
}
