package mvcc

import (
	"testing"

	"github.com/cockroachdb/pebble/v2"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// rawVersionsForKey returns every on-disk MVCC version for (store, key),
// excluding the sentinel latest-pointer entry. Used to assert pruning
// actually deletes data rather than just bumping earliestVersion.
func rawVersionsForKey(t *testing.T, db *Database, store string, key []byte) []int64 {
	t.Helper()
	prefix := prependStoreKey(store, key)
	lower := MVCCEncode(prefix, 0)
	upper := MVCCEncode(append(append([]byte{}, prefix...), 0x01), 0)
	itr, err := db.storage.NewIter(&pebble.IterOptions{LowerBound: lower, UpperBound: upper})
	require.NoError(t, err)
	defer func() { _ = itr.Close() }()

	var versions []int64
	for itr.First(); itr.Valid(); itr.Next() {
		_, vBz, ok := SplitMVCCKey(itr.Key())
		require.True(t, ok)
		v, err := decodeUint64Descending(vBz)
		require.NoError(t, err)
		if v == latestPointerVersion {
			continue
		}
		versions = append(versions, v)
	}
	return versions
}

func applyVersion(t *testing.T, db *Database, store string, v int64, key, val []byte) {
	t.Helper()
	require.NoError(t, db.ApplyChangesetSync(v, []*proto.NamedChangeSet{{
		Name:      store,
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{{Key: key, Value: val}}},
	}}))
}

func newTestDB(t *testing.T, keepLast bool) *Database {
	t.Helper()
	cfg := config.DefaultStateStoreConfig()
	cfg.Backend = "pebbledb"
	cfg.KeepLastVersion = keepLast
	store, err := OpenDB(t.TempDir(), cfg)
	require.NoError(t, err)
	db := store.(*Database)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// TestPruneDescendingOrder_DeletesOldVersions is a regression test for the
// descending-encoding prune bug: when the newest version of a key is above
// the prune height, older versions that fall below the prune height must
// still be physically deleted. The previous logic called NextPrefix() on
// hitting the newest version and leaked every older version.
func TestPruneDescendingOrder_DeletesOldVersions(t *testing.T) {
	const store = "store1"
	key := []byte("k")

	t.Run("KeepLastVersion=true leaves newest + newest-below-prune", func(t *testing.T) {
		db := newTestDB(t, true)

		applyVersion(t, db, store, 50, key, []byte("v50"))
		applyVersion(t, db, store, 100, key, []byte("v100"))
		applyVersion(t, db, store, 200, key, []byte("v200"))

		require.NoError(t, db.Prune(150))

		versions := rawVersionsForKey(t, db, store, key)
		require.ElementsMatch(t, []int64{100, 200}, versions,
			"v50 must be physically deleted; v100 kept as newest below prune; v200 kept as above prune")
	})

	t.Run("KeepLastVersion=false deletes every version <= prune", func(t *testing.T) {
		db := newTestDB(t, false)

		applyVersion(t, db, store, 50, key, []byte("v50"))
		applyVersion(t, db, store, 100, key, []byte("v100"))
		applyVersion(t, db, store, 200, key, []byte("v200"))

		require.NoError(t, db.Prune(150))

		versions := rawVersionsForKey(t, db, store, key)
		require.ElementsMatch(t, []int64{200}, versions,
			"everything at or below prune height must be deleted when KeepLastVersion=false")
	})

	t.Run("all versions above prune are retained", func(t *testing.T) {
		db := newTestDB(t, true)

		applyVersion(t, db, store, 200, key, []byte("v200"))
		applyVersion(t, db, store, 300, key, []byte("v300"))

		require.NoError(t, db.Prune(150))

		versions := rawVersionsForKey(t, db, store, key)
		require.ElementsMatch(t, []int64{200, 300}, versions)
	})

	t.Run("multiple keys pruned independently", func(t *testing.T) {
		db := newTestDB(t, true)

		k1, k2 := []byte("k1"), []byte("k2")
		applyVersion(t, db, store, 50, k1, []byte("a"))
		applyVersion(t, db, store, 100, k1, []byte("b"))
		applyVersion(t, db, store, 200, k1, []byte("c"))

		applyVersion(t, db, store, 60, k2, []byte("x"))
		applyVersion(t, db, store, 140, k2, []byte("y"))

		require.NoError(t, db.Prune(150))

		require.ElementsMatch(t, []int64{100, 200}, rawVersionsForKey(t, db, store, k1))
		require.ElementsMatch(t, []int64{140}, rawVersionsForKey(t, db, store, k2))
	})

	t.Run("latest-pointer sentinel is never pruned", func(t *testing.T) {
		db := newTestDB(t, true)
		applyVersion(t, db, store, 50, key, []byte("v50"))
		applyVersion(t, db, store, 100, key, []byte("v100"))

		require.NoError(t, db.Prune(150))

		// Sentinel must still serve the latest-value fast path.
		bz, err := db.Get(store, 1000, key)
		require.NoError(t, err)
		require.Equal(t, []byte("v100"), bz)
	})
}
