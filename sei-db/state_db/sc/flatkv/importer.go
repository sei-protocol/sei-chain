package flatkv

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	seidbtypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
	"go.opentelemetry.io/otel/metric"
)

const (
	importBatchSize = 20000
	ingestChanSize  = 1 << 16 // 64K buffered main channel
	workerChanSize  = 1024    // per-DB worker channel
)

var _ types.Importer = (*KVImporter)(nil)

// flushHookForTest, when set by tests in this package, is invoked at the
// start of every dbWorker flush. It exists solely for whitebox tests of
// the backpressure / fail-fast paths (see importer_test.go) and loads
// nil in production.
//
// Stored via atomic.Pointer (rather than a bare package-level func) so
// that any future test that calls t.Parallel() and concurrently swaps
// the hook does not race with worker goroutines reading it. The hot-path
// cost is a single atomic load per flush, equivalent to an aligned
// pointer read.
var flushHookForTest atomic.Pointer[func(string)]

// hashPool computes the LtHash for a single PebbleDB using a pool of
// single-threaded hasher goroutines. dbWorker flushes hand batches of
// already-committed key/value pairs to queue; each hasher pops batches and folds
// them into its own local LtHash. After the queue is drained, combine() sums the
// per-hasher locals into the DB's LtHash.
//
// Each hasher computes its batch single-threaded (lthash.ComputeDeltaSerial), so
// the pool itself is the unit of parallelism. This keeps hashing off the
// dbWorker's critical path: the worker only commits the PebbleDB batch and then
// hands the pairs off, so hashing overlaps with subsequent commits. Because
// MixIn is commutative and associative, it does not matter which hasher folds
// which batch, nor the order in which the locals are later combined.
type hashPool struct {
	queue  chan []lthash.KVPairWithLastValue
	wg     sync.WaitGroup
	locals []*lthash.LtHash
}

// newHashPool starts numHashers hasher goroutines, each folding popped batches
// into its own local LtHash until queue is closed (normal drain) or done fires
// (error fast-path).
func newHashPool(numHashers int, done <-chan struct{}) *hashPool {
	if numHashers < 1 {
		numHashers = 1
	}
	p := &hashPool{
		queue:  make(chan []lthash.KVPairWithLastValue, 2*numHashers),
		locals: make([]*lthash.LtHash, numHashers),
	}
	for i := 0; i < numHashers; i++ {
		local := lthash.New()
		p.locals[i] = local
		p.wg.Add(1)
		go func(local *lthash.LtHash) {
			defer p.wg.Done()
			for {
				select {
				case batch, ok := <-p.queue:
					if !ok {
						return
					}
					local.MixIn(lthash.ComputeDeltaSerial(batch))
				case <-done:
					return
				}
			}
		}(local)
	}
	return p
}

// combine sums the per-hasher local hashes into a single LtHash. Call only after
// the queue is closed and wg has been waited on.
func (p *hashPool) combine() *lthash.LtHash {
	h := lthash.New()
	for _, l := range p.locals {
		h.MixIn(l)
	}
	return h
}

// dbWorker owns a single PebbleDB. It reads key/value pairs from its channel,
// buffers them into a PebbleDB batch, and on flush commits the batch and hands
// the just-committed pairs to its hashPool. Hashing is no longer done inline.
type dbWorker struct {
	ctx     context.Context
	dir     string
	db      seidbtypes.KeyValueDB
	ch      chan rawKVPair
	batch   seidbtypes.Batch
	pending []lthash.KVPairWithLastValue
	pool    *hashPool
	flushes int64
	pairs   int64
}

func newDBWorker(ctx context.Context, dir string, db seidbtypes.KeyValueDB, pool *hashPool) *dbWorker {
	return &dbWorker{
		ctx:     ctx,
		dir:     dir,
		db:      db,
		ch:      make(chan rawKVPair, workerChanSize),
		batch:   db.NewBatch(),
		pending: make([]lthash.KVPairWithLastValue, 0, importBatchSize),
		pool:    pool,
	}
}

// run drains the worker channel until closed, flushing whenever the
// buffer reaches importBatchSize. If done fires, the worker abandons
// remaining work and exits immediately.
func (w *dbWorker) run(done <-chan struct{}) error {
	defer func() {
		if w.batch != nil {
			_ = w.batch.Close()
		}
	}()
	for {
		select {
		case kv, ok := <-w.ch:
			if !ok {
				return w.flush(done)
			}
			if err := w.batch.Set(kv.Key, kv.Value); err != nil {
				return fmt.Errorf("%s set: %w", w.dir, err)
			}
			w.pending = append(w.pending, lthash.KVPairWithLastValue{
				Key:   kv.Key,
				Value: kv.Value,
			})
			if len(w.pending) >= importBatchSize {
				if err := w.flush(done); err != nil {
					return err
				}
			}
		case <-done:
			return nil
		}
	}
}

// flush commits the current PebbleDB batch and hands the just-committed pairs to
// the hashPool, which computes their LtHash contribution asynchronously. The
// pool takes ownership of the handed-off slice, so a fresh one is allocated for
// the next batch.
func (w *dbWorker) flush(done <-chan struct{}) (err error) {
	if len(w.pending) == 0 {
		return nil
	}
	if hook := flushHookForTest.Load(); hook != nil {
		(*hook)(w.dir)
	}
	start := time.Now()
	pairCount := len(w.pending)
	defer func() {
		otelMetrics.ImportWorkerFlushLatency.Record(w.ctx, secondsSince(start),
			metric.WithAttributes(dbAttr(w.dir), successAttr(err)))
	}()

	syncOpt := seidbtypes.WriteOptions{Sync: false}
	if err := w.batch.Commit(syncOpt); err != nil {
		return fmt.Errorf("%s commit: %w", w.dir, err)
	}

	// Hand the committed batch to the hasher pool. The pool owns the slice now.
	select {
	case w.pool.queue <- w.pending:
	case <-done:
		// Aborting: the pool is being torn down; drop this batch. The import
		// is not finalized, so the committed-but-unhashed pairs are discarded.
		return nil
	}

	addImportKVPairs(w.ctx, w.dir, pairCount)
	w.flushes++
	w.pairs += int64(pairCount)
	w.batch = w.db.NewBatch()
	w.pending = make([]lthash.KVPairWithLastValue, 0, importBatchSize)
	return nil
}

// KVImporter implements types.Importer using a channel-based pipeline with
// per-DB worker goroutines. AddNode sends pairs into a buffered channel; a
// dispatcher goroutine routes each pair to the correct DB worker; each worker
// independently batches writes and computes LtHash.
type KVImporter struct {
	store   *CommitStore
	version int64

	ingestCh chan rawKVPair
	workers  map[seidbtypes.KeyValueDB]*dbWorker
	pools    map[string]*hashPool // keyed by DB dir
	wg       sync.WaitGroup

	// done is closed on the first pipeline error so that AddNode,
	// the dispatcher, and all workers bail immediately.
	done       chan struct{}
	closeOnce  sync.Once
	firstErr   atomic.Pointer[error]
	finishOnce sync.Once
	finishErr  error
}

func NewKVImporter(store *CommitStore, version int64) types.Importer {
	imp := &KVImporter{
		store:    store,
		version:  version,
		ingestCh: make(chan rawKVPair, ingestChanSize),
		workers:  make(map[seidbtypes.KeyValueDB]*dbWorker, 4),
		pools:    make(map[string]*hashPool, 4),
		done:     make(chan struct{}),
	}

	// One hasher per hardware thread, per DB pool. The pools for the small DBs
	// (code/legacy) mostly idle on an empty queue while the dominant DB's pool
	// saturates the cores. Import runs from a freshly reset store, so each pool
	// starts from a zero hash.
	numHashers := runtime.NumCPU()
	for _, ndb := range store.namedDataDBs() {
		pool := newHashPool(numHashers, imp.done)
		imp.pools[ndb.dir] = pool
		w := newDBWorker(store.ctx, ndb.dir, ndb.db, pool)
		imp.workers[ndb.db] = w
	}

	for _, w := range imp.workers {
		imp.wg.Add(1)
		go func(w *dbWorker) {
			defer imp.wg.Done()
			if err := w.run(imp.done); err != nil {
				imp.setErr(err)
			}
		}(w)
	}

	imp.wg.Add(1)
	go func() {
		defer imp.wg.Done()
		imp.dispatch()
	}()

	return imp
}

// dispatch reads from the main ingest channel, routes each pair, and sends
// it to the appropriate worker channel. It exits when ingestCh is closed
// (normal shutdown) or done fires (error fast-path).
func (imp *KVImporter) dispatch() {
	defer func() {
		for _, w := range imp.workers {
			close(w.ch)
		}
	}()

	for {
		select {
		case kv, ok := <-imp.ingestCh:
			if !ok {
				return
			}
			db, err := imp.store.routePhysicalKey(kv.Key)
			if err != nil {
				imp.setErr(fmt.Errorf("route key: %w", err))
				return
			}
			select {
			case imp.workers[db].ch <- kv:
			case <-imp.done:
				return
			}
		case <-imp.done:
			return
		}
	}
}

func (imp *KVImporter) setErr(err error) {
	if imp.firstErr.CompareAndSwap(nil, &err) {
		imp.closeOnce.Do(func() { close(imp.done) })
	}
}

func (imp *KVImporter) getErr() error {
	p := imp.firstErr.Load()
	if p == nil {
		return nil
	}
	return *p
}

func (imp *KVImporter) Err() error {
	return imp.getErr()
}

func (imp *KVImporter) AddModule(_ string) error {
	return nil
}

func (imp *KVImporter) AddNode(node *types.SnapshotNode) {
	if node.Height != 0 || node.Key == nil || node.Version != imp.version {
		return
	}
	select {
	case imp.ingestCh <- rawKVPair{Key: node.Key, Value: node.Value}:
	case <-imp.done:
	}
}

// Abort tears down the worker pipeline without finalizing the import.
// It records reason as the first pipeline error (so any in-flight worker
// also bails fast) and then runs Close, which observes the non-nil error
// and skips FinalizeImport / WriteSnapshot. The on-disk FlatKV directory
// is left at its pre-import committed version, allowing the operator to
// retry without --force.
//
// Use this when an external error (context cancellation, exporter
// failure, translator failure, etc.) makes the in-progress import
// unsafe to commit. Abort is idempotent and safe to interleave with
// Close: whichever runs first wins; later calls are no-ops.
func (imp *KVImporter) Abort(reason error) error {
	if reason == nil {
		reason = errors.New("flatkv import aborted")
	}
	imp.setErr(reason)
	return imp.Close()
}

// Close is idempotent: the first call drains workers, finalizes the import,
// and writes a snapshot; subsequent calls just return the cached result.
// Idempotency is required because the import-from-memiavl tool may invoke
// Close on both the success and error paths.
//
// If the first pipeline error has already been recorded (either by a
// worker or by Abort), Close skips FinalizeImport / WriteSnapshot so the
// store stays at its pre-import version.
func (imp *KVImporter) Close() error {
	imp.finishOnce.Do(func() {
		start := time.Now()
		var err error
		defer func() {
			otelMetrics.ImportLatency.Record(imp.store.ctx, secondsSince(start),
				metric.WithAttributes(successAttr(err)))
			flushes, pairs := imp.importStats()
			if err == nil {
				otelMetrics.CurrentVersion.Record(imp.store.ctx, imp.store.committedVersion)
				otelMetrics.CurrentSnapshotHeight.Record(imp.store.ctx, imp.store.committedVersion)
				logger.Info("FlatKV import complete",
					"version", imp.version,
					"flushes", flushes,
					"pairs", pairs,
					"elapsed", time.Since(start))
			} else {
				logger.Error("FlatKV import failed",
					"version", imp.version,
					"flushes", flushes,
					"pairs", pairs,
					"elapsed", time.Since(start),
					"err", err)
			}
			imp.finishErr = err
		}()

		close(imp.ingestCh)
		// Wait for the dispatcher and dbWorkers: once they return, every pair
		// has been committed to PebbleDB and handed to its hasher pool.
		imp.wg.Wait()

		// No more batches will be enqueued; close the pool queues and wait for
		// the hashers to finish folding everything they were handed.
		for _, pool := range imp.pools {
			close(pool.queue)
		}
		for _, pool := range imp.pools {
			pool.wg.Wait()
		}

		if err = imp.getErr(); err != nil {
			return
		}

		// Combine each pool's per-hasher local hashes into the DB's LtHash.
		for dir, pool := range imp.pools {
			imp.store.perDBWorkingLtHash[dir] = pool.combine()
		}

		if err = imp.store.FinalizeImport(imp.version); err != nil {
			err = fmt.Errorf("failed to finalize import: %w", err)
			return
		}

		// Flush all data DB memtables to SSTs before checkpointing. This is
		// REQUIRED for correctness when a bulk-import profile disables the
		// per-DB Pebble WAL: WriteSnapshot checkpoints via pebble.Checkpoint,
		// which can only recover unflushed memtable data from a WAL. Without a
		// WAL, anything still in the memtable (the trailing import batch and the
		// LocalMeta just written by FinalizeImport) would be lost from the
		// snapshot. With the WAL enabled this flush is a cheap no-op-ish safety
		// step. Must run after FinalizeImport so its LocalMeta writes are flushed.
		if err = imp.store.flushAllDBs(); err != nil {
			err = fmt.Errorf("failed to flush data DBs before snapshot: %w", err)
			return
		}

		// Write a snapshot so the imported data survives store reopen / restart.
		// Import bypasses the changelog, so without a snapshot the next
		// LoadVersion would clone from the pre-import snapshot and lose all
		// imported data.
		if err = imp.store.WriteSnapshot(""); err != nil {
			err = fmt.Errorf("failed to import when writing snapshot: %w", err)
			return
		}
	})

	return imp.finishErr
}

func (imp *KVImporter) importStats() (flushes int64, pairs int64) {
	for _, w := range imp.workers {
		flushes += w.flushes
		pairs += w.pairs
	}
	return flushes, pairs
}
