package snapshot

import (
	"bytes"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"

	"github.com/sei-protocol/sei-chain/sei-db/common/testutil"
)

// TestSnapshotIsolationUnderConcurrentMutation holds a snapshot while many goroutines mutate the
// live version, asserting the snapshot's frozen values never change. Run under -race, it also
// exercises concurrent reads of a single snapshot against concurrent writes to the live version.
func TestSnapshotIsolationUnderConcurrentMutation(t *testing.T) {
	engine := newTestEngineWithDB(t, newTestDB(nil), 8, 1<<20)

	const nKeys = 16
	keyAt := func(i int) []byte { return []byte{byte(i)} }
	for i := 0; i < nKeys; i++ {
		if err := engine.Set(keyAt(i), []byte("base")); err != nil {
			t.Fatalf("seed set: %v", err)
		}
	}

	snap, err := engine.Commit()
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if err := snap.Finalize(hashWrites(testHash)); err != nil {
		t.Fatalf("set hash: %v", err)
	}

	var wg sync.WaitGroup

	// Readers: the snapshot must always show the pre-snapshot value "base".
	for r := 0; r < 8; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for iter := 0; iter < 200; iter++ {
				for i := 0; i < nKeys; i++ {
					v, found, err := snap.Get(keyAt(i), false)
					if err != nil {
						t.Errorf("snapshot get: %v", err)
						return
					}
					if !found || !bytes.Equal(v, []byte("base")) {
						t.Errorf("isolation violated: key=%d found=%v val=%q", i, found, v)
						return
					}
				}
			}
		}()
	}

	// Writers: mutate the live version concurrently.
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for iter := 0; iter < 200; iter++ {
				for i := 0; i < nKeys; i++ {
					if err := engine.Set(keyAt(i), []byte("mutated")); err != nil {
						t.Errorf("concurrent set: %v", err)
						return
					}
				}
			}
		}()
	}

	wg.Wait()
	if err := snap.Release(); err != nil {
		t.Fatalf("release: %v", err)
	}
}

// TestConcurrentDifferential runs a single serialized writer (Commit() is contractually not
// concurrent with live writes) that produces snapshots and iterators, and a pool of concurrent reader
// goroutines that each validate one against the frozen oracle it was created at. Readers touch only
// their own oracle, so there is no shared-state race with the writer.
//
// An iterator's oracle is the model's materialized live state, captured by the writer in the same step
// that creates the iterator. Pairing them that way is also what satisfies the engine's obligation that
// creating an iterator must not race a write.
func TestConcurrentDifferential(t *testing.T) {
	rng := testutil.NewTestRandomNoPrint(7)
	keys := genKeys(rng, 30)

	db := newTestDB(nil)
	cfg := newTestConfig(8, 1<<20)
	cfg.MaxUnflushedVersions = 128
	engine := newTestEngineWithConfig(t, cfg, db)
	model := newModelEngine(nil)

	// Exactly one of snap and iter is set on any given job.
	type job struct {
		// snap is a sealed snapshot to validate against ver.
		snap Snapshot
		// ver is the immutable oracle for snap.
		ver *modelVersion
		// iter is a live-version iterator to validate against oracle.
		iter dbm.Iterator
		// oracle is the model's live state at the instant iter was created.
		oracle []kvPair
	}
	jobs := make(chan job, 64)

	// Counts iteration jobs whose oracle had rows in it. Without this the iteration half of the model
	// could silently become vacuous — comparing empty against empty and proving nothing.
	var nonEmptyIterChecks atomic.Int64

	var readers sync.WaitGroup
	for r := 0; r < 6; r++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for j := range jobs {
				if j.iter != nil {
					if len(j.oracle) > 0 {
						nonEmptyIterChecks.Add(1)
					}
					checkConcurrentIterator(t, j.iter, j.oracle)
					continue
				}
				checkConcurrentSnapshot(t, j.snap, j.ver, keys)
				if err := j.snap.Release(); err != nil {
					t.Errorf("reader release: %v", err)
				}
			}
		}()
	}

	for i := 0; i < 300; i++ {
		// Periodically hand a reader an iterator to drain while this loop keeps writing. Created here,
		// on the writer goroutine, so its construction cannot race a write; the oracle is captured in
		// the same breath, so the two describe the same instant.
		if i%25 == 0 {
			it, err := engine.Iterator(nil)
			if err != nil {
				t.Fatalf("iterator: %v", err)
			}
			jobs <- job{iter: it, oracle: model.IterateLive()}
		}

		switch pickOp(rng) {
		case opSet:
			k, v := pick(rng, keys), randVal(rng)
			if err := engine.Set(k, v); err != nil {
				t.Fatalf("set: %v", err)
			}
			model.Set(k, v)
		case opDelete:
			k := pick(rng, keys)
			if err := engine.Delete(k); err != nil {
				t.Fatalf("delete: %v", err)
			}
			model.Delete(k)
		case opBatch:
			muts := randMuts(rng, keys)
			if err := engine.BatchSet(muts); err != nil {
				t.Fatalf("batchset: %v", err)
			}
			model.BatchSet(muts)
		case opSnapshot:
			snap, err := engine.Commit()
			if err != nil {
				t.Fatalf("snapshot: %v", err)
			}
			ver := model.Commit()
			if err := snap.Finalize(hashWrites(testHash)); err != nil {
				t.Fatalf("set hash: %v", err)
			}
			// Hand off with an extra reservation so the reader owns teardown; drop the writer's
			// implicit reservation immediately.
			if err := snap.Reserve(); err != nil {
				t.Fatalf("reserve: %v", err)
			}
			jobs <- job{snap: snap, ver: model.versions[ver]}
			if err := snap.Release(); err != nil {
				t.Fatalf("writer release: %v", err)
			}
		}
	}
	close(jobs)
	readers.Wait()

	require.Positive(t, nonEmptyIterChecks.Load(),
		"the model must have checked at least one iterator against a non-empty oracle")
}

// checkConcurrentIterator drains an iterator on a reader goroutine and compares it against the oracle
// captured when it was created, then closes it. Goroutine-safe: reports via t.Errorf rather than
// asserting, since require's failures must happen on the test goroutine.
func checkConcurrentIterator(t *testing.T, it dbm.Iterator, oracle []kvPair) {
	got, err := drainIterator(it)
	if closeErr := it.Close(); closeErr != nil {
		t.Errorf("concurrent iterator close: %v", closeErr)
	}
	if err != nil {
		t.Errorf("concurrent iterate: %v", err)
		return
	}
	if len(got) != len(oracle) {
		t.Errorf("concurrent iterator length: exp=%d got=%d", len(oracle), len(got))
		return
	}
	for i := range oracle {
		if !bytes.Equal(oracle[i].key, got[i].key) {
			t.Errorf("concurrent iterator key at %d: exp=%x got=%x", i, oracle[i].key, got[i].key)
			return
		}
		if !bytes.Equal(oracle[i].value, got[i].value) {
			t.Errorf("concurrent iterator value at %d (key=%x)", i, oracle[i].key)
			return
		}
	}
}

// checkConcurrentSnapshot validates a snapshot's reads against its immutable oracle version.
// Goroutine-safe: reports via t.Errorf rather than asserting.
//
// Iteration is validated separately by checkConcurrentIterator, since an iterator covers the mutable
// version rather than a sealed one and so needs a different oracle.
func checkConcurrentSnapshot(t *testing.T, snap Snapshot, ver *modelVersion, keys [][]byte) {
	for _, k := range keys {
		v, found, err := snap.Get(k, false)
		if err != nil {
			t.Errorf("concurrent get: %v", err)
			return
		}
		expV, expFound := ver.full[string(k)]
		if found != expFound {
			t.Errorf("concurrent found mismatch key=%x: exp=%v got=%v", k, expFound, found)
			return
		}
		if found && !bytes.Equal(v, expV) {
			t.Errorf("concurrent value mismatch key=%x", k)
			return
		}
	}

}
