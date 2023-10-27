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

func NewStateStoreDB(dir string, backendType BackendType) (types.StateStore, error) {
	fmt.Printf("Current registered backends: %v\n", backends)
	initializer, ok := backends[backendType]
	if !ok {
		return nil, fmt.Errorf("unsupported backend: %s", backendType)
	}
	db, err := initializer(dir)
	if err != nil {
		return nil, err
	}
	return db, nil
}
