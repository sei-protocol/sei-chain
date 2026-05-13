package mvcc

import (
	"testing"

	"github.com/cockroachdb/pebble/v2"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// TestDescendingMVCCMarker_FreshDBWritesMarker verifies that opening an empty
// pebbledb writes the descending-MVCC sentinel so subsequent opens fast-path,
// and that the DB opens in descending mode.
func TestDescendingMVCCMarker_FreshDBWritesMarker(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultStateStoreConfig()
	cfg.Backend = "pebbledb"

	store, err := OpenDB(dir, cfg)
	require.NoError(t, err)
	db := store.(*Database)
	require.True(t, db.descending, "fresh DB must open in descending mode")
	require.NoError(t, store.Close())

	// Reopen the raw pebble DB and check the sentinel is there.
	raw, err := pebble.Open(dir, &pebble.Options{Comparer: MVCCComparer})
	require.NoError(t, err)
	defer func() { _ = raw.Close() }()

	val, closer, err := raw.Get([]byte(descendingMVCCMarkerKey))
	require.NoError(t, err)
	require.NotEmpty(t, val)
	require.NoError(t, closer.Close())
}

// TestDescendingMVCCMarker_LegacyDBOpensInAscendingMode simulates a DB
// written by the legacy ascending-version build and asserts OpenDB does NOT
// error, instead returning a Database that operates in ascending mode. It
// then performs a write + read round-trip to confirm correctness, and
// verifies no descending marker was written to the legacy DB.
func TestDescendingMVCCMarker_LegacyDBOpensInAscendingMode(t *testing.T) {
	dir := t.TempDir()

	// Seed the directory with a legacy-style DB: some ascending-encoded data
	// plus a latestVersionKey, but no descending marker.
	{
		raw, err := pebble.Open(dir, &pebble.Options{Comparer: MVCCComparer})
		require.NoError(t, err)
		// Write a value for ("store1", "k") at version 1 using the ascending
		// encoding, matching what a legacy build would have persisted.
		prefixedKey := MVCCEncodeAscending(prependStoreKey("store1", []byte("k")), 1)
		prefixedVal := MVCCEncodeAscending([]byte("v1"), 0)
		require.NoError(t, raw.Set(prefixedKey, prefixedVal, pebble.Sync))
		// Set latestVersionKey so detectMVCCMode sees a populated DB.
		var ts [VersionSize]byte
		ts[0] = 1
		require.NoError(t, raw.Set([]byte(latestVersionKey), ts[:], pebble.Sync))
		require.NoError(t, raw.Close())
	}

	cfg := config.DefaultStateStoreConfig()
	cfg.Backend = "pebbledb"

	store, err := OpenDB(dir, cfg)
	require.NoError(t, err, "legacy DB must open without error")
	db := store.(*Database)
	require.False(t, db.descending, "legacy DB must open in ascending mode")

	// Pre-existing value is readable via the ascending path.
	got, err := db.Get("store1", 1, []byte("k"))
	require.NoError(t, err)
	require.Equal(t, []byte("v1"), got)

	// Writes land in ascending encoding and round-trip correctly.
	require.NoError(t, db.ApplyChangesetSync(2, []*proto.NamedChangeSet{{
		Name:      "store1",
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{{Key: []byte("k"), Value: []byte("v2")}}},
	}}))
	got, err = db.Get("store1", 2, []byte("k"))
	require.NoError(t, err)
	require.Equal(t, []byte("v2"), got)

	require.NoError(t, store.Close())

	// Confirm we did NOT stamp the descending marker on a legacy DB.
	raw, err := pebble.Open(dir, &pebble.Options{Comparer: MVCCComparer})
	require.NoError(t, err)
	defer func() { _ = raw.Close() }()

	_, closer, err := raw.Get([]byte(descendingMVCCMarkerKey))
	require.ErrorIs(t, err, pebble.ErrNotFound, "legacy DB must stay unmarked")
	if closer != nil {
		_ = closer.Close()
	}
}

// TestDescendingMVCCMarker_RoundTrip writes data with OpenDB, reopens, and
// confirms the second open succeeds in descending mode (marker is honored).
func TestDescendingMVCCMarker_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultStateStoreConfig()
	cfg.Backend = "pebbledb"

	store, err := OpenDB(dir, cfg)
	require.NoError(t, err)
	db := store.(*Database)
	require.True(t, db.descending)
	applyVersion(t, db, "store1", 1, []byte("k"), []byte("v"))
	require.NoError(t, db.Close())

	store2, err := OpenDB(dir, cfg)
	require.NoError(t, err)
	db2 := store2.(*Database)
	require.True(t, db2.descending, "marked DB must reopen in descending mode")
	require.NoError(t, store2.Close())
}
