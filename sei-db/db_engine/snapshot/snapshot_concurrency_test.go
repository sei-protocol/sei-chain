package snapshot

import (
	"bytes"
	"sync"
	"testing"

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
		engine.Set(keyAt(i), []byte("base"))
	}

	snap, err := engine.Snapshot()
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if err := snap.SetHash(testHash); err != nil {
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
					engine.Set(keyAt(i), []byte("mutated"))
				}
			}
		}()
	}

	wg.Wait()
	if err := snap.Release(); err != nil {
		t.Fatalf("release: %v", err)
	}
}

// TestConcurrentDifferential runs a single serialized writer (Snapshot() is contractually not
// concurrent with live writes) that produces snapshots, and a pool of concurrent reader goroutines
// that each validate a snapshot against the immutable oracle version it was sealed at. Readers touch
// only their own frozen modelVersion, so there is no shared-state race with the writer.
func TestConcurrentDifferential(t *testing.T) {
	rng := testutil.NewTestRandomNoPrint(7)
	keys := genKeys(rng, 30)

	db := newTestDB(nil)
	cfg := newTestConfig(8, 1<<20)
	cfg.MaxUnflushedVersions = 128
	engine := newTestEngineWithConfig(t, cfg, db)
	model := newModelEngine(nil)
	hashKey := cfg.HashKey

	type job struct {
		snap Snapshot
		ver  *modelVersion
	}
	jobs := make(chan job, 64)

	var readers sync.WaitGroup
	for r := 0; r < 6; r++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for j := range jobs {
				checkConcurrentSnapshot(t, j.snap, j.ver, keys, hashKey)
				if err := j.snap.Release(); err != nil {
					t.Errorf("reader release: %v", err)
				}
			}
		}()
	}

	for i := 0; i < 300; i++ {
		switch pickOp(rng) {
		case opSet:
			k, v := pick(rng, keys), randVal(rng)
			engine.Set(k, v)
			model.Set(k, v)
		case opDelete:
			k := pick(rng, keys)
			engine.Delete(k)
			model.Delete(k)
		case opBatch:
			muts := randMuts(rng, keys)
			if err := engine.BatchSet(muts); err != nil {
				t.Fatalf("batchset: %v", err)
			}
			model.BatchSet(muts)
		case opSnapshot:
			snap, err := engine.Snapshot()
			if err != nil {
				t.Fatalf("snapshot: %v", err)
			}
			ver := model.Snapshot()
			if err := snap.SetHash(testHash); err != nil {
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
}

// checkConcurrentSnapshot validates a snapshot's reads and iteration against its immutable oracle
// version. Goroutine-safe: reports via t.Errorf rather than asserting.
func checkConcurrentSnapshot(t *testing.T, snap Snapshot, ver *modelVersion, keys [][]byte, hashKey string) {
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

	pairs, err := drainIterator(snap.Iterator())
	if err != nil {
		t.Errorf("concurrent iterate: %v", err)
		return
	}
	// Filter the metadata hash key (whose presence depends on non-deterministic flush timing).
	got := pairs[:0]
	for _, kv := range pairs {
		if string(kv.key) != hashKey {
			got = append(got, kv)
		}
	}
	exp := sortedEntries(ver.full)
	if len(got) != len(exp) {
		t.Errorf("concurrent iterator length: exp=%d got=%d", len(exp), len(got))
		return
	}
	for i := range exp {
		if !bytes.Equal(exp[i].key, got[i].key) || !bytes.Equal(exp[i].value, got[i].value) {
			t.Errorf("concurrent iterator mismatch at %d", i)
			return
		}
	}
}
