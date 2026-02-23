package evm

import iavl "github.com/sei-protocol/sei-chain/sei-iavl"

// EVMDBEngine abstracts a single versioned KV store for one EVM data type.
// Implementations exist for PebbleDB (default) and RocksDB (build tag: rocksdbBackend).
type EVMDBEngine interface {
	Get(key []byte, version int64) ([]byte, error)
	Has(key []byte, version int64) (bool, error)
	Set(key, value []byte, version int64) error
	Delete(key []byte, version int64) error
	ApplyBatch(pairs []*iavl.KVPair, version int64) error

	GetLatestVersion() int64
	SetLatestVersion(version int64) error
	GetEarliestVersion() int64
	SetEarliestVersion(version int64) error

	Prune(version int64) error
	Close() error
}

// EVMDBOpener is a factory function that opens an EVMDBEngine for a given store type.
type EVMDBOpener func(dir string, storeType EVMStoreType) (EVMDBEngine, error)

var evmBackends = map[string]EVMDBOpener{}

// RegisterEVMBackend registers a named backend factory for EVM sub-databases.
func RegisterEVMBackend(name string, opener EVMDBOpener) {
	evmBackends[name] = opener
}

// GetEVMBackend returns the registered opener for the given backend name.
func GetEVMBackend(name string) (EVMDBOpener, bool) {
	opener, ok := evmBackends[name]
	return opener, ok
}
