package snapshot

import (
	"errors"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// iterateUserData collects the full iteration of the engine's mutable version, which the engine
// guarantees is exactly the user data — the metadata hash key is filtered internally. Returns the
// pairs in iteration order.
func iterateUserData(t *testing.T, engine SnapshotEngine) []kvPair {
	t.Helper()
	it, err := engine.Iterator(nil)
	require.NoError(t, err)
	return collectIterator(t, it)
}

func TestIteratorInMemoryAscending(t *testing.T) {
	engine := newTestEngineWithDB(t, newTestDB(nil), 4, 1<<20)
	require.NoError(t, engine.Set([]byte("c"), []byte("3")))
	require.NoError(t, engine.Set([]byte("a"), []byte("1")))
	require.NoError(t, engine.Set([]byte("b"), []byte("2")))

	got := iterateUserData(t, engine)
	require.Equal(t, []kvPair{
		{key: []byte("a"), value: []byte("1")},
		{key: []byte("b"), value: []byte("2")},
		{key: []byte("c"), value: []byte("3")},
	}, got)
}

func TestIteratorOverrideShadowsDB(t *testing.T) {
	engine, _ := newTestEngine(t, map[string][]byte{"k": []byte("old")}, 1, 1<<20)
	require.NoError(t, engine.Set([]byte("k"), []byte("new")))

	got := iterateUserData(t, engine)
	require.Equal(t, []kvPair{{key: []byte("k"), value: []byte("new")}}, got)
}

func TestIteratorTombstoneSuppressesDBKey(t *testing.T) {
	engine, _ := newTestEngine(t, map[string][]byte{"gone": []byte("v"), "keep": []byte("v")}, 1, 1<<20)
	require.NoError(t, engine.Delete([]byte("gone")))

	got := iterateUserData(t, engine)
	require.Equal(t, []kvPair{{key: []byte("keep"), value: []byte("v")}}, got)
}

func TestIteratorMergesMemoryAndDB(t *testing.T) {
	engine, _ := newTestEngine(t, map[string][]byte{"a": []byte("1"), "c": []byte("3")}, 4, 1<<20)
	require.NoError(t, engine.Set([]byte("b"), []byte("2")))
	require.NoError(t, engine.Set([]byte("d"), []byte("4")))

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
		require.NoError(t, engine.Set(k, v))
		want = append(want, kvPair{key: k, value: v})
	}
	sort.Slice(want, func(i, j int) bool { return string(want[i].key) < string(want[j].key) })

	require.Equal(t, want, iterateUserData(t, engine))
}

// The reserved metadata keyspace is engine-internal and must never appear in iteration, even once a
// flush has written it to the underlying DB. (The DB-side values belong to the most recently flushed
// version and are generally stale relative to this snapshot; exposing them would pair data-at-V with
// metadata-at-W.) The whole prefix is filtered, not just one key.
func TestIteratorExcludesReservedPrefix(t *testing.T) {
	db := newTestDB(nil)
	engine := newTestEngineWithDB(t, db, 1, 1<<20)
	reservedPrefix := engine.(*snapshotEngine).config.ReservedPrefix

	// Flush snap1 so several reserved-prefix keys land in the DB.
	metaKeys := []string{"_meta/hash", "_meta/version", "_meta/x:evm/hash"}
	writes := make([]*proto.KVPair, 0, len(metaKeys))
	for _, key := range metaKeys {
		writes = append(writes, &proto.KVPair{Key: []byte(key), Value: []byte("meta")})
	}
	require.NoError(t, engine.Set([]byte("k"), []byte("v")))
	snap1, err := engine.Commit()
	require.NoError(t, err)
	require.NoError(t, snap1.Finalize(writes))
	awaitFlushed(t, snap1, time.Second)
	require.NoError(t, snap1.Release())
	for _, key := range metaKeys {
		require.True(t, db.has(key), "the flush must have written %q to the DB", key)
	}

	// Iteration reads through to the DB, where the metadata now lives; it must be filtered.
	all := iterateUserData(t, engine)

	for _, kv := range all {
		require.NotContains(t, string(kv.key), reservedPrefix,
			"iteration must not expose the reserved metadata keyspace")
	}
	require.Equal(t, []kvPair{{key: []byte("k"), value: []byte("v")}}, all)
}

// A DB backend whose iterator returns a nil slice for a stored zero-length value must not have
// that key mistaken for a tombstone and dropped: the engine normalizes it to a non-nil empty
// value, matching what a caller expects for a key that exists with an empty value.
func TestIteratorNormalizesNilDBValueToEmpty(t *testing.T) {
	// cloneBytes preserves nil, so the seeded nil value flows through the test DB's iterator as a
	// nil slice — the shape a misbehaving backend would produce.
	engine, _ := newTestEngine(t, map[string][]byte{"empty": nil, "k": []byte("v")}, 1, 1<<20)

	got := iterateUserData(t, engine)
	require.Equal(t, []kvPair{
		{key: []byte("empty"), value: []byte{}},
		{key: []byte("k"), value: []byte("v")},
	}, got)
	require.NotNil(t, got[0].value, "a found empty value must be non-nil")
}

func TestIteratorCloseIsIdempotent(t *testing.T) {
	engine := newTestEngineWithDB(t, newTestDB(nil), 1, 1<<20)
	require.NoError(t, engine.Set([]byte("k"), []byte("v")))

	it, err := engine.Iterator(nil)
	require.NoError(t, err)
	require.NoError(t, it.Close())
	require.NoError(t, it.Close())
}

// An open iterator must block every write path, so its view cannot shift beneath it, and closing it
// must release them all.
func TestOpenIteratorBlocksWrites(t *testing.T) {
	engine := newTestEngineWithDB(t, newTestDB(nil), 4, 1<<20)
	require.NoError(t, engine.Set([]byte("k"), []byte("v")))

	it, err := engine.Iterator(nil)
	require.NoError(t, err)

	require.ErrorContains(t, engine.Set([]byte("k"), []byte("v2")), "iterator",
		"Set must be refused while an iterator is open")
	require.ErrorContains(t, engine.Delete([]byte("k")), "iterator",
		"Delete must be refused while an iterator is open")
	require.ErrorContains(t, engine.BatchSet([]*proto.KVPair{{Key: []byte("k"), Value: []byte("v3")}}),
		"iterator", "BatchSet must be refused while an iterator is open")
	_, err = engine.Commit()
	require.ErrorContains(t, err, "iterator", "Commit must be refused while an iterator is open")

	require.NoError(t, it.Close())

	// Every path is writable again.
	require.NoError(t, engine.Set([]byte("k"), []byte("v2")))
	require.NoError(t, engine.Delete([]byte("gone")))
	require.NoError(t, engine.BatchSet([]*proto.KVPair{{Key: []byte("k"), Value: []byte("v3")}}))
	_, err = engine.Commit()
	require.NoError(t, err)
}

// Two iterators open at once must both have to be closed before writes resume — the block is counted,
// not a flag.
func TestWriteBlockIsCountedAcrossIterators(t *testing.T) {
	engine := newTestEngineWithDB(t, newTestDB(nil), 2, 1<<20)
	require.NoError(t, engine.Set([]byte("k"), []byte("v")))

	first, err := engine.Iterator(nil)
	require.NoError(t, err)
	second, err := engine.Iterator(nil)
	require.NoError(t, err)

	require.NoError(t, first.Close())
	require.ErrorContains(t, engine.Set([]byte("k"), []byte("v2")), "iterator",
		"the second iterator must still block writes")

	// Closing is idempotent and must not double-release the block.
	require.NoError(t, first.Close())
	require.ErrorContains(t, engine.Set([]byte("k"), []byte("v2")), "iterator",
		"a repeat Close must not release another iterator's block")

	require.NoError(t, second.Close())
	require.NoError(t, engine.Set([]byte("k"), []byte("v2")))
}

// A bricked engine must refuse to build iterators, for the same reason it refuses reads: it can no
// longer vouch for its data.
func TestIteratorAfterBrickFails(t *testing.T) {
	db := newTestDB(map[string][]byte{"k": []byte("v")})
	engine := newTestEngineWithDB(t, db, 1, 1<<20)

	db.getErrKeys = map[string]error{"k": errors.New("io boom")}
	_, _, err := engine.Get([]byte("k"), true)
	require.ErrorContains(t, err, "io boom")
	db.getErrKeys = nil

	require.Eventually(t, func() bool {
		it, iterErr := engine.Iterator(nil)
		if iterErr == nil {
			require.NoError(t, it.Close())
			return false
		}
		return true
	}, 2*time.Second, time.Millisecond, "a bricked engine must stop building iterators")
}

// --- bounds and direction ---

// iterateWith collects a full iteration under the given options.
func iterateWith(t *testing.T, engine SnapshotEngine, opts *types.IterOptions) []kvPair {
	t.Helper()
	it, err := engine.Iterator(opts)
	require.NoError(t, err)
	return collectIterator(t, it)
}

// keysOf reduces an iteration to its keys, which is what the bounds and ordering tests assert on.
func keysOf(pairs []kvPair) []string {
	out := make([]string, 0, len(pairs))
	for _, p := range pairs {
		out = append(out, string(p.key))
	}
	return out
}

// boundedEngine seeds "a".."f" with the odd letters on disk and the even ones in memory, so every
// bounds and direction case exercises the merge rather than one side alone.
func boundedEngine(t *testing.T) SnapshotEngine {
	t.Helper()
	engine, _ := newTestEngine(t, map[string][]byte{
		"a": []byte("1"), "c": []byte("3"), "e": []byte("5"),
	}, 4, 1<<20)
	require.NoError(t, engine.Set([]byte("b"), []byte("2")))
	require.NoError(t, engine.Set([]byte("d"), []byte("4")))
	require.NoError(t, engine.Set([]byte("f"), []byte("6")))
	return engine
}

func TestIteratorLowerBoundIsInclusive(t *testing.T) {
	engine := boundedEngine(t)
	got := iterateWith(t, engine, &types.IterOptions{LowerBound: []byte("c")})
	require.Equal(t, []string{"c", "d", "e", "f"}, keysOf(got))
}

func TestIteratorUpperBoundIsExclusive(t *testing.T) {
	engine := boundedEngine(t)
	got := iterateWith(t, engine, &types.IterOptions{UpperBound: []byte("d")})
	require.Equal(t, []string{"a", "b", "c"}, keysOf(got))
}

func TestIteratorBothBounds(t *testing.T) {
	engine := boundedEngine(t)
	got := iterateWith(t, engine, &types.IterOptions{LowerBound: []byte("b"), UpperBound: []byte("e")})
	require.Equal(t, []string{"b", "c", "d"}, keysOf(got))
}

// Bounds must filter the in-memory overlay, not only the DB read. A memory-only key outside the range
// would otherwise leak through the merge.
func TestIteratorBoundsFilterInMemoryOverrides(t *testing.T) {
	engine := newTestEngineWithDB(t, newTestDB(nil), 4, 1<<20)
	for _, key := range []string{"a", "b", "c", "d"} {
		require.NoError(t, engine.Set([]byte(key), []byte("v")))
	}
	got := iterateWith(t, engine, &types.IterOptions{LowerBound: []byte("b"), UpperBound: []byte("d")})
	require.Equal(t, []string{"b", "c"}, keysOf(got))
}

func TestIteratorEmptyRangeYieldsNothing(t *testing.T) {
	engine := boundedEngine(t)
	got := iterateWith(t, engine, &types.IterOptions{LowerBound: []byte("x"), UpperBound: []byte("z")})
	require.Empty(t, got)
}

func TestIteratorDescending(t *testing.T) {
	engine := boundedEngine(t)
	got := iterateWith(t, engine, &types.IterOptions{Reverse: true})
	require.Equal(t, []string{"f", "e", "d", "c", "b", "a"}, keysOf(got))
}

func TestIteratorDescendingWithBounds(t *testing.T) {
	engine := boundedEngine(t)
	got := iterateWith(t, engine, &types.IterOptions{
		LowerBound: []byte("b"), UpperBound: []byte("e"), Reverse: true,
	})
	require.Equal(t, []string{"d", "c", "b"}, keysOf(got))
}

// On a key present in both memory and the DB the override wins, in either direction.
func TestIteratorDescendingOverrideShadowsDB(t *testing.T) {
	engine, _ := newTestEngine(t, map[string][]byte{"j": []byte("old"), "k": []byte("old")}, 1, 1<<20)
	require.NoError(t, engine.Set([]byte("k"), []byte("new")))

	got := iterateWith(t, engine, &types.IterOptions{Reverse: true})
	require.Equal(t, []kvPair{
		{key: []byte("k"), value: []byte("new")},
		{key: []byte("j"), value: []byte("old")},
	}, got)
}

func TestIteratorDescendingTombstoneSuppressesDBKey(t *testing.T) {
	engine, _ := newTestEngine(t, map[string][]byte{"gone": []byte("v"), "keep": []byte("v")}, 1, 1<<20)
	require.NoError(t, engine.Delete([]byte("gone")))

	got := iterateWith(t, engine, &types.IterOptions{Reverse: true})
	require.Equal(t, []kvPair{{key: []byte("keep"), value: []byte("v")}}, got)
}

// Regression test for the merge's exhaustion handling under reverse. compareTips decides "one side is
// exhausted" before it compares keys, and those branches must not be inverted along with the key
// comparison: if they were, the exhausted side would win every remaining round and iteration would
// stop early, dropping the tail of whichever side still had data. Both orders of exhaustion are
// covered — overrides running out first, then the DB running out first.
func TestIteratorDescendingDrainsBothSidesAfterOneExhausts(t *testing.T) {
	t.Run("overrides exhaust first", func(t *testing.T) {
		// Descending, the memory keys ("y","z") come first and run out while the DB still holds a..c.
		engine, _ := newTestEngine(t, map[string][]byte{
			"a": []byte("1"), "b": []byte("2"), "c": []byte("3"),
		}, 2, 1<<20)
		require.NoError(t, engine.Set([]byte("y"), []byte("y")))
		require.NoError(t, engine.Set([]byte("z"), []byte("z")))

		got := iterateWith(t, engine, &types.IterOptions{Reverse: true})
		require.Equal(t, []string{"z", "y", "c", "b", "a"}, keysOf(got))
	})

	t.Run("db exhausts first", func(t *testing.T) {
		// Descending, the DB keys ("y","z") come first and run out while memory still holds a..c.
		engine, _ := newTestEngine(t, map[string][]byte{"y": []byte("y"), "z": []byte("z")}, 2, 1<<20)
		for _, key := range []string{"a", "b", "c"} {
			require.NoError(t, engine.Set([]byte(key), []byte("v")))
		}

		got := iterateWith(t, engine, &types.IterOptions{Reverse: true})
		require.Equal(t, []string{"z", "y", "c", "b", "a"}, keysOf(got))
	})
}

// The same coverage ascending, so a sign error that happened to be symmetric cannot hide.
func TestIteratorAscendingDrainsBothSidesAfterOneExhausts(t *testing.T) {
	engine, _ := newTestEngine(t, map[string][]byte{"a": []byte("1"), "b": []byte("2")}, 2, 1<<20)
	for _, key := range []string{"x", "y", "z"} {
		require.NoError(t, engine.Set([]byte(key), []byte("v")))
	}

	got := iterateWith(t, engine, nil)
	require.Equal(t, []string{"a", "b", "x", "y", "z"}, keysOf(got))
}
