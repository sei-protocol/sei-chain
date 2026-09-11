package view

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestShardVersionedReads(t *testing.T) {
	s := newTestShard(t, 4096, newTestDB(nil))
	require.NoError(t, setShardKey(s, []byte("k"), []byte("v1")))
	require.Equal(t, uint64(2), commitShard(t, s)) // seals v1, live -> v2
	require.NoError(t, setShardKey(s, []byte("k"), []byte("v2")))

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
	require.NoError(t, setShardKey(s, []byte("k"), []byte("v1")))
	commitShard(t, s) // v2
	commitShard(t, s) // v3; no write at v2
	require.NoError(t, setShardKey(s, []byte("k"), []byte("v3")))

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
	require.NoError(t, setShardKey(s, []byte("a"), []byte("1")))
	commitShard(t, s) // seals v1, live -> v2
	require.NoError(t, setShardKey(s, []byte("b"), []byte("2")))
	commitShard(t, s) // seals v2, live -> v3 (GetDiffs only covers sealed versions)

	diffs, err := s.GetDiffsForVersions(1, 3) // [1, 3) => versions 1 and 2
	require.NoError(t, err)
	require.Len(t, diffs, 2)
	require.Equal(t, []byte("1"), diffs[0]["a"])
	require.Equal(t, []byte("2"), diffs[1]["b"])
}

func TestShardGetDiffsForVersionsRejectsBadRange(t *testing.T) {
	s := newTestShard(t, 4096, newTestDB(nil))
	commitShard(t, s) // oldest=1, current=2

	_, err := s.GetDiffsForVersions(3, 1)
	require.Error(t, err, "firstVersion > lastVersion")

	_, err = s.GetDiffsForVersions(0, 2)
	require.Error(t, err, "firstVersion below oldest")
}

func TestShardDeleteWritesTombstone(t *testing.T) {
	s := newTestShard(t, 4096, newTestDB(nil))
	require.NoError(t, setShardKey(s, []byte("k"), []byte("v")))
	require.NoError(t, deleteShardKey(s, []byte("k")))

	// Delete in the same version overwrites the value with a tombstone (nil).
	val, found, err := s.Get([]byte("k"), s.currentVersion, false)
	require.NoError(t, err)
	require.False(t, found)
	require.Nil(t, val)
}

func TestShardDropVersionsPushesLatestToDB(t *testing.T) {
	s := newTestShard(t, 4096, newTestDB(nil))
	require.NoError(t, setShardKey(s, []byte("k"), []byte("v1")))
	commitShard(t, s) // v2
	require.NoError(t, setShardKey(s, []byte("k"), []byte("v2")))
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

// setShardKey writes one key through the shard's batch write, which is its only write path.
func setShardKey(s *shard, key []byte, value []byte) error {
	return s.BatchSet([]BatchKVPair{{Key: string(key), Value: value}})
}

// deleteShardKey removes one key through the shard's batch write.
func deleteShardKey(s *shard, key []byte) error {
	return s.BatchSet([]BatchKVPair{{Key: string(key), Delete: true}})
}

// Writes pick a shard with ShardString and reads pick one with Shard. If those ever disagreed, a key
// written through the batch API would be looked for in a different shard than it landed in, and
// would read as absent.
func TestShardStringPicksSameShardAsShardBytes(t *testing.T) {
	manager, err := newShardManager(8)
	require.NoError(t, err)

	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("evm/%d/some-reasonably-long-physical-key", i)
		require.Equal(t, manager.Shard([]byte(key)), manager.ShardString(key),
			"key %q must hash to the same shard whichever form it arrives in", key)
	}

	// The forms a manager actually sees: an empty key, a single byte, and the two EVM key lengths.
	for _, key := range []string{"", "k", "evm/\x0a01234567890123456789", "evm/\x03" + string(make([]byte, 52))} {
		require.Equal(t, manager.Shard([]byte(key)), manager.ShardString(key),
			"key %q must hash to the same shard whichever form it arrives in", key)
	}
}
