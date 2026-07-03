package mvcc

import (
	"testing"

	"github.com/cockroachdb/pebble/v2"
	"github.com/stretchr/testify/require"
	"golang.org/x/exp/slices"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	sstest "github.com/sei-protocol/sei-chain/sei-db/db_engine/test"
)

const compactionTestStore = "store1" // matches the store key used by sstest.FillData

func newCompactionTestDB(t *testing.T) *Database {
	t.Helper()

	cfg := config.DefaultStateStoreConfig()
	cfg.Backend = "pebbledb"

	store, err := OpenDB(t.TempDir(), cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	return store.(*Database)
}

// TestPruneCompactsDeletedRange verifies that a prune which deletes keys triggers
// a compaction of the pruned range, and that live data survives it. Without the
// post-prune compaction the deleted keys would linger as tombstones and make
// every later prune scan progressively slower (the root cause of the head-lag
// that creeps in with node uptime).
func TestPruneCompactsDeletedRange(t *testing.T) {
	db := newCompactionTestDB(t)

	require.NoError(t, sstest.FillData(db, 10, 50))

	// Push the data into SSTables so the post-prune compaction has files to act on.
	require.NoError(t, db.storage.Flush())
	compactionsBefore := db.storage.Metrics().Compact.Count

	require.NoError(t, db.Prune(25))

	compactionsAfter := db.storage.Metrics().Compact.Count
	require.Greater(t, compactionsAfter, compactionsBefore,
		"a prune that deletes keys should compact the range it pruned")

	// Live data is preserved: versions <= 25 are gone, later versions remain.
	bz, err := db.Get(compactionTestStore, 25, []byte("key000"))
	require.NoError(t, err)
	require.Nil(t, bz)

	bz, err = db.Get(compactionTestStore, 50, []byte("key000"))
	require.NoError(t, err)
	require.Equal(t, []byte("val000-050"), bz)
}

// TestPruneWithoutDeletionsSkipsCompaction verifies the guard that skips
// compaction entirely when a prune pass deleted nothing, so idle prunes stay
// cheap. The data is deliberately left in the memtable (no flush) so no
// background compaction can be scheduled and pollute the count.
func TestPruneWithoutDeletionsSkipsCompaction(t *testing.T) {
	db := newCompactionTestDB(t)

	require.NoError(t, sstest.FillData(db, 4, 4))

	compactionsBefore := db.storage.Metrics().Compact.Count
	// No version is <= 0, so this prune deletes nothing and must not compact.
	require.NoError(t, db.Prune(0))
	require.Equal(t, compactionsBefore, db.storage.Metrics().Compact.Count,
		"a prune that deletes nothing must not trigger a compaction")
}

// TestCompactPrunedRangeSingleKey verifies the inclusive-bound math for the
// degenerate case where a prune deleted exactly one key (first == last). Pebble
// rejects Compact unless start < end, so the helper must derive an end bound
// strictly greater than the single deleted key.
func TestCompactPrunedRangeSingleKey(t *testing.T) {
	db := newCompactionTestDB(t)

	key := db.mvccEncode([]byte("s/k:store1/key000"), 1)

	// The derived end bound must sort strictly after the deleted key under the
	// MVCC comparer (the default for this config).
	end := append(slices.Clone(key), 0)
	require.Equal(t, -1, MVCCKeyCompare(key, end))

	// And the compaction itself must not be rejected.
	require.NoError(t, db.compactPrunedRange(key, key))
}

// TestPruneAscendingCompactsDeletedRange covers the legacy ascending-encoding
// prune path end to end: a prune that deletes keys must compact the pruned
// range while preserving live data. New DBs use the descending path, so the
// directory is first seeded with a legacy-style DB to force ascending mode.
func TestPruneAscendingCompactsDeletedRange(t *testing.T) {
	dir := t.TempDir()

	// Seed a legacy-style DB: ascending-encoded data plus a latestVersionKey but
	// no descending marker, so OpenDB selects ascending mode.
	{
		raw, err := pebble.Open(dir, &pebble.Options{Comparer: MVCCComparer})
		require.NoError(t, err)
		seedKey := MVCCEncodeAscending(prependStoreKey(compactionTestStore, []byte("seed")), 1)
		require.NoError(t, raw.Set(seedKey, MVCCEncodeAscending([]byte("v"), 0), pebble.Sync))
		var ts [VersionSize]byte
		ts[0] = 1
		require.NoError(t, raw.Set([]byte(latestVersionKey), ts[:], pebble.Sync))
		require.NoError(t, raw.Close())
	}

	cfg := config.DefaultStateStoreConfig()
	cfg.Backend = "pebbledb"
	store, err := OpenDB(dir, cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	db := store.(*Database)
	require.False(t, db.descending, "seeded legacy DB must open in ascending mode")

	require.NoError(t, sstest.FillData(db, 10, 50))

	// Push the data into SSTables so the post-prune compaction has files to act on.
	require.NoError(t, db.storage.Flush())
	compactionsBefore := db.storage.Metrics().Compact.Count

	require.NoError(t, db.Prune(25))

	require.Greater(t, db.storage.Metrics().Compact.Count, compactionsBefore,
		"an ascending prune that deletes keys should compact the range it pruned")

	// Live data is preserved: versions <= 25 are gone, later versions remain.
	bz, err := db.Get(compactionTestStore, 25, []byte("key000"))
	require.NoError(t, err)
	require.Nil(t, bz)

	bz, err = db.Get(compactionTestStore, 50, []byte("key000"))
	require.NoError(t, err)
	require.Equal(t, []byte("val000-050"), bz)
}
