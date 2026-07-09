package mvcc

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"path/filepath"
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

	dbm "github.com/tendermint/tm-db"

	errorutils "github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	pebbledbmetrics "github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb"
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

	// maxConcurrentCompactions is the upper bound for the number of compactions
	// Pebble may run in parallel. Pebble's default range is {1,1}, but a single
	// compactor cannot keep up with the tombstone churn that pruning generates,
	// so deleted data accumulates and slows every subsequent prune scan. Allowing
	// Pebble to burst up to a few compactions clears that backlog.
	maxConcurrentCompactions = 4
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

	operationMetrics *pebbledbmetrics.OperationMetrics
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
		// Let Pebble run several compactions in parallel so it can keep up with
		// the tombstone churn produced by pruning. See maxConcurrentCompactions.
		CompactionConcurrencyRange: func() (int, int) { return 1, maxConcurrentCompactions },
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
		operationMetrics: pebbledbmetrics.NewOperationMetrics(
			config.EnableReadWriteMetrics,
			filepath.Base(dataDir),
		),
	}
	database.latestVersion.Store(latestVersion)
	database.earliestVersion.Store(earliestVersion)

	if config.KeepRecent < 0 {
		_ = db.Close()
		return nil, errors.New("KeepRecent must be non-negative")
	}
	walKeepRecent := math.Max(MinWALEntriesToKeep, float64(config.AsyncWriteBuffer+1))
	// Buffer WAL writes when the store itself applies changes asynchronously:
	// blocking the caller on WAL durability while the pebble apply is deferred
	// costs ~half the caller's write budget without a stronger guarantee than
	// the async apply already provides (SS is rebuildable from upstream state).
	streamHandler, err := wal.NewChangelogWAL(utils.GetChangelogPath(dataDir), wal.Config{
		KeepRecent:      uint64(walKeepRecent),
		PruneInterval:   time.Duration(config.PruneIntervalSeconds) * time.Second,
		WriteBufferSize: config.AsyncWriteBuffer,
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
	if err == nil {
		db.operationMetrics.AddWrite(1)
	}
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
	return retrieveVersionKey(db, latestVersionKey)
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
			err := db.storage.Set([]byte(earliestVersionKey), ts[:], defaultWriteOpts)
			if err == nil {
				db.operationMetrics.AddWrite(1)
			}
			return err
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
	return retrieveVersionKey(db, earliestVersionKey)
}

// retrieveVersionKey reads a little-endian uint64 version from the given
// metadata key. Returns 0 when the key is absent (fresh DB).
func retrieveVersionKey(db *pebble.DB, key string) (int64, error) {
	bz, closer, err := db.Get([]byte(key))
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
	u := binary.LittleEndian.Uint64(bz)
	if u > math.MaxInt64 {
		return 0, fmt.Errorf("version at %q overflows int64: %d", key, u)
	}
	return int64(u), nil
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
func (db *Database) Get(storeKey string, targetVersion int64, key []byte) (_ []byte, _err error) {
	if targetVersion < db.GetEarliestVersion() {
		return nil, nil
	}
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
	b, err := NewBatch(db.storage, version, db.descending, db.operationMetrics)
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

// compactPrunedRange compacts only the span of keys that a prune pass deleted so
// Pebble reclaims the tombstoned space right away. Without it, deleted keys pile
// up as un-compacted tombstones and every subsequent full-DB prune scan has to
// read through them, which makes prune latency creep upward the longer a node
// stays up (and is why restarting a node temporarily relieves head-lag: the
// reopen triggers compaction). first and last are the smallest and largest
// encoded keys deleted during the pass, in Pebble comparer order; both are nil
// when nothing was deleted, in which case compaction is skipped entirely.
func (db *Database) compactPrunedRange(first, last []byte) error {
	if first == nil {
		return nil
	}
	// Pebble's Compact treats [start, end] as an inclusive range but requires
	// start < end. Appending a zero byte extends the user-key portion of last,
	// yielding a key strictly greater than it under both the MVCC and default
	// comparers, so the entire deleted span is covered.
	end := append(slices.Clone(last), 0)
	return db.storage.Compact(context.Background(), first, end, true)
}

// Iterator dispatches between descending- and ascending-mode implementations
// depending on the on-disk encoding detected at open time.
func (db *Database) Iterator(storeKey string, version int64, start, end []byte) (dbm.Iterator, error) {
	if db.descending {
		return db.iteratorDescending(storeKey, version, start, end)
	}
	return db.iteratorAscending(storeKey, version, start, end)
}

// ReverseIterator dispatches between descending- and ascending-mode
// implementations depending on the on-disk encoding detected at open time.
func (db *Database) ReverseIterator(storeKey string, version int64, start, end []byte) (dbm.Iterator, error) {
	if db.descending {
		return db.reverseIteratorDescending(storeKey, version, start, end)
	}
	return db.reverseIteratorAscending(storeKey, version, start, end)
}

// ---------------------------------------------------------------------------
// Descending-mode implementation (the fast path used by DBs created by this
// build). Versions of a logical key sort newest-first on disk, so Pebble's
// First() / SeekGE lands directly on the latest visible version without
// iterating older ones. The ascending-mode counterparts live in
// db_ascending.go for legacy DBs.
// ---------------------------------------------------------------------------

func (db *Database) hasDescending(storeKey string, version int64, key []byte) (bool, error) {
	if version < db.GetEarliestVersion() {
		return false, nil
	}

	val, err := db.getDescending(storeKey, version, key)
	if err != nil {
		return false, err
	}

	return val != nil, nil
}

func (db *Database) getDescending(storeKey string, targetVersion int64, key []byte) (_ []byte, _err error) {
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
	}()
	if targetVersion < db.GetEarliestVersion() {
		return nil, nil
	}

	db.operationMetrics.AddRead(1)
	prefixedVal, err := getMVCCSliceDescending(db.storage, storeKey, key, targetVersion)
	if err != nil {
		if errors.Is(err, errorutils.ErrRecordNotFound) {
			return nil, nil
		}

		return nil, fmt.Errorf("failed to perform PebbleDB read: %w", err)
	}

	return visibleValueAtVersionDescending(prefixedVal, targetVersion)
}

// pruneDescending attempts to prune all versions up to and including the current version
// Get the range of keys, manually iterate over them and delete them
// We add a heuristic to skip over a module's keys during pruning if it hasn't been updated
// since the last time pruning occurred.
// NOTE: There is a rare case when a module's keys are skipped during pruning even though
// it has been updated. This occurs when that module's keys are updated in between pruning runs, the node after is restarted.
// This is not a large issue given the next time that module is updated, it will be properly pruned thereafter.
func (db *Database) pruneDescending(version int64) (_err error) {
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
		counter                         int
		prevKey                         []byte
		keptBelowPrune                  bool
		prevStore                       string
		scanReads                       int64
		firstDeletedKey, lastDeletedKey []byte
	)

	for itr.First(); itr.Valid(); {
		scanReads++
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

		// Reset per-logical-key state when the logical key changes.
		if !bytes.Equal(prevKey, currKey) {
			prevKey = slices.Clone(currKey)
			keptBelowPrune = false

			// Fast path: under descending encoding, versions of a key are stored
			// newest-first. When the newest real version is above the prune
			// height, seek directly to the first version <= prune height for
			// this key instead of iterating through every above-prune version.
			if currVersionDecoded > version {
				itr.SeekGE(MVCCEncodeDescending(currKey, version))
				continue
			}
		}

		// Descending iteration: for a given logical key we see newest→oldest.
		// Versions > prune height are always kept. For versions <= prune
		// height, keep only the newest one when KeepLastVersion is true;
		// delete every other such version.
		if currVersionDecoded <= version {
			if db.config.KeepLastVersion && !keptBelowPrune {
				keptBelowPrune = true
			} else {
				if err := batch.Delete(currKeyEncoded, nil); err != nil {
					return err
				}
				// Track the deleted span (keys are visited in comparer order, so
				// the first delete is the smallest and the last is the largest)
				// to compact just that range once pruning completes.
				if firstDeletedKey == nil {
					firstDeletedKey = currKeyEncoded
				}
				lastDeletedKey = currKeyEncoded
				counter++
				if counter >= PruneCommitBatchSize {
					writeCount := int64(batch.Count())
					if err := batch.Commit(defaultWriteOpts); err != nil {
						return err
					}
					db.operationMetrics.AddWrite(writeCount)
					counter = 0
					batch.Reset()
				}
			}
		}

		itr.Next()
	}

	// Commit any leftover delete ops in batch
	if counter > 0 {
		writeCount := int64(batch.Count())
		err = batch.Commit(defaultWriteOpts)
		if err != nil {
			return err
		}
		db.operationMetrics.AddWrite(writeCount)
	}
	db.operationMetrics.AddRead(scanReads)

	if err := db.SetEarliestVersion(earliestVersion, false); err != nil {
		return err
	}
	return db.compactPrunedRange(firstDeletedKey, lastDeletedKey)
}

func (db *Database) iteratorDescending(storeKey string, version int64, start, end []byte) (dbm.Iterator, error) {
	if (start != nil && len(start) == 0) || (end != nil && len(end) == 0) {
		return nil, errorutils.ErrKeyEmpty
	}

	if start != nil && end != nil && bytes.Compare(start, end) > 0 {
		return nil, errorutils.ErrStartAfterEnd
	}

	lowerBound := MVCCEncodeDescending(prependStoreKey(storeKey, start), 0)

	var upperBound []byte
	if end != nil {
		upperBound = MVCCEncodeDescending(prependStoreKey(storeKey, end), 0)
	} else {
		upperBound = iteratorUpperBoundForStoreDescending(storeKey)
	}

	itr, err := db.storage.NewIter(&pebble.IterOptions{LowerBound: lowerBound, UpperBound: upperBound})
	if err != nil {
		return nil, fmt.Errorf("failed to create PebbleDB iterator: %w", err)
	}

	return newPebbleDBIterator(itr, storePrefix(storeKey), start, end, version, db.GetEarliestVersion(), false, db.config.UseDefaultComparer, storeKey, db.operationMetrics), nil
}

func (db *Database) reverseIteratorDescending(storeKey string, version int64, start, end []byte) (dbm.Iterator, error) {
	if (start != nil && len(start) == 0) || (end != nil && len(end) == 0) {
		return nil, errorutils.ErrKeyEmpty
	}

	if start != nil && end != nil && bytes.Compare(start, end) > 0 {
		return nil, errorutils.ErrStartAfterEnd
	}

	lowerBound := MVCCEncodeDescending(prependStoreKey(storeKey, start), 0)

	var upperBound []byte
	if end != nil {
		upperBound = MVCCEncodeDescending(prependStoreKey(storeKey, end), 0)
	} else {
		upperBound = MVCCEncodeDescending(prefixEnd(storePrefix(storeKey)), 0)
	}

	itr, err := db.storage.NewIter(&pebble.IterOptions{LowerBound: lowerBound, UpperBound: upperBound})
	if err != nil {
		return nil, fmt.Errorf("failed to create PebbleDB iterator: %w", err)
	}

	return newPebbleDBIterator(itr, storePrefix(storeKey), start, end, version, db.GetEarliestVersion(), true, db.config.UseDefaultComparer, storeKey, db.operationMetrics), nil
}

func getMVCCSliceDescending(db *pebble.DB, storeKey string, key []byte, version int64) (_ []byte, err error) {
	prefixedKey := prependStoreKey(storeKey, key)
	itr, err := db.NewIter(&pebble.IterOptions{
		LowerBound: MVCCEncodeDescending(prefixedKey, version),
		UpperBound: iteratorUpperBoundForLogicalKeyDescending(prefixedKey),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create PebbleDB iterator: %w", err)
	}
	defer func() {
		err = errorutils.Join(err, itr.Close())
	}()

	if !itr.First() {
		return nil, errorutils.ErrRecordNotFound
	}
	return decodeMVCCEntryDescending(itr.Key(), itr.Value(), prefixedKey, version)
}

// decodeMVCCEntryDescending validates that the iterator's current entry
// belongs to prefixedKey at a version <= target and returns a safe copy of the
// value. Assumes descending version encoding.
func decodeMVCCEntryDescending(rawIterKey, rawIterValue, prefixedKey []byte, version int64) ([]byte, error) {
	userKey, vBz, ok := SplitMVCCKey(rawIterKey)
	if !ok {
		return nil, fmt.Errorf("invalid PebbleDB MVCC key: %s", rawIterKey)
	}
	if !bytes.Equal(userKey, prefixedKey) {
		return nil, errorutils.ErrRecordNotFound
	}
	keyVersion, err := decodeUint64Descending(vBz)
	if err != nil {
		return nil, fmt.Errorf("failed to decode key version: %w", err)
	}
	if keyVersion > version {
		return nil, errorutils.ErrRecordNotFound
	}
	return slices.Clone(rawIterValue), nil
}

func visibleValueAtVersionDescending(prefixedVal []byte, targetVersion int64) ([]byte, error) {
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

func iteratorUpperBoundForStoreDescending(storeKey string) []byte {
	upperStorePrefix := prefixEnd(storePrefix(storeKey))
	if upperStorePrefix == nil {
		return nil
	}
	return MVCCEncodeDescending(upperStorePrefix, 0)
}

func iteratorUpperBoundForLogicalKeyDescending(key []byte) []byte {
	upperKeyPrefix := prefixEnd(key)
	if upperKeyPrefix == nil {
		return nil
	}
	return MVCCEncodeDescending(upperKeyPrefix, 0)
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
		batch, err := NewBatch(db.storage, version, db.descending, db.operationMetrics)
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

				batch, err = NewBatch(db.storage, version, db.descending, db.operationMetrics)
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

	batch, err := NewBatch(db.storage, version, db.descending, db.operationMetrics)
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
				batch, err = NewBatch(db.storage, version, db.descending, db.operationMetrics)
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
