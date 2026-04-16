package flatkv

import (
	"fmt"
	"sync"
	"sync/atomic"

	seidbtypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
	iavl "github.com/sei-protocol/sei-chain/sei-iavl/proto"
)

const (
	importBatchSize = 20000
	ingestChanSize  = 1 << 16 // 64K buffered main channel
	workerChanSize  = 1024    // per-DB worker channel
)

var _ types.Importer = (*KVImporter)(nil)

// dbWorker owns a single PebbleDB and its LtHash accumulation. It reads
// key/value pairs from its channel, buffers them into a PebbleDB batch,
// and flushes (commit + LtHash update) when the buffer is full or the
// channel is closed.
type dbWorker struct {
	dir     string
	db      seidbtypes.KeyValueDB
	ch      chan rawKVPair
	batch   seidbtypes.Batch
	ltPairs []lthash.KVPairWithLastValue
	ltHash  *lthash.LtHash
}

func newDBWorker(dir string, db seidbtypes.KeyValueDB, ltHash *lthash.LtHash) *dbWorker {
	return &dbWorker{
		dir:     dir,
		db:      db,
		ch:      make(chan rawKVPair, workerChanSize),
		batch:   db.NewBatch(),
		ltPairs: make([]lthash.KVPairWithLastValue, 0, importBatchSize),
		ltHash:  ltHash,
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
func (w *dbWorker) flush() error {
	if len(w.ltPairs) == 0 {
		return nil
	}

	// TODO:In theory, we could offload lattice hash calculation to a work pool and get parallelism between DB operations and hash calculations. Cryptosim performance makes me think we could probably get a 2-3x speedup from this, assuming receiving data from the network isn't the bottleneck.
	newHash, _ := lthash.ComputeLtHash(w.ltHash, w.ltPairs)
	w.ltHash = newHash

	syncOpt := seidbtypes.WriteOptions{Sync: false}
	if err := w.batch.Commit(syncOpt); err != nil {
		return fmt.Errorf("%s commit: %w", w.dir, err)
	}

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
	done      chan struct{}
	closeOnce sync.Once
	firstErr  atomic.Pointer[error]
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
			ndb.dir,
			ndb.db,
			store.perDBWorkingLtHash[ndb.dir],
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

func (imp *KVImporter) Close() error {
	close(imp.ingestCh)
	imp.wg.Wait()

	if err := imp.getErr(); err != nil {
		return err
	}

	for _, w := range imp.workers {
		imp.store.perDBWorkingLtHash[w.dir] = w.ltHash
	}

	if err := imp.store.FinalizeImport(imp.version); err != nil {
		return fmt.Errorf("failed to finalize import: %w", err)
	}

	// Write a snapshot so the imported data survives store reopen / restart.
	// Import bypasses the WAL, so without a snapshot the next LoadVersion
	// would clone from the pre-import snapshot and lose all imported data.
	if err := imp.store.WriteSnapshot(""); err != nil {
		return fmt.Errorf("failed to import when writing snapshot: %w", err)
	}

	return nil
}
