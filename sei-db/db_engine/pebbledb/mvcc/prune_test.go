package mvcc

import (
	"testing"

	"github.com/cockroachdb/pebble/v2"
	"github.com/cockroachdb/pebble/v2/vfs"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// rawVersionsForKey returns every on-disk MVCC version for (store, key). Used
// to assert pruning actually deletes data rather than just bumping
// earliestVersion.
func rawVersionsForKey(t *testing.T, db *Database, store string, key []byte) []int64 {
	t.Helper()
	prefix := prependStoreKey(store, key)
	lower := MVCCEncodeDescending(prefix, 0)
	upper := MVCCEncodeDescending(append(append([]byte{}, prefix...), 0x01), 0)
	itr, err := db.storage.NewIter(&pebble.IterOptions{LowerBound: lower, UpperBound: upper})
	require.NoError(t, err)
	defer func() { _ = itr.Close() }()

	var versions []int64
	for itr.First(); itr.Valid(); itr.Next() {
		_, vBz, ok := SplitMVCCKey(itr.Key())
		require.True(t, ok)
		v, err := decodeUint64Descending(vBz)
		require.NoError(t, err)
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

	t.Run("idle store still prunes against previous earliest marker", func(t *testing.T) {
		db := newTestDB(t, true)

		applyVersion(t, db, store, 50, key, []byte("v50"))
		applyVersion(t, db, store, 100, key, []byte("v100"))

		require.NoError(t, db.Prune(150))

		versions := rawVersionsForKey(t, db, store, key)
		require.ElementsMatch(t, []int64{100}, versions,
			"prune must not use the just-advanced marker to skip this store")
	})

}

func TestPruneAdvancesEarliestBeforeDeletingHistory(t *testing.T) {
	db := newTestDB(t, true)

	require.NoError(t, db.storage.Set([]byte("invalid-mvcc-key"), []byte("value"), defaultWriteOpts))

	err := db.Prune(10)
	require.Error(t, err)
	require.Equal(t, int64(11), db.GetEarliestVersion(),
		"earliest marker must advance before a later prune failure")
}

// TestPruneAfterFailedPassRescansIdleStores covers the other half of raising the
// marker first: the pass that follows a failure cannot use that marker as its
// skip baseline. store1 goes idle at version 100, below the raised marker, so
// skipping it would leave v50 on disk with no read able to reach it.
func TestPruneAfterFailedPassRescansIdleStores(t *testing.T) {
	const store = "store1"
	key := []byte("k")
	db := newTestDB(t, true)

	applyVersion(t, db, store, 50, key, []byte("v50"))
	applyVersion(t, db, store, 100, key, []byte("v100"))

	// "invalid-mvcc-key" sorts ahead of every "s/k:" store key, so the pass
	// fails after raising the marker and before deleting anything.
	badKey := []byte("invalid-mvcc-key")
	require.NoError(t, db.storage.Set(badKey, []byte("value"), defaultWriteOpts))
	require.Error(t, db.Prune(150))
	require.Equal(t, int64(151), db.GetEarliestVersion())
	require.ElementsMatch(t, []int64{50, 100}, rawVersionsForKey(t, db, store, key),
		"the failed pass must not have deleted anything")

	require.NoError(t, db.storage.Delete(badKey, defaultWriteOpts))
	require.NoError(t, db.Prune(150))

	require.ElementsMatch(t, []int64{100}, rawVersionsForKey(t, db, store, key),
		"the pass after a failure must rescan a store the raised marker would skip")
}

// TestAdvanceEarliestVersionAcceptsAHigherMarker pins the outcome a prune pass
// sees when another writer moves the marker past its target. Raising the marker
// now runs ahead of the deletes, so reporting that as a failure would cost the
// whole pass rather than just the marker write.
func TestAdvanceEarliestVersionAcceptsAHigherMarker(t *testing.T) {
	db := newTestDB(t, true)

	require.NoError(t, db.SetEarliestVersion(200, false))
	require.NoError(t, db.advanceEarliestVersion(151))
	require.Equal(t, int64(200), db.GetEarliestVersion(),
		"the target must not lower a marker another writer raised past it")
}

// TestAdvanceEarliestVersionReturnsPersistenceFailure pins that Pebble must
// accept the metadata write before the in-memory marker moves. Otherwise a
// later call with the same target would see the target in memory, return nil,
// and let pruning delete history under a marker that was never persisted.
func TestAdvanceEarliestVersionReturnsPersistenceFailure(t *testing.T) {
	fs := vfs.NewMem()
	storage, err := pebble.Open("db", &pebble.Options{FS: fs})
	require.NoError(t, err)
	require.NoError(t, storage.Close())

	storage, err = pebble.Open("db", &pebble.Options{FS: fs, ReadOnly: true})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, storage.Close()) })

	db := &Database{storage: storage}
	err = db.advanceEarliestVersion(151)

	require.ErrorIs(t, err, pebble.ErrReadOnly)
	require.Zero(t, db.GetEarliestVersion(),
		"a failed metadata write must not move the in-memory marker")
}
