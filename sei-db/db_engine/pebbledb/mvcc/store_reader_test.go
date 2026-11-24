package mvcc

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// readStore returns every key a StoreReader yields for storeKey in the DB at
// dir, with its version and value.
func readStore(t *testing.T, dir, storeKey string) []scannedEntry {
	t.Helper()
	r, err := OpenStoreReader(dir, storeKey)
	require.NoError(t, err)
	defer func() { require.NoError(t, r.Close()) }()
	var got []scannedEntry
	for {
		ok, err := r.Next()
		require.NoError(t, err)
		if !ok {
			return got
		}
		got = append(got, scannedEntry{key: string(r.Key()), version: r.Version(), value: string(r.Value())})
	}
}

// A StoreReader yields each key of its store once, in key order, with the
// newest version, in either encoding, and skips a key that version deletes.
func TestStoreReaderYieldsTheNewestValues(t *testing.T) {
	for _, descending := range []bool{false, true} {
		t.Run(fmt.Sprintf("descending %v", descending), func(t *testing.T) {
			db, dir := newMergeTestDB(t, descending)
			applyVersion(t, db, mergeTestStore, 10, []byte("a"), []byte("a@10"))
			applyVersion(t, db, mergeTestStore, 11, []byte("b"), []byte("b@11"))
			applyVersion(t, db, "other", 11, []byte("x"), []byte("x@11"))
			applyVersion(t, db, mergeTestStore, 12, []byte("a"), []byte("a@12"))
			applyVersion(t, db, mergeTestStore, 12, []byte("c"), []byte("c@12"))
			deleteVersion(t, db, mergeTestStore, 13, []byte("b"))
			require.NoError(t, db.Close())

			require.Equal(t, []scannedEntry{
				{key: "a", version: 12, value: "a@12"},
				{key: "c", version: 12, value: "c@12"},
			}, readStore(t, dir, mergeTestStore))
		})
	}
}

// Entries above the latest version marker are not read, as a read at the
// latest version does not see them.
func TestStoreReaderStopsAtTheLatestVersion(t *testing.T) {
	db, dir := newMergeTestDB(t, true)
	applyVersion(t, db, mergeTestStore, 10, []byte("a"), []byte("a@10"))
	applyVersion(t, db, mergeTestStore, 20, []byte("a"), []byte("a@20"))
	applyVersion(t, db, mergeTestStore, 20, []byte("b"), []byte("b@20"))
	require.NoError(t, db.SetLatestVersion(15))
	require.NoError(t, db.Close())

	require.Equal(t, []scannedEntry{{key: "a", version: 10, value: "a@10"}}, readStore(t, dir, mergeTestStore))
}

// A changelog the DB has applied is accepted, and one holding a version above
// the DB's latest is refused, since a read at the latest version would miss
// what only opening the DB applies.
func TestStoreReaderChecksTheChangelog(t *testing.T) {
	for _, tc := range []struct {
		name             string
		changelogVersion int64
		wantErr          string
	}{
		{name: "applied", changelogVersion: 10},
		{name: "ahead", changelogVersion: 11, wantErr: "changelog holds version 11 above latest version 10"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, dir := newMergeTestDB(t, true)
			applyVersion(t, db, mergeTestStore, 10, []byte("a"), []byte("a@10"))
			require.NoError(t, db.Close())
			writeChangelogEntry(t, dir, tc.changelogVersion)

			if tc.wantErr == "" {
				require.Equal(t, []scannedEntry{{key: "a", version: 10, value: "a@10"}}, readStore(t, dir, mergeTestStore))
				return
			}
			_, err := OpenStoreReader(dir, mergeTestStore)
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}
