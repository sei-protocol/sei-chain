package ss

import "fmt"

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

type BackendInitializer func(dir string) (StateStore, error)

var backends = map[BackendType]BackendInitializer{}

func RegisterBackend(backendType BackendType, initializer BackendInitializer) {
	backends[backendType] = initializer
}

func NewStateStoreDB(dir string, backendType BackendType) (StateStore, error) {
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
