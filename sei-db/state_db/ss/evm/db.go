package evm

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"path/filepath"
	"runtime"
	"sync/atomic"

	"github.com/cockroachdb/pebble"
	iavl "github.com/sei-protocol/sei-chain/sei-iavl"
)

const (
	// Version metadata keys
	latestVersionKey   = "latest_version"
	earliestVersionKey = "earliest_version"
)

var defaultWriteOpts = &pebble.WriteOptions{Sync: false}

// EVMDatabase represents a single PebbleDB instance for a specific EVM data type.
// Uses pebble.DefaultComparer (lexicographic byte ordering) instead of MVCCComparer.
type EVMDatabase struct {
	storeType EVMStoreType
	storage   *pebble.DB

	// Version tracking (atomic for concurrent access)
	latestVersion   atomic.Int64
	earliestVersion atomic.Int64
}

// OpenDB opens a PebbleDB with default comparer for EVM data
func OpenDB(dir string, storeType EVMStoreType) (*EVMDatabase, error) {
	opts := &pebble.Options{
		Comparer:                 pebble.DefaultComparer,
		MaxConcurrentCompactions: func() int { return runtime.NumCPU() },
	}

	dbPath := filepath.Join(dir, StoreTypeName(storeType))
	db, err := pebble.Open(dbPath, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to open EVM DB %s: %w", StoreTypeName(storeType), err)
	}

	evmDB := &EVMDatabase{
		storeType: storeType,
		storage:   db,
	}
	// latestVersion and earliestVersion are zero-initialized by default

	// Load version metadata
	if err := evmDB.loadVersionMetadata(); err != nil {
		_ = db.Close()
		return nil, err
	}

	return evmDB, nil
}

func (db *EVMDatabase) loadVersionMetadata() error {
	// Load latest version
	val, closer, err := db.storage.Get([]byte(latestVersionKey))
	if err == nil {
		db.latestVersion.Store(int64(binary.BigEndian.Uint64(val))) //nolint:gosec // version values are always valid int64
		_ = closer.Close()
	} else if err != pebble.ErrNotFound {
		return err
	}

	// Load earliest version
	val, closer, err = db.storage.Get([]byte(earliestVersionKey))
	if err == nil {
		db.earliestVersion.Store(int64(binary.BigEndian.Uint64(val))) //nolint:gosec // version values are always valid int64
		_ = closer.Close()
	} else if err != pebble.ErrNotFound {
		return err
	}

	return nil
}

// encodeKey creates a versioned key: key || version (big-endian, 8 bytes)
func encodeKey(key []byte, version int64) []byte {
	encoded := make([]byte, len(key)+8)
	copy(encoded, key)
	binary.BigEndian.PutUint64(encoded[len(key):], uint64(version)) //nolint:gosec // version is always non-negative
	return encoded
}

// decodeKey extracts key and version from encoded key
func decodeKey(encoded []byte) ([]byte, int64) {
	if len(encoded) < 8 {
		return encoded, 0
	}
	key := encoded[:len(encoded)-8]
	version := int64(binary.BigEndian.Uint64(encoded[len(encoded)-8:])) //nolint:gosec // version values are always valid int64
	return key, version
}

// Get retrieves a value for a key at a specific version
// Returns the latest version <= requested version
func (db *EVMDatabase) Get(key []byte, version int64) ([]byte, error) {
	// Search for the latest version <= requested version
	// We iterate from the requested version down
	iter, err := db.storage.NewIter(&pebble.IterOptions{
		LowerBound: encodeKey(key, 0),
		UpperBound: encodeKey(key, version+1),
	})
	if err != nil {
		return nil, err
	}
	defer func() { _ = iter.Close() }()

	// Seek to the last key in range (latest version <= requested)
	if !iter.Last() {
		return nil, nil // Key not found
	}

	foundKey, foundVersion := decodeKey(iter.Key())
	if !bytes.Equal(foundKey, key) || foundVersion > version {
		return nil, nil
	}

	value := iter.Value()
	if len(value) == 0 {
		return nil, nil // Tombstone
	}

	result := make([]byte, len(value))
	copy(result, value)
	return result, nil
}

// Has checks if a key exists at a specific version
func (db *EVMDatabase) Has(key []byte, version int64) (bool, error) {
	val, err := db.Get(key, version)
	if err != nil {
		return false, err
	}
	return val != nil, nil
}

// Set writes a key-value pair at a specific version
func (db *EVMDatabase) Set(key, value []byte, version int64) error {
	encodedKey := encodeKey(key, version)
	if err := db.storage.Set(encodedKey, value, defaultWriteOpts); err != nil {
		return err
	}
	return db.updateLatestVersion(version)
}

// Delete marks a key as deleted at a specific version (tombstone)
func (db *EVMDatabase) Delete(key []byte, version int64) error {
	encodedKey := encodeKey(key, version)
	// Write nil value as tombstone
	if err := db.storage.Set(encodedKey, nil, defaultWriteOpts); err != nil {
		return err
	}
	return db.updateLatestVersion(version)
}

// ApplyBatch applies a batch of KV pairs at a specific version
func (db *EVMDatabase) ApplyBatch(pairs []*iavl.KVPair, version int64) error {
	if len(pairs) == 0 {
		return nil
	}

	batch := db.storage.NewBatch()
	defer func() { _ = batch.Close() }()

	for _, pair := range pairs {
		encodedKey := encodeKey(pair.Key, version)
		if pair.Value == nil || pair.Delete {
			// Tombstone
			if err := batch.Set(encodedKey, nil, nil); err != nil {
				return err
			}
		} else {
			if err := batch.Set(encodedKey, pair.Value, nil); err != nil {
				return err
			}
		}
	}

	if err := batch.Commit(defaultWriteOpts); err != nil {
		return err
	}

	return db.SetLatestVersion(version)
}

func (db *EVMDatabase) updateLatestVersion(version int64) error {
	if version > db.latestVersion.Load() {
		return db.SetLatestVersion(version)
	}
	return nil
}

// GetLatestVersion returns the latest version in this database
func (db *EVMDatabase) GetLatestVersion() int64 {
	return db.latestVersion.Load()
}

// SetLatestVersion updates the latest version (async write for performance)
func (db *EVMDatabase) SetLatestVersion(version int64) error {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, uint64(version)) //nolint:gosec // version is always non-negative
	if err := db.storage.Set([]byte(latestVersionKey), buf, defaultWriteOpts); err != nil {
		return err
	}
	db.latestVersion.Store(version)
	return nil
}

// GetEarliestVersion returns the earliest version in this database
func (db *EVMDatabase) GetEarliestVersion() int64 {
	return db.earliestVersion.Load()
}

// SetEarliestVersion updates the earliest version (async write for performance)
func (db *EVMDatabase) SetEarliestVersion(version int64) error {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, uint64(version)) //nolint:gosec // version is always non-negative
	if err := db.storage.Set([]byte(earliestVersionKey), buf, defaultWriteOpts); err != nil {
		return err
	}
	db.earliestVersion.Store(version)
	return nil
}

// Prune removes versions older than the given version
func (db *EVMDatabase) Prune(version int64) error {
	// Iterate through all keys and delete versions < given version
	iter, err := db.storage.NewIter(nil)
	if err != nil {
		return err
	}
	defer func() { _ = iter.Close() }()

	batch := db.storage.NewBatch()
	defer func() { _ = batch.Close() }()

	batchSize := 0
	const maxBatchSize = 10000

	for iter.First(); iter.Valid(); iter.Next() {
		key := iter.Key()
		// Skip metadata keys
		if bytes.Equal(key, []byte(latestVersionKey)) || bytes.Equal(key, []byte(earliestVersionKey)) {
			continue
		}

		_, keyVersion := decodeKey(key)
		if keyVersion < version {
			if err := batch.Delete(key, nil); err != nil {
				return err
			}
			batchSize++

			if batchSize >= maxBatchSize {
				if err := batch.Commit(defaultWriteOpts); err != nil {
					return err
				}
				batch.Reset()
				batchSize = 0
			}
		}
	}

	if batchSize > 0 {
		if err := batch.Commit(defaultWriteOpts); err != nil {
			return err
		}
	}

	return db.SetEarliestVersion(version)
}

// Close closes the database
func (db *EVMDatabase) Close() error {
	if db.storage == nil {
		return nil
	}
	err := db.storage.Close()
	db.storage = nil
	return err
}

// Iterator returns an iterator over keys in the given range at a specific version
func (db *EVMDatabase) Iterator(start, end []byte, version int64) (*EVMIterator, error) {
	return newEVMIterator(db.storage, start, end, version, false)
}

// ReverseIterator returns a reverse iterator over keys in the given range
func (db *EVMDatabase) ReverseIterator(start, end []byte, version int64) (*EVMIterator, error) {
	return newEVMIterator(db.storage, start, end, version, true)
}

// EVMIterator iterates over versioned keys, returning the latest version <= target for each unique key
type EVMIterator struct {
	// Pre-computed results
	keys   [][]byte
	values [][]byte
	index  int
	valid  bool
}

func newEVMIterator(db *pebble.DB, start, end []byte, version int64, reverse bool) (*EVMIterator, error) {
	// Collect all unique keys with their latest valid value
	keyValues := make(map[string][]byte)  // key -> latest value at or before version
	keyVersions := make(map[string]int64) // key -> version of the value we have

	iter, err := db.NewIter(nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = iter.Close() }()

	for iter.First(); iter.Valid(); iter.Next() {
		iterKey := iter.Key()

		// Skip metadata keys (check raw key before decoding, since metadata keys don't have version suffix)
		if bytes.Equal(iterKey, []byte(latestVersionKey)) || bytes.Equal(iterKey, []byte(earliestVersionKey)) {
			continue
		}

		rawKey, keyVersion := decodeKey(iterKey)
		keyStr := string(rawKey)

		// Skip if version is too new
		if keyVersion > version {
			continue
		}

		// Check bounds
		if start != nil && bytes.Compare(rawKey, start) < 0 {
			continue
		}
		if end != nil && bytes.Compare(rawKey, end) >= 0 {
			continue
		}

		// Check if this is a newer version than what we have
		existingVersion, exists := keyVersions[keyStr]
		if !exists || keyVersion > existingVersion {
			value := iter.Value()
			if len(value) == 0 {
				// Tombstone - store nil to indicate deletion
				keyValues[keyStr] = nil
			} else {
				// Copy value
				valueCopy := make([]byte, len(value))
				copy(valueCopy, value)
				keyValues[keyStr] = valueCopy
			}
			keyVersions[keyStr] = keyVersion
		}
	}

	// Collect non-tombstone keys
	var keys [][]byte
	var values [][]byte
	for keyStr, value := range keyValues {
		if value != nil { // Skip tombstones
			keys = append(keys, []byte(keyStr))
			values = append(values, value)
		}
	}

	// Sort keys
	sortKeyValues(keys, values, reverse)

	it := &EVMIterator{
		keys:   keys,
		values: values,
		index:  0,
		valid:  len(keys) > 0,
	}

	return it, nil
}

// sortKeyValues sorts keys and values together
func sortKeyValues(keys [][]byte, values [][]byte, reverse bool) {
	// Simple bubble sort (fine for small datasets in tests)
	n := len(keys)
	for i := 0; i < n-1; i++ {
		for j := 0; j < n-i-1; j++ {
			shouldSwap := false
			if reverse {
				shouldSwap = bytes.Compare(keys[j], keys[j+1]) < 0
			} else {
				shouldSwap = bytes.Compare(keys[j], keys[j+1]) > 0
			}
			if shouldSwap {
				keys[j], keys[j+1] = keys[j+1], keys[j]
				values[j], values[j+1] = values[j+1], values[j]
			}
		}
	}
}

func (it *EVMIterator) Valid() bool {
	return it.valid && it.index < len(it.keys)
}

func (it *EVMIterator) Key() []byte {
	if !it.Valid() {
		return nil
	}
	return it.keys[it.index]
}

func (it *EVMIterator) Value() []byte {
	if !it.Valid() {
		return nil
	}
	return it.values[it.index]
}

func (it *EVMIterator) Next() {
	if !it.Valid() {
		return
	}
	it.index++
}

func (it *EVMIterator) Close() error {
	return nil
}
