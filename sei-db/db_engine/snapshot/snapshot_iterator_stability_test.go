package snapshot

import (
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// This file pins one property: an iterator serves the engine's state as of the instant it was
// created, for as long as it is held, no matter what the engine does afterwards.
//
// That property is what makes it safe to hand an iterator to a consumer running on another thread —
// a query handler, or a state-sync export — while block commits continue.
//
// The interesting cases are the ones where the data physically moves after the iterator is built:
// a commit seals the version it copied from, a flush writes that version to the backing store, and
// retirement then drops it out of the shards' in-memory maps entirely. An iterator that aliased
// shard state unsafely would go wrong at exactly those points, so they are tested explicitly rather
// than left to the simple in-memory case.

// sortedPairs turns a key -> value map into the ascending kvPair slice an iterator should produce.
func sortedPairs(m map[string]string) []kvPair {
	out := make([]kvPair, 0, len(m))
	for k, v := range m {
		out = append(out, kvPair{key: []byte(k), value: []byte(v)})
	}
	sort.Slice(out, func(i, j int) bool { return string(out[i].key) < string(out[j].key) })
	return out
}

// reversed returns pairs in descending key order, for asserting a reverse iterator.
func reversed(pairs []kvPair) []kvPair {
	out := make([]kvPair, len(pairs))
	for i, p := range pairs {
		out[len(pairs)-1-i] = p
	}
	return out
}

// sealFlushRetire seals the engine's current version, waits for it to reach the backing store, then
// waits for it to be dropped from memory. On return the data that was staged in the shards lives
// only in the backing store and the read cache.
//
// Finalize and the flush wait both happen before Release: the engine may flush a still-reserved
// version, but it stops tracking one that has been released and retired, and AwaitFlush needs it
// tracked.
func sealFlushRetire(t *testing.T, engine SnapshotEngine) {
	t.Helper()
	snap, err := engine.Commit()
	require.NoError(t, err)
	version := snap.(*snapshotImpl).version
	require.NoError(t, snap.Finalize(nil))
	awaitFlushed(t, snap, 2*time.Second)
	require.NoError(t, snap.Release())
	awaitRetired(t, engine, version)
}

// Every write path must be accepted while an iterator is open, and none of them may be visible
// through it.
func TestWritesProceedWhileIteratorIsOpen(t *testing.T) {
	engine, _ := newTestEngine(t, map[string][]byte{"a": []byte("1"), "b": []byte("2")}, 4, 1<<20)
	require.NoError(t, engine.Set([]byte("c"), []byte("3")))

	it, err := engine.Iterator(nil)
	require.NoError(t, err)

	require.NoError(t, engine.Set([]byte("d"), []byte("4")), "adding a key")
	require.NoError(t, engine.Set([]byte("a"), []byte("clobbered")), "overwriting a key")
	require.NoError(t, engine.Delete([]byte("b")), "deleting a key")
	require.NoError(t, engine.BatchSet([]*proto.KVPair{
		{Key: []byte("e"), Value: []byte("5")},
		{Key: []byte("c"), Delete: true},
	}), "a batch mixing a write and a delete")

	require.Equal(t, sortedPairs(map[string]string{"a": "1", "b": "2", "c": "3"}), collectIterator(t, it),
		"the iterator must serve the instant it was created, not the writes that followed")
}

// Sealing the version an iterator copied from must be accepted and must not disturb it.
func TestIteratorSurvivesCommit(t *testing.T) {
	engine, _ := newTestEngine(t, map[string][]byte{"a": []byte("1")}, 4, 1<<20)
	require.NoError(t, engine.Set([]byte("b"), []byte("2")))

	it, err := engine.Iterator(nil)
	require.NoError(t, err)

	snap, err := engine.Commit()
	require.NoError(t, err, "Commit must be accepted while an iterator is open")
	finalizeAndRelease(t, snap)

	require.NoError(t, engine.Set([]byte("c"), []byte("3")))

	require.Equal(t, sortedPairs(map[string]string{"a": "1", "b": "2"}), collectIterator(t, it))
}

// The case where a wrong answer would actually be plausible: after the iterator is built, its data
// is sealed, written to the backing store, and then dropped from the shards' in-memory maps. The
// iterator holds references into those maps, so if retirement invalidated them this is where it
// would show.
func TestIteratorSurvivesFlushAndRetirement(t *testing.T) {
	db := newTestDB(map[string][]byte{"disk": []byte("d")})
	engine := newTestEngineWithDB(t, db, 4, 1<<20)

	want := map[string]string{"disk": "d"}
	for i := 0; i < 20; i++ {
		key, value := fmt.Sprintf("mem-%02d", i), fmt.Sprintf("v%02d", i)
		require.NoError(t, engine.Set([]byte(key), []byte(value)))
		want[key] = value
	}

	it, err := engine.Iterator(nil)
	require.NoError(t, err)

	sealFlushRetire(t, engine)

	// Leave the shards holding entirely different data than when the iterator was made.
	require.NoError(t, engine.Set([]byte("mem-00"), []byte("clobbered")))
	require.NoError(t, engine.Delete([]byte("mem-01")))
	require.NoError(t, engine.Set([]byte("mem-99"), []byte("new")))

	require.Equal(t, sortedPairs(want), collectIterator(t, it))
}

// The property holds for every iterator shape, not only a full ascending scan, so each is asserted.
func TestBoundedAndReverseIteratorsSurviveWrites(t *testing.T) {
	all := map[string]string{"a": "1", "b": "2", "c": "3", "d": "4", "e": "5"}
	inRange := map[string]string{"b": "2", "c": "3", "d": "4"}

	for _, tc := range []struct {
		name string
		opts *types.IterOptions
		want []kvPair
	}{
		{"ascending bounded", &types.IterOptions{
			LowerBound: []byte("b"), UpperBound: []byte("e"),
		}, sortedPairs(inRange)},
		{"descending bounded", &types.IterOptions{
			LowerBound: []byte("b"), UpperBound: []byte("e"), Reverse: true,
		}, reversed(sortedPairs(inRange))},
		{"descending unbounded", &types.IterOptions{
			Reverse: true,
		}, reversed(sortedPairs(all))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Half on disk, half staged, so both sides of the merge are exercised under bounds.
			engine, _ := newTestEngine(t, map[string][]byte{"a": []byte("1"), "c": []byte("3")}, 4, 1<<20)
			for _, k := range []string{"b", "d", "e"} {
				require.NoError(t, engine.Set([]byte(k), []byte(all[k])))
			}

			it, err := engine.Iterator(tc.opts)
			require.NoError(t, err)

			sealFlushRetire(t, engine)
			require.NoError(t, engine.Set([]byte("c"), []byte("clobbered")))
			require.NoError(t, engine.Delete([]byte("d")))

			require.Equal(t, tc.want, collectIterator(t, it))
		})
	}
}

// A key deleted before the iterator is built must stay absent from it, and a key deleted afterwards
// must stay visible. Both directions matter: the first is a tombstone captured in the iterator's
// copy, the second is a tombstone it must never see.
func TestIteratorTombstonesAreFixedAtCreation(t *testing.T) {
	engine, _ := newTestEngine(t, map[string][]byte{
		"deleted-before": []byte("v"), "deleted-after": []byte("v"), "kept": []byte("v"),
	}, 4, 1<<20)
	require.NoError(t, engine.Delete([]byte("deleted-before")))

	it, err := engine.Iterator(nil)
	require.NoError(t, err)

	require.NoError(t, engine.Delete([]byte("deleted-after")))
	sealFlushRetire(t, engine)

	require.Equal(t, sortedPairs(map[string]string{"deleted-after": "v", "kept": "v"}),
		collectIterator(t, it))
}

// Iterators created at different points must each keep their own instant, so one holder cannot see
// another's view.
func TestIteratorsHoldIndependentInstants(t *testing.T) {
	engine, _ := newTestEngine(t, map[string][]byte{"base": []byte("0")}, 4, 1<<20)

	require.NoError(t, engine.Set([]byte("k"), []byte("first")))
	first, err := engine.Iterator(nil)
	require.NoError(t, err)

	commitFinalizeRelease(t, engine)
	require.NoError(t, engine.Set([]byte("k"), []byte("second")))
	second, err := engine.Iterator(nil)
	require.NoError(t, err)

	commitFinalizeRelease(t, engine)
	require.NoError(t, engine.Set([]byte("k"), []byte("third")))

	require.Equal(t, sortedPairs(map[string]string{"base": "0", "k": "first"}), collectIterator(t, first))
	require.Equal(t, sortedPairs(map[string]string{"base": "0", "k": "second"}), collectIterator(t, second))
}

// A key present both on disk and in the shards must resolve to the staged value for the iterator's
// whole life, including after the staged value has been flushed over the disk one and retired.
func TestIteratorKeepsOverrideWinnerAcrossFlush(t *testing.T) {
	db := newTestDB(map[string][]byte{"shared": []byte("disk"), "disk-only": []byte("d")})
	engine := newTestEngineWithDB(t, db, 4, 1<<20)
	require.NoError(t, engine.Set([]byte("shared"), []byte("staged")))
	require.NoError(t, engine.Set([]byte("mem-only"), []byte("m")))

	it, err := engine.Iterator(nil)
	require.NoError(t, err)

	sealFlushRetire(t, engine)
	require.NoError(t, engine.Set([]byte("shared"), []byte("clobbered")))

	require.Equal(t, sortedPairs(map[string]string{
		"shared": "staged", "disk-only": "d", "mem-only": "m",
	}), collectIterator(t, it))
}

// Under the race detector: writers commit in a loop while an iterator is walked to exhaustion. The
// iterator's contents are fixed at creation, which is what gives this test an oracle.
func TestIteratorIsStableUnderConcurrentCommits(t *testing.T) {
	engine, _ := newTestEngine(t, map[string][]byte{"a": []byte("1")}, 8, 1<<20)
	want := map[string]string{"a": "1"}
	for i := 0; i < 200; i++ {
		key, value := fmt.Sprintf("k-%03d", i), fmt.Sprintf("v-%03d", i)
		require.NoError(t, engine.Set([]byte(key), []byte(value)))
		want[key] = value
	}

	it, err := engine.Iterator(nil)
	require.NoError(t, err)

	stop := make(chan struct{})
	var commits atomic.Int64
	var writerErr error
	var writers sync.WaitGroup

	// Closed once the writer has completed a full round, so the drain below cannot outrun it.
	firstRound := make(chan struct{})
	var signalled sync.Once
	signalFirstRound := func() { signalled.Do(func() { close(firstRound) }) }

	writers.Add(1)
	go func() {
		defer writers.Done()
		// Fires on the error paths too, so a writer that fails cannot leave the drain blocked forever.
		defer signalFirstRound()
		for round := 0; ; round++ {
			select {
			case <-stop:
				return
			default:
			}
			// Clobber keys the iterator is holding, and add new ones, then seal it all.
			if err := engine.Set([]byte(fmt.Sprintf("k-%03d", round%200)), []byte("clobbered")); err != nil {
				writerErr = err
				return
			}
			if err := engine.Set([]byte(fmt.Sprintf("new-%03d", round)), []byte("v")); err != nil {
				writerErr = err
				return
			}
			snap, err := engine.Commit()
			if err != nil {
				writerErr = err
				return
			}
			if err := snap.Finalize(nil); err != nil {
				writerErr = err
				return
			}
			if err := snap.Release(); err != nil {
				writerErr = err
				return
			}
			commits.Add(1)
			signalFirstRound()
		}
	}()

	// A full write-and-seal must land while the iterator is open, or the drain finishes first and the
	// assertions below hold vacuously.
	<-firstRound

	got, drainErr := drainIterator(it)
	close(stop)
	writers.Wait()

	// Without this the test passes vacuously: a writer that is refused makes no writes, so the
	// iterator trivially still holds its creation-time contents.
	require.NoError(t, writerErr, "writes must proceed while an iterator is open")
	require.Positive(t, commits.Load(), "the writer must have committed at least once")

	require.NoError(t, drainErr)
	require.NoError(t, it.Close())
	require.Equal(t, sortedPairs(want), got)
}

// Creating an iterator concurrently with a write is NOT safe: the engine copies each shard's staged
// values under that shard's own lock, so a batch spanning two shards can be half-visible to a
// reader being built. flatKV meets this obligation by holding its own lock across creation.
//
// This asserts the positive — a creation serialized against writes always yields one coherent
// instant. The negative is not asserted because provoking the mixed view deterministically would
// need a hook in the engine's creation path, which is not worth carrying for a documented caller
// obligation.
func TestSerializedCreationYieldsOneCoherentInstant(t *testing.T) {
	engine, _ := newTestEngine(t, nil, 8, 1<<20)

	// Keys chosen to span shards; the batch is atomic from the writer's point of view, so a reader
	// must see all of it or none of it.
	batch := make([]*proto.KVPair, 0, 32)
	before := make(map[string]string, 32)
	after := make(map[string]string, 32)
	for i := 0; i < 32; i++ {
		key := fmt.Sprintf("spread-%02d", i)
		require.NoError(t, engine.Set([]byte(key), []byte("before")))
		batch = append(batch, &proto.KVPair{Key: []byte(key), Value: []byte("after")})
		before[key] = "before"
		after[key] = "after"
	}

	var lock sync.Mutex
	for round := 0; round < 50; round++ {
		lock.Lock()
		it, err := engine.Iterator(nil)
		lock.Unlock()
		require.NoError(t, err)

		var batchErr error
		var writer sync.WaitGroup
		writer.Add(1)
		go func() {
			defer writer.Done()
			lock.Lock()
			defer lock.Unlock()
			batchErr = engine.BatchSet(batch)
		}()

		got := collectIterator(t, it)
		writer.Wait()

		// Without this the test passes vacuously: a refused batch never changes the view, so
		// "before" is trivially coherent.
		require.NoError(t, batchErr, "round %d: the batch must be accepted while an iterator is open", round)

		// Either instant is legitimate; a mixture is not.
		if len(got) > 0 && string(got[0].value) == "after" {
			require.Equal(t, sortedPairs(after), got, "round %d saw a torn view", round)
		} else {
			require.Equal(t, sortedPairs(before), got, "round %d saw a torn view", round)
		}

		require.NoError(t, engine.BatchSet(revert(batch)))
	}
}

// revert turns a write batch into one that restores the "before" value for the same keys.
func revert(batch []*proto.KVPair) []*proto.KVPair {
	out := make([]*proto.KVPair, 0, len(batch))
	for _, pair := range batch {
		out = append(out, &proto.KVPair{Key: pair.Key, Value: []byte("before")})
	}
	return out
}

// Iterators are undefined behaviour once the engine has closed, and the engine makes a best-effort
// attempt to say so rather than letting the holder walk into a closed database.
func TestCloseReportsOpenIterators(t *testing.T) {
	engine, _ := newTestEngine(t, map[string][]byte{"k": []byte("v")}, 4, 1<<20)

	it, err := engine.Iterator(nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = it.Close() })

	require.ErrorContains(t, engine.Close(), "iterator",
		"closing with an iterator open must name the leak rather than closing silently")
}
