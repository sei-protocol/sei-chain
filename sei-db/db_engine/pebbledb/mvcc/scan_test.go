package mvcc

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

type scannedEntry struct {
	key     string
	version int64
	value   string
}

// Scan visits every live entry of one store with its version, in either
// encoding, and counts deletions without visiting them.
func TestScanVisitsLiveEntries(t *testing.T) {
	for _, descending := range []bool{false, true} {
		t.Run(fmt.Sprintf("descending %v", descending), func(t *testing.T) {
			db, dir := newMergeTestDB(t, descending)
			applyVersion(t, db, mergeTestStore, 10, []byte("a"), []byte("a@10"))
			applyVersion(t, db, mergeTestStore, 11, []byte("b"), []byte("b@11"))
			applyVersion(t, db, "other", 11, []byte("x"), []byte("x@11"))
			applyVersion(t, db, mergeTestStore, 12, []byte("a"), []byte("a@12"))
			deleteVersion(t, db, mergeTestStore, 13, []byte("b"))
			require.NoError(t, db.SetEarliestVersion(5, true))
			require.NoError(t, db.Close())

			var got []scannedEntry
			report, err := Scan(dir, ScanOptions{
				StoreKey: mergeTestStore,
				Visit: func(key []byte, version int64, value []byte) error {
					got = append(got, scannedEntry{key: string(key), version: version, value: string(value)})
					return nil
				},
			})
			require.NoError(t, err)
			require.ElementsMatch(t, []scannedEntry{
				{key: "a", version: 10, value: "a@10"},
				{key: "a", version: 12, value: "a@12"},
				{key: "b", version: 11, value: "b@11"},
			}, got)
			require.Equal(t, int64(3), report.Entries)
			require.Equal(t, int64(1), report.Deletions)
			require.Equal(t, descending, report.Descending)
			require.Equal(t, int64(5), report.Earliest)
			require.Equal(t, int64(13), report.Latest)
			require.Equal(t, []string{"other", mergeTestStore}, report.StoreKeys)
			require.Zero(t, report.ChangelogAhead)
		})
	}
}

// A changelog holding a version above the latest marker is reported rather
// than refused, and the DB is still read.
func TestScanReportsUnappliedChangelog(t *testing.T) {
	db, dir := newMergeTestDB(t, true)
	applyVersion(t, db, mergeTestStore, 10, []byte("a"), []byte("a@10"))
	require.NoError(t, db.Close())
	writeChangelogEntry(t, dir, 11)

	report, err := Scan(dir, ScanOptions{StoreKey: mergeTestStore})
	require.NoError(t, err)
	require.Equal(t, int64(11), report.ChangelogAhead)
	require.Equal(t, int64(1), report.Entries)
}

func TestScanStopsOnVisitError(t *testing.T) {
	db, dir := newMergeTestDB(t, true)
	applyVersion(t, db, mergeTestStore, 10, []byte("a"), []byte("a@10"))
	applyVersion(t, db, mergeTestStore, 11, []byte("b"), []byte("b@11"))
	require.NoError(t, db.Close())

	stop := errors.New("stop")
	report, err := Scan(dir, ScanOptions{
		StoreKey: mergeTestStore,
		Visit:    func([]byte, int64, []byte) error { return stop },
	})
	require.ErrorIs(t, err, stop)
	require.Equal(t, int64(1), report.Entries)
}
