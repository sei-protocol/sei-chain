package mvcc

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
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

	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/wal"
)

const (
	VersionSize = 8

	PrefixStore        = "s/k:"
	LenPrefixStore     = 4
	StorePrefixTpl     = "s/k:%s/" // s/k:<storeKey>
	latestVersionKey   = "s/_latest"
	earliestVersionKey = "s/_earliest"
	// descendingMVCCMarkerKey flags that the DB was initialized with the
	// descending-version MVCC encoding. Its absence on a populated DB means
	// the data was written by the legacy ascending-version build and is not
	// safe to read with this code.
	descendingMVCCMarkerKey = "s/_mvcc_descending"
	tombstoneVal            = "TOMBSTONE"

	// TODO: Make configurable
	ImportCommitBatchSize = 10000
	PruneCommitBatchSize  = 50
	DeleteCommitBatchSize = 50
	MinWALEntriesToKeep   = 1000
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
	earliestVersion atomic.Int64
	// Latest version for db
	latestVersion atomic.Int64
	// descending indicates whether this DB uses descending-version MVCC
	// encoding (fresh DBs created by this build) or the legacy
	// ascending-version encoding (DBs created by the previous build). The
	// mode is detected on open and is immutable for the lifetime of the
	// Database.
	descending bool

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

	descending, err := detectMVCCMode(db)
	if err != nil {
		_ = db.Close()
		return nil, err
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
		descending:      descending,
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

// mvccEncode encodes a key with the MVCC version encoding matching this
// Database's on-disk mode.
func (db *Database) mvccEncode(key []byte, version int64) []byte {
	if db.descending {
		return MVCCEncodeDescending(key, version)
	}
	return MVCCEncodeAscending(key, version)
}

// decodeVersion decodes an on-disk MVCC version using the encoding matching
// this Database's mode.
func (db *Database) decodeVersion(vBz []byte) (int64, error) {
	if db.descending {
		return decodeUint64Descending(vBz)
	}
	return decodeUint64Ascending(vBz)
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

// detectMVCCMode inspects the DB to determine which MVCC encoding to use.
//
//   - If the descendingMVCCMarkerKey sentinel is present, the DB was created
//     by this build and is in descending mode.
//   - If the marker is absent but latestVersionKey is present, the DB was
//     populated by the legacy ascending-version build. We open it in
//     ascending mode without writing the marker (legacy DBs stay unmarked
//     forever).
//   - If both markers are absent the DB is fresh; we write the descending
//     marker and return descending mode.
func detectMVCCMode(db *pebble.DB) (bool, error) {
	if _, closer, err := db.Get([]byte(descendingMVCCMarkerKey)); err == nil {
		_ = closer.Close()
		return true, nil
	} else if !errors.Is(err, pebble.ErrNotFound) {
		return false, fmt.Errorf("reading descending-MVCC marker: %w", err)
	}

	if _, closer, err := db.Get([]byte(latestVersionKey)); err == nil {
		_ = closer.Close()
		// Legacy DB: no marker, has data. Open in ascending mode.
		return false, nil
	} else if !errors.Is(err, pebble.ErrNotFound) {
		return false, fmt.Errorf("reading latest version marker: %w", err)
	}

	// Fresh DB: mark it and use descending mode.
	if err := db.Set([]byte(descendingMVCCMarkerKey), []byte{1}, defaultWriteOpts); err != nil {
		return false, fmt.Errorf("writing descending-MVCC marker: %w", err)
	}
	return true, nil
}

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

// Has dispatches between descending- and ascending-mode implementations
// depending on the on-disk encoding detected at open time.
func (db *Database) Has(storeKey string, version int64, key []byte) (bool, error) {
	if db.descending {
		return db.hasDescending(storeKey, version, key)
	}
	return db.hasAscending(storeKey, version, key)
}

// Get dispatches between descending- and ascending-mode implementations
// depending on the on-disk encoding detected at open time.
func (db *Database) Get(storeKey string, targetVersion int64, key []byte) ([]byte, error) {
	if db.descending {
		return db.getDescending(storeKey, targetVersion, key)
	}
	return db.getAscending(storeKey, targetVersion, key)
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
	b, err := NewBatch(db.storage, version, db.descending)
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

// Prune dispatches between descending- and ascending-mode implementations
// depending on the on-disk encoding detected at open time.
func (db *Database) Prune(version int64) error {
	if db.descending {
		return db.pruneDescending(version)
	}
	return db.pruneAscending(version)
}

// Iterator dispatches between descending- and ascending-mode implementations
// depending on the on-disk encoding detected at open time.
func (db *Database) Iterator(storeKey string, version int64, start, end []byte) (types.DBIterator, error) {
	if db.descending {
		return db.iteratorDescending(storeKey, version, start, end)
	}
	return db.iteratorAscending(storeKey, version, start, end)
}

// ReverseIterator dispatches between descending- and ascending-mode
// implementations depending on the on-disk encoding detected at open time.
func (db *Database) ReverseIterator(storeKey string, version int64, start, end []byte) (types.DBIterator, error) {
	if db.descending {
		return db.reverseIteratorDescending(storeKey, version, start, end)
	}
	return db.reverseIteratorAscending(storeKey, version, start, end)
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
		batch, err := NewBatch(db.storage, version, db.descending)
		if err != nil {
			panic(err)
		}

		var counter int
		for entry := range ch {
			if entry.StoreKey == "" || len(entry.Key) == 0 {
				continue
			}
			err := batch.Set(entry.StoreKey, entry.Key, entry.Value)
			if err != nil {
				panic(err)
			}

			counter++
			if counter%ImportCommitBatchSize == 0 {
				if err := batch.Write(); err != nil {
					panic(err)
				}

				batch, err = NewBatch(db.storage, version, db.descending)
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
	lowerBound := db.mvccEncode(prependStoreKey(storeKey, nil), 0)
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

		currVersionDecoded, err := db.decodeVersion(currVersion)
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

	batch, err := NewBatch(db.storage, version, db.descending)
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
				batch, err = NewBatch(db.storage, version, db.descending)
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
	return bytes.HasPrefix(key, []byte("s/_"))
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
