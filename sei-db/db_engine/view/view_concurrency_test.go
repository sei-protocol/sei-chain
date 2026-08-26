package view

import (
	"bytes"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"

	"github.com/sei-protocol/sei-chain/sei-db/common/testutil"
)

// TestViewIsolationUnderConcurrentMutation holds a view while many goroutines mutate the
// live version, asserting the view's frozen values never change. Run under -race, it also
// exercises concurrent reads of a single view against concurrent writes to the live version.
func TestViewIsolationUnderConcurrentMutation(t *testing.T) {
	manager := newTestManagerWithDB(t, newTestDB(nil), 8, 1<<20)

	const nKeys = 16
	keyAt := func(i int) []byte { return []byte{byte(i)} }
	for i := 0; i < nKeys; i++ {
		if err := manager.Set(keyAt(i), []byte("base")); err != nil {
			t.Fatalf("seed set: %v", err)
		}
	}

	view, err := manager.Commit()
	if err != nil {
		t.Fatalf("view: %v", err)
	}
	if err := view.Finalize(hashWrites(testHash)); err != nil {
		t.Fatalf("set hash: %v", err)
	}

	var wg sync.WaitGroup

	// Readers: the view must always show the pre-view value "base".
	for r := 0; r < 8; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for iter := 0; iter < 200; iter++ {
				for i := 0; i < nKeys; i++ {
					v, found, err := view.Get(keyAt(i), false)
					if err != nil {
						t.Errorf("view get: %v", err)
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
					if err := manager.Set(keyAt(i), []byte("mutated")); err != nil {
						t.Errorf("concurrent set: %v", err)
						return
					}
				}
			}
		}()
	}

	wg.Wait()
	if err := view.Release(); err != nil {
		t.Fatalf("release: %v", err)
	}
}

// TestConcurrentDifferential runs a single serialized writer (Commit() is contractually not
// concurrent with live writes) that produces views and iterators, and a pool of concurrent reader
// goroutines that each validate one against the frozen oracle it was created at. Readers touch only
// their own oracle, so there is no shared-state race with the writer.
//
// An iterator's oracle is the model's materialized live state, captured by the writer in the same step
// that creates the iterator. Pairing them that way is also what satisfies the manager's obligation that
// creating an iterator must not race a write.
func TestConcurrentDifferential(t *testing.T) {
	rng := testutil.NewTestRandomNoPrint(7)
	keys := genKeys(rng, 30)

	db := newTestDB(nil)
	cfg := newTestConfig(8, 1<<20)
	cfg.MaxUnflushedVersions = 128
	manager := newTestManagerWithConfig(t, cfg, db)
	model := newModelManager(nil)

	// Exactly one of view and iter is set on any given job.
	type job struct {
		// view is a sealed view to validate against ver.
		view View
		// ver is the immutable oracle for view.
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
				checkConcurrentView(t, j.view, j.ver, keys)
				if err := j.view.Release(); err != nil {
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
			it, err := manager.Iterator(nil)
			if err != nil {
				t.Fatalf("iterator: %v", err)
			}
			jobs <- job{iter: it, oracle: model.IterateLive()}
		}

		switch pickOp(rng) {
		case opSet:
			k, v := pick(rng, keys), randVal(rng)
			if err := manager.Set(k, v); err != nil {
				t.Fatalf("set: %v", err)
			}
			model.Set(k, v)
		case opDelete:
			k := pick(rng, keys)
			if err := manager.Delete(k); err != nil {
				t.Fatalf("delete: %v", err)
			}
			model.Delete(k)
		case opBatch:
			muts := randMuts(rng, keys)
			if err := manager.BatchSet(muts); err != nil {
				t.Fatalf("batchset: %v", err)
			}
			model.BatchSet(muts)
		case opView:
			view, err := manager.Commit()
			if err != nil {
				t.Fatalf("view: %v", err)
			}
			ver := model.Commit()
			if err := view.Finalize(hashWrites(testHash)); err != nil {
				t.Fatalf("set hash: %v", err)
			}
			// Hand off with an extra reservation so the reader owns teardown; drop the writer's
			// implicit reservation immediately.
			if err := view.Reserve(); err != nil {
				t.Fatalf("reserve: %v", err)
			}
			jobs <- job{view: view, ver: model.versions[ver]}
			if err := view.Release(); err != nil {
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

// checkConcurrentView validates a view's reads against its immutable oracle version.
// Goroutine-safe: reports via t.Errorf rather than asserting.
//
// Iteration is validated separately by checkConcurrentIterator, since an iterator covers the mutable
// version rather than a sealed one and so needs a different oracle.
func checkConcurrentView(t *testing.T, view View, ver *modelVersion, keys [][]byte) {
	for _, k := range keys {
		v, found, err := view.Get(k, false)
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
