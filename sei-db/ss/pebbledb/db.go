package pebbledb

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"

	"github.com/cockroachdb/pebble"
	"github.com/cockroachdb/pebble/bloom"
	errorutils "github.com/sei-protocol/sei-db/common/errors"
	"github.com/sei-protocol/sei-db/config"
	"github.com/sei-protocol/sei-db/proto"
	"github.com/sei-protocol/sei-db/ss/types"
	"golang.org/x/exp/slices"
)

const (
	VersionSize = 8

	PrefixStore        = "s/k:"
	LenPrefixStore     = 4
	StorePrefixTpl     = "s/k:%s/"   // s/k:<storeKey>
	latestVersionKey   = "s/_latest" // NB: latestVersionKey key must be lexically smaller than StorePrefixTpl
	earliestVersionKey = "s/_earliest"
	tombstoneVal       = "TOMBSTONE"

	// TODO: Make configurable
	ImportCommitBatchSize = 10000
	PruneCommitBatchSize  = 50
)

var (
	_ types.StateStore = (*Database)(nil)

	defaultWriteOpts = pebble.NoSync
)

type Database struct {
	storage *pebble.DB
	config  config.StateStoreConfig
	// Earliest version for db after pruning
	earliestVersion int64

	// Map of module to when each was last updated
	// Used in pruning to skip over stores that have not been updated recently
	storeKeyDirty sync.Map
}

func New(dataDir string, config config.StateStoreConfig) (*Database, error) {
	cache := pebble.NewCache(1024 * 1024 * 32)
	defer cache.Unref()
	opts := &pebble.Options{
		Cache:                       cache,
		Comparer:                    MVCCComparer,
		FormatMajorVersion:          pebble.FormatNewest,
		L0CompactionThreshold:       2,
		L0StopWritesThreshold:       1000,
		LBaseMaxBytes:               64 << 20, // 64 MB
		Levels:                      make([]pebble.LevelOptions, 7),
		MaxConcurrentCompactions:    func() int { return 3 }, // TODO: Make Configurable
		MemTableSize:                64 << 20,
		MemTableStopWritesThreshold: 4,
	}

	for i := 0; i < len(opts.Levels); i++ {
		l := &opts.Levels[i]
		l.BlockSize = 32 << 10       // 32 KB
		l.IndexBlockSize = 256 << 10 // 256 KB
		l.FilterPolicy = bloom.FilterPolicy(10)
		l.FilterType = pebble.TableFilter
		// TODO: Consider compression only for specific layers like bottommost
		l.Compression = pebble.ZstdCompression
		if i > 0 {
			l.TargetFileSize = opts.Levels[i-1].TargetFileSize * 2
		}
		l.EnsureDefaults()
	}

	opts.Levels[6].FilterPolicy = nil
	opts.FlushSplitBytes = opts.Levels[0].TargetFileSize
	opts = opts.EnsureDefaults()

	db, err := pebble.Open(dataDir, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to open PebbleDB: %w", err)
	}

	earliestVersion, err := retrieveEarliestVersion(db)
	if err != nil {
		return nil, fmt.Errorf("failed to open PebbleDB: %w", err)
	}

	return &Database{
		storage:         db,
		config:          config,
		earliestVersion: earliestVersion,
	}, nil
}

func NewWithDB(storage *pebble.DB) *Database {
	return &Database{
		storage: storage,
	}
}

func (db *Database) Close() error {
	err := db.storage.Close()
	db.storage = nil
	return err
}

func (db *Database) SetLatestVersion(version int64) error {
	var ts [VersionSize]byte
	binary.LittleEndian.PutUint64(ts[:], uint64(version))
	return db.storage.Set([]byte(latestVersionKey), ts[:], defaultWriteOpts)
}

func (db *Database) GetLatestVersion() (int64, error) {
	bz, closer, err := db.storage.Get([]byte(latestVersionKey))
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			// in case of a fresh database
			return 0, nil
		}

		return 0, err
	}

	if len(bz) == 0 {
		return 0, closer.Close()
	}

	return int64(binary.LittleEndian.Uint64(bz)), closer.Close()
}

func (db *Database) SetEarliestVersion(version int64) error {
	db.earliestVersion = version

	var ts [VersionSize]byte
	binary.LittleEndian.PutUint64(ts[:], uint64(version))
	return db.storage.Set([]byte(earliestVersionKey), ts[:], defaultWriteOpts)
}

func (db *Database) GetEarliestVersion() (int64, error) {
	return db.earliestVersion, nil
}

// Retrieves earliest version from db
func retrieveEarliestVersion(db *pebble.DB) (int64, error) {
	bz, closer, err := db.Get([]byte(earliestVersionKey))
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			// in case of a fresh database
			return 0, nil
		}

		return 0, err
	}

	if len(bz) == 0 {
		return 0, closer.Close()
	}

	return int64(binary.LittleEndian.Uint64(bz)), closer.Close()
}

func (db *Database) Has(storeKey string, version int64, key []byte) (bool, error) {
	if version < db.earliestVersion {
		return false, nil
	}

	val, err := db.Get(storeKey, version, key)
	if err != nil {
		return false, err
	}

	return val != nil, nil
}

func (db *Database) Get(storeKey string, targetVersion int64, key []byte) ([]byte, error) {
	if targetVersion < db.earliestVersion {
		return nil, nil
	}

	prefixedVal, err := getMVCCSlice(db.storage, storeKey, key, targetVersion)
	if err != nil {
		if errors.Is(err, errorutils.ErrRecordNotFound) {
			return nil, nil
		}

		return nil, fmt.Errorf("failed to perform PebbleDB read: %w", err)
	}

	valBz, tombBz, ok := SplitMVCCKey(prefixedVal)
	if !ok {
		return nil, fmt.Errorf("invalid PebbleDB MVCC value: %s", prefixedVal)
	}

	// A tombstone of zero or a target version that is less than the tombstone
	// version means the key is not deleted at the target version.
	if len(tombBz) == 0 {
		return valBz, nil
	}

	tombstone, err := decodeUint64Ascending(tombBz)
	if err != nil {
		return nil, fmt.Errorf("failed to decode value tombstone: %w", err)
	}

	// A tombstone of zero or a target version that is less than the tombstone
	// version means the key is not deleted at the target version.
	if targetVersion < tombstone {
		return valBz, nil
	}

	// the value is considered deleted
	return nil, nil
}

func (db *Database) ApplyChangeset(version int64, cs *proto.NamedChangeSet) error {
	// Check if version is 0 and change it to 1
	// We do this specifically since keys written as part of genesis state come in as version 0
	// But pebbledb treats version 0 as special, so apply the changeset at version 1 instead
	if version == 0 {
		version = 1
	}

	b, err := NewBatch(db.storage, version)
	if err != nil {
		return err
	}

	for _, kvPair := range cs.Changeset.Pairs {
		if kvPair.Value == nil {
			if err := b.Delete(cs.Name, kvPair.Key); err != nil {
				return err
			}
		} else {
			if err := b.Set(cs.Name, kvPair.Key, kvPair.Value); err != nil {
				return err
			}
		}
	}

	// Mark the store as updated
	db.storeKeyDirty.Store(cs.Name, version)

	return b.Write()
}

// Prune attempts to prune all versions up to and including the current version
// Get the range of keys, manually iterate over them and delete them
// We add a heuristic to skip over a module's keys during pruning if it hasn't been updated
// since the last time pruning occurred.
// NOTE: There is a rare case when a module's keys are skipped during pruning even though
// it has been updated. This occurs when that module is updated in between pruning runs, the node after is restarted.
// This is not a large issue given the next time that module is updated, it will be properly pruned thereafter.
func (db *Database) Prune(version int64) error {
	earliestVersion := version + 1 // we increment by 1 to include the provided version

	itr, err := db.storage.NewIter(nil)
	if err != nil {
		return err
	}
	defer itr.Close()

	batch := db.storage.NewBatch()
	defer batch.Close()

	var (
		counter                                 int
		prevKey, prevKeyEncoded, prevValEncoded []byte
		prevVersionDecoded                      int64
		prevStore                               string
	)

	for itr.First(); itr.Valid(); {
		currKeyEncoded := slices.Clone(itr.Key())

		// Ignore metadata entry for version during pruning
		if bytes.Equal(currKeyEncoded, []byte(latestVersionKey)) || bytes.Equal(currKeyEncoded, []byte(earliestVersionKey)) {
			itr.Next()
			continue
		}

		// Store current key and version
		currKey, currVersion, currOK := SplitMVCCKey(currKeyEncoded)
		if !currOK {
			return fmt.Errorf("invalid MVCC key")
		}

		storeKey, err := parseStoreKey(currKey)
		if err != nil {
			// XXX: This should never happen given we skip the metadata keys.
			return err
		}

		// For every new module visited, check to see last time it was updated
		if storeKey != prevStore {
			prevStore = storeKey
			updated, ok := db.storeKeyDirty.Load(storeKey)
			versionUpdated, typeOk := updated.(int64)
			// Skip a store's keys if version it was last updated is less than last prune height
			if !ok || (typeOk && versionUpdated < db.earliestVersion) {
				itr.SeekGE(storePrefix(storeKey + "0"))
				continue
			}
		}

		currVersionDecoded, err := decodeUint64Ascending(currVersion)
		if err != nil {
			return err
		}

		// Seek to next key if we are at a version which is higher than prune height
		// Do not seek to next key if KeepLastVersion is false and we need to delete the previous key in pruning
		if currVersionDecoded > version && (db.config.KeepLastVersion || prevVersionDecoded > version) {
			itr.NextPrefix()
			continue
		}

		// Delete a key if another entry for that key exists at a larger version than original but leq to the prune height
		// Also delete a key if it has been tombstoned and its version is leq to the prune height
		// Also delete a key if KeepLastVersion is false and version is leq to the prune height
		if prevVersionDecoded <= version && (bytes.Equal(prevKey, currKey) || valTombstoned(prevValEncoded) || !db.config.KeepLastVersion) {
			err = batch.Delete(prevKeyEncoded, nil)
			if err != nil {
				return err
			}

			counter++
			if counter >= PruneCommitBatchSize {
				err = batch.Commit(defaultWriteOpts)
				if err != nil {
					return err
				}

				counter = 0
				batch.Reset()
			}
		}

		// Update prevKey and prevVersion for next iteration
		prevKey = currKey
		prevVersionDecoded = currVersionDecoded
		prevKeyEncoded = currKeyEncoded
		prevValEncoded = slices.Clone(itr.Value())

		itr.Next()
	}

	// Commit any leftover delete ops in batch
	if counter > 0 {
		err = batch.Commit(defaultWriteOpts)
		if err != nil {
			return err
		}
	}

	return db.SetEarliestVersion(earliestVersion)
}

func (db *Database) Iterator(storeKey string, version int64, start, end []byte) (types.DBIterator, error) {
	if (start != nil && len(start) == 0) || (end != nil && len(end) == 0) {
		return nil, errorutils.ErrKeyEmpty
	}

	if start != nil && end != nil && bytes.Compare(start, end) > 0 {
		return nil, errorutils.ErrStartAfterEnd
	}

	lowerBound := MVCCEncode(prependStoreKey(storeKey, start), 0)

	var upperBound []byte
	if end != nil {
		upperBound = MVCCEncode(prependStoreKey(storeKey, end), 0)
	}

	itr, err := db.storage.NewIter(&pebble.IterOptions{LowerBound: lowerBound, UpperBound: upperBound})
	if err != nil {
		return nil, fmt.Errorf("failed to create PebbleDB iterator: %w", err)
	}

	return newPebbleDBIterator(itr, storePrefix(storeKey), start, end, version, db.earliestVersion, false), nil
}

func (db *Database) ReverseIterator(storeKey string, version int64, start, end []byte) (types.DBIterator, error) {
	if (start != nil && len(start) == 0) || (end != nil && len(end) == 0) {
		return nil, errorutils.ErrKeyEmpty
	}

	if start != nil && end != nil && bytes.Compare(start, end) > 0 {
		return nil, errorutils.ErrStartAfterEnd
	}

	lowerBound := MVCCEncode(prependStoreKey(storeKey, start), 0)

	var upperBound []byte
	if end != nil {
		upperBound = MVCCEncode(prependStoreKey(storeKey, end), 0)
	}

	itr, err := db.storage.NewIter(&pebble.IterOptions{LowerBound: lowerBound, UpperBound: upperBound})
	if err != nil {
		return nil, fmt.Errorf("failed to create PebbleDB iterator: %w", err)
	}

	return newPebbleDBIterator(itr, storePrefix(storeKey), start, end, version, db.earliestVersion, true), nil
}

// Import loads the initial version of the state in parallel with numWorkers goroutines
// TODO: Potentially add retries instead of panics
func (db *Database) Import(version int64, ch <-chan types.SnapshotNode) error {
	var wg sync.WaitGroup

	worker := func() {
		defer wg.Done()
		batch, err := NewBatch(db.storage, version)
		if err != nil {
			panic(err)
		}

		var counter int
		for entry := range ch {
			err := batch.Set(entry.StoreKey, entry.Key, entry.Value)
			if err != nil {
				panic(err)
			}

			counter++
			if counter%ImportCommitBatchSize == 0 {
				if err := batch.Write(); err != nil {
					panic(err)
				}

				batch, err = NewBatch(db.storage, version)
				if err != nil {
					panic(err)
				}
			}
		}

		if batch.Size() > 0 {
			if err := batch.Write(); err != nil {
				panic(err)
			}
		}
	}

	wg.Add(db.config.ImportNumWorkers)
	for i := 0; i < db.config.ImportNumWorkers; i++ {
		go worker()
	}

	wg.Wait()

	return nil
}

// RawIterate iterates over all keys and values for a store
func (db *Database) RawIterate(storeKey string, fn func(key []byte, value []byte, version int64) bool) (bool, error) {
	// Iterate through all keys and values for a store
	lowerBound := MVCCEncode(prependStoreKey(storeKey, nil), 0)

	itr, err := db.storage.NewIter(&pebble.IterOptions{LowerBound: lowerBound})
	if err != nil {
		return false, fmt.Errorf("failed to create PebbleDB iterator: %w", err)
	}
	defer itr.Close()

	for itr.First(); itr.Valid(); itr.Next() {
		currKeyEncoded := itr.Key()

		// Ignore metadata entry for version
		if bytes.Equal(currKeyEncoded, []byte(latestVersionKey)) || bytes.Equal(currKeyEncoded, []byte(earliestVersionKey)) {
			continue
		}

		// Store current key and version
		currKey, currVersion, currOK := SplitMVCCKey(currKeyEncoded)
		if !currOK {
			return false, fmt.Errorf("invalid MVCC key")
		}

		// Only iterate through module
		if storeKey != "" && !bytes.HasPrefix(currKey, storePrefix(storeKey)) {
			break
		}

		currVersionDecoded, err := decodeUint64Ascending(currVersion)
		if err != nil {
			return false, err
		}

		// Decode the value
		currValEncoded := itr.Value()
		if valTombstoned(currValEncoded) {
			continue
		}
		valBz, _, ok := SplitMVCCKey(currValEncoded)
		if !ok {
			return false, fmt.Errorf("invalid PebbleDB MVCC value: %s", currKey)
		}

		// Call callback fn
		if fn(currKey, valBz, currVersionDecoded) {
			return true, nil
		}

	}

	return false, nil
}

func storePrefix(storeKey string) []byte {
	return []byte(fmt.Sprintf(StorePrefixTpl, storeKey))
}

func prependStoreKey(storeKey string, key []byte) []byte {
	if storeKey == "" {
		return key
	}
	return append(storePrefix(storeKey), key...)
}

// Parses store from key with format "s/k:{store}/..."
func parseStoreKey(key []byte) (string, error) {
	// Convert byte slice to string only once
	keyStr := string(key)

	if !strings.HasPrefix(keyStr, PrefixStore) {
		return "", fmt.Errorf("not a valid store key")
	}

	// Find the first occurrence of "/" after the prefix
	slashIndex := strings.Index(keyStr[LenPrefixStore:], "/")
	if slashIndex == -1 {
		return "", fmt.Errorf("not a valid store key")
	}

	// Return the substring between the prefix and the first "/"
	return keyStr[LenPrefixStore : LenPrefixStore+slashIndex], nil
}

func getMVCCSlice(db *pebble.DB, storeKey string, key []byte, version int64) ([]byte, error) {
	// end domain is exclusive, so we need to increment the version by 1
	if version < math.MaxInt64 {
		version++
	}

	itr, err := db.NewIter(&pebble.IterOptions{
		LowerBound: MVCCEncode(prependStoreKey(storeKey, key), 0),
		UpperBound: MVCCEncode(prependStoreKey(storeKey, key), version),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create PebbleDB iterator: %w", err)
	}
	defer func() {
		err = errorutils.Join(err, itr.Close())
	}()

	if !itr.Last() {
		return nil, errorutils.ErrRecordNotFound
	}

	_, vBz, ok := SplitMVCCKey(itr.Key())
	if !ok {
		return nil, fmt.Errorf("invalid PebbleDB MVCC key: %s", itr.Key())
	}

	keyVersion, err := decodeUint64Ascending(vBz)
	if err != nil {
		return nil, fmt.Errorf("failed to decode key version: %w", err)
	}
	if keyVersion > version {
		return nil, fmt.Errorf("key version too large: %d", keyVersion)
	}

	return slices.Clone(itr.Value()), nil
}

func valTombstoned(value []byte) bool {
	if value == nil {
		return false
	}
	_, tombBz, ok := SplitMVCCKey(value)
	if !ok {
		// XXX: This should not happen as that would indicate we have a malformed
		// MVCC value.
		panic(fmt.Sprintf("invalid PebbleDB MVCC value: %s", value))
	}

	// If the tombstone suffix is empty, we consider this a zero value and thus it
	// is not tombstoned.
	if len(tombBz) == 0 {
		return false
	}

	return true
}
