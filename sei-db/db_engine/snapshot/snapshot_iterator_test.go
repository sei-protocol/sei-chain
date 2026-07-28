package snapshot

import (
	"errors"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// iterateUserData collects the full iteration of the engine's mutable version, which the engine
// guarantees is exactly the user data — the metadata hash key is filtered internally. Returns the
// pairs in iteration order.
func iterateUserData(t *testing.T, engine SnapshotEngine) []kvPair {
	t.Helper()
	it, err := engine.Iterator()
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

// The metadata hash key is engine-internal and must never appear in iteration, even once a flush
// has written it to the underlying DB. (The DB-side value is the most recently flushed hash,
// which is generally stale relative to the snapshot; exposing it would pair data-at-V with
// hash-at-W. Consumers get the snapshot's hash from AwaitHash.)
func TestIteratorExcludesHashKey(t *testing.T) {
	db := newTestDB(nil)
	engine := newTestEngineWithDB(t, db, 1, 1<<20)
	hashKey := engine.(*snapshotEngine).config.HashKey

	// Flush snap1 so the hash key lands in the DB.
	require.NoError(t, engine.Set([]byte("k"), []byte("v")))
	snap1, err := engine.Commit()
	require.NoError(t, err)
	require.NoError(t, snap1.SetHash(testHash))
	awaitFlushed(t, snap1, time.Second)
	require.NoError(t, snap1.Release())
	require.True(t, db.has(hashKey), "the flush must have written the hash key to the DB")

	// Iteration reads through to the DB, where the hash key now lives; it must be filtered.
	all := iterateUserData(t, engine)

	for _, kv := range all {
		require.NotEqual(t, hashKey, string(kv.key), "iteration must not expose the metadata hash key")
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

	it, err := engine.Iterator()
	require.NoError(t, err)
	require.NoError(t, it.Close())
	require.NoError(t, it.Close())
}

// An open iterator must block every write path, so its view cannot shift beneath it, and closing it
// must release them all.
func TestOpenIteratorBlocksWrites(t *testing.T) {
	engine := newTestEngineWithDB(t, newTestDB(nil), 4, 1<<20)
	require.NoError(t, engine.Set([]byte("k"), []byte("v")))

	it, err := engine.Iterator()
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

	first, err := engine.Iterator()
	require.NoError(t, err)
	second, err := engine.Iterator()
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
		it, iterErr := engine.Iterator()
		if iterErr == nil {
			require.NoError(t, it.Close())
			return false
		}
		return true
	}, 2*time.Second, time.Millisecond, "a bricked engine must stop building iterators")
}
