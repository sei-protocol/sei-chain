package evm

import (
	"fmt"
	"sync"

	commonevm "github.com/sei-protocol/sei-chain/sei-db/common/evm"
	"github.com/sei-protocol/sei-chain/sei-db/common/logger"
	iavl "github.com/sei-protocol/sei-chain/sei-iavl"
)

// asyncBufferSize is the per-DB channel buffer for async writes.
const asyncBufferSize = 100

// dbWrite is a unit of work enqueued to a per-DB background worker.
type dbWrite struct {
	version int64
	pairs   []*iavl.KVPair
}

// EVMStateStore manages multiple EVMDatabase instances, one per EVM data type.
// Each database has an optional background goroutine for async writes.
type EVMStateStore struct {
	databases map[EVMStoreType]*EVMDatabase
	dir       string
	logger    logger.Logger

	// Per-DB async write channels and worker goroutines
	asyncChs map[EVMStoreType]chan dbWrite
	asyncWg  sync.WaitGroup
}

// NewEVMStateStore creates a new EVM state store with all sub-databases.
// Each database gets a background worker goroutine for async writes.
func NewEVMStateStore(dir string, log logger.Logger) (*EVMStateStore, error) {
	store := &EVMStateStore{
		databases: make(map[EVMStoreType]*EVMDatabase),
		asyncChs:  make(map[EVMStoreType]chan dbWrite),
		dir:       dir,
		logger:    log,
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

		// Start per-DB background worker
		ch := make(chan dbWrite, asyncBufferSize)
		store.asyncChs[storeType] = ch
		store.asyncWg.Add(1)
		go store.asyncWorker(db, ch)
	}

	return store, nil
}

// asyncWorker processes writes from a per-DB channel until it's closed.
func (s *EVMStateStore) asyncWorker(db *EVMDatabase, ch <-chan dbWrite) {
	defer s.asyncWg.Done()
	for w := range ch {
		if err := db.ApplyBatch(w.pairs, w.version); err != nil {
			s.logger.Error("async EVM write failed", "storeType", StoreTypeName(db.storeType), "version", w.version, "error", err)
			continue
		}
		_ = db.SetLatestVersion(w.version)
	}
}

// GetDB returns the database for a specific store type
func (s *EVMStateStore) GetDB(storeType EVMStoreType) *EVMDatabase {
	return s.databases[storeType]
}

// Get retrieves a value using the full EVM key (with prefix)
func (s *EVMStateStore) Get(key []byte, version int64) ([]byte, error) {
	storeType, strippedKey := commonevm.ParseEVMKey(key)
	if storeType == StoreEmpty {
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
	if storeType == StoreEmpty {
		return false, nil
	}

	db := s.databases[storeType]
	if db == nil {
		return false, nil
	}

	return db.Has(strippedKey, version)
}

// ApplyChangeset applies changes from multiple store types synchronously (blocking).
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

// ApplyChangesetParallel applies changes to multiple store types in parallel (blocking).
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

// ApplyChangesetAsync enqueues changes to per-DB background workers and returns immediately.
// Each sub-database has its own buffered channel, so writes are truly non-blocking
// unless the channel is full (backpressure).
func (s *EVMStateStore) ApplyChangesetAsync(version int64, changes map[EVMStoreType][]*iavl.KVPair) error {
	for storeType, pairs := range changes {
		ch, ok := s.asyncChs[storeType]
		if !ok || len(pairs) == 0 {
			continue
		}
		ch <- dbWrite{version: version, pairs: pairs}
	}
	return nil
}

// GetLatestVersion returns the minimum latest version across all databases.
// Using min ensures crash-recovery correctness: if a crash interrupts
// SetLatestVersion mid-loop, some DBs may be one version ahead of others.
// min() guarantees WAL replay starts from the furthest-behind DB, and
// replaying to an already-current DB is harmless (writes are idempotent).
// Under normal operation all DBs are at the same version, so min == max.
func (s *EVMStateStore) GetLatestVersion() int64 {
	var minVersion int64 = -1
	for _, db := range s.databases {
		if v := db.GetLatestVersion(); minVersion < 0 || v < minVersion {
			minVersion = v
		}
	}
	if minVersion < 0 {
		return 0
	}
	return minVersion
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

// Close closes all async channels, waits for workers to drain, then closes databases.
// Safe to call multiple times.
func (s *EVMStateStore) Close() error {
	// Close all async channels to signal workers to stop (safe: only close once)
	for st, ch := range s.asyncChs {
		close(ch)
		delete(s.asyncChs, st)
	}
	// Wait for all workers to finish processing queued writes
	s.asyncWg.Wait()

	var lastErr error
	for _, db := range s.databases {
		if err := db.Close(); err != nil {
			lastErr = err
		}
	}
	return lastErr
}
