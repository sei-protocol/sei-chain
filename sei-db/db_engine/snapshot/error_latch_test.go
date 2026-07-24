package snapshot

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A DB read error must fail the key permanently: a retry that succeeded after the error was
// propagated could fork the chain. These tests pin the latch semantics for every read path.

func TestSingleGetErrorIsPermanent(t *testing.T) {
	db := newTestDB(map[string][]byte{"k": []byte("v")})
	engine := newTestEngineWithDB(t, db, 1, 1<<20)

	db.getErr = errors.New("io boom")
	_, _, err := engine.Get([]byte("k"), true)
	require.ErrorContains(t, err, "io boom")

	// The fault "clears", but the engine must keep failing the key rather than retry.
	db.getErr = nil
	_, _, err = engine.Get([]byte("k"), true)
	require.ErrorContains(t, err, "io boom", "a failed read must never be retried")
}

func TestBatchGetErrorIsPermanent(t *testing.T) {
	db := newTestDB(map[string][]byte{"k": []byte("v")})
	engine := newTestEngineWithDB(t, db, 1, 1<<20)

	db.getErr = errors.New("io boom")
	_, err := engine.BatchGet([][]byte{[]byte("k")})
	require.ErrorContains(t, err, "io boom")

	db.getErr = nil
	_, _, err = engine.Get([]byte("k"), true)
	require.ErrorContains(t, err, "io boom", "a failed batch read must never be retried")
	_, err = engine.BatchGet([][]byte{[]byte("k")})
	require.ErrorContains(t, err, "io boom", "the latch must also fail subsequent batch reads")
}

// A batch where one key fails must still drain and cache the surviving reads, latch the failed
// key, and leave no entry stranded in statusScheduled.
func TestBatchGetPartialFailureLeavesCoherentState(t *testing.T) {
	db := newTestDB(map[string][]byte{
		"k1": []byte("v1"),
		"k2": []byte("v2"),
		"k3": []byte("v3"),
	})
	engine := newTestEngineWithDB(t, db, 1, 1<<20)
	shard := engine.(*snapshotEngine).shards[0]

	db.getErrKeys = map[string]error{"k2": errors.New("io boom")}
	_, err := engine.BatchGet([][]byte{[]byte("k1"), []byte("k2"), []byte("k3")})
	require.ErrorContains(t, err, "io boom")
	db.getErrKeys = nil

	// bulkInjectValues runs asynchronously; every entry must reach a terminal state.
	require.Eventually(t, func() bool {
		shard.lock.Lock()
		defer shard.lock.Unlock()
		expected := map[string]valueStatus{
			"k1": statusAvailable,
			"k2": statusFailed,
			"k3": statusAvailable,
		}
		for key, wantStatus := range expected {
			entry, ok := shard.dbCache[key]
			if !ok || entry.status != wantStatus {
				return false
			}
		}
		return true
	}, 2*time.Second, time.Millisecond, "batch entries did not reach their terminal states")

	// Survivors read back correctly; the failed key stays failed even though the fault cleared.
	v, found, err := engine.Get([]byte("k1"), true)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []byte("v1"), v)

	v, found, err = engine.Get([]byte("k3"), true)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []byte("v3"), v)

	_, _, err = engine.Get([]byte("k2"), true)
	require.ErrorContains(t, err, "io boom")
}

// A key already latched as statusFailed must not abort BatchGet classification early: keys earlier
// in the batch that were flipped to statusScheduled must still be scheduled, drained, and cached.
// Regression test: returning early stranded those entries with no producer, hanging every future
// reader of those keys.
func TestBatchGetWithLatchedKeyLeavesNoStrandedEntries(t *testing.T) {
	db := newTestDB(map[string][]byte{"k1": []byte("v1")})
	engine := newTestEngineWithDB(t, db, 1, 1<<20)
	shard := engine.(*snapshotEngine).shards[0]

	// Latch k2 as permanently failed.
	db.getErrKeys = map[string]error{"k2": errors.New("io boom")}
	_, _, err := engine.Get([]byte("k2"), true)
	require.ErrorContains(t, err, "io boom")
	db.getErrKeys = nil

	// k1 is uncached and precedes the latched key, so classification flips it to statusScheduled
	// before the batch observes k2's latched failure. The batch must still fail with k2's error.
	_, err = engine.BatchGet([][]byte{[]byte("k1"), []byte("k2")})
	require.ErrorContains(t, err, "io boom")

	// bulkInjectValues runs asynchronously; k1 must reach a terminal state, not stay scheduled.
	require.Eventually(t, func() bool {
		shard.lock.Lock()
		defer shard.lock.Unlock()
		entry, ok := shard.dbCache["k1"]
		return ok && entry.status == statusAvailable
	}, 2*time.Second, time.Millisecond, "k1 was left stranded in a non-terminal state")

	// k1 must read back promptly (this blocked forever before the fix).
	var v []byte
	var found bool
	var getErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		v, found, getErr = engine.Get([]byte("k1"), true)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Get(k1) hung on a stranded statusScheduled entry")
	}
	require.NoError(t, getErr)
	require.True(t, found)
	require.Equal(t, []byte("v1"), v)
}

// Two goroutines racing on one failing key: both must observe the error, the key must be read
// from the DB exactly once (the failure latch is what guarantees a failed read is never retried,
// on any interleaving), and the entry must end latched as statusFailed.
func TestConcurrentReadersOfFailingKeyBothError(t *testing.T) {
	db := newTestDB(nil)
	engine := newTestEngineWithDB(t, db, 1, 1<<20)
	shard := engine.(*snapshotEngine).shards[0]

	// Set the fault knobs only after construction: NewSnapshotEngine performs an initial-hash
	// read through the same DB, which must neither block on the gate nor observe the error.
	// Baseline the read counter past that construction-time read.
	base := db.getCalls.Load()
	db.getGate = make(chan struct{})
	db.getErr = errors.New("io boom")

	// Close the gate exactly once, and unconditionally on test failure: t.Cleanup runs LIFO, so
	// this releases any gated in-flight read before the engine/pool teardown registered at
	// construction — a failed assertion must not deadlock the pool drain.
	releaseGate := sync.OnceFunc(func() { close(db.getGate) })
	t.Cleanup(releaseGate)

	readErrs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			_, _, err := engine.Get([]byte("k"), true)
			readErrs <- err
		}()
	}

	// Hold the gate until the single read is in flight, so the second reader has the chance to
	// park on it. This wait is monotone convergence, not a race: the counter provably reaches
	// base+1 and (thanks to the latch) provably never exceeds it.
	require.Eventually(t, func() bool { return db.getCalls.Load() == base+1 },
		10*time.Second, time.Millisecond, "the scheduled DB read never started")
	releaseGate()

	for i := 0; i < 2; i++ {
		select {
		case err := <-readErrs:
			require.ErrorContains(t, err, "io boom")
		case <-time.After(2 * time.Second):
			t.Fatal("reader did not unblock")
		}
	}

	// The coalescing/latch property, asserted where it is deterministic: however the readers
	// interleaved, the failing key was read exactly once.
	require.Equal(t, base+1, db.getCalls.Load(),
		"a failed key must be read from the DB exactly once, on any interleaving")

	require.Eventually(t, func() bool {
		shard.lock.Lock()
		defer shard.lock.Unlock()
		entry, ok := shard.dbCache["k"]
		return ok && entry.status == statusFailed
	}, 2*time.Second, time.Millisecond, "entry was not latched as failed")
}
