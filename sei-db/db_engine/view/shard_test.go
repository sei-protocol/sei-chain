package view

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestShardVersionedReads(t *testing.T) {
	s := newTestShard(t, 4096, newTestDB(nil))
	require.NoError(t, s.Set([]byte("k"), []byte("v1")))
	require.Equal(t, uint64(2), commitShard(t, s)) // seals v1, live -> v2
	require.NoError(t, s.Set([]byte("k"), []byte("v2")))

	for _, tc := range []struct {
		version uint64
		want    string
	}{{1, "v1"}, {2, "v2"}} {
		val, found, err := s.Get([]byte("k"), tc.version, false)
		require.NoError(t, err, "version=%d", tc.version)
		require.True(t, found, "version=%d", tc.version)
		require.Equal(t, tc.want, string(val), "version=%d", tc.version)
	}
}

func TestShardGetMostRecentValueAtOrBelowVersion(t *testing.T) {
	s := newTestShard(t, 4096, newTestDB(nil))
	require.NoError(t, s.Set([]byte("k"), []byte("v1")))
	commitShard(t, s) // v2
	commitShard(t, s) // v3; no write at v2
	require.NoError(t, s.Set([]byte("k"), []byte("v3")))

	// Reading at v2 (no write there) returns v1 (highest version <= 2).
	val, found, err := s.Get([]byte("k"), 2, false)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "v1", string(val))
}

func TestShardValidateVersionUnderflow(t *testing.T) {
	s := newTestShard(t, 4096, newTestDB(nil))
	_, _, err := s.Get([]byte("k"), 0, false)
	require.ErrorContains(t, err, "oldest")
}

func TestShardValidateVersionOverflow(t *testing.T) {
	s := newTestShard(t, 4096, newTestDB(nil))
	_, _, err := s.Get([]byte("k"), s.currentVersion+5, false)
	require.ErrorContains(t, err, "current")
}

func TestShardSortedDiffCarriesEachSealedVersion(t *testing.T) {
	s := newTestShard(t, 4096, newTestDB(nil))
	require.NoError(t, s.Set([]byte("b"), []byte("1")))
	require.NoError(t, s.Set([]byte("a"), []byte("1")))
	commitShard(t, s) // seals v1, live -> v2
	require.NoError(t, s.Set([]byte("c"), []byte("2")))
	commitShard(t, s) // seals v2, live -> v3 (only sealed versions have an ordered diff)

	require.NoError(t, s.MaterializeSortedDiff(1))
	require.NoError(t, s.MaterializeSortedDiff(2))

	first, err := s.SortedDiff(1)
	require.NoError(t, err)
	require.Equal(t, []Write{{Key: "a", Value: []byte("1")}, {Key: "b", Value: []byte("1")}}, first,
		"a version's diff must be ordered by key")

	second, err := s.SortedDiff(2)
	require.NoError(t, err)
	require.Equal(t, []Write{{Key: "c", Value: []byte("2")}}, second)
}

// Materializing replaces the map the version was accumulated in, and says so: a second call has nothing
// left to take and must not disturb the diff already published.
func TestShardMaterializeIsIdempotentAndDropsTheMap(t *testing.T) {
	s := newTestShard(t, 4096, newTestDB(nil))
	require.NoError(t, s.Set([]byte("k"), []byte("v")))
	commitShard(t, s)

	require.NoError(t, s.MaterializeSortedDiff(1))

	s.lock.RLock()
	_, mapStillThere := s.versionDiffs[1]
	s.lock.RUnlock()
	require.False(t, mapStillThere, "materializing must drop the version's diff map")

	require.NoError(t, s.MaterializeSortedDiff(1))
	entries, err := s.SortedDiff(1)
	require.NoError(t, err)
	require.Equal(t, []Write{{Key: "k", Value: []byte("v")}}, entries)
}

func TestShardSortedDiffRejectsUnsealedVersions(t *testing.T) {
	s := newTestShard(t, 4096, newTestDB(nil))
	commitShard(t, s) // oldest=1, current=2

	_, err := s.SortedDiff(2)
	require.Error(t, err, "the current version is not sealed")

	require.Error(t, s.MaterializeSortedDiff(2), "the current version cannot be materialized")
	require.Error(t, s.MaterializeSortedDiff(0), "a version below the oldest is not tracked")
}

func TestShardDeleteWritesTombstone(t *testing.T) {
	s := newTestShard(t, 4096, newTestDB(nil))
	require.NoError(t, s.Set([]byte("k"), []byte("v")))
	require.NoError(t, s.Delete([]byte("k")))

	// Delete in the same version overwrites the value with a tombstone (nil).
	val, found, err := s.Get([]byte("k"), s.currentVersion, false)
	require.NoError(t, err)
	require.False(t, found)
	require.Nil(t, val)
}

func TestShardDropVersionsPushesLatestToDB(t *testing.T) {
	s := newTestShard(t, 4096, newTestDB(nil))
	require.NoError(t, s.Set([]byte("k"), []byte("v1")))
	commitShard(t, s) // v2
	require.NoError(t, s.Set([]byte("k"), []byte("v2")))
	commitShard(t, s) // v3

	// Drop versions [1, 3): their data collapses into the dbCache, latest value winning.
	require.NoError(t, s.DropVersions(1, 3))

	val, found, err := s.Get([]byte("k"), s.currentVersion, false)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "v2", string(val))
}

func TestShardDropVersionsRejectsBadRange(t *testing.T) {
	s := newTestShard(t, 4096, newTestDB(nil))
	commitShard(t, s)
	require.Error(t, s.DropVersions(2, 1)) // first >= last
	require.Error(t, s.DropVersions(2, 3)) // first != oldest
}

// TestShardConcurrentReadsCollapseToOneDBRead verifies that two concurrent Gets for the same
// uncached key issue only a single read to the backing store.
func TestShardConcurrentReadsCollapseToOneDBRead(t *testing.T) {
	db := newTestDB(map[string][]byte{"k": []byte("v")})
	db.getGate = make(chan struct{}) // hold the read open until we release it
	s := newTestShard(t, 4096, db)

	type result struct {
		val   []byte
		found bool
		err   error
	}
	results := make(chan result, 2)
	for i := 0; i < 2; i++ {
		go func() {
			val, found, err := s.Get([]byte("k"), s.currentVersion, true)
			results <- result{val, found, err}
		}()
	}

	// Wait for the first (and only) DB read to be in flight, then release it.
	for db.getCalls.Load() == 0 {
	}
	close(db.getGate)

	for i := 0; i < 2; i++ {
		r := <-results
		require.NoError(t, r.err)
		require.True(t, r.found)
		require.Equal(t, "v", string(r.val))
	}
	require.Equal(t, int64(1), db.getCalls.Load(), "concurrent Gets must collapse to one DB read")
}
