package evm

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/cockroachdb/pebble/v2"
	"github.com/cockroachdb/pebble/v2/bloom"
	"github.com/cockroachdb/pebble/v2/sstable"
	"golang.org/x/exp/slices"

	iavl "github.com/sei-protocol/sei-chain/sei-iavl"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/types"
)

const (
	VersionSize        = 8
	latestVersionKey   = "_latest"
	earliestVersionKey = "_earliest"
)

var defaultWriteOpts = pebble.NoSync

// EVMDatabase is a single PebbleDB for one EVM store type using default comparer
type EVMDatabase struct {
	storage         *pebble.DB
	storeType       EVMStoreType
	latestVersion   atomic.Int64
	earliestVersion atomic.Int64
	mu              sync.RWMutex
}

// OpenEVMDB opens a PebbleDB with default comparer for EVM data
func OpenEVMDB(dataDir string, storeType EVMStoreType) (*EVMDatabase, error) {
	cache := pebble.NewCache(1024 * 1024 * 16) // 16MB cache per EVM DB
	defer cache.Unref()

	opts := &pebble.Options{
		Cache:                       cache,
		Comparer:                    pebble.DefaultComparer, // Use default comparer for EVM stores
		FormatMajorVersion:          pebble.FormatVirtualSSTables,
		L0CompactionThreshold:       2,
		L0StopWritesThreshold:       1000,
		LBaseMaxBytes:               64 << 20,
		MemTableSize:                64 << 20,
		MemTableStopWritesThreshold: 4,
	}

	// Configure levels
	for i := 0; i < len(opts.Levels); i++ {
		l := &opts.Levels[i]
		l.BlockSize = 32 << 10
		l.IndexBlockSize = 256 << 10
		l.FilterPolicy = bloom.FilterPolicy(10)
		l.FilterType = pebble.TableFilter
		l.Compression = func() *sstable.CompressionProfile { return sstable.ZstdCompression }
		if i == 0 {
			l.EnsureL0Defaults()
		} else {
			l.EnsureL1PlusDefaults(&opts.Levels[i-1])
		}
	}
	opts.Levels[6].FilterPolicy = nil

	db, err := pebble.Open(dataDir, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to open EVM PebbleDB for %s: %w", storeType, err)
	}

	earliestVersion, err := retrieveVersion(db, earliestVersionKey)
	if err != nil {
		_ = db.Close()
		return nil, err
	}

	latestVersion, err := retrieveVersion(db, latestVersionKey)
	if err != nil {
		_ = db.Close()
		return nil, err
	}

	database := &EVMDatabase{
		storage:   db,
		storeType: storeType,
	}
	database.latestVersion.Store(latestVersion)
	database.earliestVersion.Store(earliestVersion)

	return database, nil
}

func (db *EVMDatabase) Close() error {
	if db.storage == nil {
		return nil
	}
	err := db.storage.Close()
	db.storage = nil
	return err
}

// encodeKey creates key with version suffix: key + version (big-endian)
func encodeKey(key []byte, version int64) []byte {
	result := make([]byte, len(key)+VersionSize)
	copy(result, key)
	binary.BigEndian.PutUint64(result[len(key):], uint64(version))
	return result
}

// decodeKey extracts key and version from encoded key
func decodeKey(encoded []byte) (key []byte, version int64, ok bool) {
	if len(encoded) < VersionSize {
		return nil, 0, false
	}
	keyLen := len(encoded) - VersionSize
	key = encoded[:keyLen]
	version = int64(binary.BigEndian.Uint64(encoded[keyLen:]))
	return key, version, true
}

func (db *EVMDatabase) Get(key []byte, targetVersion int64) ([]byte, error) {
	if targetVersion < db.earliestVersion.Load() {
		return nil, nil
	}

	// Scan for the latest version <= targetVersion
	upperBound := encodeKey(key, targetVersion+1)
	lowerBound := encodeKey(key, 0)

	itr, err := db.storage.NewIter(&pebble.IterOptions{
		LowerBound: lowerBound,
		UpperBound: upperBound,
	})
	if err != nil {
		return nil, err
	}
	defer itr.Close()

	if !itr.Last() {
		return nil, nil
	}

	// Verify it's the same key
	foundKey, _, ok := decodeKey(itr.Key())
	if !ok || !slices.Equal(foundKey, key) {
		return nil, nil
	}

	value := slices.Clone(itr.Value())
	// Check for tombstone (empty value means deleted)
	if len(value) == 0 {
		return nil, nil
	}
	return value, nil
}

func (db *EVMDatabase) Has(key []byte, version int64) (bool, error) {
	val, err := db.Get(key, version)
	return val != nil, err
}

func (db *EVMDatabase) Set(key, value []byte, version int64) error {
	encodedKey := encodeKey(key, version)
	return db.storage.Set(encodedKey, value, defaultWriteOpts)
}

func (db *EVMDatabase) Delete(key []byte, version int64) error {
	// Write tombstone (empty value)
	encodedKey := encodeKey(key, version)
	return db.storage.Set(encodedKey, nil, defaultWriteOpts)
}

func (db *EVMDatabase) SetLatestVersion(version int64) error {
	db.latestVersion.Store(version)
	var ts [VersionSize]byte
	binary.LittleEndian.PutUint64(ts[:], uint64(version))
	return db.storage.Set([]byte(latestVersionKey), ts[:], defaultWriteOpts)
}

func (db *EVMDatabase) GetLatestVersion() int64 {
	return db.latestVersion.Load()
}

func (db *EVMDatabase) SetEarliestVersion(version int64) error {
	db.earliestVersion.Store(version)
	var ts [VersionSize]byte
	binary.LittleEndian.PutUint64(ts[:], uint64(version))
	return db.storage.Set([]byte(earliestVersionKey), ts[:], defaultWriteOpts)
}

func (db *EVMDatabase) GetEarliestVersion() int64 {
	return db.earliestVersion.Load()
}

func retrieveVersion(db *pebble.DB, key string) (int64, error) {
	bz, closer, err := db.Get([]byte(key))
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return 0, nil
		}
		return 0, err
	}
	defer closer.Close()

	if len(bz) == 0 {
		return 0, nil
	}
	uz := binary.LittleEndian.Uint64(bz)
	if uz > math.MaxInt64 {
		return 0, fmt.Errorf("version overflows int64: %d", uz)
	}
	return int64(uz), nil
}

// Iterator returns an iterator over the EVM database
func (db *EVMDatabase) Iterator(start, end []byte, version int64) (types.DBIterator, error) {
	return newEVMIterator(db.storage, start, end, version, db.earliestVersion.Load(), false)
}

// ReverseIterator returns a reverse iterator over the EVM database
func (db *EVMDatabase) ReverseIterator(start, end []byte, version int64) (types.DBIterator, error) {
	return newEVMIterator(db.storage, start, end, version, db.earliestVersion.Load(), true)
}

// EVMStateStore manages multiple EVM databases (storage, balance, nonce, code)
type EVMStateStore struct {
	dbs     map[EVMStoreType]*EVMDatabase
	baseDir string
	mu      sync.RWMutex
}

// NewEVMStateStore creates a new EVM state store with separate DBs
func NewEVMStateStore(baseDir string) (*EVMStateStore, error) {
	store := &EVMStateStore{
		dbs:     make(map[EVMStoreType]*EVMDatabase),
		baseDir: baseDir,
	}

	for _, storeType := range AllEVMStoreTypes() {
		dbPath := filepath.Join(baseDir, string(storeType))
		db, err := OpenEVMDB(dbPath, storeType)
		if err != nil {
			store.Close()
			return nil, fmt.Errorf("failed to open %s db: %w", storeType, err)
		}
		store.dbs[storeType] = db
	}

	return store, nil
}

func (s *EVMStateStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var lastErr error
	for _, db := range s.dbs {
		if err := db.Close(); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

// GetDB returns the database for a specific store type
func (s *EVMStateStore) GetDB(storeType EVMStoreType) *EVMDatabase {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.dbs[storeType]
}

// Get retrieves a value from the appropriate EVM database
func (s *EVMStateStore) Get(storeType EVMStoreType, key []byte, version int64) ([]byte, error) {
	db := s.GetDB(storeType)
	if db == nil {
		return nil, fmt.Errorf("unknown EVM store type: %s", storeType)
	}
	return db.Get(key, version)
}

// Set stores a value in the appropriate EVM database
func (s *EVMStateStore) Set(storeType EVMStoreType, key, value []byte, version int64) error {
	db := s.GetDB(storeType)
	if db == nil {
		return fmt.Errorf("unknown EVM store type: %s", storeType)
	}
	return db.Set(key, value, version)
}

// Delete removes a key from the appropriate EVM database
func (s *EVMStateStore) Delete(storeType EVMStoreType, key []byte, version int64) error {
	db := s.GetDB(storeType)
	if db == nil {
		return fmt.Errorf("unknown EVM store type: %s", storeType)
	}
	return db.Delete(key, version)
}

// ApplyChangeset applies a changeset to all relevant EVM databases
func (s *EVMStateStore) ApplyChangeset(version int64, changes map[EVMStoreType][]*iavl.KVPair) error {
	for storeType, pairs := range changes {
		db := s.GetDB(storeType)
		if db == nil {
			continue
		}
		for _, pair := range pairs {
			var err error
			if pair.Value == nil || pair.Delete {
				err = db.Delete(pair.Key, version)
			} else {
				err = db.Set(pair.Key, pair.Value, version)
			}
			if err != nil {
				return err
			}
		}
		if err := db.SetLatestVersion(version); err != nil {
			return err
		}
	}
	return nil
}

// GetLatestVersion returns the latest version across all EVM databases
func (s *EVMStateStore) GetLatestVersion() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var maxVersion int64
	for _, db := range s.dbs {
		if v := db.GetLatestVersion(); v > maxVersion {
			maxVersion = v
		}
	}
	return maxVersion
}

// SetLatestVersion sets the latest version for all EVM databases
func (s *EVMStateStore) SetLatestVersion(version int64) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, db := range s.dbs {
		if err := db.SetLatestVersion(version); err != nil {
			return err
		}
	}
	return nil
}

// Prune removes old versions from all EVM databases
func (s *EVMStateStore) Prune(version int64) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for storeType, db := range s.dbs {
		if err := pruneEVMDB(db, version); err != nil {
			return fmt.Errorf("failed to prune %s: %w", storeType, err)
		}
	}
	return nil
}

func pruneEVMDB(db *EVMDatabase, version int64) error {
	itr, err := db.storage.NewIter(nil)
	if err != nil {
		return err
	}
	defer itr.Close()

	batch := db.storage.NewBatch()
	defer batch.Close()

	var (
		prevKey        []byte
		prevVersion    int64
		prevKeyEncoded []byte
		counter        int
	)

	for itr.First(); itr.Valid(); itr.Next() {
		// Skip metadata keys
		if isMetadataKey(itr.Key()) {
			continue
		}

		currKey, currVersion, ok := decodeKey(itr.Key())
		if !ok {
			continue
		}

		// Delete previous version if same key and version <= prune target
		if slices.Equal(prevKey, currKey) && prevVersion <= version {
			if err := batch.Delete(prevKeyEncoded, nil); err != nil {
				return err
			}
			counter++
			if counter >= 50 {
				if err := batch.Commit(defaultWriteOpts); err != nil {
					return err
				}
				batch.Reset()
				counter = 0
			}
		}

		prevKey = slices.Clone(currKey)
		prevVersion = currVersion
		prevKeyEncoded = slices.Clone(itr.Key())
	}

	if counter > 0 {
		if err := batch.Commit(defaultWriteOpts); err != nil {
			return err
		}
	}

	return db.SetEarliestVersion(version + 1)
}

func isMetadataKey(key []byte) bool {
	return len(key) > 0 && key[0] == '_'
}

// EVMIterator implements types.DBIterator for EVM database
type EVMIterator struct {
	itr             *pebble.Iterator
	start, end      []byte
	version         int64
	earliestVersion int64
	reverse         bool
	valid           bool
	currentKey      []byte
	currentValue    []byte
	err             error
}

func newEVMIterator(db *pebble.DB, start, end []byte, version, earliestVersion int64, reverse bool) (*EVMIterator, error) {
	var lowerBound, upperBound []byte
	if start != nil {
		lowerBound = encodeKey(start, 0)
	}
	if end != nil {
		upperBound = encodeKey(end, 0)
	}

	itr, err := db.NewIter(&pebble.IterOptions{
		LowerBound: lowerBound,
		UpperBound: upperBound,
	})
	if err != nil {
		return nil, err
	}

	it := &EVMIterator{
		itr:             itr,
		start:           start,
		end:             end,
		version:         version,
		earliestVersion: earliestVersion,
		reverse:         reverse,
	}

	if reverse {
		it.valid = itr.Last()
	} else {
		it.valid = itr.First()
	}

	if it.valid {
		it.advance()
	}

	return it, nil
}

func (it *EVMIterator) advance() {
	for it.itr.Valid() {
		key, keyVersion, ok := decodeKey(it.itr.Key())
		if !ok {
			it.moveNext()
			continue
		}

		// Skip versions that are too new or too old
		if keyVersion > it.version || keyVersion < it.earliestVersion {
			it.moveNext()
			continue
		}

		// Check for tombstone
		value := it.itr.Value()
		if len(value) == 0 {
			it.moveNext()
			continue
		}

		// Found valid entry
		it.currentKey = slices.Clone(key)
		it.currentValue = slices.Clone(value)
		it.valid = true

		// Skip to next key prefix to avoid duplicate versions
		it.skipToNextKey(key)
		return
	}

	it.valid = false
}

func (it *EVMIterator) moveNext() {
	if it.reverse {
		it.itr.Prev()
	} else {
		it.itr.Next()
	}
}

func (it *EVMIterator) skipToNextKey(currentKey []byte) {
	for {
		it.moveNext()
		if !it.itr.Valid() {
			return
		}
		key, _, ok := decodeKey(it.itr.Key())
		if !ok || !slices.Equal(key, currentKey) {
			return
		}
	}
}

func (it *EVMIterator) Domain() ([]byte, []byte) {
	return it.start, it.end
}

func (it *EVMIterator) Valid() bool {
	return it.valid && it.err == nil
}

func (it *EVMIterator) Next() {
	if !it.valid {
		return
	}
	it.advance()
}

func (it *EVMIterator) Key() []byte {
	if !it.valid {
		return nil
	}
	return it.currentKey
}

func (it *EVMIterator) Value() []byte {
	if !it.valid {
		return nil
	}
	return it.currentValue
}

func (it *EVMIterator) Error() error {
	if it.err != nil {
		return it.err
	}
	return it.itr.Error()
}

func (it *EVMIterator) Close() error {
	return it.itr.Close()
}

var _ types.DBIterator = (*EVMIterator)(nil)
