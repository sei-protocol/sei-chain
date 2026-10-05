// Package undo is a Pebble state store that keeps recent history as a bucketed undo log.
//
// When block h changes a key, the store records the value the key held before h, under
//
//	bucket(h) | key | h        with bucket(h) = h / bucketSize
//
// and nothing else: the value after the latest block lives in the state commit store, which this
// store reads through a CurrentView. The state after block T is the first record above T, or the
// current value when there is none. Buckets are contiguous key ranges, so dropping history is one
// Excise per expired bucket rather than a scan and a tombstone per version.
package undo

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/cockroachdb/pebble/v2/bloom"
	"github.com/cockroachdb/pebble/v2/sstable"
	"github.com/sei-protocol/seilog"
	"go.opentelemetry.io/otel/metric"
	"golang.org/x/sync/errgroup"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	seidbmetrics "github.com/sei-protocol/sei-chain/sei-db/common/metrics"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/controller"
	pebbledbmetrics "github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

var logger = seilog.NewLogger("db", "db-engine", "pebbledb", "undo")

var _ controller.PrunableStore = (*Database)(nil)

const (
	// cacheSize bounds the block cache, which holds the filter, index and data blocks a read probes.
	// Pebble reserves every memtable's size out of this cache, so it has to exceed the memtables by
	// a wide margin or nothing is cached at all.
	cacheSize = 2 << 30

	// minConcurrentCompactions and maxConcurrentCompactions bound how many compactions run at once,
	// tuned with the memtable and level sizes below for hundreds of thousands of records a second.
	minConcurrentCompactions = 6
	maxConcurrentCompactions = 20

	// writeParallelism is how many batches a block's records are split across, and minBatchRecords
	// the fewest records a batch takes, so a small block stays one batch.
	writeParallelism = 8
	minBatchRecords  = 1024

	// deleteBatchBytes bounds a batch of the deletes a rollback stages.
	deleteBatchBytes = 16 << 20

	// metricsRefreshInterval is how often Pebble's internal stats and the queue depth are resampled.
	metricsRefreshInterval = 10 * time.Second

	valueAbsent  = 0
	valuePresent = 1

	// module is the state commit store module whose history the store keeps.
	module = keys.EVMStoreKey

	// how many blocks a bucket covers. changing this is breaking.
	// the retention window divided by this value should be a single digit or two digits
	defaultBucketSize = 10000
)

var (
	latestVersionKey   = metadataKey("latest")
	earliestVersionKey = metadataKey("earliest")
	bucketSizeKey      = metadataKey("bucket_size")
)

// CurrentView reads current state as of one committed block. The state commit store's views are
// CurrentViews.
type CurrentView interface {
	Get(module string, key []byte) (value []byte, found bool)
	Close()
}

// Database is an undo-log state store. Blocks arrive in height order, each with the prior values of
// the keys it changed and a view of current state after it.
type Database struct {
	storage    *pebble.DB
	bucketSize uint64

	// latestHeight is the height of the current head: every record at or below it is written.
	latestHeight atomic.Uint64

	// head is the current-state view reads fall back to, with the height it describes.
	head atomic.Pointer[head]

	// mu guards the earliest served height against the open views, so a prune never excises a
	// bucket an open view reads. earliestHeight only rises while views can open; a rollback, which
	// runs with the store open nowhere else, may lower it.
	mu             sync.Mutex
	earliestHeight atomic.Uint64
	openViews      map[uint64]int

	// pruneMu serializes prune passes and guards prunedBucket, the lowest bucket not yet excised.
	pruneMu      sync.Mutex
	prunedBucket uint64

	pending     chan pendingBlock
	writerDone  chan struct{}
	queue       *seidbmetrics.QueueMeter
	stopMetrics func()
	closeOnce   sync.Once
}

// pendingBlock is one entry of the write queue: a block, or, with done set, a barrier.
type pendingBlock struct {
	version int64
	prior   []*proto.KVPair
	current CurrentView
	done    chan struct{}
}

// OpenDB opens the undo-log database in dataDir.
func OpenDB(dataDir string, cfg config.StateStoreConfig) (*Database, error) {
	cache := pebble.NewCache(cacheSize)
	defer cache.Unref()
	storage, err := pebble.Open(dataDir, newPebbleOptions(cache))
	if err != nil {
		return nil, fmt.Errorf("undo: open pebble: %w", err)
	}
	db, err := newDatabase(storage, dataDir, cfg)
	if err != nil {
		return nil, errors.Join(err, storage.Close())
	}
	return db, nil
}

func newPebbleOptions(cache *pebble.Cache) *pebble.Options {
	opts := &pebble.Options{
		Cache:    cache,
		Comparer: Comparer,
		// Excise needs virtual SSTables. Pinned, as in the MVCC store, so a Pebble upgrade cannot
		// silently move the on-disk format. The fixed-position Split also requires this format:
		// synthetic prefixes, introduced in FormatSyntheticPrefixSuffix, are not supported.
		FormatMajorVersion:          pebble.FormatVirtualSSTables,
		L0CompactionThreshold:       6,
		L0StopWritesThreshold:       1000,
		LBaseMaxBytes:               512 << 20,
		MemTableSize:                256 << 20,
		MemTableStopWritesThreshold: 4,
		CompactionConcurrencyRange: func() (int, int) {
			return minConcurrentCompactions, maxConcurrentCompactions
		},
	}
	for i := range opts.Levels {
		l := &opts.Levels[i]
		l.BlockSize = 32 << 10
		l.IndexBlockSize = 256 << 10
		// Every level keeps its filter, the bottom one included: a read probes one bucket after
		// another, most of which never saw the key, and the filter is what rejects them.
		l.FilterPolicy = bloom.FilterPolicy(10)
		l.FilterType = pebble.TableFilter
		if i == 0 {
			// Flushes compress everything written, on one goroutine, and compaction rewrites L0
			// within minutes, so L0 takes the cheapest compression the pinned format supports.
			l.Compression = func() *sstable.CompressionProfile { return sstable.SnappyCompression }
			l.EnsureL0Defaults()
		} else {
			l.Compression = func() *sstable.CompressionProfile { return sstable.ZstdCompression }
			l.EnsureL1PlusDefaults(&opts.Levels[i-1])
		}
	}
	return opts
}

func newDatabase(storage *pebble.DB, dataDir string, cfg config.StateStoreConfig) (*Database, error) {
	bucketSize, err := loadBucketSize(storage)
	if err != nil {
		return nil, err
	}
	latest, err := readMarker(storage, latestVersionKey)
	if err != nil {
		return nil, err
	}
	earliest, err := readMarker(storage, earliestVersionKey)
	if err != nil {
		return nil, err
	}
	db := &Database{
		storage:    storage,
		bucketSize: bucketSize,
		openViews:  map[uint64]int{},
		pending:    make(chan pendingBlock, max(cfg.AsyncWriteBuffer, 1)),
		writerDone: make(chan struct{}),
		queue:      seidbmetrics.NewQueueMeter(meter, "pebble_undo_pending_changes"),
	}
	db.latestHeight.Store(latest)
	db.earliestHeight.Store(earliest)
	go db.writeInBackground()

	name := dataDir
	if abs, err := filepath.Abs(dataDir); err == nil {
		name = abs
	}
	stopPebbleStats := pebbledbmetrics.NewPebbleMetrics(storage, name, metricsRefreshInterval)
	samplingCtx, stopSampling := context.WithCancel(context.Background())
	db.queue.SampleDepth(samplingCtx, int(metricsRefreshInterval.Seconds()), func() int { return len(db.pending) })
	db.stopMetrics = func() {
		stopPebbleStats()
		stopSampling()
	}
	return db, nil
}

// loadBucketSize returns the persisted bucket size, recording the default for a new database.
func loadBucketSize(storage *pebble.DB) (uint64, error) {
	stored, err := readMarker(storage, bucketSizeKey)
	if err != nil {
		return 0, err
	}
	if stored > 0 {
		if defaultBucketSize != stored {
			logger.Warn("undo log keeps the bucket size it was created with",
				"bucketSize", stored, "defaultBucketSize", defaultBucketSize)
		}
		return stored, nil
	}
	if err := writeMarker(storage, bucketSizeKey, defaultBucketSize, pebble.Sync); err != nil {
		return 0, fmt.Errorf("undo: record bucket size: %w", err)
	}
	return defaultBucketSize, nil
}

// readMarker returns the value stored under key, 0 when there is none. Markers fit in an int64, the
// type heights leave the store as.
func readMarker(storage *pebble.DB, key []byte) (uint64, error) {
	bz, closer, err := storage.Get(key)
	if errors.Is(err, pebble.ErrNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("undo: read marker %q: %w", key, err)
	}
	defer func() { _ = closer.Close() }()
	if len(bz) != 8 {
		return 0, fmt.Errorf("undo: marker %q has %d bytes", key, len(bz))
	}
	u := binary.LittleEndian.Uint64(bz)
	if u > math.MaxInt64 {
		return 0, fmt.Errorf("undo: marker %q overflows int64: %d", key, u)
	}
	return u, nil
}

func writeMarker(storage *pebble.DB, key []byte, value uint64, opts *pebble.WriteOptions) error {
	var bz [8]byte
	binary.LittleEndian.PutUint64(bz[:], value)
	return storage.Set(key, bz[:], opts)
}

// heightOf returns version as a height, refusing a negative one.
func heightOf(version int64) (uint64, error) {
	if version < 0 {
		return 0, fmt.Errorf("undo: negative block height %d", version)
	}
	return uint64(version), nil
}

// Resume binds current state as of version as the view reads fall back to, and must be called once,
// before the first block is applied. version is where the state commit store stands, at or above
// the log's latest block.
//
// A log behind version is missing the records of the blocks in between, which only their execution
// could supply, so history below version stops being served.
func (db *Database) Resume(version int64, current CurrentView) error {
	height, err := db.resume(version)
	if err != nil {
		current.Close()
		return err
	}
	db.publishHead(height, current)
	return nil
}

func (db *Database) resume(version int64) (uint64, error) {
	height, err := heightOf(version)
	if err != nil {
		return 0, err
	}
	latest := db.latestHeight.Load()
	if latest > height {
		return 0, fmt.Errorf("undo: log at %d is ahead of current state at %d", latest, height)
	}
	if latest == height {
		return height, nil
	}
	if latest > 0 {
		logger.Warn("undo log is behind current state; history below the current height is dropped",
			"undoLatest", latest, "current", height)
	}
	if err := db.raiseEarliest(height); err != nil {
		return 0, err
	}
	if err := writeMarker(db.storage, latestVersionKey, height, pebble.Sync); err != nil {
		return 0, fmt.Errorf("undo: record resumed height %d: %w", height, err)
	}
	return height, nil
}

// ApplyBlock queues prior balance, nonce, code hash, storage and code values for version,
// with Delete marking prior absence, and takes ownership of the post-block current view.
// Unsupported key families and malformed records are fatal write errors.
func (db *Database) ApplyBlock(version int64, prior []*proto.KVPair, current CurrentView) {
	seidbmetrics.Send(db.queue, db.pending, pendingBlock{version: version, prior: prior, current: current})
}

// WaitForPendingWrites blocks until every block queued so far is applied.
func (db *Database) WaitForPendingWrites() {
	done := make(chan struct{})
	db.pending <- pendingBlock{done: done}
	<-done
}

func (db *Database) writeInBackground() {
	defer close(db.writerDone)
	for block := range db.pending {
		if block.done != nil {
			close(block.done)
			continue
		}
		if err := db.applyBlock(block.version, block.prior, block.current); err != nil {
			// A block that cannot be written leaves a gap every later read below it would answer wrongly.
			panic(err)
		}
	}
}

// applyBlock writes block version and makes current the view reads fall back to.
func (db *Database) applyBlock(version int64, prior []*proto.KVPair, current CurrentView) error {
	height, err := db.writeBlock(version, prior)
	if err != nil {
		current.Close()
		return err
	}
	db.publishHead(height, current)
	return nil
}

// writeBlock writes block version's records, then moves the latest-height marker to it, and returns
// the block's height. A crash in between leaves records only above the marker, which the block
// rewrites when it is applied again.
func (db *Database) writeBlock(version int64, prior []*proto.KVPair) (height uint64, err error) {
	start := time.Now()
	defer func() {
		otelMetrics.applyLatency.Record(context.Background(), time.Since(start).Seconds(), successAttr(err))
		otelMetrics.applyRecords.Record(context.Background(), int64(len(prior)))
	}()
	if height, err = heightOf(version); err != nil {
		return 0, err
	}
	// Blocks are contiguous: a skipped block leaves no records, and every read below it would
	// silently answer with a later value.
	if latest := db.latestHeight.Load(); height != latest+1 {
		return 0, fmt.Errorf("undo: block %d does not follow the latest applied block %d", height, latest)
	}
	if err := validateRecords(prior); err != nil {
		return 0, err
	}
	bucket := height / db.bucketSize

	// Pebble inserts a batch into the memtable on one goroutine, and concurrent batches
	// concurrently, so the records go out as batches committed in parallel.
	var g errgroup.Group
	for records := range slices.Chunk(prior, max(minBatchRecords, (len(prior)+writeParallelism-1)/writeParallelism)) {
		g.Go(func() error { return db.writeRecords(records, bucket, height) })
	}
	if err := g.Wait(); err != nil {
		return 0, err
	}
	if err := writeMarker(db.storage, latestVersionKey, height, pebble.NoSync); err != nil {
		return 0, fmt.Errorf("undo: record latest height %d: %w", height, err)
	}
	return height, nil
}

// validateRecords checks that every prior record has a supported EVM key and value length.
func validateRecords(prior []*proto.KVPair) error {
	for _, pair := range prior {
		if pair == nil {
			return errors.New("undo: nil prior record")
		}
		keyLen, valueLen := keyLayout(pair.Key)
		if keyLen == 0 {
			return fmt.Errorf("undo: unsupported EVM key %x", pair.Key)
		}
		if len(pair.Key) != keyLen {
			return fmt.Errorf("undo: key %x has length %d, want %d", pair.Key, len(pair.Key), keyLen)
		}
		if !pair.Delete && valueLen >= 0 && len(pair.Value) != valueLen {
			return fmt.Errorf("undo: value for key %x has length %d, want %d", pair.Key, len(pair.Value), valueLen)
		}
	}
	return nil
}

// writeRecords commits the records of prior, all written by the block at height, as one batch.
func (db *Database) writeRecords(prior []*proto.KVPair, bucket, height uint64) error {
	b := db.storage.NewBatch()
	defer func() { _ = b.Close() }()
	for _, pair := range prior {
		d := b.SetDeferred(recordKeyLen(pair.Key), valueLen(pair))
		encodeRecordKey(d.Key, bucket, pair.Key, height)
		encodeValue(d.Value, pair)
		if err := d.Finish(); err != nil {
			return fmt.Errorf("undo: stage record: %w", err)
		}
	}
	if err := b.Commit(pebble.NoSync); err != nil {
		return fmt.Errorf("undo: commit block %d: %w", height, err)
	}
	return nil
}

// head is a current-state view and the height it describes. The store holds one reference and
// every view pinning it holds another; the last one released closes the view.
type head struct {
	height uint64
	view   CurrentView
	refs   atomic.Int64
}

func (h *head) release() {
	if h.refs.Add(-1) == 0 {
		h.view.Close()
	}
}

// publishHead makes current, as of height, the view reads fall back to.
func (db *Database) publishHead(height uint64, current CurrentView) {
	next := &head{height: height, view: current}
	next.refs.Store(1)
	if prev := db.head.Swap(next); prev != nil {
		prev.release()
	}
	db.latestHeight.Store(height)
}

// acquireHead pins the current head, or returns nil when none is bound.
func (db *Database) acquireHead() *head {
	for {
		h := db.head.Load()
		if h == nil {
			return nil
		}
		refs := h.refs.Load()
		if refs == 0 {
			// Retired between the load and here; the replacement is already published.
			continue
		}
		if h.refs.CompareAndSwap(refs, refs+1) {
			return h
		}
	}
}

// View reads the store at one height. It pins the current-state view it falls back to and the
// buckets it reads, holding back both the state commit store and pruning, so it must be closed
// promptly.
type View struct {
	db        *Database
	head      *head
	height    uint64
	closeOnce sync.Once
}

// OpenView returns a view of the state after block version, and false when the store does not
// serve version.
func (db *Database) OpenView(version int64) (*View, bool) {
	height, err := heightOf(version)
	if err != nil {
		return nil, false
	}
	h := db.acquireHead()
	if h == nil {
		return nil, false
	}
	if height > h.height || !db.pinHeight(height) {
		h.release()
		return nil, false
	}
	return &View{db: db, head: h, height: height}, true
}

// pinHeight registers a view at height, which the prune passes from here on keep readable, and
// reports false when height is already below the earliest served height.
func (db *Database) pinHeight(height uint64) bool {
	db.mu.Lock()
	defer db.mu.Unlock()
	if height < db.earliestHeight.Load() {
		return false
	}
	db.openViews[height]++
	return true
}

func (db *Database) unpinHeight(height uint64) {
	db.mu.Lock()
	defer db.mu.Unlock()
	if db.openViews[height]--; db.openViews[height] == 0 {
		delete(db.openViews, height)
	}
}

// Close releases the view's pins. Idempotent.
func (v *View) Close() {
	v.closeOnce.Do(func() {
		v.db.unpinHeight(v.height)
		v.head.release()
	})
}

// Get returns a supported EVM key's value at the view's height, nil when it did not exist.
// Unsupported or malformed keys return an error.
func (v *View) Get(key []byte) (_ []byte, err error) {
	keyLen, _ := keyLayout(key)
	if keyLen == 0 || len(key) != keyLen {
		return nil, fmt.Errorf("undo: unsupported or malformed EVM key %x", key)
	}
	start := time.Now()
	probes := 0
	defer func() {
		otelMetrics.getLatency.Record(context.Background(), time.Since(start).Seconds(), successAttr(err))
		otelMetrics.getProbes.Record(context.Background(), int64(probes))
	}()
	h := v.head
	if v.height == h.height {
		return foundValue(h.view.Get(module, key)), nil
	}

	first := (v.height + 1) / v.db.bucketSize
	last := h.height / v.db.bucketSize
	itr, err := v.db.storage.NewIter(&pebble.IterOptions{
		LowerBound:   bucketBoundary(first),
		UpperBound:   bucketBoundary(last + 1),
		UseL6Filters: true,
	})
	if err != nil {
		return nil, fmt.Errorf("undo: create iterator: %w", err)
	}
	defer func() { err = errors.Join(err, itr.Close()) }()

	var seekKey []byte
	for bucket := first; bucket <= last; bucket++ {
		probes++
		seekKey = appendRecordKey(seekKey[:0], bucket, key, v.height+1)
		if !itr.SeekPrefixGE(seekKey) {
			if err := itr.Error(); err != nil {
				return nil, fmt.Errorf("undo: seek bucket %d: %w", bucket, err)
			}
			continue
		}
		height, err := decodeRecordHeight(itr.Key())
		if err != nil {
			return nil, err
		}
		// A record above the head belongs to a block the head's view does not include yet.
		if height > h.height {
			continue
		}
		return decodeValue(itr.Value())
	}
	return foundValue(h.view.Get(module, key)), nil
}

// GetLatestVersion returns the latest block whose records are written and readable.
func (db *Database) GetLatestVersion() int64 {
	return int64(db.latestHeight.Load()) //nolint:gosec // heights enter the store as int64
}

// GetEarliestVersion returns the lowest height a view can open at.
func (db *Database) GetEarliestVersion() int64 {
	return int64(db.earliestHeight.Load()) //nolint:gosec // heights enter the store as int64
}

// raiseEarliest makes height the lowest served height, durably before it takes effect, so a
// restart never serves history already excised. A lower height is ignored.
func (db *Database) raiseEarliest(height uint64) error {
	if height <= db.earliestHeight.Load() {
		return nil
	}
	if err := writeMarker(db.storage, earliestVersionKey, height, pebble.Sync); err != nil {
		return fmt.Errorf("undo: record earliest height %d: %w", height, err)
	}
	db.mu.Lock()
	defer db.mu.Unlock()
	db.earliestHeight.Store(max(height, db.earliestHeight.Load()))
	return nil
}

// firstNeededBucket returns the lowest bucket a view at the earliest height or at an open view's
// height reads: a read at T needs records above T only.
func (db *Database) firstNeededBucket() uint64 {
	db.mu.Lock()
	defer db.mu.Unlock()
	lowest := db.earliestHeight.Load()
	for height := range db.openViews {
		lowest = min(lowest, height)
	}
	return (lowest + 1) / db.bucketSize
}

// Name identifies the store to the collector it is pruned by.
func (db *Database) Name() string {
	return "EVM SS undo"
}

// PruneHistory drops the history below blockNumber, clamped to the head, keeping blockNumber
// readable. It excises every bucket no read from there on needs, except those an open view still
// reads, which a later call excises.
func (db *Database) PruneHistory(blockNumber uint64) error {
	head := db.latestHeight.Load()
	if blockNumber == 0 || head == 0 {
		return nil
	}
	db.pruneMu.Lock()
	defer db.pruneMu.Unlock()
	if err := db.raiseEarliest(min(blockNumber, head)); err != nil {
		return err
	}
	firstKept := db.firstNeededBucket()
	if firstKept <= db.prunedBucket {
		return nil
	}
	start := time.Now()
	if err := db.storage.Excise(context.Background(), pebble.KeyRange{
		Start: bucketBoundary(db.prunedBucket),
		End:   bucketBoundary(firstKept),
	}); err != nil {
		return fmt.Errorf("undo: excise buckets [%d, %d): %w", db.prunedBucket, firstKept, err)
	}
	otelMetrics.exciseLatency.Record(context.Background(), time.Since(start).Seconds())
	otelMetrics.bucketsExcised.Add(context.Background(), int64(firstKept-db.prunedBucket)) //nolint:gosec // bucket counts are small
	logger.Info("excised undo buckets", "from", db.prunedBucket, "to", firstKept,
		"earliest", db.earliestHeight.Load(), "elapsed", time.Since(start))
	db.prunedBucket = firstKept
	return nil
}

// PruneSnapshots does nothing: the undo log keeps no snapshots.
func (db *Database) PruneSnapshots(uint64) error {
	return nil
}

// ExternalPruning reports true: the undo log has no pruner of its own.
func (db *Database) ExternalPruning() bool {
	return true
}

// GetRollbackFloor returns head - rollbackWindow, or 0 when the window reaches below genesis: the
// records above any target can be discarded without a snapshot.
func (db *Database) GetRollbackFloor(rollbackWindow uint64) uint64 {
	head := db.latestHeight.Load()
	if head <= rollbackWindow {
		return 0
	}
	return head - rollbackWindow
}

// GetLatestBlock returns the latest applied block, 0 when none is.
func (db *Database) GetLatestBlock() (uint64, error) {
	return db.latestHeight.Load(), nil
}

// DiscardStateAbove removes every record above target from the undo log in dataDir and leaves the log
// at target, served from target when its earliest height was above it. A log below target, or none
// at all, is left alone. The database must not be open elsewhere.
func DiscardStateAbove(dataDir string, cfg config.StateStoreConfig, target int64) (err error) {
	if _, statErr := os.Stat(dataDir); errors.Is(statErr, os.ErrNotExist) {
		return nil
	}
	db, err := OpenDB(dataDir, cfg)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	height, err := heightOf(target)
	if err != nil {
		return err
	}
	return db.discardAbove(height)
}

// discardAbove removes every record above target, then moves the latest-height marker to target and
// lowers the earliest served height to it. Buckets above target's are excised and target's own
// bucket is scanned.
func (db *Database) discardAbove(target uint64) error {
	latest := db.latestHeight.Load()
	if latest < target {
		return nil
	}
	targetBucket := target / db.bucketSize
	if err := db.exciseBucketsAbove(targetBucket); err != nil {
		return err
	}
	if err := db.deleteRecordsAbove(targetBucket, target); err != nil {
		return err
	}
	// The latest marker moves first: a crash before the earliest one follows leaves a log that
	// refuses reads rather than one that serves heights whose records are gone, and running the
	// rollback again completes it.
	if err := writeMarker(db.storage, latestVersionKey, target, pebble.Sync); err != nil {
		return fmt.Errorf("undo: record latest height %d: %w", target, err)
	}
	db.latestHeight.Store(target)
	if err := db.lowerEarliest(target); err != nil {
		return err
	}
	logger.Info("discarded undo records above the target", "target", target, "previousLatest", latest)
	return nil
}

// exciseBucketsAbove removes every bucket above bucket, up to the metadata. It reaches past the
// latest-height marker because a crash between a block's records and its marker leaves records
// above the marker, which can open a bucket the marker never reached.
func (db *Database) exciseBucketsAbove(bucket uint64) error {
	if err := db.storage.Excise(context.Background(), pebble.KeyRange{
		Start: bucketBoundary(bucket + 1),
		End:   bucketBoundary(metadataBucket),
	}); err != nil {
		return fmt.Errorf("undo: excise buckets above %d: %w", bucket, err)
	}
	return nil
}

// lowerEarliest makes height the earliest served height when the earliest is above it. A rollback
// below the earliest height leaves no record above height, and a read at height needs none.
func (db *Database) lowerEarliest(height uint64) error {
	if db.earliestHeight.Load() <= height {
		return nil
	}
	if err := writeMarker(db.storage, earliestVersionKey, height, pebble.Sync); err != nil {
		return fmt.Errorf("undo: record earliest height %d: %w", height, err)
	}
	db.earliestHeight.Store(height)
	return nil
}

// deleteRecordsAbove deletes the records of bucket written above height, committing every
// deleteBatchBytes.
func (db *Database) deleteRecordsAbove(bucket uint64, height uint64) error {
	itr, err := db.storage.NewIter(&pebble.IterOptions{
		LowerBound: bucketBoundary(bucket),
		UpperBound: bucketBoundary(bucket + 1),
	})
	if err != nil {
		return fmt.Errorf("undo: create iterator: %w", err)
	}
	defer func() { _ = itr.Close() }()
	b := db.storage.NewBatch()
	defer func() { _ = b.Close() }()
	for itr.First(); itr.Valid(); itr.Next() {
		recordHeight, err := decodeRecordHeight(itr.Key())
		if err != nil {
			return err
		}
		if recordHeight <= height {
			continue
		}
		if err := b.Delete(itr.Key(), nil); err != nil {
			return fmt.Errorf("undo: stage delete: %w", err)
		}
		if b.Len() >= deleteBatchBytes {
			if err := b.Commit(pebble.NoSync); err != nil {
				return fmt.Errorf("undo: commit deletes: %w", err)
			}
			b.Reset()
		}
	}
	if err := itr.Error(); err != nil {
		return fmt.Errorf("undo: scan bucket %d: %w", bucket, err)
	}
	return b.Commit(pebble.Sync)
}

// Close drains the write queue, releases the current-state view and closes Pebble. Views must be
// closed first. Idempotent.
func (db *Database) Close() error {
	var err error
	db.closeOnce.Do(func() {
		close(db.pending)
		<-db.writerDone
		db.stopMetrics()
		if h := db.head.Swap(nil); h != nil {
			h.release()
		}
		err = db.storage.Close()
	})
	return err
}

func valueLen(pair *proto.KVPair) int {
	if pair.Delete {
		return 1
	}
	return 1 + len(pair.Value)
}

func encodeValue(dst []byte, pair *proto.KVPair) {
	if pair.Delete {
		dst[0] = valueAbsent
		return
	}
	dst[0] = valuePresent
	copy(dst[1:], pair.Value)
}

// decodeValue returns a copy of the value a record holds, nil when the key did not exist.
func decodeValue(v []byte) ([]byte, error) {
	if len(v) == 0 {
		return nil, errors.New("undo: empty record value")
	}
	switch v[0] {
	case valueAbsent:
		return nil, nil
	case valuePresent:
		// Non-nil even when empty: a found empty value is distinct from an absent key.
		return append([]byte{}, v[1:]...), nil
	default:
		return nil, fmt.Errorf("undo: unknown record tag %d", v[0])
	}
}

// foundValue returns value when found, as a non-nil slice even when empty, and nil otherwise.
func foundValue(value []byte, found bool) []byte {
	if !found {
		return nil
	}
	if value == nil {
		return []byte{}
	}
	return value
}

func successAttr(err error) metric.MeasurementOption {
	if err != nil {
		return failure
	}
	return success
}
