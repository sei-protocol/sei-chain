package view

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestShardVersionedReads(t *testing.T) {
	s := newTestShard(t, 4096, newTestDB(nil))
	require.NoError(t, s.Set([]byte("k"), []byte("v1")))
	require.Equal(t, uint64(2), s.Commit()) // seals v1, live -> v2
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
	_ = s.Commit() // v2
	_ = s.Commit() // v3; no write at v2
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

func TestShardGetDiffsForVersions(t *testing.T) {
	s := newTestShard(t, 4096, newTestDB(nil))
	require.NoError(t, s.Set([]byte("a"), []byte("1")))
	_ = s.Commit() // seals v1, live -> v2
	require.NoError(t, s.Set([]byte("b"), []byte("2")))
	_ = s.Commit() // seals v2, live -> v3 (GetDiffs only covers sealed versions)

	diffs, err := s.GetDiffsForVersions(1, 3) // [1, 3) => versions 1 and 2
	require.NoError(t, err)
	require.Len(t, diffs, 2)
	require.Equal(t, []byte("1"), diffs[0]["a"])
	require.Equal(t, []byte("2"), diffs[1]["b"])
}

func TestShardGetDiffsForVersionsRejectsBadRange(t *testing.T) {
	s := newTestShard(t, 4096, newTestDB(nil))
	_ = s.Commit() // oldest=1, current=2

	_, err := s.GetDiffsForVersions(3, 1)
	require.Error(t, err, "firstVersion > lastVersion")

	_, err = s.GetDiffsForVersions(0, 2)
	require.Error(t, err, "firstVersion below oldest")
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
	_ = s.Commit() // v2
	require.NoError(t, s.Set([]byte("k"), []byte("v2")))
	_ = s.Commit() // v3

	// Drop versions [1, 3): their data collapses into the dbCache, latest value winning.
	require.NoError(t, s.DropVersions(1, 3))

	val, found, err := s.Get([]byte("k"), s.currentVersion, false)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "v2", string(val))
}

func TestShardDropVersionsRejectsBadRange(t *testing.T) {
	s := newTestShard(t, 4096, newTestDB(nil))
	_ = s.Commit()
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
