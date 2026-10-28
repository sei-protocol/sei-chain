// Package undo is a Pebble state store that keeps recent history as a bucketed undo log.
//
// When block h changes a key, the store records the value the key held before h, under
//
//	bucket(h) | key | h        with bucket(h) = h / bucketSize
//
// and nothing else: the value after the latest block lives in the state commit store, which this
// store reads through a LiveStateView. An account's balance, nonce and code hash are one key, and
// its code and each storage slot one key apiece. The state after block T is the first record above
// T, or the current value when there is none. Buckets are contiguous key ranges, so dropping history
// is one Excise per expired bucket rather than a scan and a tombstone per version.
package undo

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	controllertypes "github.com/sei-protocol/sei-chain/sei-db/controller/types"

	"github.com/cockroachdb/pebble/v2"
	"github.com/cockroachdb/pebble/v2/bloom"
	"github.com/cockroachdb/pebble/v2/sstable"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/seilog"
	"go.opentelemetry.io/otel/metric"
	"golang.org/x/sync/errgroup"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	seidbmetrics "github.com/sei-protocol/sei-chain/sei-db/common/metrics"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	pebbledbmetrics "github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
)

var logger = seilog.NewLogger("db", "db-engine", "pebbledb", "undo")

var _ controllertypes.PrunableStore = (*Database)(nil)

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

// LiveStateView is a read-only view of the live state store at one committed block.
// It remains unchanged as the store advances.
type LiveStateView interface {
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

	// lifecycleMu excludes Resume and Close from queue submissions, pruning, opening views and
	// reads, and guards closed.
	lifecycleMu sync.RWMutex
	closed      bool

	// historyMu serializes every change to the served history: a resume, a prune pass and a
	// rollback, each of which moves earliestHeight. It guards prunedBucket, the lowest bucket not
	// yet excised.
	historyMu    sync.Mutex
	prunedBucket uint64

	pending     chan pendingBlock
	writerDone  chan struct{}
	queue       *seidbmetrics.QueueMeter
	stopMetrics func()
}

// pendingBlock is one entry of the write queue: a block, or, with done set, a barrier.
type pendingBlock struct {
	version int64
	changes *gigatypes.StateChangeSet
	current LiveStateView
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

// Resume binds current state as of version and enables block writes. It must succeed once before
// ApplyBlock is called. version must be at or above the log's latest block.
//
// A log behind version is missing the records of the blocks in between, which only their execution
// could supply, so history below version stops being served.
func (db *Database) Resume(version int64, current LiveStateView) error {
	db.lifecycleMu.Lock()
	defer db.lifecycleMu.Unlock()
	if db.closed {
		current.Close()
		return errors.New("undo: resume on a closed store")
	}
	if db.head.Load() != nil {
		current.Close()
		return errors.New("undo: store already resumed")
	}
	height, err := db.resume(version)
	if err != nil {
		current.Close()
		return err
	}
	db.publishHead(height, current)
	return nil
}

// Resumed reports whether a Resume has succeeded and the store is not closed.
func (db *Database) Resumed() bool {
	return db.head.Load() != nil
}

func (db *Database) resume(version int64) (uint64, error) {
	height, err := heightOf(version)
	if err != nil {
		return 0, err
	}
	db.historyMu.Lock()
	defer db.historyMu.Unlock()
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

// ApplyBlock queues block version's change set, whose prior values the store records, and takes
// ownership of the post-block current view. changes is read in the background, so it must not change
// afterwards; nil means the block changed nothing. It panics before a successful Resume or after Close.
func (db *Database) ApplyBlock(version int64, changes *gigatypes.StateChangeSet, current LiveStateView) {
	if err := db.enqueue(pendingBlock{version: version, changes: changes, current: current}); err != nil {
		current.Close()
		panic(err)
	}
}

// WaitForPendingWrites blocks until every block queued so far is applied, including during Close.
func (db *Database) WaitForPendingWrites() {
	done := make(chan struct{})
	if err := db.enqueue(pendingBlock{done: done}); err != nil {
		<-db.writerDone
		return
	}
	<-done
}

// enqueue submits a block or a write barrier to an open store.
func (db *Database) enqueue(block pendingBlock) error {
	db.lifecycleMu.RLock()
	defer db.lifecycleMu.RUnlock()
	if db.closed {
		return errors.New("undo: write queue is closed")
	}
	if block.done == nil && db.head.Load() == nil {
		return errors.New("undo: store is not resumed")
	}
	seidbmetrics.Send(db.queue, db.pending, block)
	return nil
}

func (db *Database) writeInBackground() {
	defer close(db.writerDone)
	for block := range db.pending {
		if block.done != nil {
			close(block.done)
			continue
		}
		if err := db.applyBlock(block.version, block.changes, block.current); err != nil {
			// A block that cannot be written leaves a gap every later read below it would answer wrongly.
			panic(err)
		}
	}
}

// applyBlock writes block version and makes current the view reads fall back to.
func (db *Database) applyBlock(version int64, changes *gigatypes.StateChangeSet, current LiveStateView) error {
	height, err := db.writeBlock(version, changes)
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
func (db *Database) writeBlock(version int64, changes *gigatypes.StateChangeSet) (height uint64, err error) {
	start := time.Now()
	records := recordCount(changes)
	defer func() {
		otelMetrics.applyLatency.Record(context.Background(), time.Since(start).Seconds(), successAttr(err))
		otelMetrics.applyRecords.Record(context.Background(), int64(records))
	}()
	if height, err = heightOf(version); err != nil {
		return 0, err
	}
	// Blocks are contiguous: a skipped block leaves no records, and every read below it would
	// silently answer with a later value.
	if latest := db.latestHeight.Load(); height != latest+1 {
		return 0, fmt.Errorf("undo: block %d does not follow the latest applied block %d", height, latest)
	}
	if err := db.writeRecords(changes, records, height/db.bucketSize, height); err != nil {
		return 0, err
	}
	if err := writeMarker(db.storage, latestVersionKey, height, pebble.NoSync); err != nil {
		return 0, fmt.Errorf("undo: record latest height %d: %w", height, err)
	}
	return height, nil
}

// recordCount returns how many records changes writes: one per account and slot it changed, and one
// more per code change.
func recordCount(changes *gigatypes.StateChangeSet) int {
	if changes == nil {
		return 0
	}
	count := len(changes.Accounts) + len(changes.Storage)
	for _, change := range changes.Accounts {
		if change.CodeChanged() {
			count++
		}
	}
	return count
}

// writeRecords writes a record of the state before the block at height of every account and slot
// changes writes, and of the code of every account whose code it changes; records is how many that
// is, as recordCount reports. A storage clear writes none: it only clears accounts created in the
// same transaction, whose storage was empty.
func (db *Database) writeRecords(changes *gigatypes.StateChangeSet, records int, bucket, height uint64) error {
	if changes == nil {
		return nil
	}
	w := &recordWriter{
		db:     db,
		bucket: bucket,
		height: height,
		size:   max(minBatchRecords, (records+writeParallelism-1)/writeParallelism),
	}
	var account [accountLen]byte
	for _, change := range changes.Accounts {
		w.add(accountRecord, change.Address[:], nil, appendAccount(account[:0], &change.Prior))
		if change.CodeChanged() {
			w.add(codeRecord, change.Address[:], nil, change.Prior.Code)
		}
	}
	for _, change := range changes.Storage {
		w.add(storageRecord, change.Address[:], change.Key[:], nonZero(change.PriorValue[:]))
	}
	return w.finish()
}

// appendAccount appends to dst the value of an account record holding prior.
func appendAccount(dst []byte, prior *gigatypes.AccountState) []byte {
	if prior.IsEmpty() {
		return dst
	}
	var balance [wordLen]byte
	if prior.Balance != nil {
		prior.Balance.FillBytes(balance[:])
	}
	dst = binary.BigEndian.AppendUint64(append(dst, balance[:]...), prior.Nonce)
	if len(prior.Code) != 0 {
		dst = append(dst, crypto.Keccak256(prior.Code)...)
	}
	return dst
}

// nonZero returns value, or nil when it is empty or all zeros.
func nonZero(value []byte) []byte {
	for _, b := range value {
		if b != 0 {
			return value
		}
	}
	return nil
}

// recordWriter stages one block's records into batches of size records, each committed in the
// background once full. Pebble inserts a batch into the memtable on one goroutine, and concurrent
// batches concurrently, so a large block goes out as batches committed in parallel.
type recordWriter struct {
	db             *Database
	bucket, height uint64
	size           int

	batch  *pebble.Batch
	staged int
	err    error
	g      errgroup.Group
}

// add stages the record of recordType | addr | slot holding value, where an empty value is a key that
// did not exist.
func (w *recordWriter) add(recordType byte, addr, slot, value []byte) {
	if w.err != nil {
		return
	}
	if w.batch == nil {
		w.batch = w.db.storage.NewBatch()
	}
	var buf [storageKeyLen]byte
	buf[0] = recordType
	key := buf[:1+copy(buf[1:], addr)+copy(buf[1+len(addr):], slot)]
	d := w.batch.SetDeferred(recordKeyLen(key), len(value))
	encodeRecordKey(d.Key, w.bucket, key, w.height)
	copy(d.Value, value)
	if err := d.Finish(); err != nil {
		w.err = fmt.Errorf("undo: stage record: %w", err)
		return
	}
	if w.staged++; w.staged == w.size {
		w.commit()
	}
}

// commit commits the staged batch in the background.
func (w *recordWriter) commit() {
	b, height := w.batch, w.height
	w.batch, w.staged = nil, 0
	w.g.Go(func() error {
		defer func() { _ = b.Close() }()
		if err := b.Commit(pebble.NoSync); err != nil {
			return fmt.Errorf("undo: commit block %d: %w", height, err)
		}
		return nil
	})
}

// finish commits what is still staged and waits for every batch.
func (w *recordWriter) finish() error {
	if w.batch != nil {
		if w.err != nil {
			_ = w.batch.Close()
		} else {
			w.commit()
		}
	}
	return errors.Join(w.err, w.g.Wait())
}

// head is a current-state view and the height it describes. The store holds one reference and
// every read in flight holds another; the last one released closes the view.
type head struct {
	height uint64
	view   LiveStateView
	refs   atomic.Int64
}

func (h *head) release() {
	if h.refs.Add(-1) == 0 {
		h.view.Close()
	}
}

// publishHead makes current, as of height, the view reads fall back to.
func (db *Database) publishHead(height uint64, current LiveStateView) {
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

// View reads the store at one height. It pins the buckets it reads, holding back pruning, so it must
// be closed promptly. Its reads fail once the database is closed.
type View struct {
	db        *Database
	height    uint64
	closeOnce sync.Once

	// closed records whether Close has been called.
	closed utils.CloseMarker[View]
}

// OpenView returns a view of the state after block version, and false when the store is closed or
// does not serve version.
func (db *Database) OpenView(version int64) (*View, bool) {
	db.lifecycleMu.RLock()
	defer db.lifecycleMu.RUnlock()
	if db.closed {
		return nil, false
	}
	height, err := heightOf(version)
	if err != nil {
		return nil, false
	}
	if h := db.head.Load(); h == nil || height > h.height || !db.pinHeight(height) {
		return nil, false
	}
	v := &View{db: db, height: height}
	v.closed = utils.MustClose(v, "undo view")
	return v, true
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

// Close releases the view's pin. Idempotent.
func (v *View) Close() {
	v.closeOnce.Do(func() {
		v.closed.Close(v)
		v.db.unpinHeight(v.height)
	})
}

// Get returns a supported EVM key's value at the view's height, nil when it did not exist.
// Unsupported or malformed keys, and reads after the database is closed, return an error.
func (v *View) Get(key []byte) (_ []byte, err error) {
	kind, stripped := keys.ParseEVMKey(key)
	history, ok := historyKey(kind, stripped)
	if !ok {
		return nil, fmt.Errorf("undo: unsupported or malformed EVM key %x", key)
	}
	v.db.lifecycleMu.RLock()
	defer v.db.lifecycleMu.RUnlock()
	if v.db.closed {
		return nil, errors.New("undo: read on a closed store")
	}
	start := time.Now()
	probes := 0
	defer func() {
		otelMetrics.getLatency.Record(context.Background(), time.Since(start).Seconds(), successAttr(err))
		otelMetrics.getProbes.Record(context.Background(), int64(probes))
	}()
	// The newest head answers a read at any height below it the same way, since every record up to
	// it is written before it is published.
	h := v.db.acquireHead()
	defer h.release()
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
		seekKey = appendRecordKey(seekKey[:0], bucket, history, v.height+1)
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
		return valueIn(kind, itr.Value())
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

// raiseEarliest makes height the lowest served height when it is above the current one. The caller
// must hold historyMu.
func (db *Database) raiseEarliest(height uint64) error {
	if height <= db.earliestHeight.Load() {
		return nil
	}
	return db.setEarliestLocked(height)
}

// lowerEarliest makes height the lowest served height when it is below the current one. A rollback
// below the earliest height leaves no record above height, and a read at height needs none. The
// caller must hold historyMu.
func (db *Database) lowerEarliest(height uint64) error {
	if height >= db.earliestHeight.Load() {
		return nil
	}
	return db.setEarliestLocked(height)
}

// setEarliestLocked durably records height as the lowest served height, then publishes it.
// The caller must hold historyMu.
func (db *Database) setEarliestLocked(height uint64) error {
	if err := writeMarker(db.storage, earliestVersionKey, height, pebble.Sync); err != nil {
		return fmt.Errorf("undo: record earliest height %d: %w", height, err)
	}
	db.mu.Lock()
	defer db.mu.Unlock()
	db.earliestHeight.Store(height)
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
// reads, which a later call excises. It fails once the store is closed.
func (db *Database) PruneHistory(blockNumber uint64) error {
	db.lifecycleMu.RLock()
	defer db.lifecycleMu.RUnlock()
	if db.closed {
		return errors.New("undo: prune on a closed store")
	}
	head := db.latestHeight.Load()
	if blockNumber == 0 || head == 0 {
		return nil
	}
	db.historyMu.Lock()
	defer db.historyMu.Unlock()
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
	db.historyMu.Lock()
	defer db.historyMu.Unlock()
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

// Close drains the write queue, releases the current-state view and closes Pebble. Views still open
// fail their reads from then on. Idempotent.
func (db *Database) Close() error {
	db.lifecycleMu.Lock()
	defer db.lifecycleMu.Unlock()
	if db.closed {
		return nil
	}
	db.closed = true
	close(db.pending)
	<-db.writerDone
	db.stopMetrics()
	if h := db.head.Swap(nil); h != nil {
		h.release()
	}
	return db.storage.Close()
}

// valueIn returns a copy of the value an EVM key of kind holds in a record of its history, nil when
// the key held none.
func valueIn(kind keys.EVMKeyKind, record []byte) ([]byte, error) {
	if len(record) == 0 {
		return nil, nil
	}
	switch kind {
	case keys.EVMKeyBalance, keys.EVMKeyNonce, keys.EVMKeyCodeHash:
		field, err := accountField(kind, record)
		if err != nil {
			return nil, err
		}
		// A zero field reads as absent.
		record = nonZero(field)
	}
	return bytes.Clone(record), nil
}

// accountField returns the part of an account record's value holding the field of kind, empty for a
// code hash the account did not hold.
func accountField(kind keys.EVMKeyKind, account []byte) ([]byte, error) {
	if len(account) != accountNoCodeLen && len(account) != accountLen {
		return nil, fmt.Errorf("undo: account record has %d bytes", len(account))
	}
	switch kind {
	case keys.EVMKeyBalance:
		return account[:wordLen], nil
	case keys.EVMKeyNonce:
		return account[wordLen:accountNoCodeLen], nil

	default:
		return account[accountNoCodeLen:], nil
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
