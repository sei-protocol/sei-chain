//go:build rocksdbBackend
// +build rocksdbBackend

package rocksdb

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/linxGnu/grocksdb"
	errorutils "github.com/sei-protocol/sei-db/common/errors"
	"github.com/sei-protocol/sei-db/common/logger"
	"github.com/sei-protocol/sei-db/common/utils"
	"github.com/sei-protocol/sei-db/config"
	"github.com/sei-protocol/sei-db/proto"
	"github.com/sei-protocol/sei-db/ss/types"
	"github.com/sei-protocol/sei-db/ss/util"
	"github.com/sei-protocol/sei-db/stream/changelog"
	"golang.org/x/exp/slices"
)

const (
	TimestampSize = 8

	StorePrefixTpl     = "s/k:%s/"
	latestVersionKey   = "s/latest"
	earliestVersionKey = "s/earliest"

	// TODO: Make configurable
	ImportCommitBatchSize = 10000
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

	// tsLow reflects the full_history_ts_low CF value. Since pruning is done in
	// a lazy manner, we use this value to prevent reads for versions that will
	// be purged in the next compaction.
	tsLow int64

	// Earliest version for db after pruning
	earliestVersion int64

	asyncWriteWG sync.WaitGroup

	// Changelog used to support async write
	streamHandler *changelog.Stream

	// Pending changes to be written to the DB
	pendingChanges chan VersionedChangesets
}

func New(dataDir string, config config.StateStoreConfig) (*Database, error) {
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

	earliestVersion, err := retrieveEarliestVersion(storage)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve earliest version: %w", err)
	}

	database := &Database{
		storage:         storage,
		config:          config,
		cfHandle:        cfHandle,
		tsLow:           tsLow,
		earliestVersion: earliestVersion,
		pendingChanges:  make(chan VersionedChangesets, config.AsyncWriteBuffer),
	}

	if config.DedicatedChangelog {
		streamHandler, _ := changelog.NewStream(
			logger.NewNopLogger(),
			utils.GetChangelogPath(dataDir),
			changelog.Config{
				DisableFsync:  true,
				ZeroCopy:      true,
				KeepRecent:    uint64(config.KeepRecent),
				PruneInterval: 300 * time.Second,
			},
		)
		database.streamHandler = streamHandler
		go database.writeAsyncInBackground()
	}

	return database, nil
}

func NewWithDB(storage *grocksdb.DB, cfHandle *grocksdb.ColumnFamilyHandle) (*Database, error) {
	slice, err := storage.GetFullHistoryTsLow(cfHandle)
	if err != nil {
		return nil, fmt.Errorf("failed to get full_history_ts_low: %w", err)
	}

	var tsLow int64
	tsLowBz := copyAndFreeSlice(slice)
	if len(tsLowBz) > 0 {
		tsLow = int64(binary.LittleEndian.Uint64(tsLowBz))
	}

	earliestVersion, err := retrieveEarliestVersion(storage)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve earliest version: %w", err)
	}

	return &Database{
		storage:         storage,
		cfHandle:        cfHandle,
		tsLow:           tsLow,
		earliestVersion: earliestVersion,
	}, nil
}

func (db *Database) Close() error {
	if db.streamHandler != nil {
		// Close the changelog stream first
		db.streamHandler.Close()
		// Close the pending changes channel to signal the background goroutine to stop
		close(db.pendingChanges)
		// Wait for the async writes to finish processing all buffered items
		db.asyncWriteWG.Wait()
		// Only set to nil after background goroutine has finished
		db.streamHandler = nil
	}

	db.storage.Close()

	db.storage = nil
	db.cfHandle = nil

	return nil
}

func (db *Database) getSlice(storeKey string, version int64, key []byte) (*grocksdb.Slice, error) {
	return db.storage.GetCF(
		newTSReadOptions(version),
		db.cfHandle,
		prependStoreKey(storeKey, key),
	)
}

func (db *Database) SetLatestVersion(version int64) error {
	var ts [TimestampSize]byte
	binary.LittleEndian.PutUint64(ts[:], uint64(version))

	return db.storage.Put(defaultWriteOpts, []byte(latestVersionKey), ts[:])
}

func (db *Database) GetLatestVersion() (int64, error) {
	bz, err := db.storage.GetBytes(defaultReadOpts, []byte(latestVersionKey))
	if err != nil {
		return 0, err
	}

	if len(bz) == 0 {
		// in case of a fresh database
		return 0, nil
	}

	return int64(binary.LittleEndian.Uint64(bz)), nil
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

func (db *Database) GetEarliestVersion() (int64, error) {
	return db.earliestVersion, nil
}

func (db *Database) Has(storeKey string, version int64, key []byte) (bool, error) {
	if version < db.earliestVersion {
		return false, nil
	}

	slice, err := db.getSlice(storeKey, version, key)
	if err != nil && !errors.Is(err, errorutils.ErrRecordNotFound) {
		return false, err
	}

	return slice.Exists(), nil
}

func (db *Database) Get(storeKey string, version int64, key []byte) ([]byte, error) {
	if version < db.earliestVersion {
		return nil, errorutils.ErrRecordNotFound
	}

	slice, err := db.getSlice(storeKey, version, key)
	if err != nil {
		return nil, fmt.Errorf("failed to get RocksDB slice: %w", err)
	}

	if !slice.Exists() {
		slice.Free()
		return nil, errorutils.ErrRecordNotFound
	}

	return copyAndFreeSlice(slice), nil
}

func (db *Database) ApplyChangeset(version int64, cs *proto.NamedChangeSet) error {
	// Check if version is 0 and change it to 1
	// We do this specifically since keys written as part of genesis state come in as version 0
	// But pebbledb treats version 0 as special, so apply the changeset at version 1 instead
	// Port this over to rocksdb for consistency
	if version == 0 {
		version = 1
	}

	b := NewBatch(db, version)

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

	return b.Write()
}

func (db *Database) ApplyChangesetAsync(version int64, changesets []*proto.NamedChangeSet) error {
	// Write to WAL first
	if db.streamHandler != nil {
		entry := proto.ChangelogEntry{
			Version: version,
		}
		entry.Changesets = changesets
		entry.Upgrades = nil
		err := db.streamHandler.WriteNextEntry(entry)
		if err != nil {
			return err
		}
	}
	// Then write to pending changes
	db.pendingChanges <- VersionedChangesets{
		Version:    version,
		Changesets: changesets,
	}

	return nil
}

func (db *Database) writeAsyncInBackground() {
	db.asyncWriteWG.Add(1)
	defer db.asyncWriteWG.Done()
	for nextChange := range db.pendingChanges {
		if db.streamHandler != nil {
			version := nextChange.Version
			for _, cs := range nextChange.Changesets {
				err := db.ApplyChangeset(version, cs)
				if err != nil {
					panic(err)
				}
			}
			err := db.SetLatestVersion(version)
			if err != nil {
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
		return nil, errorutils.ErrKeyEmpty
	}

	if start != nil && end != nil && bytes.Compare(start, end) > 0 {
		return nil, errorutils.ErrStartAfterEnd
	}

	prefix := storePrefix(storeKey)
	start, end = util.IterateWithPrefix(prefix, start, end)

	itr := db.storage.NewIteratorCF(newTSReadOptions(version), db.cfHandle)
	return NewRocksDBIterator(itr, prefix, start, end, version, db.earliestVersion, false), nil
}

func (db *Database) ReverseIterator(storeKey string, version int64, start, end []byte) (types.DBIterator, error) {
	if (start != nil && len(start) == 0) || (end != nil && len(end) == 0) {
		return nil, errorutils.ErrKeyEmpty
	}

	if start != nil && end != nil && bytes.Compare(start, end) > 0 {
		return nil, errorutils.ErrStartAfterEnd
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

	latestVersion, err := db.GetLatestVersion()
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
	defer rocksItr.Close()

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

// WriteBlockRangeHash writes a hash for a range of blocks to the database
func (db *Database) WriteBlockRangeHash(storeKey string, beginBlockRange, endBlockRange int64, hash []byte) error {
	panic("implement me")
}

// retrieveEarliestVersion retrieves the earliest version from the database
func retrieveEarliestVersion(storage *grocksdb.DB) (int64, error) {
	bz, err := storage.GetBytes(defaultReadOpts, []byte(earliestVersionKey))
	if err != nil {
		fmt.Printf("warning: rocksdb get for earliestVersionKey failed: %v", err)
		return 0, nil
	}

	if len(bz) == 0 {
		// in case of a fresh database
		return 0, nil
	}

	return int64(binary.LittleEndian.Uint64(bz)), nil
}
