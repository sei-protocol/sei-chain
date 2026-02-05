package evm

import (
	"fmt"
	"sync"

	commonevm "github.com/sei-protocol/sei-chain/sei-db/common/evm"
	iavl "github.com/sei-protocol/sei-chain/sei-iavl"
)

// EVMStateStore manages multiple EVMDatabase instances, one per EVM data type
type EVMStateStore struct {
	databases map[EVMStoreType]*EVMDatabase
	dir       string
}

// NewEVMStateStore creates a new EVM state store with all sub-databases
func NewEVMStateStore(dir string) (*EVMStateStore, error) {
	store := &EVMStateStore{
		databases: make(map[EVMStoreType]*EVMDatabase),
		dir:       dir,
	}

	// Open a database for each EVM store type
	for _, storeType := range AllEVMStoreTypes() {
		db, err := OpenDB(dir, storeType)
		if err != nil {
			// Close any already opened DBs
			_ = store.Close()
			return nil, fmt.Errorf("failed to open EVM DB for %s: %w", StoreTypeName(storeType), err)
		}
		store.databases[storeType] = db
	}

	return store, nil
}

// GetDB returns the database for a specific store type
func (s *EVMStateStore) GetDB(storeType EVMStoreType) *EVMDatabase {
	return s.databases[storeType]
}

// Get retrieves a value using the full EVM key (with prefix)
func (s *EVMStateStore) Get(key []byte, version int64) ([]byte, error) {
	storeType, strippedKey := commonevm.ParseEVMKey(key)
	if storeType == StoreUnknown {
		return nil, nil
	}

	db := s.databases[storeType]
	if db == nil {
		return nil, nil
	}

	return db.Get(strippedKey, version)
}

// Has checks if a key exists
func (s *EVMStateStore) Has(key []byte, version int64) (bool, error) {
	storeType, strippedKey := commonevm.ParseEVMKey(key)
	if storeType == StoreUnknown {
		return false, nil
	}

	db := s.databases[storeType]
	if db == nil {
		return false, nil
	}

	return db.Has(strippedKey, version)
}

// ApplyChangeset applies changes from multiple store types
func (s *EVMStateStore) ApplyChangeset(version int64, changes map[EVMStoreType][]*iavl.KVPair) error {
	for storeType, pairs := range changes {
		db := s.databases[storeType]
		if db == nil {
			continue
		}
		if err := db.ApplyBatch(pairs, version); err != nil {
			return fmt.Errorf("failed to apply batch for %s: %w", StoreTypeName(storeType), err)
		}
	}
	return nil
}

// ApplyChangesetParallel applies changes to multiple store types in parallel
func (s *EVMStateStore) ApplyChangesetParallel(version int64, changes map[EVMStoreType][]*iavl.KVPair) error {
	if len(changes) == 0 {
		return nil
	}

	// If only one store type, no need for parallelism
	if len(changes) == 1 {
		return s.ApplyChangeset(version, changes)
	}

	var wg sync.WaitGroup
	errCh := make(chan error, len(changes))

	for storeType, pairs := range changes {
		db := s.databases[storeType]
		if db == nil {
			continue
		}

		wg.Add(1)
		go func(db *EVMDatabase, pairs []*iavl.KVPair) {
			defer wg.Done()
			if err := db.ApplyBatch(pairs, version); err != nil {
				errCh <- err
			}
		}(db, pairs)
	}

	wg.Wait()
	close(errCh)

	// Return first error if any
	for err := range errCh {
		return err
	}
	return nil
}

// GetLatestVersion returns the maximum latest version across all databases
func (s *EVMStateStore) GetLatestVersion() int64 {
	var maxVersion int64
	for _, db := range s.databases {
		if v := db.GetLatestVersion(); v > maxVersion {
			maxVersion = v
		}
	}
	return maxVersion
}

// SetLatestVersion sets the latest version on all databases
func (s *EVMStateStore) SetLatestVersion(version int64) error {
	for _, db := range s.databases {
		if err := db.SetLatestVersion(version); err != nil {
			return err
		}
	}
	return nil
}

// GetEarliestVersion returns the minimum earliest version across all databases
func (s *EVMStateStore) GetEarliestVersion() int64 {
	var minVersion int64 = -1
	for _, db := range s.databases {
		v := db.GetEarliestVersion()
		if minVersion < 0 || v < minVersion {
			minVersion = v
		}
	}
	if minVersion < 0 {
		return 0
	}
	return minVersion
}

// SetEarliestVersion sets the earliest version on all databases
func (s *EVMStateStore) SetEarliestVersion(version int64) error {
	for _, db := range s.databases {
		if err := db.SetEarliestVersion(version); err != nil {
			return err
		}
	}
	return nil
}

// Prune removes old versions from all databases in parallel
func (s *EVMStateStore) Prune(version int64) error {
	if len(s.databases) == 0 {
		return nil
	}

	var wg sync.WaitGroup
	errCh := make(chan error, len(s.databases))

	for _, db := range s.databases {
		wg.Add(1)
		go func(db *EVMDatabase) {
			defer wg.Done()
			if err := db.Prune(version); err != nil {
				errCh <- err
			}
		}(db)
	}

	wg.Wait()
	close(errCh)

	// Return first error if any
	for err := range errCh {
		return err
	}
	return nil
}

// Close closes all databases
func (s *EVMStateStore) Close() error {
	var lastErr error
	for _, db := range s.databases {
		if err := db.Close(); err != nil {
			lastErr = err
		}
	}
	return lastErr
}
