package mvcc

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/cockroachdb/pebble/v2/bloom"
	"github.com/cockroachdb/pebble/v2/sstable"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"golang.org/x/exp/slices"

	errorutils "github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/wal"
)

const (
	VersionSize = 8

	PrefixStore          = "s/k:"
	LenPrefixStore       = 4
	StorePrefixTpl       = "s/k:%s/" // s/k:<storeKey>
	LatestStorePrefixTpl = "s/l:%s/" // s/l:<storeKey>
	latestVersionKey     = "s/_latest"
	earliestVersionKey   = "s/_earliest"
	tombstoneVal         = "TOMBSTONE"

	// TODO: Make configurable
	ImportCommitBatchSize = 10000
	PruneCommitBatchSize  = 50
	DeleteCommitBatchSize = 50
	MinWALEntriesToKeep   = 1000
)

var (
	_ types.StateStore          = (*Database)(nil)
	_ types.TraceableStateStore = (*Database)(nil)

	defaultWriteOpts = pebble.NoSync
)

type tracedDatabase struct {
	*Database
	collector   types.ReadTraceCollector
	readSession *historicalReadSession
}

type readTraceCloserRegistry interface {
	AddReadTraceCloser(io.Closer)
}

type historicalReadSession struct {
	snapshot        *pebble.Snapshot
	iterators       map[string]*pebble.Iterator
	cache           map[historicalReadCacheKey]historicalReadCacheValue
	keepLastVersion bool
	mu              sync.Mutex
	closed          bool
}

type historicalReadCacheKey struct {
	storeKey string
	version  int64
	key      string
}

type historicalReadCacheValue struct {
	value []byte
	found bool
}

type Database struct {
	storage      *pebble.DB
	asyncWriteWG sync.WaitGroup
	config       config.StateStoreConfig
	// Earliest version for db after pruning
	earliestVersion atomic.Int64
	// Latest version for db
	latestVersion atomic.Int64

	// Map of module to when each was last updated
	// Used in pruning to skip over stores that have not been updated recently
	storeKeyDirty sync.Map

	// Changelog used to support async write
	streamHandler wal.ChangelogWAL

	// Pending changes to be written to the DB
	pendingChanges chan VersionedChangesets

	// Cancel function for background metrics collection
	metricsCancel context.CancelFunc
}

type VersionedChangesets struct {
	Version    int64
	Changesets []*proto.NamedChangeSet
	Done       chan struct{} // non-nil for barrier: closed when this entry is processed
}

func OpenDB(dataDir string, config config.StateStoreConfig) (types.StateStore, error) {
	cache := pebble.NewCache(1024 * 1024 * 32)
	defer cache.Unref()

	// Select comparer based on config. Note: UseDefaultComparer is NOT backwards compatible
	// with existing databases created with MVCCComparer - Pebble will refuse to open due to
	// comparer name mismatch. Only use UseDefaultComparer for NEW databases.
	var comparer *pebble.Comparer
	if config.UseDefaultComparer {
		comparer = pebble.DefaultComparer
	} else {
		// TODO: Delete once we remove support
		comparer = MVCCComparer
	}

	opts := &pebble.Options{
		Cache:    cache,
		Comparer: comparer,
		// FormatMajorVersion is pinned to a specific version to prevent accidental
		// breaking changes when updating the pebble dependency. Using FormatNewest
		// would cause the on-disk format to silently upgrade when pebble is updated,
		// making the database incompatible with older software versions.
		// When upgrading this version, ensure it's an intentional, documented change.
		FormatMajorVersion:          pebble.FormatVirtualSSTables,
		L0CompactionThreshold:       2,
		L0StopWritesThreshold:       1000,
		LBaseMaxBytes:               64 << 20, // 64 MB
		MemTableSize:                64 << 20,
		MemTableStopWritesThreshold: 4,
	}

	// Configure L0 with explicit settings
	opts.Levels[0].BlockSize = 32 << 10       // 32 KB
	opts.Levels[0].IndexBlockSize = 256 << 10 // 256 KB
	opts.Levels[0].FilterPolicy = bloom.FilterPolicy(10)
	opts.Levels[0].FilterType = pebble.TableFilter
	opts.Levels[0].Compression = func() *sstable.CompressionProfile { return sstable.ZstdCompression }
	opts.Levels[0].EnsureL0Defaults()

	// Configure L1+ levels, inheriting from previous level
	for i := 1; i < len(opts.Levels); i++ {
		l := &opts.Levels[i]
		l.BlockSize = 32 << 10       // 32 KB
		l.IndexBlockSize = 256 << 10 // 256 KB
		l.FilterPolicy = bloom.FilterPolicy(10)
		l.FilterType = pebble.TableFilter
		l.Compression = func() *sstable.CompressionProfile { return sstable.ZstdCompression }
		l.EnsureL1PlusDefaults(&opts.Levels[i-1])
	}

	// Disable bloom filter at bottommost level (L6) - bloom filters are less useful
	// at the bottom level since most data lives there and false positive rate is low
	opts.Levels[6].FilterPolicy = nil

	//TODO: add a new config and check if readonly = true to support readonly mode

	db, err := pebble.Open(dataDir, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to open PebbleDB: %w", err)
	}

	// Initialize earliest version
	earliestVersion, err := retrieveEarliestVersion(db)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to retrieve earliest version: %w", err)
	}

	// Initialize latest version
	latestVersion, err := retrieveLatestVersion(db)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to retrieve latest version: %w", err)
	}

	database := &Database{
		storage:         db,
		asyncWriteWG:    sync.WaitGroup{},
		config:          config,
		earliestVersion: atomic.Int64{},
		latestVersion:   atomic.Int64{},
		pendingChanges:  make(chan VersionedChangesets, config.AsyncWriteBuffer),
	}
	database.latestVersion.Store(latestVersion)
	database.earliestVersion.Store(earliestVersion)

	if config.KeepRecent < 0 {
		_ = db.Close()
		return nil, errors.New("KeepRecent must be non-negative")
	}
	walKeepRecent := math.Max(MinWALEntriesToKeep, float64(config.AsyncWriteBuffer+1))
	streamHandler, err := wal.NewChangelogWAL(utils.GetChangelogPath(dataDir), wal.Config{
		KeepRecent:    uint64(walKeepRecent),
		PruneInterval: time.Duration(config.PruneIntervalSeconds) * time.Second,
	})
	if err != nil {
		return nil, err
	}
	database.streamHandler = streamHandler
	database.asyncWriteWG.Add(1)
	go database.writeAsyncInBackground()

	// Start background metrics collection
	metricsCtx, metricsCancel := context.WithCancel(context.Background())
	database.metricsCancel = metricsCancel
	go database.collectMetricsInBackground(metricsCtx)

	return database, nil
}

func (db *Database) Close() error {
	// Stop background metrics collection
	if db.metricsCancel != nil {
		db.metricsCancel()
	}

	if db.streamHandler != nil {
		// First, stop accepting new pending changes and drain the worker
		close(db.pendingChanges)
		// Wait for the async writes to finish
		db.asyncWriteWG.Wait()
		// Now close the WAL stream
		_ = db.streamHandler.Close()
		db.streamHandler = nil
	}
	// Make Close idempotent: Pebble panics if Close is called twice.
	if db.storage == nil {
		return nil
	}
	err := db.storage.Close()
	db.storage = nil
	return err
}

// PebbleMetrics returns the underlying Pebble DB metrics for observability (e.g. compaction/flush counts).
// Returns nil if the database is closed.
func (db *Database) PebbleMetrics() *pebble.Metrics {
	if db.storage == nil {
		return nil
	}
	return db.storage.Metrics()
}

func (db *Database) SetLatestVersion(version int64) error {
	if version < 0 {
		return fmt.Errorf("version must be non-negative")
	}
	db.latestVersion.Store(version)
	var ts [VersionSize]byte
	binary.LittleEndian.PutUint64(ts[:], uint64(version))
	err := db.storage.Set([]byte(latestVersionKey), ts[:], defaultWriteOpts)
	return err
}

func (db *Database) GetLatestVersion() int64 {
	return db.latestVersion.Load()
}

// Retrieve latestVersion from db, if not found, return 0.
func retrieveLatestVersion(db *pebble.DB) (int64, error) {
	bz, closer, err := db.Get([]byte(latestVersionKey))
	defer func() {
		if closer != nil {
			_ = closer.Close()
		}
	}()
	if err != nil || len(bz) == 0 {
		if errors.Is(err, pebble.ErrNotFound) {
			return 0, nil
		}
		return 0, err
	}

	uz := binary.LittleEndian.Uint64(bz)
	if uz > math.MaxInt64 {
		return 0, fmt.Errorf("latest version in database overflows int64: %d", uz)
	}
	return int64(uz), nil
}

func (db *Database) SetEarliestVersion(version int64, ignoreVersion bool) error {
	if version < 0 {
		return fmt.Errorf("version must be non-negative")
	}
	earliestVersion := db.earliestVersion.Load()
	if version > earliestVersion || ignoreVersion {
		swapped := db.earliestVersion.CompareAndSwap(earliestVersion, version)
		if swapped {
			var ts [VersionSize]byte
			binary.LittleEndian.PutUint64(ts[:], uint64(version))
			return db.storage.Set([]byte(earliestVersionKey), ts[:], defaultWriteOpts)
		} else {
			return fmt.Errorf("failed to set earliest version to: %d", version)
		}
	}
	return nil
}

func (db *Database) GetEarliestVersion() int64 {
	return db.earliestVersion.Load()
}

// Retrieves earliest version from db, if not found, return 0
func retrieveEarliestVersion(db *pebble.DB) (int64, error) {
	bz, closer, err := db.Get([]byte(earliestVersionKey))
	defer func() {
		if closer != nil {
			_ = closer.Close()
		}
	}()
	if err != nil || len(bz) == 0 {
		if errors.Is(err, pebble.ErrNotFound) {
			return 0, nil
		}
		return 0, err
	}

	ubz := binary.LittleEndian.Uint64(bz)
	if ubz > math.MaxInt64 {
		return 0, fmt.Errorf("earliest version in database overflows int64: %d", ubz)
	}
	return int64(ubz), nil
}

func (db *Database) Has(storeKey string, version int64, key []byte) (bool, error) {
	return db.hasWithCollector(storeKey, version, key, nil)
}

func (db *Database) hasWithCollector(storeKey string, version int64, key []byte, collector types.ReadTraceCollector) (bool, error) {
	start := time.Now()
	defer recordReadTrace(collector, types.ReadTraceEvent{
		StoreKey:      storeKey,
		Layer:         "mvcc",
		Operation:     "has",
		DurationNanos: time.Since(start).Nanoseconds(),
		Key:           slices.Clone(key),
	})
	if version < db.GetEarliestVersion() {
		return false, nil
	}

	val, err := db.getWithCollector(storeKey, version, key, collector)
	if err != nil {
		return false, err
	}

	return val != nil, nil
}

func (db *Database) Get(storeKey string, targetVersion int64, key []byte) (_ []byte, _err error) {
	return db.getWithCollector(storeKey, targetVersion, key, nil)
}

func (db *Database) getWithCollector(storeKey string, targetVersion int64, key []byte, collector types.ReadTraceCollector) (_ []byte, _err error) {
	startTime := time.Now()
	defer func() {
		otelMetrics.getLatency.Record(
			context.Background(),
			time.Since(startTime).Seconds(),
			metric.WithAttributes(
				attribute.Bool("success", _err == nil),
				attribute.String("store", storeKey),
			),
		)
		recordReadTrace(collector, types.ReadTraceEvent{
			StoreKey:      storeKey,
			Layer:         "mvcc",
			Operation:     "get",
			DurationNanos: time.Since(startTime).Nanoseconds(),
			Key:           slices.Clone(key),
		})
	}()
	if targetVersion < db.GetEarliestVersion() {
		return nil, nil
	}

	if value, found, err := getLatestIndexedValue(db.storage, storeKey, key, targetVersion, db.GetEarliestVersion(), db.config.KeepLastVersion, collector); err != nil {
		return nil, err
	} else if found {
		return value, nil
	}

	prefixedVal, err := getMVCCSlice(db.storage, storeKey, key, targetVersion, collector)
	if err != nil {
		if errors.Is(err, errorutils.ErrRecordNotFound) {
			return nil, nil
		}

		return nil, fmt.Errorf("failed to perform PebbleDB read: %w", err)
	}

	return visibleValueAtVersion(prefixedVal, targetVersion)
}

func (db *Database) ApplyChangesetSync(version int64, changeset []*proto.NamedChangeSet) (_err error) {
	startTime := time.Now()
	defer func() {
		otelMetrics.applyChangesetLatency.Record(
			context.Background(),
			time.Since(startTime).Seconds(),
			metric.WithAttributes(attribute.Bool("success", _err == nil)),
		)
	}()
	// Check if version is 0 and change it to 1
	// We do this specifically since keys written as part of genesis state come in as version 0
	// But pebbledb treats version 0 as special, so apply the changeset at version 1 instead
	if version == 0 {
		version = 1
	}

	// Create batch and persist latest version in the batch
	b, err := NewBatch(db.storage, version)
	if err != nil {
		return err
	}

	for _, cs := range changeset {
		for _, kvPair := range cs.Changeset.Pairs {
			if kvPair.Value == nil {
				if err := b.Delete(cs.Name, kvPair.Key); err != nil {
					return err
				}
			} else if err := b.Set(cs.Name, kvPair.Key, kvPair.Value); err != nil {
				return err
			}
		}
		// Mark the store as updated
		db.storeKeyDirty.Store(cs.Name, version)
	}

	if err := b.Write(); err != nil {
		return err
	}
	// Update latest version after all writes succeed (only if higher to avoid lowering it when writing out of order)
	if version > db.latestVersion.Load() {
		db.latestVersion.Store(version)
	}
	return nil
}

func (db *Database) ApplyChangesetAsync(version int64, changesets []*proto.NamedChangeSet) (_err error) {
	startTime := time.Now()
	defer func() {
		otelMetrics.applyChangesetAsyncLatency.Record(
			context.Background(),
			time.Since(startTime).Seconds(),
			metric.WithAttributes(attribute.Bool("success", _err == nil)),
		)
		// Record pending queue depth
		otelMetrics.pendingChangesQueueDepth.Record(
			context.Background(),
			int64(len(db.pendingChanges)),
		)
	}()
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
	// Add to pending changes first
	db.pendingChanges <- VersionedChangesets{
		Version:    version,
		Changesets: changesets,
	}
	return nil
}

func (db *Database) writeAsyncInBackground() {
	defer db.asyncWriteWG.Done()
	for nextChange := range db.pendingChanges {
		if nextChange.Done != nil {
			close(nextChange.Done)
			continue
		}
		version := nextChange.Version
		if err := db.ApplyChangesetSync(version, nextChange.Changesets); err != nil {
			panic(err)
		}
	}
}

// WaitForPendingWrites waits for all pending writes to be processed
func (db *Database) WaitForPendingWrites() {
	done := make(chan struct{})
	db.pendingChanges <- VersionedChangesets{Done: done}
	<-done
}

// Prune attempts to prune all versions up to and including the current version
// Get the range of keys, manually iterate over them and delete them
// We add a heuristic to skip over a module's keys during pruning if it hasn't been updated
// since the last time pruning occurred.
// NOTE: There is a rare case when a module's keys are skipped during pruning even though
// it has been updated. This occurs when that module's keys are updated in between pruning runs, the node after is restarted.
// This is not a large issue given the next time that module is updated, it will be properly pruned thereafter.
func (db *Database) Prune(version int64) (_err error) {
	// Defensive check: ensure database is not closed
	if db.storage == nil {
		return errors.New("pebbledb: database is closed")
	}

	startTime := time.Now()
	defer func() {
		otelMetrics.pruneLatency.Record(
			context.Background(),
			time.Since(startTime).Seconds(),
			metric.WithAttributes(
				attribute.Bool("success", _err == nil),
			),
		)
	}()

	earliestVersion := version + 1 // we increment by 1 to include the provided version

	itr, err := db.storage.NewIter(nil)
	if err != nil {
		return err
	}
	defer func() { _ = itr.Close() }()

	batch := db.storage.NewBatch()
	defer func() { _ = batch.Close() }()

	var (
		counter            int
		prevKey            []byte
		prevVersionDecoded int64
		prevStore          string
	)

	for itr.First(); itr.Valid(); {
		currKeyEncoded := slices.Clone(itr.Key())

		// Ignore metadata entries during pruning
		if isMetadataKey(currKeyEncoded) {
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
			if !ok || (typeOk && versionUpdated < db.GetEarliestVersion()) {
				itr.SeekGE(storePrefix(storeKey + "0"))
				continue
			}
		}

		currVersionDecoded, err := decodeUint64Descending(currVersion)
		if err != nil {
			return err
		}

		// Seek to next key if we are at a version which is higher than prune height
		// Do not seek to next key if KeepLastVersion is false and we need to delete the previous key in pruning
		if currVersionDecoded > version && (db.config.KeepLastVersion || prevVersionDecoded > version) {
			itr.NextPrefix()
			continue
		}

		// With descending MVCC ordering, the first version seen for a logical key is
		// the newest one. Any later version for the same key is older and can be
		// pruned once it falls below the prune height. If KeepLastVersion is false,
		// even the first/only version at or below the prune height can be deleted.
		if currVersionDecoded <= version && (bytes.Equal(prevKey, currKey) || !db.config.KeepLastVersion) {
			err = batch.Delete(currKeyEncoded, nil)
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
	return db.iteratorWithCollector(storeKey, version, start, end, nil)
}

func (db *Database) iteratorWithCollector(storeKey string, version int64, start, end []byte, collector types.ReadTraceCollector) (types.DBIterator, error) {
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
	} else {
		upperBound = iteratorUpperBoundForStore(storeKey)
	}

	iterStart := time.Now()
	itr, err := db.storage.NewIter(&pebble.IterOptions{LowerBound: lowerBound, UpperBound: upperBound})
	if err != nil {
		return nil, fmt.Errorf("failed to create PebbleDB iterator: %w", err)
	}
	recordReadTrace(collector, types.ReadTraceEvent{
		StoreKey:      storeKey,
		Layer:         "pebble",
		Operation:     "newIter",
		DurationNanos: time.Since(iterStart).Nanoseconds(),
		Start:         slices.Clone(lowerBound),
		End:           slices.Clone(upperBound),
	})

	return newPebbleDBIterator(itr, storePrefix(storeKey), start, end, version, db.GetEarliestVersion(), false, storeKey, collector), nil
}

// Taken from pebbledb prefix upper bound
// Returns smallest key strictly greater than the prefix
func prefixEnd(b []byte) []byte {
	end := make([]byte, len(b))
	copy(end, b)
	for i := len(end) - 1; i >= 0; i-- {
		if end[i] != 0xFF {
			end[i]++
			return end[:i+1]
		}
	}
	return nil
}

func (db *Database) ReverseIterator(storeKey string, version int64, start, end []byte) (types.DBIterator, error) {
	return db.reverseIteratorWithCollector(storeKey, version, start, end, nil)
}

func (db *Database) reverseIteratorWithCollector(storeKey string, version int64, start, end []byte, collector types.ReadTraceCollector) (types.DBIterator, error) {
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
	} else {
		upperBound = MVCCEncode(prefixEnd(storePrefix(storeKey)), 0)
	}

	iterStart := time.Now()
	itr, err := db.storage.NewIter(&pebble.IterOptions{LowerBound: lowerBound, UpperBound: upperBound})
	if err != nil {
		return nil, fmt.Errorf("failed to create PebbleDB iterator: %w", err)
	}
	recordReadTrace(collector, types.ReadTraceEvent{
		StoreKey:      storeKey,
		Layer:         "pebble",
		Operation:     "newIter",
		DurationNanos: time.Since(iterStart).Nanoseconds(),
		Start:         slices.Clone(lowerBound),
		End:           slices.Clone(upperBound),
		Reverse:       true,
	})

	return newPebbleDBIterator(itr, storePrefix(storeKey), start, end, version, db.GetEarliestVersion(), true, storeKey, collector), nil
}

// Import loads the initial version of the state in parallel with numWorkers goroutines
// TODO: Potentially add retries instead of panics
func (db *Database) Import(version int64, ch <-chan types.SnapshotNode) (_err error) {
	startTime := time.Now()
	defer func() {
		otelMetrics.importLatency.Record(
			context.Background(),
			time.Since(startTime).Seconds(),
			metric.WithAttributes(
				attribute.Bool("success", _err == nil),
			),
		)
	}()

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
	prefix := storePrefix(storeKey)

	itr, err := db.storage.NewIter(&pebble.IterOptions{LowerBound: lowerBound})
	if err != nil {
		return false, fmt.Errorf("failed to create PebbleDB iterator: %w", err)
	}
	defer func() { _ = itr.Close() }()

	for itr.First(); itr.Valid(); itr.Next() {
		currKeyEncoded := itr.Key()

		// Ignore metadata entries
		if isMetadataKey(currKeyEncoded) {
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

		currVersionDecoded, err := decodeUint64Descending(currVersion)
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

func isMetadataKey(key []byte) bool {
	return bytes.HasPrefix(key, []byte("s/_")) || bytes.HasPrefix(key, []byte("s/l:"))
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

func getMVCCSlice(db *pebble.DB, storeKey string, key []byte, version int64, collector types.ReadTraceCollector) ([]byte, error) {
	totalStart := time.Now()
	defer func() {
		recordReadTrace(collector, types.ReadTraceEvent{
			StoreKey:      storeKey,
			Layer:         "mvcc",
			Operation:     "getMVCCSlice",
			DurationNanos: time.Since(totalStart).Nanoseconds(),
			Key:           slices.Clone(key),
		})
	}()
	prefixedKey := prependStoreKey(storeKey, key)
	seekKey := MVCCEncode(prefixedKey, version)
	lowerBound := seekKey
	upperBound := iteratorUpperBoundForLogicalKey(prefixedKey)
	iterStart := time.Now()
	itr, err := db.NewIter(&pebble.IterOptions{
		LowerBound: lowerBound,
		UpperBound: upperBound,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create PebbleDB iterator: %w", err)
	}
	recordReadTrace(collector, types.ReadTraceEvent{
		StoreKey:      storeKey,
		Layer:         "pebble",
		Operation:     "newIter",
		DurationNanos: time.Since(iterStart).Nanoseconds(),
		Start:         slices.Clone(lowerBound),
		End:           slices.Clone(upperBound),
		Reverse:       true,
	})
	defer func() {
		closeStart := time.Now()
		err = errorutils.Join(err, itr.Close())
		recordReadTrace(collector, types.ReadTraceEvent{
			StoreKey:      storeKey,
			Layer:         "pebble",
			Operation:     "iterClose",
			DurationNanos: time.Since(closeStart).Nanoseconds(),
			Key:           slices.Clone(key),
			Reverse:       true,
		})
	}()

	firstStart := time.Now()
	firstOK := itr.First()
	recordReadTrace(collector, types.ReadTraceEvent{
		StoreKey:      storeKey,
		Layer:         "pebble",
		Operation:     "first",
		DurationNanos: time.Since(firstStart).Nanoseconds(),
		Key:           slices.Clone(key),
	})
	if !firstOK {
		return nil, errorutils.ErrRecordNotFound
	}

	keyReadStart := time.Now()
	rawIterKey := slices.Clone(itr.Key())
	recordReadTrace(collector, types.ReadTraceEvent{
		StoreKey:      storeKey,
		Layer:         "pebble",
		Operation:     "iterKey",
		DurationNanos: time.Since(keyReadStart).Nanoseconds(),
		Key:           rawIterKey,
		Reverse:       true,
	})

	splitKeyStart := time.Now()
	userKey, vBz, ok := SplitMVCCKey(rawIterKey)
	recordReadTrace(collector, types.ReadTraceEvent{
		StoreKey:      storeKey,
		Layer:         "mvcc",
		Operation:     "splitKey",
		DurationNanos: time.Since(splitKeyStart).Nanoseconds(),
		Key:           rawIterKey,
	})
	if !ok {
		return nil, fmt.Errorf("invalid PebbleDB MVCC key: %s", rawIterKey)
	}
	if !bytes.Equal(userKey, prefixedKey) {
		return nil, errorutils.ErrRecordNotFound
	}

	decodeVersionStart := time.Now()
	keyVersion, err := decodeUint64Descending(vBz)
	recordReadTrace(collector, types.ReadTraceEvent{
		StoreKey:      storeKey,
		Layer:         "mvcc",
		Operation:     "decodeKeyVersion",
		DurationNanos: time.Since(decodeVersionStart).Nanoseconds(),
		Key:           rawIterKey,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to decode key version: %w", err)
	}
	if keyVersion > version {
		return nil, fmt.Errorf("key version too large: %d", keyVersion)
	}

	valueReadStart := time.Now()
	rawIterValue := itr.Value()
	recordReadTrace(collector, types.ReadTraceEvent{
		StoreKey:      storeKey,
		Layer:         "pebble",
		Operation:     "iterValue",
		DurationNanos: time.Since(valueReadStart).Nanoseconds(),
		Key:           rawIterKey,
		Reverse:       true,
	})

	valueCloneStart := time.Now()
	clonedValue := slices.Clone(rawIterValue)
	recordReadTrace(collector, types.ReadTraceEvent{
		StoreKey:      storeKey,
		Layer:         "mvcc",
		Operation:     "cloneValue",
		DurationNanos: time.Since(valueCloneStart).Nanoseconds(),
		Key:           rawIterKey,
	})

	return clonedValue, nil
}

func getMVCCSliceWithSession(session *historicalReadSession, storeKey string, key []byte, version int64, collector types.ReadTraceCollector) ([]byte, error) {
	totalStart := time.Now()
	defer func() {
		recordReadTrace(collector, types.ReadTraceEvent{
			StoreKey:      storeKey,
			Layer:         "mvcc",
			Operation:     "getMVCCSlice",
			DurationNanos: time.Since(totalStart).Nanoseconds(),
			Key:           slices.Clone(key),
		})
	}()

	prefixedKey := prependStoreKey(storeKey, key)
	seekKey := MVCCEncode(prefixedKey, version)

	itr, created, iterDuration, err := session.getOrCreateIterator(storeKey)
	if err != nil {
		return nil, err
	}
	if created {
		recordReadTrace(collector, types.ReadTraceEvent{
			StoreKey:      storeKey,
			Layer:         "pebble",
			Operation:     "newIter",
			DurationNanos: iterDuration.Nanoseconds(),
			Start:         slices.Clone(MVCCEncode(prependStoreKey(storeKey, nil), 0)),
			End:           slices.Clone(iteratorUpperBoundForStore(storeKey)),
		})
	}

	seekStart := time.Now()
	session.mu.Lock()
	ok := itr.SeekGE(seekKey)
	var (
		rawIterKey   []byte
		rawIterValue []byte
	)
	if ok {
		rawIterKey = slices.Clone(itr.Key())
		rawIterValue = slices.Clone(itr.Value())
	}
	iterErr := itr.Error()
	session.mu.Unlock()
	recordReadTrace(collector, types.ReadTraceEvent{
		StoreKey:      storeKey,
		Layer:         "pebble",
		Operation:     "seekGE",
		DurationNanos: time.Since(seekStart).Nanoseconds(),
		Key:           slices.Clone(seekKey),
	})
	if iterErr != nil {
		return nil, iterErr
	}
	if !ok {
		return nil, errorutils.ErrRecordNotFound
	}

	keyReadStart := time.Now()
	recordReadTrace(collector, types.ReadTraceEvent{
		StoreKey:      storeKey,
		Layer:         "pebble",
		Operation:     "iterKey",
		DurationNanos: time.Since(keyReadStart).Nanoseconds(),
		Key:           rawIterKey,
		Reverse:       true,
	})

	splitKeyStart := time.Now()
	userKey, vBz, ok := SplitMVCCKey(rawIterKey)
	recordReadTrace(collector, types.ReadTraceEvent{
		StoreKey:      storeKey,
		Layer:         "mvcc",
		Operation:     "splitKey",
		DurationNanos: time.Since(splitKeyStart).Nanoseconds(),
		Key:           rawIterKey,
	})
	if !ok {
		return nil, fmt.Errorf("invalid PebbleDB MVCC key: %s", rawIterKey)
	}
	if !bytes.Equal(userKey, prefixedKey) {
		return nil, errorutils.ErrRecordNotFound
	}

	decodeVersionStart := time.Now()
	keyVersion, err := decodeUint64Descending(vBz)
	recordReadTrace(collector, types.ReadTraceEvent{
		StoreKey:      storeKey,
		Layer:         "mvcc",
		Operation:     "decodeKeyVersion",
		DurationNanos: time.Since(decodeVersionStart).Nanoseconds(),
		Key:           rawIterKey,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to decode key version: %w", err)
	}
	if keyVersion > version {
		return nil, fmt.Errorf("key version too large: %d", keyVersion)
	}

	valueReadStart := time.Now()
	recordReadTrace(collector, types.ReadTraceEvent{
		StoreKey:      storeKey,
		Layer:         "pebble",
		Operation:     "iterValue",
		DurationNanos: time.Since(valueReadStart).Nanoseconds(),
		Key:           rawIterKey,
		Reverse:       true,
	})

	valueCloneStart := time.Now()
	clonedValue := slices.Clone(rawIterValue)
	recordReadTrace(collector, types.ReadTraceEvent{
		StoreKey:      storeKey,
		Layer:         "mvcc",
		Operation:     "cloneValue",
		DurationNanos: time.Since(valueCloneStart).Nanoseconds(),
		Key:           rawIterKey,
	})

	return clonedValue, nil
}

func (db *Database) WithReadTraceCollector(collector types.ReadTraceCollector) types.StateStore {
	if collector == nil {
		return db
	}
	session := newHistoricalReadSession(db.storage)
	session.keepLastVersion = db.config.KeepLastVersion
	traced := &tracedDatabase{Database: db, collector: collector, readSession: session}
	if registry, ok := collector.(readTraceCloserRegistry); ok {
		registry.AddReadTraceCloser(session)
	}
	return traced
}

func (db *tracedDatabase) Get(storeKey string, version int64, key []byte) ([]byte, error) {
	return db.getWithSession(storeKey, version, key)
}

func (db *tracedDatabase) Has(storeKey string, version int64, key []byte) (bool, error) {
	start := time.Now()
	defer recordReadTrace(db.collector, types.ReadTraceEvent{
		StoreKey:      storeKey,
		Layer:         "mvcc",
		Operation:     "has",
		DurationNanos: time.Since(start).Nanoseconds(),
		Key:           slices.Clone(key),
	})
	val, err := db.getWithSession(storeKey, version, key)
	if err != nil {
		return false, err
	}
	return val != nil, nil
}

func (db *tracedDatabase) Iterator(storeKey string, version int64, start, end []byte) (types.DBIterator, error) {
	return db.Database.iteratorWithCollector(storeKey, version, start, end, db.collector)
}

func (db *tracedDatabase) ReverseIterator(storeKey string, version int64, start, end []byte) (types.DBIterator, error) {
	return db.Database.reverseIteratorWithCollector(storeKey, version, start, end, db.collector)
}

func recordReadTrace(collector types.ReadTraceCollector, event types.ReadTraceEvent) {
	if collector == nil {
		return
	}
	collector.RecordReadTrace(event)
}

func (db *tracedDatabase) getWithSession(storeKey string, targetVersion int64, key []byte) (_ []byte, _err error) {
	startTime := time.Now()
	defer func() {
		otelMetrics.getLatency.Record(
			context.Background(),
			time.Since(startTime).Seconds(),
			metric.WithAttributes(
				attribute.Bool("success", _err == nil),
				attribute.String("store", storeKey),
			),
		)
		recordReadTrace(db.collector, types.ReadTraceEvent{
			StoreKey:      storeKey,
			Layer:         "mvcc",
			Operation:     "get",
			DurationNanos: time.Since(startTime).Nanoseconds(),
			Key:           slices.Clone(key),
		})
	}()
	if targetVersion < db.GetEarliestVersion() {
		return nil, nil
	}

	if val, found, err := getLatestIndexedValueFromSession(db.readSession, storeKey, key, targetVersion, db.GetEarliestVersion(), db.collector); err != nil {
		return nil, err
	} else if found {
		db.readSession.store(storeKey, targetVersion, key, val)
		return val, nil
	}

	if val, found := db.readSession.lookup(storeKey, targetVersion, key); found {
		recordReadTrace(db.collector, types.ReadTraceEvent{
			StoreKey:      storeKey,
			Layer:         "mvcc",
			Operation:     "readCacheHit",
			DurationNanos: 0,
			Key:           slices.Clone(key),
		})
		return val, nil
	}
	recordReadTrace(db.collector, types.ReadTraceEvent{
		StoreKey:      storeKey,
		Layer:         "mvcc",
		Operation:     "readCacheMiss",
		DurationNanos: 0,
		Key:           slices.Clone(key),
	})

	prefixedVal, err := getMVCCSliceWithSession(db.readSession, storeKey, key, targetVersion, db.collector)
	if err != nil {
		if errors.Is(err, errorutils.ErrRecordNotFound) {
			db.readSession.store(storeKey, targetVersion, key, nil)
			return nil, nil
		}
		return nil, fmt.Errorf("failed to perform PebbleDB read: %w", err)
	}

	val, err := visibleValueAtVersion(prefixedVal, targetVersion)
	if err != nil {
		return nil, err
	}
	db.readSession.store(storeKey, targetVersion, key, val)
	return val, nil
}

func visibleValueAtVersion(prefixedVal []byte, targetVersion int64) ([]byte, error) {
	valBz, tombBz, ok := SplitMVCCKey(prefixedVal)
	if !ok {
		return nil, fmt.Errorf("invalid PebbleDB MVCC value: %s", prefixedVal)
	}
	if len(tombBz) == 0 {
		return valBz, nil
	}
	tombstone, err := decodeUint64Descending(tombBz)
	if err != nil {
		return nil, fmt.Errorf("failed to decode value tombstone: %w", err)
	}
	if targetVersion < tombstone {
		return valBz, nil
	}
	return nil, nil
}

func newHistoricalReadSession(db *pebble.DB) *historicalReadSession {
	session := &historicalReadSession{
		snapshot:        db.NewSnapshot(),
		iterators:       map[string]*pebble.Iterator{},
		cache:           map[historicalReadCacheKey]historicalReadCacheValue{},
		keepLastVersion: true,
	}
	return session
}

func (s *historicalReadSession) lookup(storeKey string, version int64, key []byte) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.cache[historicalReadCacheKey{storeKey: storeKey, version: version, key: string(key)}]
	if !ok {
		return nil, false
	}
	if !entry.found {
		return nil, true
	}
	return slices.Clone(entry.value), true
}

func (s *historicalReadSession) store(storeKey string, version int64, key []byte, value []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cacheValue := historicalReadCacheValue{found: value != nil}
	if value != nil {
		cacheValue.value = slices.Clone(value)
	}
	s.cache[historicalReadCacheKey{storeKey: storeKey, version: version, key: string(key)}] = cacheValue
}

func (s *historicalReadSession) getOrCreateIterator(storeKey string) (*pebble.Iterator, bool, time.Duration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if itr, ok := s.iterators[storeKey]; ok {
		return itr, false, 0, nil
	}
	start := time.Now()
	itr, err := s.snapshot.NewIter(&pebble.IterOptions{
		LowerBound: MVCCEncode(prependStoreKey(storeKey, nil), 0),
		UpperBound: iteratorUpperBoundForStore(storeKey),
	})
	if err != nil {
		return nil, false, 0, err
	}
	s.iterators[storeKey] = itr
	return itr, true, time.Since(start), nil
}

func (s *historicalReadSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	var lastErr error
	for _, itr := range s.iterators {
		if err := itr.Close(); err != nil {
			lastErr = err
		}
	}
	if s.snapshot != nil {
		if err := s.snapshot.Close(); err != nil {
			lastErr = err
		}
	}
	s.iterators = nil
	s.cache = nil
	s.snapshot = nil
	return lastErr
}

func iteratorUpperBoundForStore(storeKey string) []byte {
	upperStorePrefix := prefixEnd(storePrefix(storeKey))
	if upperStorePrefix == nil {
		return nil
	}
	return MVCCEncode(upperStorePrefix, 0)
}

func iteratorUpperBoundForLogicalKey(key []byte) []byte {
	upperKeyPrefix := prefixEnd(key)
	if upperKeyPrefix == nil {
		return nil
	}
	return MVCCEncode(upperKeyPrefix, 0)
}

func latestIndexPrefix(storeKey string) []byte {
	return []byte(fmt.Sprintf(LatestStorePrefixTpl, storeKey))
}

func latestIndexKey(storeKey string, key []byte) []byte {
	return append(latestIndexPrefix(storeKey), key...)
}

func encodeLatestIndexValue(version int64, prefixedVal []byte) []byte {
	var versionBz [VersionSize]byte
	binary.LittleEndian.PutUint64(versionBz[:], uint64(version))
	return append(versionBz[:], prefixedVal...)
}

func decodeLatestIndexValue(bz []byte) (int64, []byte, error) {
	if len(bz) < VersionSize {
		return 0, nil, fmt.Errorf("latest index entry too short: %d", len(bz))
	}
	version := binary.LittleEndian.Uint64(bz[:VersionSize])
	if version > math.MaxInt64 {
		return 0, nil, fmt.Errorf("latest index version overflows int64: %d", version)
	}
	return int64(version), bz[VersionSize:], nil
}

func getLatestIndexedValue(db *pebble.DB, storeKey string, key []byte, targetVersion int64, earliestVersion int64, keepLastVersion bool, collector types.ReadTraceCollector) ([]byte, bool, error) {
	start := time.Now()
	latestKey := latestIndexKey(storeKey, key)
	val, closer, err := db.Get(latestKey)
	recordReadTrace(collector, types.ReadTraceEvent{
		StoreKey:      storeKey,
		Layer:         "pebble",
		Operation:     "latestGet",
		DurationNanos: time.Since(start).Nanoseconds(),
		Key:           slices.Clone(latestKey),
	})
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			recordReadTrace(collector, types.ReadTraceEvent{
				StoreKey:      storeKey,
				Layer:         "mvcc",
				Operation:     "latestIndexMiss",
				DurationNanos: 0,
				Key:           slices.Clone(key),
			})
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("failed latest-index lookup: %w", err)
	}
	defer func() { _ = closer.Close() }()

	latestVersion, prefixedVal, err := decodeLatestIndexValue(utils.Clone(val))
	if err != nil {
		return nil, false, err
	}
	if latestVersion < earliestVersion && !keepLastVersion {
		recordReadTrace(collector, types.ReadTraceEvent{
			StoreKey:      storeKey,
			Layer:         "mvcc",
			Operation:     "latestIndexStale",
			DurationNanos: 0,
			Key:           slices.Clone(key),
		})
		return nil, false, nil
	}
	if latestVersion > targetVersion {
		recordReadTrace(collector, types.ReadTraceEvent{
			StoreKey:      storeKey,
			Layer:         "mvcc",
			Operation:     "latestIndexTooNew",
			DurationNanos: 0,
			Key:           slices.Clone(key),
		})
		return nil, false, nil
	}
	recordReadTrace(collector, types.ReadTraceEvent{
		StoreKey:      storeKey,
		Layer:         "mvcc",
		Operation:     "latestIndexHit",
		DurationNanos: 0,
		Key:           slices.Clone(key),
	})
	value, err := visibleValueAtVersion(prefixedVal, targetVersion)
	if err != nil {
		return nil, false, err
	}
	return value, true, nil
}

func getLatestIndexedValueFromSession(session *historicalReadSession, storeKey string, key []byte, targetVersion int64, earliestVersion int64, collector types.ReadTraceCollector) ([]byte, bool, error) {
	start := time.Now()
	latestKey := latestIndexKey(storeKey, key)
	val, closer, err := session.snapshot.Get(latestKey)
	recordReadTrace(collector, types.ReadTraceEvent{
		StoreKey:      storeKey,
		Layer:         "pebble",
		Operation:     "latestGet",
		DurationNanos: time.Since(start).Nanoseconds(),
		Key:           slices.Clone(latestKey),
	})
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			recordReadTrace(collector, types.ReadTraceEvent{
				StoreKey:      storeKey,
				Layer:         "mvcc",
				Operation:     "latestIndexMiss",
				DurationNanos: 0,
				Key:           slices.Clone(key),
			})
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("failed latest-index lookup: %w", err)
	}
	defer func() { _ = closer.Close() }()

	latestVersion, prefixedVal, err := decodeLatestIndexValue(utils.Clone(val))
	if err != nil {
		return nil, false, err
	}
	if latestVersion < earliestVersion && !session.keepLastVersion {
		recordReadTrace(collector, types.ReadTraceEvent{
			StoreKey:      storeKey,
			Layer:         "mvcc",
			Operation:     "latestIndexStale",
			DurationNanos: 0,
			Key:           slices.Clone(key),
		})
		return nil, false, nil
	}
	if latestVersion > targetVersion {
		recordReadTrace(collector, types.ReadTraceEvent{
			StoreKey:      storeKey,
			Layer:         "mvcc",
			Operation:     "latestIndexTooNew",
			DurationNanos: 0,
			Key:           slices.Clone(key),
		})
		return nil, false, nil
	}
	recordReadTrace(collector, types.ReadTraceEvent{
		StoreKey:      storeKey,
		Layer:         "mvcc",
		Operation:     "latestIndexHit",
		DurationNanos: 0,
		Key:           slices.Clone(key),
	})
	value, err := visibleValueAtVersion(prefixedVal, targetVersion)
	if err != nil {
		return nil, false, err
	}
	return value, true, nil
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

// collectMetricsInBackground periodically collects PebbleDB internal metrics
func (db *Database) collectMetricsInBackground(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second) // Collect metrics every 10 seconds
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			db.collectAndRecordMetrics(ctx)
		}
	}
}

// collectAndRecordMetrics collects PebbleDB internal metrics and records them
func (db *Database) collectAndRecordMetrics(ctx context.Context) {
	if db.storage == nil {
		return
	}

	m := db.storage.Metrics()

	// Compaction metrics - report raw counts
	otelMetrics.compactionCount.Add(ctx, m.Compact.Count)
	otelMetrics.compactionDuration.Record(ctx, m.Compact.Duration.Seconds())

	// Flush metrics - report raw counts
	otelMetrics.flushCount.Add(ctx, m.Flush.Count)
	otelMetrics.flushDuration.Record(ctx, m.Flush.WriteThroughput.WorkDuration.Seconds())
	otelMetrics.flushBytesWritten.Add(ctx, m.Flush.WriteThroughput.Bytes)

	// Storage metrics per level with level as attribute
	for level := 0; level < len(m.Levels); level++ {
		levelMetrics := m.Levels[level]
		levelAttr := attribute.Int("level", level)

		otelMetrics.sstableCount.Record(ctx, levelMetrics.TablesCount, metric.WithAttributes(levelAttr))
		otelMetrics.sstableTotalSize.Record(ctx, levelMetrics.TablesSize, metric.WithAttributes(levelAttr))
		otelMetrics.compactionBytesRead.Add(ctx, int64(levelMetrics.TableBytesIn), metric.WithAttributes(levelAttr))           //nolint:gosec
		otelMetrics.compactionBytesWritten.Add(ctx, int64(levelMetrics.TableBytesCompacted), metric.WithAttributes(levelAttr)) //nolint:gosec
	}

	// Memtable metrics
	otelMetrics.memtableCount.Record(ctx, m.MemTable.Count)
	otelMetrics.memtableTotalSize.Record(ctx, int64(m.MemTable.Size)) //nolint:gosec

	// WAL metrics
	otelMetrics.walSize.Record(ctx, int64(m.WAL.Size)) //nolint:gosec

	// Cache metrics - report raw counts
	otelMetrics.cacheHits.Add(ctx, m.BlockCache.Hits)
	otelMetrics.cacheMisses.Add(ctx, m.BlockCache.Misses)
	otelMetrics.cacheSize.Record(ctx, m.BlockCache.Size)
}
