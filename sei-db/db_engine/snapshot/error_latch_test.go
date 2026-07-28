package snapshot

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A DB read error is fatal: it bricks the engine and takes every shard out of service, so no read
// succeeds afterwards. These tests pin that for every read path, plus the internal coherence the
// read paths must leave behind (no entry stranded in statusScheduled, a failing key read from the DB
// exactly once).
//
// They deliberately do not pin *when* reads stop. Convergence is the guarantee; reads racing the
// original failure may legitimately go either way.

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

// A batch where one key fails must still drain every read it started and leave each entry in a
// terminal state, with nothing stranded in statusScheduled. The surviving values are cached but never
// served: the failure has taken the shard out of service.
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
			entry, ok := shard.cache.entries[key]
			if !ok || entry.status != wantStatus {
				return false
			}
		}
		return true
	}, 2*time.Second, time.Millisecond, "batch entries did not reach their terminal states")

	// The survivors were cached successfully, but one key's failure poisons the whole shard: reads of
	// keys that never failed are refused too, and the fault having cleared changes nothing.
	for _, key := range []string{"k1", "k2", "k3"} {
		_, _, err = engine.Get([]byte(key), true)
		require.ErrorContains(t, err, "io boom",
			"read of %q must be refused after another key's read failed", key)
	}
}

// A BatchGet issued after a read has failed is refused before any classification happens, so it
// cannot start reads it will not drain and cannot create entries it will not resolve.
//
// This replaces a regression test for a stranding bug in BatchGet's classification loop (returning
// early on an already-failed key stranded preceding keys in statusScheduled). That path is no longer
// reachable: an entry only becomes statusFailed in the same critical section that takes the shard out
// of service, so a batch that would encounter one is refused first. Live stranding coverage is now in
// TestBatchGetPartialFailureLeavesCoherentState, where the failure happens mid-batch.
func TestBatchGetAfterFailureIsRefusedBeforeClassifying(t *testing.T) {
	db := newTestDB(map[string][]byte{"k1": []byte("v1")})
	engine := newTestEngineWithDB(t, db, 1, 1<<20)
	shard := engine.(*snapshotEngine).shards[0]

	db.getErrKeys = map[string]error{"k2": errors.New("io boom")}
	_, _, err := engine.Get([]byte("k2"), true)
	require.ErrorContains(t, err, "io boom")
	db.getErrKeys = nil

	// k1 is uncached and precedes the failed key. The batch must be refused, and must return promptly
	// rather than blocking on anything.
	done := make(chan struct{})
	var batchErr error
	go func() {
		defer close(done)
		_, batchErr = engine.BatchGet([][]byte{[]byte("k1"), []byte("k2")})
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("BatchGet hung instead of being refused")
	}
	require.ErrorContains(t, batchErr, "io boom")

	// k1 was never classified, so it has no entry at all — in particular none stranded in
	// statusScheduled holding a channel no producer will ever write to.
	shard.lock.Lock()
	_, ok := shard.cache.entries["k1"]
	shard.lock.Unlock()
	require.False(t, ok, "a refused batch must not create cache entries")
}

// A failed DB read must stop the engine, not just the key that failed. Halting is the caller's job;
// this pins that a caller which ignores the error gets nowhere.
func TestReadFailureStopsTheEngine(t *testing.T) {
	db := newTestDB(map[string][]byte{"k1": []byte("v1"), "k2": []byte("v2")})
	// Several shards, so the read that fails and the reads that must stop land in different ones.
	engine := newTestEngineWithDB(t, db, 4, 1<<20)

	// Warm k2 into its shard's cache, so refusing it later cannot be explained by a DB read.
	v, found, err := engine.Get([]byte("k2"), true)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []byte("v2"), v)

	db.getErrKeys = map[string]error{"k1": errors.New("io boom")}
	_, _, err = engine.Get([]byte("k1"), true)
	require.ErrorContains(t, err, "io boom")
	db.getErrKeys = nil

	// Convergence, not immediacy: the brick is fired after the failing read is released, so allow it
	// to land. Every shard must stop, including the one that never saw a failure.
	require.Eventually(t, func() bool {
		_, _, readErr := engine.Get([]byte("k2"), true)
		return readErr != nil
	}, 2*time.Second, time.Millisecond, "a cached read in an unaffected shard was still served")

	_, err = engine.BatchGet([][]byte{[]byte("k2")})
	require.Error(t, err, "BatchGet must be refused too")

	_, err = engine.Commit()
	require.Error(t, err, "Commit must be refused once the engine is bricked")

	// Close surfaces the original cause rather than a bare ErrEngineClosed.
	require.ErrorContains(t, engine.Close(), "io boom")
}

// Two goroutines racing on one failing key: both must observe the error, and the key must be read
// from the DB exactly once on any interleaving (a failed read is never retried). The entry must end
// in the statusFailed terminal state so neither reader is stranded.
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
		entry, ok := shard.cache.entries["k"]
		return ok && entry.status == statusFailed
	}, 2*time.Second, time.Millisecond, "entry was not latched as failed")
}
