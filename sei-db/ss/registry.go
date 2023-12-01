package ss

import (
	"fmt"

	"github.com/sei-protocol/sei-db/ss/types"
)

type BackendType string

const (
	// RocksDBBackend represents rocksdb
	// - use rocksdb build tag
	RocksDBBackend BackendType = "rocksdb"

	// PebbleDBBackend represents pebbledb
	PebbleDBBackend BackendType = "pebbledb"

	// SQLiteBackend represents sqlite
	SQLiteBackend BackendType = "sqlite"
)

type BackendInitializer func(dir string) (types.StateStore, error)

var backends = map[BackendType]BackendInitializer{}

func RegisterBackend(backendType BackendType, initializer BackendInitializer) {
	backends[backendType] = initializer
}

// NewStateStoreDB Create a new state store with the specified backend type
func NewStateStoreDB(dir string, backendType string) (types.StateStore, error) {
	initializer, ok := backends[BackendType(backendType)]
	if !ok {
		return nil, fmt.Errorf("unsupported backend: %s", backendType)
	}
	db, err := initializer(dir)
	if err != nil {
		return nil, err
	}
	return db, nil
}
