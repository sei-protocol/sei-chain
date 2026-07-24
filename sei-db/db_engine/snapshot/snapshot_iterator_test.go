package snapshot

import (
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// iterateUserData snapshots the engine, hashes it, collects the full iteration (which the engine
// guarantees is exactly the user data — the metadata hash key is filtered internally), and
// releases. Returns the pairs in iteration order.
func iterateUserData(t *testing.T, engine SnapshotEngine) []kvPair {
	t.Helper()
	snap, err := engine.Commit()
	require.NoError(t, err)
	require.NoError(t, snap.SetHash(testHash))
	out := collectIterator(t, snap.Iterator())
	require.NoError(t, snap.Release())
	return out
}

func TestIteratorInMemoryAscending(t *testing.T) {
	engine := newTestEngineWithDB(t, newTestDB(nil), 4, 1<<20)
	engine.Set([]byte("c"), []byte("3"))
	engine.Set([]byte("a"), []byte("1"))
	engine.Set([]byte("b"), []byte("2"))

	got := iterateUserData(t, engine)
	require.Equal(t, []kvPair{
		{key: []byte("a"), value: []byte("1")},
		{key: []byte("b"), value: []byte("2")},
		{key: []byte("c"), value: []byte("3")},
	}, got)
}

func TestIteratorOverrideShadowsDB(t *testing.T) {
	engine, _ := newTestEngine(t, map[string][]byte{"k": []byte("old")}, 1, 1<<20)
	engine.Set([]byte("k"), []byte("new"))

	got := iterateUserData(t, engine)
	require.Equal(t, []kvPair{{key: []byte("k"), value: []byte("new")}}, got)
}

func TestIteratorTombstoneSuppressesDBKey(t *testing.T) {
	engine, _ := newTestEngine(t, map[string][]byte{"gone": []byte("v"), "keep": []byte("v")}, 1, 1<<20)
	engine.Delete([]byte("gone"))

	got := iterateUserData(t, engine)
	require.Equal(t, []kvPair{{key: []byte("keep"), value: []byte("v")}}, got)
}

func TestIteratorMergesMemoryAndDB(t *testing.T) {
	engine, _ := newTestEngine(t, map[string][]byte{"a": []byte("1"), "c": []byte("3")}, 4, 1<<20)
	engine.Set([]byte("b"), []byte("2"))
	engine.Set([]byte("d"), []byte("4"))

	got := iterateUserData(t, engine)
	require.Equal(t, []kvPair{
		{key: []byte("a"), value: []byte("1")},
		{key: []byte("b"), value: []byte("2")},
		{key: []byte("c"), value: []byte("3")},
		{key: []byte("d"), value: []byte("4")},
	}, got)
}

func TestIteratorMergesAcrossShards(t *testing.T) {
	engine := newTestEngineWithDB(t, newTestDB(nil), 8, 1<<20)
	var want []kvPair
	for i := 0; i < 50; i++ {
		k := []byte(fmt.Sprintf("key-%02d", i))
		v := []byte(fmt.Sprintf("val-%02d", i))
		engine.Set(k, v)
		want = append(want, kvPair{key: k, value: v})
	}
	sort.Slice(want, func(i, j int) bool { return string(want[i].key) < string(want[j].key) })

	require.Equal(t, want, iterateUserData(t, engine))
}

// The metadata hash key is engine-internal and must never appear in iteration, even once a flush
// has written it to the underlying DB. (The DB-side value is the most recently flushed hash,
// which is generally stale relative to the snapshot; exposing it would pair data-at-V with
// hash-at-W. Consumers get the snapshot's hash from AwaitHash.)
func TestIteratorExcludesHashKey(t *testing.T) {
	db := newTestDB(nil)
	engine := newTestEngineWithDB(t, db, 1, 1<<20)
	hashKey := engine.(*snapshotEngine).config.HashKey

	// Flush snap1 so the hash key lands in the DB.
	engine.Set([]byte("k"), []byte("v"))
	snap1, err := engine.Commit()
	require.NoError(t, err)
	require.NoError(t, snap1.SetHash(testHash))
	awaitFlushed(t, snap1, time.Second)
	require.NoError(t, snap1.Release())
	require.True(t, db.has(hashKey), "the flush must have written the hash key to the DB")

	// snap2's iteration reads through to the DB, where the hash key now lives; it must be filtered.
	snap2, err := engine.Commit()
	require.NoError(t, err)
	require.NoError(t, snap2.SetHash(testHash))
	all := collectIterator(t, snap2.Iterator())
	require.NoError(t, snap2.Release())

	for _, kv := range all {
		require.NotEqual(t, hashKey, string(kv.key), "iteration must not expose the metadata hash key")
	}
	require.Equal(t, []kvPair{{key: []byte("k"), value: []byte("v")}}, all)
}

func TestIteratorCloseIsIdempotent(t *testing.T) {
	engine := newTestEngineWithDB(t, newTestDB(nil), 1, 1<<20)
	engine.Set([]byte("k"), []byte("v"))
	snap, err := engine.Commit()
	require.NoError(t, err)
	require.NoError(t, snap.SetHash(testHash))

	it := snap.Iterator()
	require.NoError(t, it.Close())
	require.NoError(t, it.Close())
	require.NoError(t, snap.Release())
}

func TestIteratorAfterEngineShutdownSurfacesError(t *testing.T) {
	db := newTestDB(nil)
	engine := newTestEngineWithDB(t, db, 1, 1<<20)
	engine.Set([]byte("k"), []byte("v"))
	snap, err := engine.Commit()
	require.NoError(t, err)
	require.NoError(t, snap.SetHash(testHash)) // held (not released) across Close

	require.NoError(t, engine.Close())

	it := snap.Iterator()
	_, _, _, err = it.Next()
	require.Error(t, err, "iterator built after engine shutdown must surface an error")
	require.NoError(t, it.Close())
}
