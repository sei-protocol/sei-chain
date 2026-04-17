package mvcc

import (
	"encoding/binary"
	"testing"

	"github.com/cockroachdb/pebble/v2"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/config"
)

// TestDescendingMVCCMarker_FreshDBWritesMarker verifies that opening an empty
// pebbledb writes the descending-MVCC sentinel so subsequent opens fast-path.
func TestDescendingMVCCMarker_FreshDBWritesMarker(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultStateStoreConfig()
	cfg.Backend = "pebbledb"

	store, err := OpenDB(dir, cfg)
	require.NoError(t, err)
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

// TestDescendingMVCCMarker_LegacyDBRejected simulates a DB written by the old
// ascending-version build (latestVersionKey present, no marker) and asserts we
// refuse to open it rather than silently returning wrong versions.
func TestDescendingMVCCMarker_LegacyDBRejected(t *testing.T) {
	dir := t.TempDir()

	raw, err := pebble.Open(dir, &pebble.Options{Comparer: MVCCComparer})
	require.NoError(t, err)
	var ts [VersionSize]byte
	binary.LittleEndian.PutUint64(ts[:], uint64(42))
	require.NoError(t, raw.Set([]byte(latestVersionKey), ts[:], pebble.Sync))
	require.NoError(t, raw.Close())

	cfg := config.DefaultStateStoreConfig()
	cfg.Backend = "pebbledb"

	_, err = OpenDB(dir, cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "state sync required")
}

// TestDescendingMVCCMarker_RoundTrip writes data with OpenDB, reopens, and
// confirms the second open succeeds (marker is honored, no false rejection).
func TestDescendingMVCCMarker_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultStateStoreConfig()
	cfg.Backend = "pebbledb"

	store, err := OpenDB(dir, cfg)
	require.NoError(t, err)
	db := store.(*Database)
	applyVersion(t, db, "store1", 1, []byte("k"), []byte("v"))
	require.NoError(t, db.Close())

	store2, err := OpenDB(dir, cfg)
	require.NoError(t, err)
	require.NoError(t, store2.Close())
}
