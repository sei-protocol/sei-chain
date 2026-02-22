//go:build rocksdbBackend
// +build rocksdbBackend

package mvcc

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/linxGnu/grocksdb"
	"golang.org/x/exp/slices"

	"github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/common/logger"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/util"
	"github.com/sei-protocol/sei-chain/sei-db/wal"
)

const (
	TimestampSize = 8

	StorePrefixTpl     = "s/k:%s/"
	latestVersionKey   = "s/latest"
	earliestVersionKey = "s/earliest"

	// TODO: Make configurable
	ImportCommitBatchSize = 10000
	MinWALEntriesToKeep   = 1000
)

var (
	_ types.StateStore = (*Database)(nil)

	defaultWriteOpts = grocksdb.NewDefaultWriteOptions()
	defaultReadOpts  = grocksdb.NewDefaultReadOptions()
)

type VersionedChangesets struct {
	Version    int64
	Changesets []*proto.NamedChangeSet
}

type Database struct {
	storage  *grocksdb.DB
	config   config.StateStoreConfig
	cfHandle *grocksdb.ColumnFamilyHandle
	closed   atomic.Bool

	// tsLow reflects the full_history_ts_low CF value. Since pruning is done in
	// a lazy manner, we use this value to prevent reads for versions that will
	// be purged in the next compaction.
	tsLow int64

	// Earliest version for db after pruning
	earliestVersion int64
	// Latest version for db
	latestVersion atomic.Int64

	asyncWriteWG sync.WaitGroup

	// Changelog used to support async write
	streamHandler wal.ChangelogWAL

	// Pending changes to be written to the DB
	pendingChanges chan VersionedChangesets
}

func OpenDB(dataDir string, config config.StateStoreConfig) (*Database, error) {
	//TODO: add a new config and check if readonly = true to support readonly mode

	storage, cfHandle, err := OpenRocksDB(dataDir)
	if err != nil {
		return nil, fmt.Errorf("failed to open RocksDB: %w", err)
	}

	slice, err := storage.GetFullHistoryTsLow(cfHandle)
	if err != nil {
		return nil, fmt.Errorf("failed to get full_history_ts_low: %w", err)
	}

	var tsLow int64
	tsLowBz := copyAndFreeSlice(slice)
	if len(tsLowBz) > 0 {
		tsLow = int64(binary.LittleEndian.Uint64(tsLowBz))
	}

	// Initialize earliest version
	earliestVersion, err := retrieveEarliestVersion(storage)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve earliest version: %w", err)
	}

	latestVersion, err := retrieveLatestVersion(storage)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve earliest version: %w", err)
	}

	database := &Database{
		storage:         storage,
		config:          config,
		cfHandle:        cfHandle,
		tsLow:           tsLow,
		earliestVersion: earliestVersion,
		latestVersion:   atomic.Int64{},
		pendingChanges:  make(chan VersionedChangesets, config.AsyncWriteBuffer),
	}
	database.latestVersion.Store(latestVersion)
	walKeepRecent := math.Max(MinWALEntriesToKeep, float64(config.AsyncWriteBuffer+1))
	streamHandler, err := wal.NewChangelogWAL(logger.NewNopLogger(), utils.GetChangelogPath(dataDir), wal.Config{
		KeepRecent:    uint64(walKeepRecent),
		PruneInterval: time.Duration(config.PruneIntervalSeconds) * time.Second,
	})
	if err != nil {
		return nil, err
	}
	database.streamHandler = streamHandler
	go database.writeAsyncInBackground()

	return database, nil
}

func (db *Database) getSlice(storeKey string, version int64, key []byte) (*grocksdb.Slice, error) {
	return db.storage.GetCF(
		newTSReadOptions(version),
		db.cfHandle,
		prependStoreKey(storeKey, key),
	)
}

func (db *Database) SetLatestVersion(version int64) error {
	db.latestVersion.Store(version)
	var ts [TimestampSize]byte
	binary.LittleEndian.PutUint64(ts[:], uint64(version))
	return db.storage.Put(defaultWriteOpts, []byte(latestVersionKey), ts[:])
}

func (db *Database) GetLatestVersion() int64 {
	return db.latestVersion.Load()
}

// retrieveLatestVersion retrieves the latest version from the database, if not found, return 0.
func retrieveLatestVersion(storage *grocksdb.DB) (int64, error) {
	bz, err := storage.GetBytes(defaultReadOpts, []byte(latestVersionKey))
	if err != nil || len(bz) == 0 {
		return 0, err
	}
	uz := binary.LittleEndian.Uint64(bz)
	if uz > math.MaxInt64 {
		return 0, fmt.Errorf("latest version in rocksdb overflows int64: %d", uz)
	}

	return int64(uz), nil
}

func (db *Database) SetEarliestVersion(version int64, ignoreVersion bool) error {
	if version > db.earliestVersion || ignoreVersion {
		db.earliestVersion = version
		var ts [TimestampSize]byte
		binary.LittleEndian.PutUint64(ts[:], uint64(version))
		return db.storage.Put(defaultWriteOpts, []byte(earliestVersionKey), ts[:])
	}
	return nil
}

func (db *Database) GetEarliestVersion() int64 {
	return db.earliestVersion
}

// retrieveEarliestVersion retrieves the earliest version from the database, if not found, return 0.
func retrieveEarliestVersion(storage *grocksdb.DB) (int64, error) {
	bz, err := storage.GetBytes(defaultReadOpts, []byte(earliestVersionKey))
	if err != nil || len(bz) == 0 {
		return 0, err
	}
	ubz := binary.LittleEndian.Uint64(bz)
	if ubz > math.MaxInt64 {
		return 0, fmt.Errorf("earliest version in rocksdb overflows int64: %d", ubz)
	}
	return int64(ubz), nil
}

func (db *Database) Has(storeKey string, version int64, key []byte) (bool, error) {
	if version < db.earliestVersion {
		return false, nil
	}

	slice, err := db.getSlice(storeKey, version, key)
	if err != nil {
		return false, err
	}

	return slice.Exists(), nil
}

func (db *Database) Get(storeKey string, version int64, key []byte) ([]byte, error) {
	if version < db.earliestVersion {
		return nil, nil
	}

	slice, err := db.getSlice(storeKey, version, key)
	if err != nil {
		return nil, fmt.Errorf("failed to get RocksDB slice: %w", err)
	}

	return copyAndFreeSlice(slice), nil
}

// ApplyChangesetSync apply all changesets for a single version in blocking way
func (db *Database) ApplyChangesetSync(version int64, changeset []*proto.NamedChangeSet) error {
	// Check if version is 0 and change it to 1
	// We do this specifically since keys written as part of genesis state come in as version 0
	// But pebbledb treats version 0 as special, so apply the changeset at version 1 instead
	// Port this over to rocksdb for consistency
	if version == 0 {
		version = 1
	}

	// Update latest version in batch
	b := NewBatch(db, version)

	for _, cs := range changeset {
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
	}

	err := b.Write()
	if err != nil {
		return err
	}
	// Update latest version once all writes succeed
	db.latestVersion.Store(version)
	return nil
}

func (db *Database) ApplyChangesetAsync(version int64, changesets []*proto.NamedChangeSet) error {
	// Add to pending changes
	db.pendingChanges <- VersionedChangesets{
		Version:    version,
		Changesets: changesets,
	}
	// Write to WAL
	if db.streamHandler != nil {
		entry := proto.ChangelogEntry{
			Version: version,
		}
		entry.Changesets = changesets
		entry.Upgrades = nil
		err := db.streamHandler.Write(entry)
		if err != nil {
			return err
		}
	}
	return nil
}

func (db *Database) writeAsyncInBackground() {
	db.asyncWriteWG.Add(1)
	defer db.asyncWriteWG.Done()
	for nextChange := range db.pendingChanges {
		if db.streamHandler != nil {
			version := nextChange.Version
			if err := db.ApplyChangesetSync(version, nextChange.Changesets); err != nil {
				panic(err)
			}
		}
	}
}

// Prune attempts to prune all versions up to and including the provided version.
// This is done internally by updating the full_history_ts_low RocksDB value on
// the column families, s.t. all versions less than full_history_ts_low will be
// dropped.
//
// Note, this does NOT incur an immediate full compaction, i.e. this performs a
// lazy prune. Future compactions will honor the increased full_history_ts_low
// and trim history when possible.
func (db *Database) Prune(version int64) error {
	// Defensive check: ensure database is not closed
	if db.closed.Load() {
		return fmt.Errorf("rocksdb: database is closed")
	}

	tsLow := version + 1 // we increment by 1 to include the provided version

	var ts [TimestampSize]byte
	binary.LittleEndian.PutUint64(ts[:], uint64(tsLow))

	if err := db.storage.IncreaseFullHistoryTsLow(db.cfHandle, ts[:]); err != nil {
		return fmt.Errorf("failed to update column family full_history_ts_low: %w", err)
	}

	db.tsLow = tsLow

	return db.SetEarliestVersion(tsLow, false)
}

func (db *Database) Iterator(storeKey string, version int64, start, end []byte) (types.DBIterator, error) {
	if (start != nil && len(start) == 0) || (end != nil && len(end) == 0) {
		return nil, errors.ErrKeyEmpty
	}

	if start != nil && end != nil && bytes.Compare(start, end) > 0 {
		return nil, errors.ErrStartAfterEnd
	}

	prefix := storePrefix(storeKey)
	start, end = util.IterateWithPrefix(prefix, start, end)

	itr := db.storage.NewIteratorCF(newTSReadOptions(version), db.cfHandle)
	return NewRocksDBIterator(itr, prefix, start, end, version, db.earliestVersion, false), nil
}

func (db *Database) ReverseIterator(storeKey string, version int64, start, end []byte) (types.DBIterator, error) {
	if (start != nil && len(start) == 0) || (end != nil && len(end) == 0) {
		return nil, errors.ErrKeyEmpty
	}

	if start != nil && end != nil && bytes.Compare(start, end) > 0 {
		return nil, errors.ErrStartAfterEnd
	}

	prefix := storePrefix(storeKey)
	start, end = util.IterateWithPrefix(prefix, start, end)

	itr := db.storage.NewIteratorCF(newTSReadOptions(version), db.cfHandle)
	return NewRocksDBIterator(itr, prefix, start, end, version, db.earliestVersion, true), nil
}

// Import loads the initial version of the state in parallel with numWorkers goroutines
// TODO: Potentially add retries instead of panics
func (db *Database) Import(version int64, ch <-chan types.SnapshotNode) error {
	var wg sync.WaitGroup

	worker := func() {
		defer wg.Done()
		batch := NewBatch(db, version)

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

				batch = NewBatch(db, version)
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
// TODO: Accept list of storeKeys to export
func (db *Database) RawIterate(storeKey string, fn func(key []byte, value []byte, version int64) bool) (bool, error) {
	// If store key provided, only iterate over keys with prefix
	var prefix []byte
	if storeKey != "" {
		prefix = storePrefix(storeKey)
	}
	start, end := util.IterateWithPrefix(prefix, nil, nil)

	latestVersion, err := retrieveLatestVersion(db.storage)
	if err != nil {
		return false, err
	}

	var startTs [TimestampSize]byte
	binary.LittleEndian.PutUint64(startTs[:], uint64(0))

	var endTs [TimestampSize]byte
	binary.LittleEndian.PutUint64(endTs[:], uint64(latestVersion))

	// Set timestamp lower and upper bound to iterate over all keys in db
	readOpts := grocksdb.NewDefaultReadOptions()
	readOpts.SetIterStartTimestamp(startTs[:])
	readOpts.SetTimestamp(endTs[:])
	defer readOpts.Destroy()

	itr := db.storage.NewIteratorCF(readOpts, db.cfHandle)
	rocksItr := NewRocksDBIterator(itr, prefix, start, end, latestVersion, 1, false)
	defer func() { _ = rocksItr.Close() }()

	for rocksItr.Valid() {
		key := rocksItr.Key()
		value := rocksItr.Value()
		version := int64(binary.LittleEndian.Uint64(itr.Timestamp().Data()))

		// Call callback fn
		if fn(key, value, version) {
			return true, nil
		}

		rocksItr.Next()
	}

	return false, nil
}

func (db *Database) GetLatestMigratedKey() ([]byte, error) {
	panic("not implemented")
}

func (db *Database) GetLatestMigratedModule() (string, error) {
	panic("not implemented")
}

// newTSReadOptions returns ReadOptions used in the RocksDB column family read.
func newTSReadOptions(version int64) *grocksdb.ReadOptions {
	var ts [TimestampSize]byte
	binary.LittleEndian.PutUint64(ts[:], uint64(version))

	readOpts := grocksdb.NewDefaultReadOptions()
	readOpts.SetTimestamp(ts[:])

	return readOpts
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

// copyAndFreeSlice will copy a given RocksDB slice and free it. If the slice does
// not exist, <nil> will be returned.
func copyAndFreeSlice(s *grocksdb.Slice) []byte {
	defer s.Free()
	if !s.Exists() {
		return nil
	}

	return slices.Clone(s.Data())
}

func readOnlySlice(s *grocksdb.Slice) []byte {
	if !s.Exists() {
		return nil
	}

	return s.Data()
}

func cloneAppend(bz []byte, tail []byte) (res []byte) {
	res = make([]byte, len(bz)+len(tail))
	copy(res, bz)
	copy(res[len(bz):], tail)
	return
}
func (db *Database) RawImport(ch <-chan types.RawSnapshotNode) error {
	panic("implement me")
}

func (db *Database) Close() error {
	db.closed.Store(true)

	if db.streamHandler != nil {
		// Close the pending changes channel to signal the background goroutine to stop
		close(db.pendingChanges)
		// Wait for the async writes to finish processing all buffered items
		db.asyncWriteWG.Wait()
		// Close the changelog stream first
		_ = db.streamHandler.Close()
		// Only set to nil after background goroutine has finished
		db.streamHandler = nil
	}
	db.cfHandle = nil
	if db.storage != nil {
		db.storage.Close()
		db.storage = nil
	}

	return nil
}
