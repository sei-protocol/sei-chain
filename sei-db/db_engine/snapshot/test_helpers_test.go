package snapshot

import (
	"bytes"
	"context"
	"errors"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	dbm "github.com/tendermint/tm-db"

	"github.com/stretchr/testify/require"

	errorutils "github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/common/threading"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// testDB is a minimal in-memory types.KeyValueDB for unit tests. It is safe for concurrent use.
//
// Behavior knobs:
//   - getErr: if non-nil, Get returns this error instead of consulting the store.
//   - getErrKeys: per-key variant of getErr; a Get of a listed key returns its error.
//   - commitErr: if non-nil, batch Commit returns this error instead of applying.
//   - commitBlock: if non-nil, every batch Commit blocks until the channel is closed (or receives a
//     value). Useful for stalling the flusher to exercise lifecycle backpressure/ordering.
type testDB struct {
	mu          sync.RWMutex
	store       map[string][]byte
	getCalls    atomic.Int64
	commitCount atomic.Int64
	// Incremented when a batch Commit is entered, before it blocks on commitBlock. Lets tests
	// deterministically detect that the flusher is stalled inside a commit.
	commitEntered atomic.Int64
	getErr        error
	getErrKeys    map[string]error
	commitErr     error
	commitBlock   chan struct{}
	getGate       chan struct{}
	closed        atomic.Bool
	// Batch lifecycle counters: batchesCreated increments in NewBatch, batchesClosed on a
	// batch's first Close. Lets tests assert every created batch is released (types.Batch
	// requires Close even after a successful Commit).
	batchesCreated atomic.Int64
	batchesClosed  atomic.Int64
}

func newTestDB(seed map[string][]byte) *testDB {
	m := make(map[string][]byte, len(seed))
	for k, v := range seed {
		m[k] = cloneBytes(v)
	}
	return &testDB{store: m}
}

func (d *testDB) Get(key []byte) ([]byte, error) {
	d.getCalls.Add(1)
	if d.getGate != nil {
		<-d.getGate
	}
	if d.getErr != nil {
		return nil, d.getErr
	}
	if err, ok := d.getErrKeys[string(key)]; ok {
		return nil, err
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	v, ok := d.store[string(key)]
	if !ok {
		return nil, errorutils.ErrNotFound
	}
	return cloneBytes(v), nil
}

func (d *testDB) BatchGet(keys map[string]types.BatchGetResult) error {
	for k := range keys {
		v, err := d.Get([]byte(k))
		switch {
		case err == nil:
			keys[k] = types.BatchGetResult{Value: v}
		case errors.Is(err, errorutils.ErrNotFound):
			keys[k] = types.BatchGetResult{}
		default:
			keys[k] = types.BatchGetResult{Error: err}
		}
	}
	return nil
}

func (d *testDB) Set(key, value []byte, _ types.WriteOptions) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.store[string(key)] = cloneBytes(value)
	return nil
}

func (d *testDB) Delete(key []byte, _ types.WriteOptions) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.store, string(key))
	return nil
}

// NewIter returns a snapshot-at-creation ascending iterator: it captures a sorted copy of the store
// under the read lock, so concurrent mutations after the call do not affect iteration. Honors
// IterOptions bounds (lower inclusive, upper exclusive).
func (d *testDB) NewIter(opts *types.IterOptions) (dbm.Iterator, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var lower, upper []byte
	reverse := false
	if opts != nil {
		lower = opts.LowerBound
		upper = opts.UpperBound
		reverse = opts.Reverse
	}

	pairs := make([]kvPair, 0, len(d.store))
	for k, v := range d.store {
		kb := []byte(k)
		if lower != nil && bytes.Compare(kb, lower) < 0 {
			continue
		}
		if upper != nil && bytes.Compare(kb, upper) >= 0 {
			continue
		}
		pairs = append(pairs, kvPair{key: kb, value: cloneBytes(v)})
	}
	// A reverse iterator yields keys largest-first, so the double must too: the engine merges this
	// stream against a descending override list, and an ascending DB side would silently corrupt the
	// merge rather than fail.
	sort.Slice(pairs, func(i, j int) bool {
		cmp := bytes.Compare(pairs[i].key, pairs[j].key)
		if reverse {
			return cmp > 0
		}
		return cmp < 0
	})
	return &fakeDBIter{pairs: pairs, start: lower, end: upper}, nil
}

func (d *testDB) NewBatch() types.Batch {
	d.batchesCreated.Add(1)
	return &testBatch{db: d}
}

func (d *testDB) Flush() error { return nil }

func (d *testDB) Close() error {
	d.closed.Store(true)
	return nil
}

func (d *testDB) isClosed() bool { return d.closed.Load() }

func (d *testDB) has(key string) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	_, ok := d.store[key]
	return ok
}

func (d *testDB) get(key string) ([]byte, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	v, ok := d.store[key]
	return v, ok
}

// fakeDBIter is a forward-only dbm.Iterator over a pre-sorted, pre-copied slice. It is positioned at
// the first element on construction, matching the tm-db contract the snapshot iterator relies on.
type fakeDBIter struct {
	pairs []kvPair
	idx   int
	start []byte
	end   []byte
}

func (it *fakeDBIter) Domain() (start []byte, end []byte) { return it.start, it.end }
func (it *fakeDBIter) Valid() bool                        { return it.idx < len(it.pairs) }
func (it *fakeDBIter) Next()                              { it.idx++ }
func (it *fakeDBIter) Key() []byte                        { return it.pairs[it.idx].key }
func (it *fakeDBIter) Value() []byte                      { return it.pairs[it.idx].value }
func (it *fakeDBIter) Error() error                       { return nil }
func (it *fakeDBIter) Close() error                       { return nil }

type testBatchOp struct {
	key    []byte
	value  []byte
	delete bool
}

type testBatch struct {
	db     *testDB
	ops    []testBatchOp
	closed bool
}

func (b *testBatch) Set(key, value []byte) error {
	b.ops = append(b.ops, testBatchOp{key: cloneBytes(key), value: cloneBytes(value)})
	return nil
}

func (b *testBatch) Delete(key []byte) error {
	b.ops = append(b.ops, testBatchOp{key: cloneBytes(key), delete: true})
	return nil
}

func (b *testBatch) Commit(_ types.WriteOptions) error {
	b.db.commitEntered.Add(1)
	if b.db.commitBlock != nil {
		<-b.db.commitBlock
	}
	if b.db.commitErr != nil {
		return b.db.commitErr
	}
	b.db.commitCount.Add(1)
	b.db.mu.Lock()
	defer b.db.mu.Unlock()
	for _, op := range b.ops {
		if op.delete {
			delete(b.db.store, string(op.key))
		} else {
			b.db.store[string(op.key)] = op.value
		}
	}
	b.ops = nil
	return nil
}

// Len mirrors pebble's wire encoding (12-byte header, then per op: a kind byte, uvarint lengths,
// and payload bytes) so tests exercise the same bytes-based batch splitting as production. See the
// byte-unit contract on types.Batch.Len.
func (b *testBatch) Len() int {
	size := 12
	for _, op := range b.ops {
		size += 1 + uvarintLen(uint64(len(op.key))) + len(op.key)
		if !op.delete {
			size += uvarintLen(uint64(len(op.value))) + len(op.value)
		}
	}
	return size
}

// uvarintLen returns the encoded length of x as a uvarint.
func uvarintLen(x uint64) int {
	n := 1
	for x >= 0x80 {
		x >>= 7
		n++
	}
	return n
}
func (b *testBatch) Reset() { b.ops = nil }
func (b *testBatch) Close() error {
	if !b.closed {
		b.closed = true
		b.db.batchesClosed.Add(1)
	}
	return nil
}

// --- engine construction helpers ---

// newTestConfig returns a config suitable for unit tests. Metrics are disabled; overhead is set to 1
// so dbCache size accounting is easy to reason about.
func newTestConfig(shardCount, maxSize uint64) *SnapshotEngineConfig {
	c := DefaultTestSnapshotEngineConfig()
	c.ShardCount = shardCount
	c.MaxSize = maxSize
	c.EstimatedOverheadPerEntry = 1
	return c
}

func newTestEngine(t *testing.T, seed map[string][]byte, shardCount, maxSize uint64) (SnapshotEngine, *testDB) {
	t.Helper()
	db := newTestDB(seed)
	return newTestEngineWithDB(t, db, shardCount, maxSize), db
}

func newTestEngineWithDB(t *testing.T, db *testDB, shardCount, maxSize uint64) SnapshotEngine {
	t.Helper()
	return newTestEngineWithConfig(t, newTestConfig(shardCount, maxSize), db)
}

// newTestEngineWithConfig builds an engine and registers cleanup that closes it and then drains
// the work pool, mirroring the production teardown order (engine, then pools, then DB). Close is
// idempotent, so tests that exercise it explicitly are unaffected; its error is ignored because
// brick tests intentionally leave the engine failed.
func newTestEngineWithConfig(t *testing.T, config *SnapshotEngineConfig, db *testDB) SnapshotEngine {
	t.Helper()
	pool := threading.NewAdHocPool()
	engine, err := NewSnapshotEngine(config, db, pool, pool)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = engine.Close()
		pool.Close()
		_ = db.Close()
	})
	return engine
}

// --- shard construction helpers ---

func newTestShard(t *testing.T, maxSize uint64, db *testDB) *shard {
	t.Helper()
	config := DefaultTestSnapshotEngineConfig()
	config.EstimatedOverheadPerEntry = 0
	// A standalone shard has no engine to brick, and it takes itself out of service on a failed read
	// without help, so reporting is a no-op here.
	s, err := NewShard(context.Background(), config, db, threading.NewAdHocPool(), maxSize,
		func() error { return ErrEngineClosed },
		func(error) {})
	require.NoError(t, err)
	return s
}

// --- lifecycle & iterator helpers ---

var testHash = []byte("test-hash")

// testHashKey lives under DefaultTestSnapshotEngineConfig's reserved prefix, so finalization writes
// using it are filtered out of iteration exactly as engine metadata should be.
const testHashKey = "_meta/hash"

// hashWrites is the finalization write set a test uses to record a snapshot's hash, standing in for
// what a real consumer emits.
func hashWrites(hash []byte) []*proto.KVPair {
	return []*proto.KVPair{{Key: []byte(testHashKey), Value: hash}}
}

func finalizeAndRelease(t *testing.T, snap Snapshot) {
	t.Helper()
	require.NoError(t, snap.Finalize(hashWrites(testHash)))
	require.NoError(t, snap.Release())
}

// finalizeAwaitFlushAndRelease waits for the flush before releasing. A released version is retired out
// of the version map as soon as it flushes, and a wait that arrives after that retirement fails, which
// TestAwaitFlushAfterRetirementFails pins. Releasing first therefore makes the wait a race against the
// lifecycle goroutine.
func finalizeAwaitFlushAndRelease(t *testing.T, snap Snapshot) {
	t.Helper()
	require.NoError(t, snap.Finalize(hashWrites(testHash)))
	awaitFlushed(t, snap, time.Second)
	require.NoError(t, snap.Release())
}

func commitFinalizeRelease(t *testing.T, engine SnapshotEngine) {
	t.Helper()
	snap, err := engine.Commit()
	require.NoError(t, err)
	finalizeAndRelease(t, snap)
}

func awaitFlushed(t *testing.T, snap Snapshot, timeout time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	require.NoError(t, snap.AwaitFlush(ctx))
}

// awaitRetired blocks until the given version has been retired (dropped from the engine's version
// map), failing the test if it does not happen within a short window.
func awaitRetired(t *testing.T, engine SnapshotEngine, version uint64) {
	t.Helper()
	e := engine.(*snapshotEngine)
	require.Eventually(t, func() bool {
		e.versionLock.Lock()
		defer e.versionLock.Unlock()
		_, tracked := e.versionMap[version]
		return !tracked
	}, 2*time.Second, 2*time.Millisecond, "version %d was not retired in time", version)
}

// openIteratorCount reports how many iterators are currently open on the engine. Every iterator
// registers with every shard, so any one shard's count is the engine's count.
func openIteratorCount(engine SnapshotEngine) uint64 {
	s := engine.(*snapshotEngine).shards[0]
	s.lock.Lock()
	defer s.lock.Unlock()
	return s.openIterators
}

// isTracked reports whether the engine still tracks the given snapshot version.
func isTracked(engine SnapshotEngine, version uint64) bool {
	e := engine.(*snapshotEngine)
	e.versionLock.Lock()
	defer e.versionLock.Unlock()
	_, ok := e.versionMap[version]
	return ok
}

// drainIterator drains an Iterator into cloned key/value pairs in iteration order. It returns any
// error instead of asserting, so it is safe to call from non-test goroutines. It does NOT close the
// iterator; the caller must, or the engine reports a leak when it closes.
func drainIterator(it dbm.Iterator) ([]kvPair, error) {
	var out []kvPair
	for ; it.Valid(); it.Next() {
		out = append(out, kvPair{key: cloneBytes(it.Key()), value: cloneBytes(it.Value())})
	}
	if err := it.Error(); err != nil {
		return nil, err
	}
	return out, nil
}

// collectIterator drains an Iterator into cloned key/value pairs in iteration order, then closes it.
// Closing matters: an open iterator makes the engine refuse writes.
func collectIterator(t *testing.T, it dbm.Iterator) []kvPair {
	t.Helper()
	out, err := drainIterator(it)
	require.NoError(t, err)
	require.NoError(t, it.Close())
	return out
}
