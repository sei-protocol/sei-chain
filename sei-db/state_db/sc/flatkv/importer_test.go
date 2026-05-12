package flatkv

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// KVImporter concurrency / lifecycle tests
//
// These tests exercise paths that the higher-level Export → Import round-trip
// tests in import_export_test.go don't reach:
//   * Close idempotency (finishOnce)
//   * Err() return value across the success / error / post-Close lifecycle
//   * setErr fail-fast atomicity (firstErr CAS + closeOnce(done))
//   * AddNode after the done channel is closed (must not block)
//   * Multi-flush behavior under load larger than importBatchSize
// =============================================================================

func newKVImporterForTest(t *testing.T, version int64) (*CommitStore, *KVImporter) {
	t.Helper()
	s := setupTestStore(t)
	imp, err := s.Importer(version)
	require.NoError(t, err)
	kvi, ok := imp.(*KVImporter)
	require.True(t, ok, "expected *KVImporter, got %T", imp)
	return s, kvi
}

// TestKVImporter_CloseIdempotent_HappyPath verifies that Close can be called
// multiple times after a successful import without panicking on a re-close of
// ingestCh and that every call returns the same (nil) finishErr.
func TestKVImporter_CloseIdempotent_HappyPath(t *testing.T) {
	s, imp := newKVImporterForTest(t, 1)
	defer func() { require.NoError(t, s.Close()) }()

	imp.AddNode(&types.SnapshotNode{
		Key:     storagePhysKey(addrN(0x01), slotN(0x01)),
		Value:   padLeft32(0x11),
		Version: 1,
	})

	require.NoError(t, imp.Close())
	require.NoError(t, imp.Close(), "second Close must not panic and must return the same nil result")
	require.NoError(t, imp.Close(), "third Close must remain idempotent")
	require.NoError(t, imp.Err(), "Err() should report no error after a successful import")
}

// TestKVImporter_CloseIdempotent_AfterError verifies double-Close after a
// fail-fast error: the first Close drains the pipeline and surfaces the error;
// subsequent Close calls must return the cached finishErr without re-closing
// ingestCh (which would panic).
func TestKVImporter_CloseIdempotent_AfterError(t *testing.T) {
	s, imp := newKVImporterForTest(t, 1)
	defer func() { require.NoError(t, s.Close()) }()

	imp.AddNode(&types.SnapshotNode{
		Key:     []byte{0xDE, 0xAD},
		Value:   []byte{0x01},
		Version: 1,
	})

	first := imp.Close()
	require.Error(t, first)
	require.Contains(t, first.Error(), "route key")

	second := imp.Close()
	require.Error(t, second)
	require.Equal(t, first, second, "subsequent Close must return the same cached error")

	third := imp.Close()
	require.Equal(t, first, third)
}

// TestKVImporter_ErrLifecycle locks in the contract that Err() returns the
// first pipeline error as soon as it propagates, before Close is invoked.
// This is the path the seidb tool relies on to short-circuit a failing import
// without forcing a full Close.
func TestKVImporter_ErrLifecycle(t *testing.T) {
	s, imp := newKVImporterForTest(t, 1)
	defer func() { require.NoError(t, s.Close()) }()

	require.NoError(t, imp.Err(), "Err() should be nil before any pipeline error")

	imp.AddNode(&types.SnapshotNode{
		Key:     []byte{0xDE, 0xAD},
		Value:   []byte{0x01},
		Version: 1,
	})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if imp.Err() != nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	require.Error(t, imp.Err(), "Err() must surface the route-key error from the dispatcher")
	require.Contains(t, imp.Err().Error(), "route key")

	closeErr := imp.Close()
	require.ErrorIs(t, closeErr, imp.Err(),
		"Close result must mirror Err() once the pipeline has already failed")

	require.Equal(t, closeErr, imp.Err(),
		"Err() must remain stable after Close; it returns the cached firstErr, not finishErr")
}

// TestKVImporter_SetErrAtomicCAS exercises setErr directly to lock the
// CompareAndSwap-based fail-fast invariant: only the first error is recorded,
// and the done channel is closed exactly once even if setErr races. Without
// this, a worker that errors out after another worker already did would
// clobber firstErr and double-close done (panic).
func TestKVImporter_SetErrAtomicCAS(t *testing.T) {
	s, imp := newKVImporterForTest(t, 1)
	defer func() { require.NoError(t, s.Close()) }()

	first := errors.New("first error")
	second := errors.New("second error")

	imp.setErr(first)
	require.ErrorIs(t, imp.Err(), first)

	imp.setErr(second)
	require.ErrorIs(t, imp.Err(), first, "subsequent setErr calls must not overwrite firstErr")

	select {
	case <-imp.done:
	default:
		t.Fatalf("done channel must be closed after the first setErr")
	}

	imp.setErr(errors.New("third error"))
}

// TestKVImporter_AddNodeAfterDoneDoesNotBlock guards the AddNode select arm:
// once setErr fires and closes done, AddNode must exit via <-imp.done instead
// of blocking on a full ingestCh. We saturate ingestCh first by sending more
// pairs than its buffer, then trip the error and assert that further AddNode
// calls return promptly.
func TestKVImporter_AddNodeAfterDoneDoesNotBlock(t *testing.T) {
	s, imp := newKVImporterForTest(t, 1)
	defer func() { require.NoError(t, s.Close()) }()
	// imp.Close() drains the dispatcher + worker goroutines via wg.Wait().
	// Without it, s.Close() (the outer defer, runs second because defers are
	// LIFO) can race the dispatcher's read of s.storageDB in routePhysicalKey
	// against closeDBsOnly's write of s.storageDB = nil, tripping the race
	// detector. Discard the returned error: we tripped setErr below, so this
	// Close is on the error path and intentionally returns the synthetic err.
	defer func() { _ = imp.Close() }()

	imp.setErr(errors.New("synthetic test error"))

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < ingestChanSize+1024; i++ {
			imp.AddNode(&types.SnapshotNode{
				Key:     storagePhysKey(addrN(0x01), slotN(0x01)),
				Value:   padLeft32(0x11),
				Version: 1,
			})
		}
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("AddNode blocked after done was closed; fail-fast path is broken")
	}
}

// TestKVImporter_LargeImportTriggersMultipleFlushes drives more than
// importBatchSize pairs through a single worker so that flush() is invoked
// repeatedly. Without this, the existing happy-path tests only ever hit
// flush once (at Close), which masks any regression in the
// pairs >= importBatchSize branch.
func TestKVImporter_LargeImportTriggersMultipleFlushes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large-import test in -short mode")
	}

	const totalPairs = importBatchSize*3 + 100
	s, imp := newKVImporterForTest(t, 1)
	defer func() { require.NoError(t, s.Close()) }()

	for i := 0; i < totalPairs; i++ {
		var addr ktype.Address
		addr[16] = byte(i >> 16)
		addr[17] = byte(i >> 8)
		addr[18] = byte(i)
		var slot ktype.Slot
		slot[29] = byte(i >> 16)
		slot[30] = byte(i >> 8)
		slot[31] = byte(i)
		imp.AddNode(&types.SnapshotNode{
			Key:     storagePhysKey(addr, slot),
			Value:   padLeft32(byte(i & 0xFF)),
			Version: 1,
		})
	}

	require.NoError(t, imp.Close())

	flushes, pairs := imp.importStats()
	require.Equal(t, int64(totalPairs), pairs, "all pairs must be accounted for in importStats")
	require.GreaterOrEqual(t, flushes, int64(3),
		"importBatchSize=%d * 3 + 100 storage pairs must trigger at least 3 mid-pipeline flushes (got %d)",
		importBatchSize, flushes)
}

// TestKVImporter_AbortSkipsFinalize locks in the contract that Abort tears
// down the worker pipeline WITHOUT finalizing the import: the underlying
// CommitStore must remain at its pre-import committed version, so a
// failed offline migration can be retried without --force.
//
// Without this guarantee, the seidb import-flatkv-from-memiavl tool's
// deferred Close would commit whatever pairs happened to be buffered when
// an external error (ctx cancellation, exporter failure, translator
// failure) tripped, leaving FlatKV at the target version with only a
// partial copy of the source state.
func TestKVImporter_AbortSkipsFinalize(t *testing.T) {
	s, imp := newKVImporterForTest(t, 1)
	defer func() { require.NoError(t, s.Close()) }()

	preVersion := s.Version()

	// Add a couple of valid pairs so there is real buffered work that the
	// happy-path Close would have committed.
	imp.AddNode(&types.SnapshotNode{
		Key:     storagePhysKey(addrN(0x01), slotN(0x01)),
		Value:   padLeft32(0x11),
		Version: 1,
	})
	imp.AddNode(&types.SnapshotNode{
		Key:     storagePhysKey(addrN(0x02), slotN(0x02)),
		Value:   padLeft32(0x22),
		Version: 1,
	})

	abortReason := errors.New("synthetic external abort")
	require.ErrorIs(t, imp.Abort(abortReason), abortReason,
		"Abort must surface the supplied reason")

	require.ErrorIs(t, imp.Err(), abortReason,
		"Err() must report the abort reason")
	require.ErrorIs(t, imp.Close(), abortReason,
		"Close after Abort must be a no-op returning the cached error")

	require.Equal(t, preVersion, s.Version(),
		"Abort must not advance the store's committed version (no FinalizeImport)")
}

// TestKVImporter_AbortNilReasonStillAborts ensures Abort with nil substitutes
// a generic reason rather than silently no-op'ing into a finalize.
func TestKVImporter_AbortNilReasonStillAborts(t *testing.T) {
	s, imp := newKVImporterForTest(t, 1)
	defer func() { require.NoError(t, s.Close()) }()

	preVersion := s.Version()

	imp.AddNode(&types.SnapshotNode{
		Key:     storagePhysKey(addrN(0x01), slotN(0x01)),
		Value:   padLeft32(0x11),
		Version: 1,
	})

	require.Error(t, imp.Abort(nil), "Abort(nil) must still surface a non-nil reason")
	require.Error(t, imp.Err())
	require.Equal(t, preVersion, s.Version(),
		"Abort(nil) must not finalize the import")
}

// TestKVImporter_AbortAfterCloseIsNoop confirms the finishOnce contract:
// once Close has finalized successfully, a later Abort cannot retroactively
// invalidate the committed state. The store stays advanced; the abort
// reason is not surfaced through Err().
func TestKVImporter_AbortAfterCloseIsNoop(t *testing.T) {
	s, imp := newKVImporterForTest(t, 1)
	defer func() { require.NoError(t, s.Close()) }()

	imp.AddNode(&types.SnapshotNode{
		Key:     storagePhysKey(addrN(0x01), slotN(0x01)),
		Value:   padLeft32(0x11),
		Version: 1,
	})

	require.NoError(t, imp.Close())
	postCloseVersion := s.Version()
	require.Equal(t, int64(1), postCloseVersion, "successful Close must advance the store")

	require.NoError(t, imp.Abort(errors.New("too late")),
		"Abort after a successful Close must return the cached nil result")
	require.Equal(t, postCloseVersion, s.Version(),
		"Abort cannot rewind a committed version")
}

// TestKVImporter_BackpressureBlocksProducerUntilWorkersDrain explicitly
// exercises the backpressure path. It gates every dbWorker.flush() on a
// release channel via flushHookForTest, sends enough pairs to overflow
// ingestCh + worker.ch + the in-flight worker batch, and asserts that:
//
//  1. While flushes are gated, the producer goroutine is observably blocked
//     (i.e. AddNode is sitting on its <-imp.ingestCh send arm) — the
//     producer does NOT finish even after a soak period.
//  2. After the gate is released, the producer drains, Close succeeds, and
//     every pair is persisted.
//
// Without this test the only coverage of true backpressure is incidental
// (TestImportMemiavlModulesToFlatKVHandlesLargeDataset). A regression that
// broke AddNode's <-imp.done arm or the dispatcher's worker.ch select
// would silently pass as long as data still landed correctly.
func TestKVImporter_BackpressureBlocksProducerUntilWorkersDrain(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping backpressure test in -short mode")
	}

	release := make(chan struct{})
	var flushObserved atomic.Int64
	hook := func(string) {
		flushObserved.Add(1)
		<-release
	}
	flushHookForTest.Store(&hook)
	t.Cleanup(func() { flushHookForTest.Store(nil) })

	s, imp := newKVImporterForTest(t, 1)
	defer func() { require.NoError(t, s.Close()) }()

	// Producer can advance at most ingestChanSize + workerChanSize +
	// importBatchSize pairs before any worker drains. Send strictly more
	// than that so AddNode is forced to block once flushes are gated.
	const totalPairs = ingestChanSize + workerChanSize + importBatchSize + 8192

	producerDone := make(chan struct{})
	go func() {
		defer close(producerDone)
		for i := 0; i < totalPairs; i++ {
			var addr ktype.Address
			addr[16] = byte(i >> 16)
			addr[17] = byte(i >> 8)
			addr[18] = byte(i)
			var slot ktype.Slot
			slot[29] = byte(i >> 16)
			slot[30] = byte(i >> 8)
			slot[31] = byte(i)
			imp.AddNode(&types.SnapshotNode{
				Key:     storagePhysKey(addr, slot),
				Value:   padLeft32(byte(i & 0xFF)),
				Version: 1,
			})
		}
	}()

	// Wait for the first flush to hit the gate. By this point the storage
	// worker has consumed importBatchSize pairs and the producer is racing
	// ahead to fill ingestCh.
	deadline := time.Now().Add(5 * time.Second)
	for flushObserved.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("no flush observed within 5s; worker pipeline not running")
		}
		time.Sleep(time.Millisecond)
	}

	// Soak: give the producer ample time to fill ingestCh and block on
	// AddNode. If backpressure works, producerDone must NOT be closed yet.
	time.Sleep(200 * time.Millisecond)
	select {
	case <-producerDone:
		t.Fatalf("producer finished while flushes were gated; backpressure was not exercised")
	default:
	}

	close(release)

	select {
	case <-producerDone:
	case <-time.After(30 * time.Second):
		t.Fatalf("producer never finished after gate release; pipeline deadlocked")
	}

	require.NoError(t, imp.Close())

	flushes, pairs := imp.importStats()
	require.Equal(t, int64(totalPairs), pairs, "every pair must be persisted")
	require.GreaterOrEqual(t, flushes, int64(2),
		"expected multiple flushes for %d storage pairs (got %d)", totalPairs, flushes)
}
