package memiavl

import (
	"context"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/common/logger"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	iavl "github.com/sei-protocol/sei-chain/sei-iavl"
)

func TestRewriteSnapshot(t *testing.T) {
	db, err := OpenDB(logger.NewNopLogger(), 0, Options{
		Dir:             t.TempDir(),
		CreateIfMissing: true,
		InitialStores:   []string{"test"},
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) }) // Ensure DB cleanup

	for i, changes := range ChangeSets {
		cs := []*proto.NamedChangeSet{
			{
				Name:      "test",
				Changeset: changes,
			},
		}

		t.Run(strconv.Itoa(i), func(t *testing.T) {
			require.NoError(t, db.ApplyChangeSets(cs))
			v, err := db.Commit()
			require.NoError(t, err)
			require.Equal(t, i+1, int(v))
			require.Equal(t, RefHashes[i], db.lastCommitInfo.StoreInfos[0].CommitId.Hash)
			require.NoError(t, db.RewriteSnapshot(context.Background()))
			require.NoError(t, db.Reload())
		})
	}
}

func TestRemoveSnapshotDir(t *testing.T) {
	dbDir := t.TempDir()

	snapshotDir := filepath.Join(dbDir, snapshotName(0))
	tmpDir := snapshotDir + "-tmp"
	if err := os.MkdirAll(tmpDir, os.ModePerm); err != nil {
		t.Fatalf("Failed to create dummy snapshot directory: %v", err)
	}
	db, err := OpenDB(logger.NewNopLogger(), 0, Options{
		Config:          Config{SnapshotKeepRecent: 0},
		Dir:             dbDir,
		CreateIfMissing: true,
		InitialStores:   []string{"test"},
	})
	require.NoError(t, err)
	require.NoError(t, db.Close())

	_, err = os.Stat(tmpDir)
	require.True(t, os.IsNotExist(err), "Expected temporary snapshot directory to be deleted, but it still exists")

	err = os.MkdirAll(tmpDir, os.ModePerm)
	require.NoError(t, err)

	_, err = OpenDB(logger.NewNopLogger(), 0, Options{
		Dir:      dbDir,
		ReadOnly: true,
	})
	require.NoError(t, err)

	_, err = os.Stat(tmpDir)
	require.False(t, os.IsNotExist(err))

	db, err = OpenDB(logger.NewNopLogger(), 0, Options{
		Dir: dbDir,
	})
	require.NoError(t, err)

	_, err = os.Stat(tmpDir)
	require.True(t, os.IsNotExist(err))

	require.NoError(t, db.Close())
}

func TestRewriteSnapshotBackground(t *testing.T) {
	db, err := OpenDB(logger.NewNopLogger(), 0, Options{
		Config:          Config{SnapshotKeepRecent: 0}, // only a single snapshot is kept
		Dir:             t.TempDir(),
		CreateIfMissing: true,
		InitialStores:   []string{"test"},
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) }) // Ensure DB cleanup and goroutine termination

	// spin up goroutine to keep querying the tree
	stopCh := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				tree := db.TreeByName("test")
				if tree == nil {
					return
				}
				value := tree.Get([]byte("hello1"))
				// check value only if we got valid result (snapshot might be swapping)
				if value != nil {
					require.Equal(t, "world1", string(value))
				}
			case <-stopCh:
				return
			}
		}
	}()

	for i, changes := range ChangeSets {
		cs := []*proto.NamedChangeSet{
			{
				Name:      "test",
				Changeset: changes,
			},
		}
		require.NoError(t, db.ApplyChangeSets(cs))
		v, err := db.Commit()
		require.NoError(t, err)
		require.Equal(t, i+1, int(v))
		require.Equal(t, RefHashes[i], db.lastCommitInfo.StoreInfos[0].CommitId.Hash)
		_ = db.RewriteSnapshotBackground()
	}
	// Wait for all background snapshot rewrites to complete
	require.Eventually(t, func() bool {
		db.mtx.Lock()
		defer db.mtx.Unlock()
		_ = db.checkAsyncTasks()
		return db.snapshotRewriteChan == nil
	}, 3*time.Second, 50*time.Millisecond, "all snapshot rewrites should complete")

	close(stopCh)
	wg.Wait()

	// Wait for async prune to finish by checking the actual directory state.
	// After prune completes, only 4 entries should remain:
	// snapshot, current link, LOCK, changelog WAL dir
	require.Eventually(t, func() bool {
		entries, err := os.ReadDir(db.dir)
		return err == nil && len(entries) == 4
	}, 3*time.Second, 50*time.Millisecond, "prune should complete and leave exactly 4 entries")
	// stopCh is closed by defer above
}

// helper to commit one change to bump height
func RequireCommitWithNoError(t *testing.T, db *DB, key, val string) int64 {
	pairs := []*iavl.KVPair{{Key: []byte(key), Value: []byte(val)}}
	cs := []*proto.NamedChangeSet{
		{Name: "test", Changeset: iavl.ChangeSet{Pairs: pairs}},
	}
	require.NoError(t, db.ApplyChangeSets(cs))
	v, err := db.Commit()
	require.NoError(t, err)
	return v
}

// Ensures snapshot rewrite is triggered when current height minus last snapshot height
// exceeds the configured snapshot interval (strictly greater).
func TestSnapshotTriggerOnIntervalDiff(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(logger.NewNopLogger(), 0, Options{
		Config: Config{
			SnapshotInterval:        5,
			SnapshotKeepRecent:      0,
			SnapshotMinTimeInterval: 1, // 1 second minimum time interval for testing
		},
		Dir:             dir,
		CreateIfMissing: true,
		InitialStores:   []string{"test"},
	})
	require.NoError(t, err)

	// Heights 1..4 should NOT trigger because diff<interval
	for idx := range 4 {
		i := idx + 1
		v := RequireCommitWithNoError(t, db, "k"+strconv.Itoa(i), "v")
		require.EqualValues(t, i, v)
		// Verify snapshot rewrite should not start
		require.Never(t, func() bool {
			db.mtx.Lock()
			defer db.mtx.Unlock()
			return db.snapshotRewriteChan != nil
		}, 100*time.Millisecond, 10*time.Millisecond, "rewrite should not start at height %d", i)
		// snapshot version should remain 0 until rewrite
		require.EqualValues(t, 0, db.SnapshotVersion())
	}

	// Wait for minimum time interval to elapse (1 second + buffer)
	time.Sleep(1100 * time.Millisecond)

	// Height 5 should trigger rewrite (interval reached and time threshold met)
	v := RequireCommitWithNoError(t, db, "k6", "v")
	require.Equal(t, int64(5), v)

	// wait briefly for background rewrite to start
	require.Eventually(t, func() bool {
		return db.snapshotRewriteChan != nil
	}, 3*time.Second, 100*time.Millisecond)
	require.Eventually(t, func() bool {
		require.NoError(t, db.checkAsyncTasks())
		return db.snapshotRewriteChan == nil
	}, 5*time.Second, 100*time.Millisecond)

	// After completion, snapshot version should be 5
	require.EqualValues(t, 5, db.SnapshotVersion())

	require.NoError(t, db.Close())
}

func TestRlog(t *testing.T) {
	dir := t.TempDir()
	initialStores := []string{"test", "delete"}

	db, err := OpenDB(logger.NewNopLogger(), 0, Options{
		Dir:             dir,
		CreateIfMissing: true,
		InitialStores:   initialStores,
	})
	require.NoError(t, err)

	for _, changes := range ChangeSets {
		cs := []*proto.NamedChangeSet{
			{
				Name:      "test",
				Changeset: changes,
			},
		}
		require.NoError(t, db.ApplyChangeSets(cs))
		_, err := db.Commit()
		require.NoError(t, err)
	}

	require.Equal(t, 2, len(db.lastCommitInfo.StoreInfos))

	upgrades := []*proto.TreeNameUpgrade{
		{
			Name:       "newtest",
			RenameFrom: "test",
		},
		{
			Name:   "delete",
			Delete: true,
		},
	}
	require.NoError(t, db.ApplyUpgrades(upgrades))
	_, err = db.Commit()
	require.NoError(t, err)

	require.NoError(t, db.Close())

	// Reopen (MemIAVL will open the changelog from disk)
	db, err = OpenDB(logger.NewNopLogger(), 0, Options{Dir: dir, InitialStores: initialStores})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) }) // Close the reopened DB

	require.Equal(t, "newtest", db.lastCommitInfo.StoreInfos[0].Name)
	require.Equal(t, 1, len(db.lastCommitInfo.StoreInfos))
	require.Equal(t, RefHashes[len(RefHashes)-1], db.lastCommitInfo.StoreInfos[0].CommitId.Hash)
}

func mockNameChangeSet(name, key, value string) []*proto.NamedChangeSet {
	return []*proto.NamedChangeSet{
		{
			Name: name,
			Changeset: iavl.ChangeSet{
				Pairs: mockKVPairs(key, value),
			},
		},
	}
}

// 0/1 -> v :1
// ...
// 100 -> v: 100
func TestInitialVersion(t *testing.T) {
	name := "test"
	name1 := "new"
	name2 := "new2"
	key := "hello"
	value := "world"
	for _, initialVersion := range []int64{0, 1, 100} {
		dir := t.TempDir()
		initialStores := []string{name}

		db, err := OpenDB(logger.NewNopLogger(), 0, Options{
			Dir:             dir,
			CreateIfMissing: true,
			InitialStores:   initialStores,
		})
		require.NoError(t, err)
		db.SetInitialVersion(initialVersion)
		cs1 := mockNameChangeSet(name, key, value)
		require.NoError(t, db.ApplyChangeSets(cs1))
		v, err := db.Commit()
		require.NoError(t, err)
		if initialVersion <= 1 {
			require.Equal(t, int64(1), v)
		} else {
			require.Equal(t, initialVersion, v)
		}
		hash := db.LastCommitInfo().StoreInfos[0].CommitId.Hash
		require.Equal(t, "6032661ab0d201132db7a8fa1da6a0afe427e6278bd122c301197680ab79ca02", hex.EncodeToString(hash))
		cs2 := mockNameChangeSet(name, key, "world1")
		require.NoError(t, db.ApplyChangeSets(cs2))
		v, err = db.Commit()
		require.NoError(t, err)
		hash = db.LastCommitInfo().StoreInfos[0].CommitId.Hash
		if initialVersion <= 1 {
			require.Equal(t, int64(2), v)
			require.Equal(t, "ef0530f9bf1af56c19a3bac32a3ec4f76a6fefaacb2efd4027a2cf37240f60bb", hex.EncodeToString(hash))
		} else {
			require.Equal(t, initialVersion+1, v)
			require.Equal(t, "a719e7d699d42ea8e5637ec84675a2c28f14a00a71fb518f20aa2c395673a3b8", hex.EncodeToString(hash))
		}
		require.NoError(t, db.Close())

		// Reopen (MemIAVL will open the changelog from disk)
		db, err = OpenDB(logger.NewNopLogger(), 0, Options{Dir: dir, InitialStores: initialStores})
		require.NoError(t, err)
		require.Equal(t, uint32(initialVersion), db.initialVersion.Load())
		require.Equal(t, v, db.Version())
		require.Equal(t, hex.EncodeToString(hash), hex.EncodeToString(db.LastCommitInfo().StoreInfos[0].CommitId.Hash))

		upgrades1 := []*proto.TreeNameUpgrade{{Name: name1}}
		db.ApplyUpgrades(upgrades1)
		cs3 := mockNameChangeSet(name1, key, value)
		require.NoError(t, db.ApplyChangeSets(cs3))
		v, err = db.Commit()
		require.NoError(t, err)
		if initialVersion <= 1 {
			require.Equal(t, int64(3), v)
		} else {
			require.Equal(t, initialVersion+2, v)
		}
		require.Equal(t, 2, len(db.lastCommitInfo.StoreInfos))
		info := db.lastCommitInfo.StoreInfos[0]
		require.Equal(t, name1, info.Name)
		require.Equal(t, v, info.CommitId.Version)
		require.Equal(t, "6032661ab0d201132db7a8fa1da6a0afe427e6278bd122c301197680ab79ca02", hex.EncodeToString(info.CommitId.Hash))
		// the nodes are created with version 1, which is compatible with iavl behavior: https://github.com/cosmos/iavl/pull/660
		require.Equal(t, info.CommitId.Hash, HashNode(newLeafNode([]byte(key), []byte(value), 1)))

		require.NoError(t, db.RewriteSnapshot(context.Background()))
		require.NoError(t, db.Reload())

		upgrades2 := []*proto.TreeNameUpgrade{{Name: name2}}
		db.ApplyUpgrades(upgrades2)
		cs4 := mockNameChangeSet(name2, key, value)
		require.NoError(t, db.ApplyChangeSets(cs4))
		v, err = db.Commit()
		require.NoError(t, err)
		if initialVersion <= 1 {
			require.Equal(t, int64(4), v)
		} else {
			require.Equal(t, initialVersion+3, v)
		}
		require.Equal(t, 3, len(db.lastCommitInfo.StoreInfos))
		info2 := db.lastCommitInfo.StoreInfos[1]
		require.Equal(t, name2, info2.Name)
		require.Equal(t, v, info2.CommitId.Version)
		require.Equal(t, hex.EncodeToString(info.CommitId.Hash), hex.EncodeToString(info2.CommitId.Hash))

		require.NoError(t, db.Close())
	}
}

func TestLoadVersion(t *testing.T) {
	dir := t.TempDir()
	initialStores := []string{"test"}

	db, err := OpenDB(logger.NewNopLogger(), 0, Options{
		Dir:             dir,
		CreateIfMissing: true,
		InitialStores:   initialStores,
	})
	require.NoError(t, err)

	for i, changes := range ChangeSets {
		cs := []*proto.NamedChangeSet{
			{
				Name:      "test",
				Changeset: changes,
			},
		}
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			require.NoError(t, db.ApplyChangeSets(cs))

			// check the root hash
			require.Equal(t, RefHashes[db.Version()], db.WorkingCommitInfo().StoreInfos[0].CommitId.Hash)
			_, err := db.Commit()
			require.NoError(t, err)
		})
	}
	require.NoError(t, db.Close())

	for v, expItems := range ExpectItems {
		if v == 0 {
			continue
		}
		// Read-only loads use the same WAL to replay
		tmp, err := OpenDB(logger.NewNopLogger(), int64(v), Options{
			Dir:           dir,
			ReadOnly:      true,
			InitialStores: initialStores,
		})
		require.NoError(t, err)
		require.Equal(t, RefHashes[v-1], tmp.TreeByName("test").RootHash())
		require.Equal(t, expItems, collectIter(tmp.TreeByName("test").Iterator(nil, nil, true)))
		require.NoError(t, tmp.Close()) // Close each readonly DB instance
	}
}

func TestZeroCopy(t *testing.T) {
	db, err := OpenDB(logger.NewNopLogger(), 0, Options{
		Dir:             t.TempDir(),
		InitialStores:   []string{"test", "test2"},
		CreateIfMissing: true,
		ZeroCopy:        true,
	})
	require.NoError(t, err)
	require.NoError(t, db.ApplyChangeSets([]*proto.NamedChangeSet{
		{Name: "test", Changeset: ChangeSets[0]},
	}))
	_, err = db.Commit()
	require.NoError(t, err)
	require.NoError(t, errors.Join(
		db.RewriteSnapshot(context.Background()),
		db.Reload(),
	))

	// the test tree's root hash will reference the zero-copy value
	require.NoError(t, db.ApplyChangeSets([]*proto.NamedChangeSet{
		{Name: "test2", Changeset: ChangeSets[0]},
	}))
	_, err = db.Commit()
	require.NoError(t, err)

	commitInfo := *db.LastCommitInfo()

	value := db.TreeByName("test").Get([]byte("hello"))
	require.Equal(t, []byte("world"), value)

	db.SetZeroCopy(false)
	valueCloned := db.TreeByName("test").Get([]byte("hello"))
	require.Equal(t, []byte("world"), valueCloned)

	_ = commitInfo.StoreInfos[0].CommitId.Hash[0]

	require.NoError(t, db.Close())

	require.Equal(t, []byte("world"), valueCloned)

	// accessing the zero-copy value after the db is closed triggers segment fault.
	// reset global panic on fault setting after function finished
	defer debug.SetPanicOnFault(debug.SetPanicOnFault(true))
	require.Panics(t, func() {
		require.Equal(t, []byte("world"), value)
	})

	// it's ok to access after db closed
	_ = commitInfo.StoreInfos[0].CommitId.Hash[0]
}

func TestRlogIndexConversion(t *testing.T) {
	testCases := []struct {
		index          uint64
		version        int64
		initialVersion uint32
	}{
		{1, 1, 0},
		{1, 1, 1},
		{1, 10, 10},
		{2, 11, 10},
	}
	for _, tc := range testCases {
		require.Equal(t, tc.index, utils.VersionToIndex(tc.version, tc.initialVersion))
		require.Equal(t, tc.version, utils.IndexToVersion(tc.index, tc.initialVersion))
	}
}

// Regression test: on a fresh DB (version 0), the initial snapshot can contain 0 trees,
// but WAL replay may already contain changesets for initial store names. OpenDB must
// TestWALIndexDeltaComputation tests the O(1) delta-based WAL index conversion.
// This is critical because:
// 1. WAL indices and versions are both strictly contiguous
// 2. We compute delta once from the first WAL entry: delta = firstVersion - firstIndex
// 3. All conversions are then O(1): walIndex = version - delta
func TestWALIndexDeltaComputation(t *testing.T) {
	testCases := []struct {
		name           string
		initialVersion uint32
		numVersions    int
		rollbackTo     int64
	}{
		{
			name:           "Test wal delta=0 and version = 1",
			initialVersion: 0,
			numVersions:    5,
			rollbackTo:     3,
		},
		{
			name:           "Test wal delta=9 and version = 10",
			initialVersion: 10,
			numVersions:    5,
			rollbackTo:     12,
		},
		{
			name:           "Test wal delta=99 and version = 100",
			initialVersion: 100,
			numVersions:    5,
			rollbackTo:     102,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			initialStores := []string{"test"}

			// Open DB with initial version
			db, err := OpenDB(logger.NewNopLogger(), 0, Options{
				Dir:             dir,
				CreateIfMissing: true,
				InitialStores:   initialStores,
				InitialVersion:  tc.initialVersion,
			})
			require.NoError(t, err)

			// Commit multiple versions
			for i := 0; i < tc.numVersions; i++ {
				cs := []*proto.NamedChangeSet{
					{
						Name: "test",
						Changeset: iavl.ChangeSet{
							Pairs: []*iavl.KVPair{
								{Key: []byte("key"), Value: []byte("value" + strconv.Itoa(i))},
							},
						},
					},
				}
				require.NoError(t, db.ApplyChangeSets(cs))
				_, err = db.Commit()
				require.NoError(t, err)
			}

			// When initialVersion=0, first commit is version 1, so after N commits: version = N
			// When initialVersion=X, first commit is version X, so after N commits: version = X + N - 1
			expectedVersion := int64(tc.numVersions)
			if tc.initialVersion > 0 {
				expectedVersion = int64(tc.initialVersion) + int64(tc.numVersions) - 1
			}
			require.Equal(t, expectedVersion, db.Version())

			require.NoError(t, db.Close())

			// Reopen to verify delta is computed correctly from WAL entries
			dbReopen, err := OpenDB(logger.NewNopLogger(), 0, Options{
				Dir:           dir,
				InitialStores: initialStores,
			})
			require.NoError(t, err)

			// Now verify delta is computed correctly
			// delta = firstVersion - firstIndex
			// When initialVersion=0: firstVersion = 1, firstIndex = 1, delta = 0
			// When initialVersion=X: firstVersion = X, firstIndex = 1, delta = X - 1
			expectedDelta := int64(0)
			if tc.initialVersion > 0 {
				expectedDelta = int64(tc.initialVersion) - 1
			}
			require.Equal(t, expectedDelta, dbReopen.walIndexDelta, "WAL index delta should be computed correctly")

			// Test versionToWALIndex
			for i := 0; i < tc.numVersions; i++ {
				var version int64
				if tc.initialVersion == 0 {
					version = int64(i + 1) // versions: 1, 2, 3, 4, 5
				} else {
					version = int64(tc.initialVersion) + int64(i) // versions: 10, 11, 12, 13, 14
				}
				expectedIndex := uint64(i + 1) // WAL indices: 1, 2, 3, 4, 5
				require.Equal(t, expectedIndex, dbReopen.versionToWALIndex(version),
					"versionToWALIndex(%d) should return %d", version, expectedIndex)
			}

			require.NoError(t, dbReopen.Close())

			// Now test rollback with LoadForOverwriting
			db2, err := OpenDB(logger.NewNopLogger(), tc.rollbackTo, Options{
				Dir:                dir,
				InitialStores:      initialStores,
				LoadForOverwriting: true,
			})
			require.NoError(t, err)

			// Verify rollback worked
			require.Equal(t, tc.rollbackTo, db2.Version(), "Version should be rolled back to %d", tc.rollbackTo)

			// Verify WAL was truncated correctly
			lastIndex, err := db2.GetWAL().LastOffset()
			require.NoError(t, err)
			expectedLastIndex := uint64(tc.rollbackTo - db2.walIndexDelta)
			require.Equal(t, expectedLastIndex, lastIndex, "WAL should be truncated to index %d", expectedLastIndex)

			require.NoError(t, db2.Close())

			// Reopen without LoadForOverwriting to verify persistence
			db3, err := OpenDB(logger.NewNopLogger(), 0, Options{
				Dir:           dir,
				InitialStores: initialStores,
			})
			require.NoError(t, err)
			require.Equal(t, tc.rollbackTo, db3.Version(), "Version should persist as %d after reopen", tc.rollbackTo)

			require.NoError(t, db3.Close())
		})
	}
}

// TestWALIndexDeltaWithZeroDelta specifically tests the case where delta=0.
// This was a bug where `walIndexDelta != 0` condition incorrectly skipped truncation
// when versions started at 1 (making delta = 1 - 1 = 0).
func TestWALIndexDeltaWithZeroDelta(t *testing.T) {
	dir := t.TempDir()
	initialStores := []string{"test"}

	// Create DB with default initial version (0, so versions start at 1)
	db, err := OpenDB(logger.NewNopLogger(), 0, Options{
		Dir:             dir,
		CreateIfMissing: true,
		InitialStores:   initialStores,
	})
	require.NoError(t, err)

	// Commit 5 versions (1, 2, 3, 4, 5)
	for i := 0; i < 5; i++ {
		cs := []*proto.NamedChangeSet{
			{
				Name: "test",
				Changeset: iavl.ChangeSet{
					Pairs: []*iavl.KVPair{
						{Key: []byte("key"), Value: []byte("value" + strconv.Itoa(i))},
					},
				},
			},
		}
		require.NoError(t, db.ApplyChangeSets(cs))
		_, err = db.Commit()
		require.NoError(t, err)
	}

	require.Equal(t, int64(5), db.Version())
	// Critical: delta should be 0 (version 1 - index 1 = 0)
	require.Equal(t, int64(0), db.walIndexDelta, "Delta should be 0 when versions start at 1")

	require.NoError(t, db.Close())

	// Rollback to version 3
	db2, err := OpenDB(logger.NewNopLogger(), 3, Options{
		Dir:                dir,
		InitialStores:      initialStores,
		LoadForOverwriting: true,
	})
	require.NoError(t, err)

	// This is the key assertion that would have failed with the bug
	require.Equal(t, int64(3), db2.Version(), "Rollback should work even when delta=0")

	// Verify WAL truncation
	lastIndex, err := db2.GetWAL().LastOffset()
	require.NoError(t, err)
	require.Equal(t, uint64(3), lastIndex, "WAL should be truncated to index 3")

	require.NoError(t, db2.Close())

	// Verify rollback persisted after reopen
	db3, err := OpenDB(logger.NewNopLogger(), 0, Options{
		Dir:           dir,
		InitialStores: initialStores,
	})
	require.NoError(t, err)
	require.Equal(t, int64(3), db3.Version(), "Rollback should persist after reopen")

	require.NoError(t, db3.Close())
}

func TestEmptyValue(t *testing.T) {
	dir := t.TempDir()
	initialStores := []string{"test"}

	db, err := OpenDB(logger.NewNopLogger(), 0, Options{
		Dir:             dir,
		InitialStores:   initialStores,
		CreateIfMissing: true,
		ZeroCopy:        true,
	})
	require.NoError(t, err)

	cs1 := []*proto.NamedChangeSet{
		{Name: "test", Changeset: iavl.ChangeSet{
			Pairs: []*iavl.KVPair{
				{Key: []byte("hello1"), Value: []byte("")},
				{Key: []byte("hello2"), Value: []byte("")},
				{Key: []byte("hello3"), Value: []byte("")},
			},
		}},
	}
	require.NoError(t, db.ApplyChangeSets(cs1))
	_, err = db.Commit()
	require.NoError(t, err)

	cs2 := []*proto.NamedChangeSet{
		{Name: "test", Changeset: iavl.ChangeSet{
			Pairs: []*iavl.KVPair{{Key: []byte("hello1"), Delete: true}},
		}},
	}
	require.NoError(t, db.ApplyChangeSets(cs2))
	version, err := db.Commit()
	require.NoError(t, err)

	require.NoError(t, db.Close())

	// Reopen (MemIAVL will open the changelog from disk)
	db, err = OpenDB(logger.NewNopLogger(), 0, Options{Dir: dir, ZeroCopy: true, InitialStores: initialStores})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) }) // Close the reopened DB
	require.Equal(t, version, db.Version())
}

func TestInvalidOptions(t *testing.T) {
	dir := t.TempDir()

	_, err := OpenDB(logger.NewNopLogger(), 0, Options{Dir: dir, ReadOnly: true})
	require.Error(t, err)

	_, err = OpenDB(logger.NewNopLogger(), 0, Options{Dir: dir, ReadOnly: true, CreateIfMissing: true})
	require.Error(t, err)

	db, err := OpenDB(logger.NewNopLogger(), 0, Options{Dir: dir, CreateIfMissing: true})
	require.NoError(t, err)
	require.NoError(t, db.Close())

	_, err = OpenDB(logger.NewNopLogger(), 0, Options{Dir: dir, LoadForOverwriting: true, ReadOnly: true})
	require.Error(t, err)

	_, err = OpenDB(logger.NewNopLogger(), 0, Options{Dir: dir, ReadOnly: true})
	require.NoError(t, err)
}

func TestExclusiveLock(t *testing.T) {
	dir := t.TempDir()

	db, err := OpenDB(logger.NewNopLogger(), 0, Options{Dir: dir, CreateIfMissing: true})
	require.NoError(t, err)

	_, err = OpenDB(logger.NewNopLogger(), 0, Options{Dir: dir})
	require.Error(t, err)

	_, err = OpenDB(logger.NewNopLogger(), 0, Options{Dir: dir, ReadOnly: true})
	require.NoError(t, err)

	require.NoError(t, db.Close())

	_, err = OpenDB(logger.NewNopLogger(), 0, Options{Dir: dir})
	require.NoError(t, err)
}

func TestFastCommit(t *testing.T) {
	dir := t.TempDir()

	db, err := OpenDB(logger.NewNopLogger(), 0, Options{
		Config: Config{
			SnapshotInterval:        3,
			AsyncCommitBuffer:       10,
			SnapshotMinTimeInterval: 1, // 1 second for testing
		},
		Dir:             dir,
		CreateIfMissing: true,
		InitialStores:   []string{"test"},
	})
	require.NoError(t, err)
	initialSnapshotTime := db.lastSnapshotTime

	cs := iavl.ChangeSet{
		Pairs: []*iavl.KVPair{
			{Key: []byte("hello1"), Value: make([]byte, 1024*1024)},
		},
	}

	// the bug reproduce when the rlog writing is slower than commit,
	// that happens when rlog segment is full and create a new one,
	// the rlog writing will slow down a little bit,
	// segment size is 20m, each change set is 1m, so we need a bit more than 20 commits to reproduce.
	for i := range 30 {
		require.NoError(t, db.ApplyChangeSets([]*proto.NamedChangeSet{{Name: "test", Changeset: cs}}))
		_, err := db.Commit()
		require.NoError(t, err)
		// After reaching snapshot interval (3), wait for time threshold to be met
		if i == 2 {
			time.Sleep(1100 * time.Millisecond)
		}
	}

	require.Eventually(t, func() bool {
		require.NoError(t, db.checkBackgroundSnapshotRewrite())
		return db.snapshotRewriteChan == nil && db.lastSnapshotTime.After(initialSnapshotTime)
	}, 10*time.Second, 10*time.Millisecond, "snapshot rewrite did not finish in time")

	require.NoError(t, db.Close())
}

func TestRepeatedApplyChangeSet(t *testing.T) {
	db, err := OpenDB(logger.NewNopLogger(), 0, Options{
		Config: Config{
			SnapshotInterval:  3,
			AsyncCommitBuffer: 10,
		},
		Dir:             t.TempDir(),
		CreateIfMissing: true,
		InitialStores:   []string{"test1", "test2"},
	})
	require.NoError(t, err)

	err = db.ApplyChangeSets([]*proto.NamedChangeSet{
		{Name: "test1", Changeset: iavl.ChangeSet{
			Pairs: []*iavl.KVPair{
				{Key: []byte("hello1"), Value: []byte("world1")},
			},
		}},
		{Name: "test2", Changeset: iavl.ChangeSet{
			Pairs: []*iavl.KVPair{
				{Key: []byte("hello2"), Value: []byte("world2")},
			},
		}},
	})
	require.NoError(t, err)

	// Note: Multiple ApplyChangeSets calls are now allowed at DB level.
	// The "one changeset per tree per version" validation is enforced by CommitStore.
	err = db.ApplyChangeSets([]*proto.NamedChangeSet{{Name: "test1"}})
	require.NoError(t, err)
	err = db.ApplyChangeSet("test1", iavl.ChangeSet{
		Pairs: []*iavl.KVPair{
			{Key: []byte("hello2"), Value: []byte("world2")},
		},
	})
	require.NoError(t, err)

	_, err = db.Commit()
	require.NoError(t, err)

	err = db.ApplyChangeSet("test1", iavl.ChangeSet{
		Pairs: []*iavl.KVPair{
			{Key: []byte("hello2"), Value: []byte("world2")},
		},
	})
	require.NoError(t, err)
	err = db.ApplyChangeSet("test2", iavl.ChangeSet{
		Pairs: []*iavl.KVPair{
			{Key: []byte("hello2"), Value: []byte("world2")},
		},
	})
	require.NoError(t, err)

	// Note: At DB level, multiple ApplyChangeSet calls with the same tree name are now allowed.
	// The "one changeset per tree per version" validation is enforced by CommitStore.
	err = db.ApplyChangeSet("test1", iavl.ChangeSet{
		Pairs: []*iavl.KVPair{
			{Key: []byte("hello2"), Value: []byte("world2")},
		},
	})
	require.NoError(t, err)
	err = db.ApplyChangeSet("test2", iavl.ChangeSet{
		Pairs: []*iavl.KVPair{
			{Key: []byte("hello2"), Value: []byte("world2")},
		},
	})
	require.NoError(t, err)
}

func TestLoadMultiTreeWithCancelledContext(t *testing.T) {
	// Create a DB with some data first
	dir := t.TempDir()
	db, err := OpenDB(logger.NewNopLogger(), 0, Options{
		Dir:             dir,
		CreateIfMissing: true,
		InitialStores:   []string{"test"},
	})
	require.NoError(t, err)

	// Add some data and create a snapshot
	require.NoError(t, db.ApplyChangeSets([]*proto.NamedChangeSet{
		{Name: "test", Changeset: iavl.ChangeSet{
			Pairs: []*iavl.KVPair{{Key: []byte("key"), Value: []byte("value")}},
		}},
	}))
	_, err = db.Commit()
	require.NoError(t, err)
	require.NoError(t, db.RewriteSnapshot(context.Background()))
	require.NoError(t, db.Close())

	// Try to load with already cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err = LoadMultiTree(ctx, filepath.Join(dir, "current"), Options{
		Dir:      dir,
		ZeroCopy: true,
		Logger:   logger.NewNopLogger(),
	})
	require.Error(t, err)
	require.Equal(t, context.Canceled, err)
}

func TestCatchupWithCancelledContext(t *testing.T) {
	// Create a DB with some data
	dir := t.TempDir()
	initialStores := []string{"test"}

	db, err := OpenDB(logger.NewNopLogger(), 0, Options{
		Dir:             dir,
		CreateIfMissing: true,
		InitialStores:   initialStores,
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	wal := db.GetWAL()
	require.NotNil(t, wal)

	// Add multiple versions to have changelog entries
	for i := 0; i < 5; i++ {
		cs := []*proto.NamedChangeSet{
			{Name: "test", Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{{Key: []byte("key"), Value: []byte("value" + strconv.Itoa(i))}},
			}},
		}
		require.NoError(t, db.ApplyChangeSets(cs))
		_, err = db.Commit()
		require.NoError(t, err)
	}

	// Create snapshot at version 5
	require.NoError(t, db.RewriteSnapshot(context.Background()))

	// Load the snapshot (at version 5)
	mtree, err := LoadMultiTree(context.Background(), filepath.Join(dir, "current"), Options{
		Dir:      dir,
		ZeroCopy: true,
		Logger:   logger.NewNopLogger(),
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, mtree.Close()) })

	// Catchup with cancelled context should return error
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err = mtree.Catchup(ctx, wal, db.walIndexDelta, 0)
	// If already caught up, no error; otherwise should get context.Canceled
	if err != nil {
		require.Equal(t, context.Canceled, err)
	}
}

func TestCloseWaitsForBackgroundSnapshot(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(logger.NewNopLogger(), 0, Options{
		Config:          Config{SnapshotInterval: 1}, // Trigger snapshot on every commit
		Dir:             dir,
		CreateIfMissing: true,
		InitialStores:   []string{"test"},
	})
	require.NoError(t, err)

	// Add some data to trigger background snapshot
	require.NoError(t, db.ApplyChangeSets([]*proto.NamedChangeSet{
		{Name: "test", Changeset: iavl.ChangeSet{
			Pairs: []*iavl.KVPair{{Key: []byte("key"), Value: []byte("value")}},
		}},
	}))
	_, err = db.Commit()
	require.NoError(t, err)

	// Close should wait for background snapshot and not panic
	err = db.Close()
	require.NoError(t, err)
}

func TestCloseWithSuccessfulBackgroundSnapshot(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(logger.NewNopLogger(), 0, Options{
		Config: Config{
			SnapshotInterval:        0, // Disable auto snapshot
			SnapshotMinTimeInterval: 0,
		},
		Dir:             dir,
		CreateIfMissing: true,
		InitialStores:   []string{"test"},
	})
	require.NoError(t, err)

	// Add data and commit
	require.NoError(t, db.ApplyChangeSets([]*proto.NamedChangeSet{
		{Name: "test", Changeset: iavl.ChangeSet{
			Pairs: []*iavl.KVPair{{Key: []byte("key"), Value: []byte("value")}},
		}},
	}))
	_, err = db.Commit()
	require.NoError(t, err)

	// Manually trigger background snapshot (without going through Commit which would process the result)
	err = db.RewriteSnapshotBackground()
	require.NoError(t, err)

	// Wait for background snapshot to complete
	time.Sleep(500 * time.Millisecond)

	// Close should properly close the returned mtree from background snapshot
	// This tests the result.mtree != nil branch in Close()
	err = db.Close()
	require.NoError(t, err)
}
