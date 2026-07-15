package flatkv

import (
	"context"
	"errors"
	"fmt"
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

// dbWorker owns a single PebbleDB and its LtHash accumulation. It reads
// key/value pairs from its channel, buffers them into a PebbleDB batch,
// and flushes (commit + LtHash update) when the buffer is full or the
// channel is closed.
type dbWorker struct {
	ctx     context.Context
	dir     string
	db      seidbtypes.KeyValueDB
	ch      chan rawKVPair
	batch   seidbtypes.Batch
	ltPairs []lthash.KVPairWithLastValue
	ltHash  *lthash.LtHash
	// moduleLtHash tracks the per-module decomposition of ltHash, keyed by the
	// "<module>/" physical-key prefix. Its homomorphic sum equals ltHash.
	moduleLtHash map[string]*lthash.LtHash
	// moduleStats tracks the per-module key-count / byte totals accumulated
	// alongside moduleLtHash, keyed the same way. Mirrors the live commit path
	// so an imported store carries identical per-module stats metadata.
	moduleStats map[string]lthash.ModuleStats
	// calc is the shared lattice-hash calculator. Its worker pool is used to
	// distribute this worker's flushed pairs and compute per-module deltas —
	// the same path the live commit uses (see HashCalculator.ComputeModuleDeltas).
	calc    *lthash.HashCalculator
	flushes int64
	pairs   int64
}

func newDBWorker(ctx context.Context, dir string, db seidbtypes.KeyValueDB, calc *lthash.HashCalculator, ltHash *lthash.LtHash, moduleLtHash map[string]*lthash.LtHash, moduleStats map[string]lthash.ModuleStats) *dbWorker {
	return &dbWorker{
		ctx:          ctx,
		dir:          dir,
		db:           db,
		ch:           make(chan rawKVPair, workerChanSize),
		batch:        db.NewBatch(),
		ltPairs:      make([]lthash.KVPairWithLastValue, 0, importBatchSize),
		ltHash:       ltHash,
		moduleLtHash: moduleLtHash,
		moduleStats:  moduleStats,
		calc:         calc,
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
				return w.flush()
			}
			if err := w.batch.Set(kv.Key, kv.Value); err != nil {
				return fmt.Errorf("%s set: %w", w.dir, err)
			}
			w.ltPairs = append(w.ltPairs, lthash.KVPairWithLastValue{
				Key:   kv.Key,
				Value: kv.Value,
			})
			if len(w.ltPairs) >= importBatchSize {
				if err := w.flush(); err != nil {
					return err
				}
			}
		case <-done:
			return nil
		}
	}
}

// flush commits the current PebbleDB batch and updates the running LtHash.
func (w *dbWorker) flush() (err error) {
	if len(w.ltPairs) == 0 {
		return nil
	}
	if hook := flushHookForTest.Load(); hook != nil {
		(*hook)(w.dir)
	}
	start := time.Now()
	pairCount := len(w.ltPairs)
	defer func() {
		otelMetrics.ImportWorkerFlushLatency.Record(w.ctx, secondsSince(start),
			metric.WithAttributes(dbAttr(w.dir), successAttr(err)))
	}()

	// Per-module hashes are the primitive: distribute this batch's pairs across
	// the shared lattice-hash pool to compute each touched module's delta (the
	// same path the live commit uses), fold each delta into the running
	// per-module hash, then derive the per-DB root as their homomorphic sum.
	// This mirrors the live commit path so an imported store carries the same
	// per-module metadata and identical per-DB root a natively-committed store
	// would — and it lets a single large DB's batch fan out across every core
	// instead of being pinned to one import worker goroutine.
	deltas, err := w.calc.ComputeModuleDeltas([]lthash.DBPairs{{Dir: w.dir, Pairs: w.ltPairs}})
	if err != nil {
		return fmt.Errorf("%s compute module deltas: %w", w.dir, err)
	}
	for key, delta := range deltas {
		acc := w.moduleLtHash[key.Module]
		if acc == nil {
			acc = lthash.New()
			w.moduleLtHash[key.Module] = acc
		}
		acc.MixIn(delta.Hash)
		w.moduleStats[key.Module] = w.moduleStats[key.Module].Add(
			lthash.ModuleStats{KeyCount: delta.KeyCount, Bytes: delta.Bytes})
	}
	w.ltHash = lthash.SumModuleHashes(w.moduleLtHash)

	syncOpt := seidbtypes.WriteOptions{Sync: false}
	if err := w.batch.Commit(syncOpt); err != nil {
		return fmt.Errorf("%s commit: %w", w.dir, err)
	}

	addImportKVPairs(w.ctx, w.dir, pairCount)
	w.flushes++
	w.pairs += int64(pairCount)
	w.batch = w.db.NewBatch()
	w.ltPairs = w.ltPairs[:0]
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
		done:     make(chan struct{}),
	}

	for _, ndb := range store.namedDataDBs() {
		w := newDBWorker(
			store.ctx,
			ndb.dir,
			ndb.db,
			store.ltCalc,
			store.perDBWorkingLtHash[ndb.dir],
			cloneModuleHashes(store.perDBModuleWorkingLtHash[ndb.dir]),
			cloneModuleStats(store.perDBModuleWorkingStats[ndb.dir]),
		)
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
		imp.wg.Wait()

		if err = imp.getErr(); err != nil {
			return
		}

		for _, w := range imp.workers {
			imp.store.perDBWorkingLtHash[w.dir] = w.ltHash
			imp.store.perDBModuleWorkingLtHash[w.dir] = w.moduleLtHash
			imp.store.perDBModuleWorkingStats[w.dir] = w.moduleStats
		}

		if err = imp.store.FinalizeImport(imp.version); err != nil {
			err = fmt.Errorf("failed to finalize import: %w", err)
			return
		}

		// Write a snapshot so the imported data survives store reopen / restart.
		// Import bypasses the WAL, so without a snapshot the next LoadVersion
		// would clone from the pre-import snapshot and lose all imported data.
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
