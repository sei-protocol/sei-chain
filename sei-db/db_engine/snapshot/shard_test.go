package snapshot

import (
	"context"
	"fmt"
	"sync"
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

// DropVersions releases the shard lock part way through migrating a retired version's keys, so a read
// arriving mid-migration can find a key already gone from the versioned data. It must still get the
// right value, whether it comes from the cache the key was moved into or from the database it was
// flushed to.
//
// Reads must stay correct while a retirement covering their keys is running. Retirement moves a key out
// of the versioned data and into the read cache, and only ever touches versions that are already
// flushed, so a read must find the right value wherever it lands.
func TestShardDropVersionsServesCorrectValuesDuringMigration(t *testing.T) {
	const keyCount = 4096

	// The database holds every key, because retirement only ever happens after a flush — a reader that
	// misses both the versioned data and the cache has to find it here.
	seed := make(map[string][]byte, keyCount)
	for i := 0; i < keyCount; i++ {
		seed[string(dropTestKey(i))] = dropTestValue(i)
	}
	s := newTestShard(t, 1<<30, newTestDB(seed))

	for i := 0; i < keyCount; i++ {
		require.NoError(t, s.Set(dropTestKey(i), dropTestValue(i)))
	}
	sealed := s.Commit()

	// Readers hammer the shard at the live version while the retirement runs underneath them.
	var readers sync.WaitGroup
	stop := make(chan struct{})
	failures := make(chan error, 8)
	for reader := 0; reader < 8; reader++ {
		readers.Add(1)
		go func(offset int) {
			defer readers.Done()
			for i := 0; ; i++ {
				select {
				case <-stop:
					return
				default:
				}
				index := (i*7 + offset) % keyCount
				value, found, err := s.Get(dropTestKey(index), sealed, false)
				if err != nil {
					failures <- fmt.Errorf("read %d: %w", index, err)
					return
				}
				if !found {
					failures <- fmt.Errorf("read %d: key missing", index)
					return
				}
				if string(value) != string(dropTestValue(index)) {
					failures <- fmt.Errorf("read %d: got %q, want %q",
						index, value, dropTestValue(index))
					return
				}
			}
		}(reader)
	}

	require.NoError(t, s.DropVersions(sealed-1, sealed))
	close(stop)
	readers.Wait()

	select {
	case err := <-failures:
		t.Fatal(err)
	default:
	}

	// Every key's newest write was in the retired version, so none of them keep any history.
	require.Empty(t, s.versionedData)
}

func dropTestKey(i int) []byte {
	return []byte(fmt.Sprintf("drop/key/%06d", i))
}

func dropTestValue(i int) []byte {
	return []byte(fmt.Sprintf("drop/value/%06d", i))
}

// Retirement now runs one task per shard concurrently. This covers what the single-shard test cannot:
// that a fanned-out retirement leaves every shard consistent, losing and corrupting nothing across the
// whole key space.
//
// Reads here are sequential and after the fact. Concurrent reads against a live retirement are covered
// at the shard level by TestShardDropVersionsServesCorrectValuesDuringMigration, and engine-wide by
// TestConcurrentDifferential — which reads through sealed snapshots, because the engine contract forbids
// operating on the mutable version while Commit runs.
func TestEngineRetirementAcrossShardsKeepsEveryKey(t *testing.T) {
	const shardCount = 8
	const keyCount = 2000
	const versions = 20

	seed := make(map[string][]byte, keyCount)
	for i := 0; i < keyCount; i++ {
		seed[string(dropTestKey(i))] = dropTestValue(i)
	}
	engine, _ := newTestEngine(t, seed, shardCount, 1<<30)

	updates := make([]StringKVPair, 0, keyCount)
	for i := 0; i < keyCount; i++ {
		updates = append(updates, StringKVPair{Key: string(dropTestKey(i)), Value: dropTestValue(i)})
	}

	// Each version is flushed and released, which is what makes it retirement-eligible, so the lifecycle
	// runner fans out a retirement across all eight shards repeatedly while this loop runs.
	for v := 0; v < versions; v++ {
		require.NoError(t, engine.BatchSetString(updates))
		snap, err := engine.Commit()
		require.NoError(t, err)
		require.NoError(t, snap.Finalize(nil))
		require.NoError(t, snap.AwaitFlush(context.Background()))
		require.NoError(t, snap.Release())
	}

	impl, ok := engine.(*snapshotEngine)
	require.True(t, ok)
	impl.versionLock.Lock()
	oldest := impl.oldestVersion
	impl.versionLock.Unlock()
	require.Greater(t, oldest, uint64(1), "nothing was retired, so this proves nothing")

	// Every key must survive, whether it is now served from a shard's versioned data, its read cache, or
	// the database it was flushed to.
	for i := 0; i < keyCount; i++ {
		value, found, err := engine.Get(dropTestKey(i), false)
		require.NoError(t, err, "key %d", i)
		require.True(t, found, "key %d went missing across retirement", i)
		require.Equal(t, dropTestValue(i), value, "key %d", i)
	}
}
