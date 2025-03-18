package pebbledb

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/armon/go-metrics"
	"github.com/cockroachdb/pebble"
	"github.com/cockroachdb/pebble/bloom"
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
	VersionSize = 8

	PrefixStore                  = "s/k:"
	LenPrefixStore               = 4
	StorePrefixTpl               = "s/k:%s/"          // s/k:<storeKey>
	HashTpl                      = "s/_hash:%s:%d-%d" // "s/_hash:<storeKey>:%d-%d"
	latestVersionKey             = "s/_latest"        // NB: latestVersionKey key must be lexically smaller than StorePrefixTpl
	earliestVersionKey           = "s/_earliest"
	latestMigratedKeyMetadata    = "s/_latestMigratedKey"
	latestMigratedModuleMetadata = "s/_latestMigratedModule"
	lastRangeHashKey             = "s/_hash:latestRange"
	tombstoneVal                 = "TOMBSTONE"

	// TODO: Make configurable
	ImportCommitBatchSize = 10000
	PruneCommitBatchSize  = 50
	DeleteCommitBatchSize = 50

	// Number of workers to use for hash computation
	HashComputationWorkers = 10
)

var (
	_ types.StateStore = (*Database)(nil)

	defaultWriteOpts = pebble.NoSync
)

type Database struct {
	storage      *pebble.DB
	asyncWriteWG sync.WaitGroup
	config       config.StateStoreConfig
	// Earliest version for db after pruning
	earliestVersion int64

	// Map of module to when each was last updated
	// Used in pruning to skip over stores that have not been updated recently
	storeKeyDirty sync.Map

	// Changelog used to support async write
	streamHandler *changelog.Stream

	// Pending changes to be written to the DB
	pendingChanges chan VersionedChangesets

	// Cache for last range hashed
	lastRangeHashedCache int64
	lastRangeHashedMu    sync.RWMutex
	hashComputationMu    sync.Mutex
}

type VersionedChangesets struct {
	Version    int64
	Changesets []*proto.NamedChangeSet
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
	database := &Database{
		storage:         db,
		asyncWriteWG:    sync.WaitGroup{},
		config:          config,
		earliestVersion: earliestVersion,
		pendingChanges:  make(chan VersionedChangesets, config.AsyncWriteBuffer),
	}

	// Initialize the lastRangeHashed cache
	lastHashed, err := retrieveLastRangeHashed(db)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve last range hashed: %w", err)
	}
	database.lastRangeHashedCache = lastHashed

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

func NewWithDB(storage *pebble.DB) *Database {
	return &Database{
		storage: storage,
	}
}

func (db *Database) Close() error {
	if db.streamHandler != nil {
		db.streamHandler.Close()
		db.streamHandler = nil
		close(db.pendingChanges)
	}
	// Wait for the async writes to finish
	db.asyncWriteWG.Wait()
	err := db.storage.Close()
	db.storage = nil
	return err
}

func (db *Database) SetLatestVersion(version int64) error {
	var ts [VersionSize]byte
	binary.LittleEndian.PutUint64(ts[:], uint64(version))
	err := db.storage.Set([]byte(latestVersionKey), ts[:], defaultWriteOpts)
	return err
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

func (db *Database) SetEarliestVersion(version int64, ignoreVersion bool) error {
	if version > db.earliestVersion || ignoreVersion {
		db.earliestVersion = version

		var ts [VersionSize]byte
		binary.LittleEndian.PutUint64(ts[:], uint64(version))
		return db.storage.Set([]byte(earliestVersionKey), ts[:], defaultWriteOpts)
	}
	return nil
}

func (db *Database) GetEarliestVersion() (int64, error) {
	return db.earliestVersion, nil
}

func (db *Database) SetLastRangeHashed(latestHashed int64) error {
	var ts [VersionSize]byte
	binary.LittleEndian.PutUint64(ts[:], uint64(latestHashed))

	// Update the cache first
	db.lastRangeHashedMu.Lock()
	db.lastRangeHashedCache = latestHashed
	db.lastRangeHashedMu.Unlock()

	return db.storage.Set([]byte(lastRangeHashKey), ts[:], defaultWriteOpts)
}

// GetLastRangeHashed returns the highest block that has been fully hashed in ranges.
func (db *Database) GetLastRangeHashed() (int64, error) {
	// Return the cached value
	db.lastRangeHashedMu.RLock()
	cachedValue := db.lastRangeHashedCache
	db.lastRangeHashedMu.RUnlock()

	return cachedValue, nil
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

// SetLatestKey sets the latest key processed during migration.
func (db *Database) SetLatestMigratedKey(key []byte) error {
	return db.storage.Set([]byte(latestMigratedKeyMetadata), key, defaultWriteOpts)
}

// GetLatestKey retrieves the latest key processed during migration.
func (db *Database) GetLatestMigratedKey() ([]byte, error) {
	bz, closer, err := db.storage.Get([]byte(latestMigratedKeyMetadata))
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	defer closer.Close()
	return bz, nil
}

// SetLatestModule sets the latest module processed during migration.
func (db *Database) SetLatestMigratedModule(module string) error {
	return db.storage.Set([]byte(latestMigratedModuleMetadata), []byte(module), defaultWriteOpts)
}

// GetLatestModule retrieves the latest module processed during migration.
func (db *Database) GetLatestMigratedModule() (string, error) {
	bz, closer, err := db.storage.Get([]byte(latestMigratedModuleMetadata))
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return "", nil
		}
		return "", err
	}
	defer closer.Close()
	return string(bz), nil
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

	if db.config.HashRange > 0 {
		go func(ver int64) {
			// Try to acquire lock, return immediately if already locked
			if !db.hashComputationMu.TryLock() {
				return
			}
			defer db.hashComputationMu.Unlock()

			if err := db.computeMissingRanges(ver); err != nil {
				fmt.Printf("maybeComputeMissingRanges error: %v\n", err)
			}
		}(version)
	}

	return nil
}

func (db *Database) computeMissingRanges(latestVersion int64) error {
	lastHashed, err := db.GetLastRangeHashed()
	if err != nil {
		return fmt.Errorf("failed to get last hashed range: %w", err)
	}

	// Keep filling full chunks until we can't
	for {
		nextTarget := lastHashed + db.config.HashRange

		// If we haven't reached the next full chunk boundary yet, stop.
		// We do NOT do partial chunks.
		if nextTarget > latestVersion {
			break
		}

		// We have a full chunk from (lastHashed+1) .. nextTarget
		begin := lastHashed + 1
		end := nextTarget
		if err := db.computeHashForRange(begin, end); err != nil {
			return err
		}

		// Mark that we've completed that chunk
		lastHashed = end
		if err := db.SetLastRangeHashed(lastHashed); err != nil {
			return err
		}
	}

	return nil
}

func (db *Database) computeHashForRange(beginBlock, endBlock int64) error {
	chunkSize := endBlock - beginBlock + 1
	if chunkSize <= 0 {
		// Nothing to do
		return nil
	}

	// Use constant number of workers
	numOfWorkers := HashComputationWorkers

	// Calculate blocks per worker
	blocksPerWorker := chunkSize / int64(numOfWorkers)
	if blocksPerWorker < 1 {
		blocksPerWorker = 1
	}

	for _, moduleName := range util.Modules {
		dataCh := make(chan types.RawSnapshotNode, 10_000)

		hashCalculator := util.NewXorHashCalculator(blocksPerWorker, numOfWorkers, dataCh)

		go func(mod string) {
			defer close(dataCh)

			_, err := db.RawIterate(mod, func(key, value []byte, ver int64) bool {
				// Only feed data whose version is in [beginBlock..endBlock]
				if ver >= beginBlock && ver <= endBlock {
					dataCh <- types.RawSnapshotNode{
						StoreKey: mod,
						Key:      key,
						Value:    value,
						Version:  ver - beginBlock,
					}
				}
				return false
			})
			if err != nil {
				panic(fmt.Errorf("error scanning module %s: %w", mod, err))
			}
		}(moduleName)

		allHashes := hashCalculator.ComputeHashes()
		if len(allHashes) == 0 {
			continue
		}

		finalHash := allHashes[len(allHashes)-1]

		if err := db.WriteBlockRangeHash(moduleName, beginBlock, endBlock, finalHash); err != nil {
			return fmt.Errorf(
				"failed to write block-range hash for module %q in [%d..%d]: %w",
				moduleName, beginBlock, endBlock, err,
			)
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

// Prune attempts to prune all versions up to and including the current version
// Get the range of keys, manually iterate over them and delete them
// We add a heuristic to skip over a module's keys during pruning if it hasn't been updated
// since the last time pruning occurred.
// NOTE: There is a rare case when a module's keys are skipped during pruning even though
// it has been updated. This occurs when that module's keys are updated in between pruning runs, the node after is restarted.
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

	return db.SetEarliestVersion(earliestVersion, false)
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

func (db *Database) RawImport(ch <-chan types.RawSnapshotNode) error {
	var wg sync.WaitGroup

	worker := func() {
		defer wg.Done()
		batch, err := NewRawBatch(db.storage)
		if err != nil {
			panic(err)
		}

		var counter int
		var latestKey []byte // store the latest key from the batch
		var latestModule string
		for entry := range ch {
			err := batch.Set(entry.StoreKey, entry.Key, entry.Value, entry.Version)
			if err != nil {
				panic(err)
			}

			latestKey = entry.Key // track the latest key
			latestModule = entry.StoreKey
			counter++

			if counter%ImportCommitBatchSize == 0 {
				startTime := time.Now()

				// Commit the batch and record the latest key as metadata
				if err := batch.Write(); err != nil {
					panic(err)
				}

				// Persist the latest key in the metadata
				if err := db.SetLatestMigratedKey(latestKey); err != nil {
					panic(err)
				}

				if err := db.SetLatestMigratedModule(latestModule); err != nil {
					panic(err)
				}

				if counter%1000000 == 0 {
					fmt.Printf("Time taken to write batch counter %d: %v\n", counter, time.Since(startTime))
					metrics.IncrCounterWithLabels([]string{"sei", "migration", "nodes_imported"}, float32(1000000), []metrics.Label{
						{Name: "module", Value: latestModule},
					})
				}

				batch, err = NewRawBatch(db.storage)
				if err != nil {
					panic(err)
				}
			}
		}

		// Final batch write
		if batch.Size() > 0 {
			if err := batch.Write(); err != nil {
				panic(err)
			}

			// Persist the final latest key
			if err := db.SetLatestMigratedKey(latestKey); err != nil {
				panic(err)
			}

			if err := db.SetLatestMigratedModule(latestModule); err != nil {
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
	prefix := storePrefix(storeKey)

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
		if storeKey != "" && !bytes.HasPrefix(currKey, prefix) {
			break
		}

		// Parse prefix out of the key
		parsedKey := currKey[len(prefix):]

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
		if fn(parsedKey, valBz, currVersionDecoded) {
			return true, nil
		}

	}

	return false, nil
}

func (db *Database) DeleteKeysAtVersion(module string, version int64) error {

	batch, err := NewBatch(db.storage, version)
	if err != nil {
		return fmt.Errorf("failed to create deletion batch for module %q: %w", module, err)
	}

	deleteCounter := 0

	_, err = db.RawIterate(module, func(key, value []byte, ver int64) bool {
		if ver == version {
			if err := batch.HardDelete(module, key); err != nil {
				fmt.Printf("Error physically deleting key %q in module %q: %v\n", key, module, err)
				return true // stop iteration on error
			}
			deleteCounter++
			if deleteCounter >= DeleteCommitBatchSize {
				if err := batch.Write(); err != nil {
					fmt.Printf("Error writing deletion batch for module %q: %v\n", module, err)
					return true
				}
				deleteCounter = 0
				batch, err = NewBatch(db.storage, version)
				if err != nil {
					fmt.Printf("Error creating a new deletion batch for module %q: %v\n", module, err)
					return true
				}
			}
		}
		return false
	})
	if err != nil {
		return fmt.Errorf("error iterating module %q for deletion: %w", module, err)
	}

	// Commit any remaining deletions.
	if batch.Size() > 0 {
		if err := batch.Write(); err != nil {
			return fmt.Errorf("error writing final deletion batch for module %q: %w", module, err)
		}
	}
	return nil
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

// WriteBlockRangeHash writes a hash for a range of blocks to the database
func (db *Database) WriteBlockRangeHash(storeKey string, beginBlockRange, endBlockRange int64, hash []byte) error {
	key := []byte(fmt.Sprintf(HashTpl, storeKey, beginBlockRange, endBlockRange))
	err := db.storage.Set(key, hash, defaultWriteOpts)
	if err != nil {
		return fmt.Errorf("failed to write block range hash: %w", err)
	}
	return nil
}

func retrieveLastRangeHashed(db *pebble.DB) (int64, error) {
	bz, closer, err := db.Get([]byte(lastRangeHashKey))
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			// means we haven't hashed anything yet
			return 0, nil
		}
		return 0, err
	}
	defer closer.Close()

	if len(bz) == 0 {
		return 0, nil
	}
	return int64(binary.LittleEndian.Uint64(bz)), nil
}
