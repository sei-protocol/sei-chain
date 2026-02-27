package flatkv

import (
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"os"
	"path/filepath"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/evm"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	iavl "github.com/sei-protocol/sei-chain/sei-iavl/proto"
	"github.com/stretchr/testify/require"
)

func commitStorageEntry(t *testing.T, s *CommitStore, addr Address, slot Slot, value []byte) int64 {
	t.Helper()
	key := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slot))
	cs := &proto.NamedChangeSet{
		Name: "evm",
		Changeset: iavl.ChangeSet{
			Pairs: []*iavl.KVPair{{Key: key, Value: value}},
		},
	}
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	v, err := s.Commit()
	require.NoError(t, err)
	return v
}

func TestSnapshotCreatesDir(t *testing.T) {
	dir := t.TempDir()
	s := NewCommitStore(dir, nil, DefaultConfig())
	_, err := s.LoadVersion(0)
	require.NoError(t, err)
	defer s.Close()

	commitStorageEntry(t, s, Address{0x01}, Slot{0x01}, []byte{0xAA})

	require.NoError(t, s.WriteSnapshot(""))

	flatkvDir := filepath.Join(dir, flatkvRootDir)

	// Verify snapshot directory exists with all 4 DB subdirs
	snapDir := filepath.Join(flatkvDir, snapshotName(1))
	for _, sub := range snapshotDBDirs {
		info, err := os.Stat(filepath.Join(snapDir, sub))
		require.NoError(t, err, "subdir %s should exist", sub)
		require.True(t, info.IsDir())
	}

	// Verify current symlink points to the new snapshot
	target, err := os.Readlink(currentPath(flatkvDir))
	require.NoError(t, err)
	require.Equal(t, snapshotName(1), target)
}

func TestSnapshotIdempotent(t *testing.T) {
	dir := t.TempDir()
	s := NewCommitStore(dir, nil, DefaultConfig())
	_, err := s.LoadVersion(0)
	require.NoError(t, err)
	defer s.Close()

	commitStorageEntry(t, s, Address{0x02}, Slot{0x02}, []byte{0xBB})

	require.NoError(t, s.WriteSnapshot(""))
	require.NoError(t, s.WriteSnapshot(""))

	flatkvDir := filepath.Join(dir, flatkvRootDir)
	target, err := os.Readlink(currentPath(flatkvDir))
	require.NoError(t, err)
	require.Equal(t, snapshotName(1), target)
}

func TestOpenFromSnapshot(t *testing.T) {
	dir := t.TempDir()

	// Phase 1: create store, commit v1 and v2, snapshot at v2, commit v3
	s1 := NewCommitStore(dir, nil, DefaultConfig())
	_, err := s1.LoadVersion(0)
	require.NoError(t, err)

	commitStorageEntry(t, s1, Address{0x10}, Slot{0x01}, []byte{0x01})
	commitStorageEntry(t, s1, Address{0x10}, Slot{0x02}, []byte{0x02})

	require.NoError(t, s1.WriteSnapshot(""))
	require.Equal(t, int64(2), s1.Version())

	commitStorageEntry(t, s1, Address{0x10}, Slot{0x03}, []byte{0x03})
	require.Equal(t, int64(3), s1.Version())

	hashAtV3 := s1.RootHash()
	require.NoError(t, s1.Close())

	// Phase 2: reopen - should catchup from v2 snapshot + WAL entry for v3
	s2 := NewCommitStore(dir, nil, DefaultConfig())
	_, err = s2.LoadVersion(0)
	require.NoError(t, err)
	defer s2.Close()

	require.Equal(t, int64(3), s2.Version())
	require.Equal(t, hashAtV3, s2.RootHash())

	// Verify data from all 3 versions is present
	key1 := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(Address{0x10}, Slot{0x01}))
	key3 := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(Address{0x10}, Slot{0x03}))
	v, ok := s2.Get(key1)
	require.True(t, ok)
	require.Equal(t, []byte{0x01}, v)
	v, ok = s2.Get(key3)
	require.True(t, ok)
	require.Equal(t, []byte{0x03}, v)
}

func TestCatchupUpdatesLtHash(t *testing.T) {
	dir := t.TempDir()

	s1 := NewCommitStore(dir, nil, DefaultConfig())
	_, err := s1.LoadVersion(0)
	require.NoError(t, err)

	// Commit 5 versions, snapshot at v2
	commitStorageEntry(t, s1, Address{0x20}, Slot{0x01}, []byte{0x10})
	commitStorageEntry(t, s1, Address{0x20}, Slot{0x02}, []byte{0x20})
	require.NoError(t, s1.WriteSnapshot(""))

	commitStorageEntry(t, s1, Address{0x20}, Slot{0x03}, []byte{0x30})
	hashAtV3 := s1.RootHash()

	commitStorageEntry(t, s1, Address{0x20}, Slot{0x04}, []byte{0x40})
	commitStorageEntry(t, s1, Address{0x20}, Slot{0x05}, []byte{0x50})
	hashAtV5 := s1.RootHash()
	require.NoError(t, s1.Close())

	// Reopen: catchup from v2 snapshot through v3,v4,v5 via WAL
	s2 := NewCommitStore(dir, nil, DefaultConfig())
	_, err = s2.LoadVersion(0)
	require.NoError(t, err)
	defer s2.Close()

	require.Equal(t, int64(5), s2.Version())
	require.Equal(t, hashAtV5, s2.RootHash(), "LtHash after catchup must match original")

	_ = hashAtV3 // referenced for clarity but not re-checked here
}

func TestRollbackRewindsState(t *testing.T) {
	dir := t.TempDir()

	s := NewCommitStore(dir, nil, DefaultConfig())
	_, err := s.LoadVersion(0)
	require.NoError(t, err)

	// Commit v1..v5, snapshot at v3
	commitStorageEntry(t, s, Address{0x30}, Slot{0x01}, []byte{0x01})
	commitStorageEntry(t, s, Address{0x30}, Slot{0x02}, []byte{0x02})
	commitStorageEntry(t, s, Address{0x30}, Slot{0x03}, []byte{0x03})
	require.NoError(t, s.WriteSnapshot(""))

	commitStorageEntry(t, s, Address{0x30}, Slot{0x04}, []byte{0x04})
	hashAtV4 := s.RootHash()
	commitStorageEntry(t, s, Address{0x30}, Slot{0x05}, []byte{0x05})
	require.Equal(t, int64(5), s.Version())

	// Rollback to v4: restores from v3 snapshot, catches up to v4 via WAL
	require.NoError(t, s.Rollback(4))
	require.Equal(t, int64(4), s.Version())
	require.Equal(t, hashAtV4, s.RootHash())

	// v5's data should not exist (WAL truncated, snapshot pruned)
	key5 := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(Address{0x30}, Slot{0x05}))
	_, ok := s.Get(key5)
	require.False(t, ok, "v5 data should be gone after rollback to v4")

	// v4's data should still exist
	key4 := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(Address{0x30}, Slot{0x04}))
	v, ok := s.Get(key4)
	require.True(t, ok)
	require.Equal(t, []byte{0x04}, v)

	require.NoError(t, s.Close())
}

func TestRollbackToSnapshotExact(t *testing.T) {
	dir := t.TempDir()

	s := NewCommitStore(dir, nil, DefaultConfig())
	_, err := s.LoadVersion(0)
	require.NoError(t, err)

	commitStorageEntry(t, s, Address{0x40}, Slot{0x01}, []byte{0x01})
	commitStorageEntry(t, s, Address{0x40}, Slot{0x02}, []byte{0x02})
	hashAtV2 := s.RootHash()
	require.NoError(t, s.WriteSnapshot(""))

	commitStorageEntry(t, s, Address{0x40}, Slot{0x03}, []byte{0x03})
	require.Equal(t, int64(3), s.Version())

	require.NoError(t, s.Rollback(2))
	require.Equal(t, int64(2), s.Version())
	require.Equal(t, hashAtV2, s.RootHash())

	require.NoError(t, s.Close())
}

func TestPartialSnapshotCleanup(t *testing.T) {
	dir := t.TempDir()
	s := NewCommitStore(dir, nil, DefaultConfig())
	_, err := s.LoadVersion(0)
	require.NoError(t, err)

	commitStorageEntry(t, s, Address{0x50}, Slot{0x01}, []byte{0x01})

	// Take a valid snapshot first
	require.NoError(t, s.WriteSnapshot(""))

	flatkvDir := filepath.Join(dir, flatkvRootDir)
	prevTarget, err := os.Readlink(currentPath(flatkvDir))
	require.NoError(t, err)

	commitStorageEntry(t, s, Address{0x50}, Slot{0x02}, []byte{0x02})

	// Sabotage: close codeDB so checkpoint fails on it. We save the handle
	// to restore it for cleanup.
	savedCodeDB := s.codeDB
	require.NoError(t, s.codeDB.Close())

	err = s.WriteSnapshot("")
	require.Error(t, err, "WriteSnapshot should fail when a DB is closed")

	// Current should still point to the previous snapshot
	target, err := os.Readlink(currentPath(flatkvDir))
	require.NoError(t, err)
	require.Equal(t, prevTarget, target)

	// tmp dir should be cleaned up
	tmpPath := filepath.Join(flatkvDir, snapshotName(2)+tmpSuffix)
	_, statErr := os.Stat(tmpPath)
	require.True(t, os.IsNotExist(statErr), "tmp dir should be cleaned up on failure")

	// Restore codeDB for proper cleanup (reopen is needed for Close to work)
	s.codeDB = savedCodeDB
	_ = s.Close()
}

func TestMigrationFromFlatLayout(t *testing.T) {
	dir := t.TempDir()
	flatkvDir := filepath.Join(dir, flatkvRootDir)

	// Simulate the old flat layout by creating DB dirs directly
	for _, sub := range []string{accountDBDir, codeDBDir, storageDBDir, metadataDir, legacyDBDir} {
		dbPath := filepath.Join(flatkvDir, sub)
		require.NoError(t, os.MkdirAll(dbPath, 0750))
		// Create an actual PebbleDB so Open works
		db, err := pebbledb.Open(dbPath, types.OpenOptions{})
		require.NoError(t, err)
		require.NoError(t, db.Close())
	}

	// Ensure no current symlink exists
	_, err := os.Lstat(currentPath(flatkvDir))
	require.True(t, os.IsNotExist(err))

	// Open the store - should trigger migration
	s := NewCommitStore(dir, nil, DefaultConfig())
	_, err = s.LoadVersion(0)
	require.NoError(t, err)
	defer s.Close()

	// current symlink should now exist
	target, err := os.Readlink(currentPath(flatkvDir))
	require.NoError(t, err)
	require.Equal(t, snapshotName(0), target)

	// The old flat dirs should be gone (moved into the snapshot)
	for _, sub := range snapshotDBDirs {
		_, err := os.Stat(filepath.Join(flatkvDir, sub))
		require.True(t, os.IsNotExist(err), "flat dir %s should have been moved", sub)
	}

	// The snapshot dir should have the DB subdirs
	snapDir := filepath.Join(flatkvDir, snapshotName(0))
	for _, sub := range snapshotDBDirs {
		info, err := os.Stat(filepath.Join(snapDir, sub))
		require.NoError(t, err)
		require.True(t, info.IsDir())
	}

	require.Equal(t, int64(0), s.Version())
}

func TestOpenVersionValidation(t *testing.T) {
	dir := t.TempDir()

	// Phase 1: create store, commit some data
	s1 := NewCommitStore(dir, nil, DefaultConfig())
	_, err := s1.LoadVersion(0)
	require.NoError(t, err)

	commitStorageEntry(t, s1, Address{0x60}, Slot{0x01}, []byte{0x11})
	commitStorageEntry(t, s1, Address{0x60}, Slot{0x02}, []byte{0x22})
	hashAtV2 := s1.RootHash()
	require.NoError(t, s1.Close())

	// Phase 2: tamper with one DB's local meta to simulate an incomplete commit
	// (accountDB thinks it's at v1, but global says v2)
	flatkvDir := filepath.Join(dir, flatkvRootDir)
	snapDir, _, err := currentSnapshotDir(flatkvDir)
	require.NoError(t, err)

	accountDBPath := filepath.Join(snapDir, accountDBDir)
	db, err := pebbledb.Open(accountDBPath, types.OpenOptions{})
	require.NoError(t, err)
	lagMeta := &LocalMeta{CommittedVersion: 1}
	require.NoError(t, db.Set(DBLocalMetaKey, MarshalLocalMeta(lagMeta), types.WriteOptions{Sync: true}))
	require.NoError(t, db.Close())

	// Phase 3: reopen - should detect skew and catchup
	s2 := NewCommitStore(dir, nil, DefaultConfig())
	_, err = s2.LoadVersion(0)
	require.NoError(t, err)
	defer s2.Close()

	require.Equal(t, int64(2), s2.Version())
	require.Equal(t, hashAtV2, s2.RootHash())
}

func TestSnapshotNameParsing(t *testing.T) {
	require.Equal(t, "snapshot-00000000000000000042", snapshotName(42))

	v, err := parseSnapshotVersion("snapshot-00000000000000000042")
	require.NoError(t, err)
	require.Equal(t, int64(42), v)

	require.True(t, isSnapshotName("snapshot-00000000000000000001"))
	require.False(t, isSnapshotName("not-a-snapshot"))
	require.False(t, isSnapshotName("snapshot-short"))
}

func TestTraverseSnapshots(t *testing.T) {
	dir := t.TempDir()

	// Create some snapshot dirs
	for _, v := range []int64{10, 20, 30} {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, snapshotName(v)), 0750))
	}

	// Descending
	var desc []int64
	err := traverseSnapshots(dir, false, func(v int64) (bool, error) {
		desc = append(desc, v)
		return false, nil
	})
	require.NoError(t, err)
	require.Equal(t, []int64{30, 20, 10}, desc)

	// Ascending
	var asc []int64
	err = traverseSnapshots(dir, true, func(v int64) (bool, error) {
		asc = append(asc, v)
		return false, nil
	})
	require.NoError(t, err)
	require.Equal(t, []int64{10, 20, 30}, asc)
}

func TestSeekSnapshot(t *testing.T) {
	dir := t.TempDir()
	for _, v := range []int64{5, 10, 20} {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, snapshotName(v)), 0750))
	}

	v, err := seekSnapshot(dir, 15)
	require.NoError(t, err)
	require.Equal(t, int64(10), v)

	v, err = seekSnapshot(dir, 20)
	require.NoError(t, err)
	require.Equal(t, int64(20), v)

	_, err = seekSnapshot(dir, 3)
	require.Error(t, err)
}

func TestLoadVersionWithTarget(t *testing.T) {
	dir := t.TempDir()

	s1 := NewCommitStore(dir, nil, DefaultConfig())
	_, err := s1.LoadVersion(0)
	require.NoError(t, err)

	commitStorageEntry(t, s1, Address{0x70}, Slot{0x01}, []byte{0x01})
	commitStorageEntry(t, s1, Address{0x70}, Slot{0x02}, []byte{0x02})
	require.NoError(t, s1.WriteSnapshot(""))
	commitStorageEntry(t, s1, Address{0x70}, Slot{0x03}, []byte{0x03})
	hashAtV3 := s1.RootHash()
	commitStorageEntry(t, s1, Address{0x70}, Slot{0x04}, []byte{0x04})
	require.NoError(t, s1.Close())

	// Reopen at specific version 3
	s2 := NewCommitStore(dir, nil, DefaultConfig())
	_, err = s2.LoadVersion(3)
	require.NoError(t, err)
	defer s2.Close()

	require.Equal(t, int64(3), s2.Version())
	require.Equal(t, hashAtV3, s2.RootHash())
}

// TestSnapshotThenCatchupThenVerifyCorrectness verifies that commits after a
// snapshot do not mutate the snapshot's baseline.
func TestSnapshotThenCatchupThenVerifyCorrectness(t *testing.T) {
	dir := t.TempDir()

	addr := Address{0x7A}
	slot := Slot{0x7B}
	key := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slot))

	// Phase 1: build baseline at v2 and snapshot it.
	s1 := NewCommitStore(dir, nil, DefaultConfig())
	_, err := s1.LoadVersion(0)
	require.NoError(t, err)

	commitStorageEntry(t, s1, addr, slot, []byte{0x01})                // v1
	commitStorageEntry(t, s1, Address{0x7A}, Slot{0x7C}, []byte{0xAA}) // v2
	require.NoError(t, s1.WriteSnapshot(""))

	// Record baseline value at v2 for the same key.
	vAtV2, ok := s1.Get(key)
	require.True(t, ok)
	require.Equal(t, []byte{0x01}, vAtV2)

	// Phase 2: advance state beyond the snapshot (v3..v4).
	commitStorageEntry(t, s1, addr, slot, []byte{0x03}) // v3
	commitStorageEntry(t, s1, addr, slot, []byte{0x04}) // v4
	require.Equal(t, int64(4), s1.Version())
	require.NoError(t, s1.Close())

	// Phase 3: reopen exactly at v2. If later commits had mutated the snapshot
	// baseline in place, we'd incorrectly read 0x04 here.
	s2 := NewCommitStore(dir, nil, DefaultConfig())
	_, err = s2.LoadVersion(2)
	require.NoError(t, err)
	gotV2, ok := s2.Get(key)
	require.True(t, ok)
	require.Equal(t, []byte{0x01}, gotV2, "snapshot baseline should remain stable")
	require.NoError(t, s2.Close())

	// Phase 4: reopen latest again to ensure catchup/replay still reaches v4.
	s3 := NewCommitStore(dir, nil, DefaultConfig())
	_, err = s3.LoadVersion(0)
	require.NoError(t, err)
	defer s3.Close()

	require.Equal(t, int64(4), s3.Version())
	gotLatest, ok := s3.Get(key)
	require.True(t, ok)
	require.Equal(t, []byte{0x04}, gotLatest)
}

// TestLoadVersionMixedSequence: load-old -> load-latest -> load-old-again.
// Ensures the working directory keeps snapshots immutable across mixed loads.
func TestLoadVersionMixedSequence(t *testing.T) {
	dir := t.TempDir()

	addr := Address{0x80}
	slot := Slot{0x81}
	key := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slot))

	s := NewCommitStore(dir, nil, DefaultConfig())
	_, err := s.LoadVersion(0)
	require.NoError(t, err)

	commitStorageEntry(t, s, addr, slot, []byte{0x01})
	commitStorageEntry(t, s, addr, slot, []byte{0x02})
	hashAtV2 := s.RootHash()
	require.NoError(t, s.WriteSnapshot(""))

	commitStorageEntry(t, s, addr, slot, []byte{0x03})
	commitStorageEntry(t, s, addr, slot, []byte{0x04})
	hashAtV4 := s.RootHash()
	require.NoError(t, s.Close())

	// Round 1: load exactly v2
	s1 := NewCommitStore(dir, nil, DefaultConfig())
	_, err = s1.LoadVersion(2)
	require.NoError(t, err)
	require.Equal(t, int64(2), s1.Version())
	require.Equal(t, hashAtV2, s1.RootHash())
	v, ok := s1.Get(key)
	require.True(t, ok)
	require.Equal(t, []byte{0x02}, v)
	require.NoError(t, s1.Close())

	// Round 2: load latest (catches up through v3, v4)
	s2 := NewCommitStore(dir, nil, DefaultConfig())
	_, err = s2.LoadVersion(0)
	require.NoError(t, err)
	require.Equal(t, int64(4), s2.Version())
	require.Equal(t, hashAtV4, s2.RootHash())
	v, ok = s2.Get(key)
	require.True(t, ok)
	require.Equal(t, []byte{0x04}, v)
	require.NoError(t, s2.Close())

	// Round 3: load v2 AGAIN — snapshot must still be clean.
	s3 := NewCommitStore(dir, nil, DefaultConfig())
	_, err = s3.LoadVersion(2)
	require.NoError(t, err, "LoadVersion(2) must succeed after LoadVersion(0) dirtied working dir")
	require.Equal(t, int64(2), s3.Version())
	require.Equal(t, hashAtV2, s3.RootHash())
	v, ok = s3.Get(key)
	require.True(t, ok)
	require.Equal(t, []byte{0x02}, v)
	require.NoError(t, s3.Close())
}

// TestRollbackTargetBeforeWALStart: rollback to a version predating all WAL
// entries. The WAL must be cleared entirely to prevent re-application.
func TestRollbackTargetBeforeWALStart(t *testing.T) {
	dir := t.TempDir()

	s := NewCommitStore(dir, nil, DefaultConfig())
	_, err := s.LoadVersion(0)
	require.NoError(t, err)

	// Build: v1..v5, snapshot at v2
	commitStorageEntry(t, s, Address{0x90}, Slot{0x01}, []byte{0x01})
	commitStorageEntry(t, s, Address{0x90}, Slot{0x02}, []byte{0x02})
	hashAtV2 := s.RootHash()
	require.NoError(t, s.WriteSnapshot(""))

	commitStorageEntry(t, s, Address{0x90}, Slot{0x03}, []byte{0x03})
	commitStorageEntry(t, s, Address{0x90}, Slot{0x04}, []byte{0x04})
	commitStorageEntry(t, s, Address{0x90}, Slot{0x05}, []byte{0x05})

	// Front-truncate WAL so first entry is now v4 (simulates prior pruning).
	off, err := s.walOffsetForVersion(4)
	require.NoError(t, err)
	require.NoError(t, s.changelog.TruncateBefore(off))

	// Rollback to v2: target predates first WAL entry; should clear WAL
	// and land at the v2 snapshot exactly.
	require.NoError(t, s.Rollback(2))
	require.Equal(t, int64(2), s.Version())
	require.Equal(t, hashAtV2, s.RootHash())

	// Verify WAL is empty so a restart won't re-apply v4/v5.
	firstOff, err := s.changelog.FirstOffset()
	require.NoError(t, err)
	lastOff, err := s.changelog.LastOffset()
	require.NoError(t, err)
	require.True(t, lastOff == 0 || firstOff > lastOff, "WAL should be empty after rollback past WAL start")

	// Simulate restart: should stay at v2.
	require.NoError(t, s.Close())
	s2 := NewCommitStore(dir, nil, DefaultConfig())
	_, err = s2.LoadVersion(0)
	require.NoError(t, err)
	defer s2.Close()

	require.Equal(t, int64(2), s2.Version())
	require.Equal(t, hashAtV2, s2.RootHash())
}

// =============================================================================
// removeTmpDirs
// =============================================================================

func TestRemoveTmpDirs(t *testing.T) {
	dir := t.TempDir()

	keepDir := filepath.Join(dir, "keepme")
	tmpDir := filepath.Join(dir, "snapshot-00000000000000000005-tmp")
	removingDir := filepath.Join(dir, "snapshot-00000000000000000003-removing")

	require.NoError(t, os.MkdirAll(keepDir, 0750))
	require.NoError(t, os.MkdirAll(tmpDir, 0750))
	require.NoError(t, os.MkdirAll(removingDir, 0750))

	require.NoError(t, removeTmpDirs(dir))

	_, err := os.Stat(keepDir)
	require.NoError(t, err, "non-tmp dir should survive")

	_, err = os.Stat(tmpDir)
	require.True(t, os.IsNotExist(err), "-tmp dir should be removed")

	_, err = os.Stat(removingDir)
	require.True(t, os.IsNotExist(err), "-removing dir should be removed")
}

func TestRemoveTmpDirsNoOpOnCleanDir(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "snapshot-00000000000000000001"), 0750))
	require.NoError(t, removeTmpDirs(dir))

	entries, _ := os.ReadDir(dir)
	require.Len(t, entries, 1, "clean dir should be untouched")
}

// =============================================================================
// cloneDir / copyFile
// =============================================================================

func TestCloneDirHardlinksSST(t *testing.T) {
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "dst")

	sstData := []byte("fake sst content")
	manifestData := []byte("fake manifest")

	require.NoError(t, os.WriteFile(filepath.Join(src, "000001.sst"), sstData, 0644))
	require.NoError(t, os.WriteFile(filepath.Join(src, "MANIFEST"), manifestData, 0644))
	require.NoError(t, os.WriteFile(filepath.Join(src, "LOCK"), []byte("lock"), 0644))

	require.NoError(t, cloneDir(src, dst))

	gotSST, err := os.ReadFile(filepath.Join(dst, "000001.sst"))
	require.NoError(t, err)
	require.Equal(t, sstData, gotSST)

	gotManifest, err := os.ReadFile(filepath.Join(dst, "MANIFEST"))
	require.NoError(t, err)
	require.Equal(t, manifestData, gotManifest)

	_, err = os.Stat(filepath.Join(dst, "LOCK"))
	require.True(t, os.IsNotExist(err), "LOCK file should be skipped")

	srcInfo, _ := os.Stat(filepath.Join(src, "000001.sst"))
	dstInfo, _ := os.Stat(filepath.Join(dst, "000001.sst"))
	require.True(t, os.SameFile(srcInfo, dstInfo), ".sst should be hardlinked")
}

func TestCloneDirSkipsSubdirectories(t *testing.T) {
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "dst")

	require.NoError(t, os.MkdirAll(filepath.Join(src, "subdir"), 0750))
	require.NoError(t, os.WriteFile(filepath.Join(src, "file.txt"), []byte("data"), 0644))

	require.NoError(t, cloneDir(src, dst))

	_, err := os.Stat(filepath.Join(dst, "subdir"))
	require.True(t, os.IsNotExist(err), "subdirectories should be skipped")

	_, err = os.Stat(filepath.Join(dst, "file.txt"))
	require.NoError(t, err, "regular files should be copied")
}

func TestCopyFileContent(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "src.txt")
	dstPath := filepath.Join(dir, "dst.txt")

	data := []byte("hello world, this is a test for copyFile")
	require.NoError(t, os.WriteFile(srcPath, data, 0644))

	require.NoError(t, copyFile(srcPath, dstPath))

	got, err := os.ReadFile(dstPath)
	require.NoError(t, err)
	require.Equal(t, data, got)
}

// =============================================================================
// atomicRemoveDir
// =============================================================================

func TestAtomicRemoveDir(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "target")

	require.NoError(t, os.MkdirAll(filepath.Join(target, "sub"), 0750))
	require.NoError(t, os.WriteFile(filepath.Join(target, "sub", "f.txt"), []byte("x"), 0644))

	require.NoError(t, atomicRemoveDir(target))

	_, err := os.Stat(target)
	require.True(t, os.IsNotExist(err))

	_, err = os.Stat(target + removingSuffix)
	require.True(t, os.IsNotExist(err), "trash dir should also be gone")
}

// =============================================================================
// reuseWorkingDir
// =============================================================================

func TestReuseWorkingDir(t *testing.T) {
	workDir := t.TempDir()

	require.False(t, reuseWorkingDir(workDir, "snapshot-00000000000000000005"),
		"no SNAPSHOT_BASE file should not reuse")

	require.NoError(t, writeSnapshotBase(workDir, "snapshot-00000000000000000005"))

	require.True(t, reuseWorkingDir(workDir, "snapshot-00000000000000000005"),
		"matching base should reuse")

	require.False(t, reuseWorkingDir(workDir, "snapshot-00000000000000000010"),
		"different base should not reuse")
}

func TestCreateWorkingDirReusesExisting(t *testing.T) {
	dir := t.TempDir()

	snapDir := filepath.Join(dir, snapshotName(5))
	for _, sub := range snapshotDBDirs {
		require.NoError(t, os.MkdirAll(filepath.Join(snapDir, sub), 0750))
	}

	workDir := filepath.Join(dir, workingDirName)

	require.NoError(t, createWorkingDir(snapDir, workDir))

	marker := filepath.Join(workDir, "account", "MARKER")
	require.NoError(t, os.WriteFile(marker, []byte("test"), 0644))

	require.NoError(t, createWorkingDir(snapDir, workDir))

	_, err := os.Stat(marker)
	require.NoError(t, err, "MARKER should survive reuse (no re-clone)")
}

func TestCreateWorkingDirReclones(t *testing.T) {
	dir := t.TempDir()

	snap5 := filepath.Join(dir, snapshotName(5))
	snap10 := filepath.Join(dir, snapshotName(10))
	for _, sub := range snapshotDBDirs {
		require.NoError(t, os.MkdirAll(filepath.Join(snap5, sub), 0750))
		require.NoError(t, os.MkdirAll(filepath.Join(snap10, sub), 0750))
	}

	workDir := filepath.Join(dir, workingDirName)

	require.NoError(t, createWorkingDir(snap5, workDir))

	marker := filepath.Join(workDir, "account", "MARKER")
	require.NoError(t, os.WriteFile(marker, []byte("test"), 0644))

	require.NoError(t, createWorkingDir(snap10, workDir))

	_, err := os.Stat(marker)
	require.True(t, os.IsNotExist(err), "MARKER should be gone after re-clone from different snapshot")
}

// =============================================================================
// pruneSnapshots
// =============================================================================

func TestPruneSnapshotsKeepsRecent(t *testing.T) {
	dir := t.TempDir()
	s := NewCommitStore(dir, nil, Config{SnapshotKeepRecent: 1})
	_, err := s.LoadVersion(0)
	require.NoError(t, err)

	for i := 0; i < 5; i++ {
		commitStorageEntry(t, s, Address{byte(i + 1)}, Slot{byte(i + 1)}, []byte{byte(i + 1)})
		require.NoError(t, s.WriteSnapshot(""))
	}

	flatkvDir := filepath.Join(dir, flatkvRootDir)
	var snapshots []int64
	_ = traverseSnapshots(flatkvDir, true, func(v int64) (bool, error) {
		snapshots = append(snapshots, v)
		return false, nil
	})

	require.Len(t, snapshots, 2, "should keep current(5) + 1 recent")
	require.Contains(t, snapshots, int64(5))
	require.Contains(t, snapshots, int64(4))
	require.NoError(t, s.Close())
}

func TestPruneSnapshotsKeepAll(t *testing.T) {
	dir := t.TempDir()
	s := NewCommitStore(dir, nil, Config{SnapshotKeepRecent: 100})
	_, err := s.LoadVersion(0)
	require.NoError(t, err)
	defer s.Close()

	for i := 0; i < 3; i++ {
		commitStorageEntry(t, s, Address{byte(i + 1)}, Slot{byte(i + 1)}, []byte{byte(i + 1)})
		require.NoError(t, s.WriteSnapshot(""))
	}

	flatkvDir := filepath.Join(dir, flatkvRootDir)
	var count int
	_ = traverseSnapshots(flatkvDir, true, func(_ int64) (bool, error) {
		count++
		return false, nil
	})
	// 4 snapshots: initial snapshot-0 + three manual snapshots (1,2,3)
	require.Equal(t, 4, count, "all snapshots should be kept when KeepRecent is large")
}

// =============================================================================
// Orphan snapshot recovery
// =============================================================================

func TestOrphanSnapshotRecovery(t *testing.T) {
	dir := t.TempDir()
	flatkvDir := filepath.Join(dir, flatkvRootDir)

	snapDir := filepath.Join(flatkvDir, snapshotName(5))
	for _, sub := range snapshotDBDirs {
		require.NoError(t, os.MkdirAll(filepath.Join(snapDir, sub), 0750))
	}

	_, err := os.Lstat(currentPath(flatkvDir))
	require.True(t, os.IsNotExist(err), "no current symlink should exist")

	s := NewCommitStore(dir, nil, DefaultConfig())
	_, err = s.LoadVersion(0)
	require.NoError(t, err)
	defer s.Close()

	target, err := os.Readlink(currentPath(flatkvDir))
	require.NoError(t, err)
	require.Equal(t, snapshotName(5), target, "symlink should be recovered to orphan snapshot")
}

// =============================================================================
// Traverse helpers edge cases
// =============================================================================

func TestTraverseSnapshotsNonExistentDir(t *testing.T) {
	var versions []int64
	err := traverseSnapshots("/nonexistent/path", true, func(v int64) (bool, error) {
		versions = append(versions, v)
		return false, nil
	})
	require.NoError(t, err, "non-existent dir should not error")
	require.Empty(t, versions)
}

func TestTraverseSnapshotsSkipsBadNames(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, snapshotName(10)), 0750))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "not-a-snapshot"), 0750))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "snapshot-short"), 0750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, snapshotName(5)), []byte("file"), 0644))

	var versions []int64
	err := traverseSnapshots(dir, true, func(v int64) (bool, error) {
		versions = append(versions, v)
		return false, nil
	})
	require.NoError(t, err)
	require.Equal(t, []int64{10}, versions, "only valid snapshot dirs should be found")
}

func TestTraverseSnapshotsEarlyStop(t *testing.T) {
	dir := t.TempDir()
	for _, v := range []int64{1, 5, 10, 20} {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, snapshotName(v)), 0750))
	}

	var visited []int64
	err := traverseSnapshots(dir, false, func(v int64) (bool, error) {
		visited = append(visited, v)
		return true, nil
	})
	require.NoError(t, err)
	require.Len(t, visited, 1, "should stop after first callback returns true")
	require.Equal(t, int64(20), visited[0], "descending should visit highest first")
}

// =============================================================================
// verifyWALTail
// =============================================================================

func TestVerifyWALTailSuccess(t *testing.T) {
	dir := t.TempDir()
	s := NewCommitStore(dir, nil, DefaultConfig())
	_, err := s.LoadVersion(0)
	require.NoError(t, err)
	defer s.Close()

	commitStorageEntry(t, s, Address{0x01}, Slot{0x01}, []byte{0x01})
	commitStorageEntry(t, s, Address{0x01}, Slot{0x02}, []byte{0x02})
	commitStorageEntry(t, s, Address{0x01}, Slot{0x03}, []byte{0x03})

	require.NoError(t, s.verifyWALTail(3))
}

func TestVerifyWALTailMismatch(t *testing.T) {
	dir := t.TempDir()
	s := NewCommitStore(dir, nil, DefaultConfig())
	_, err := s.LoadVersion(0)
	require.NoError(t, err)
	defer s.Close()

	commitStorageEntry(t, s, Address{0x01}, Slot{0x01}, []byte{0x01})
	commitStorageEntry(t, s, Address{0x01}, Slot{0x02}, []byte{0x02})

	err = s.verifyWALTail(5)
	require.Error(t, err)
	require.Contains(t, err.Error(), "WAL integrity check failed")
}

// =============================================================================
// tryTruncateWAL
// =============================================================================

func TestTryTruncateWAL(t *testing.T) {
	dir := t.TempDir()
	// SnapshotKeepRecent=0 so pruneSnapshots removes snapshot-0 once
	// the manual snapshot at v5 is created; this makes v5 the earliest
	// snapshot and gives tryTruncateWAL a positive truncation offset.
	s := NewCommitStore(dir, nil, Config{SnapshotKeepRecent: 0})
	_, err := s.LoadVersion(0)
	require.NoError(t, err)
	defer s.Close()

	for i := 0; i < 5; i++ {
		commitStorageEntry(t, s, Address{byte(i + 1)}, Slot{byte(i + 1)}, []byte{byte(i + 1)})
	}

	require.NoError(t, s.WriteSnapshot(""))

	for i := 5; i < 10; i++ {
		commitStorageEntry(t, s, Address{byte(i + 1)}, Slot{byte(i + 1)}, []byte{byte(i + 1)})
	}

	firstBefore, _ := s.changelog.FirstOffset()

	s.tryTruncateWAL()

	firstAfter, _ := s.changelog.FirstOffset()
	require.Greater(t, firstAfter, firstBefore, "WAL should be truncated after snapshot")
}

func TestTryTruncateWALNoSnapshot(t *testing.T) {
	dir := t.TempDir()
	s := NewCommitStore(dir, nil, DefaultConfig())
	_, err := s.LoadVersion(0)
	require.NoError(t, err)
	defer s.Close()

	commitStorageEntry(t, s, Address{0x01}, Slot{0x01}, []byte{0x01})

	firstBefore, _ := s.changelog.FirstOffset()

	s.tryTruncateWAL()

	firstAfter, _ := s.changelog.FirstOffset()
	require.Equal(t, firstBefore, firstAfter, "no snapshot means no truncation")
}

// =============================================================================
// Rollback removes post-target snapshots
// =============================================================================

func TestRollbackRemovesPostTargetSnapshots(t *testing.T) {
	dir := t.TempDir()
	s := NewCommitStore(dir, nil, DefaultConfig())
	_, err := s.LoadVersion(0)
	require.NoError(t, err)

	for i := 0; i < 3; i++ {
		commitStorageEntry(t, s, Address{byte(i + 1)}, Slot{byte(i + 1)}, []byte{byte(i + 1)})
	}
	require.NoError(t, s.WriteSnapshot(""))

	for i := 3; i < 6; i++ {
		commitStorageEntry(t, s, Address{byte(i + 1)}, Slot{byte(i + 1)}, []byte{byte(i + 1)})
	}
	require.NoError(t, s.WriteSnapshot(""))

	for i := 6; i < 8; i++ {
		commitStorageEntry(t, s, Address{byte(i + 1)}, Slot{byte(i + 1)}, []byte{byte(i + 1)})
	}

	flatkvDir := filepath.Join(dir, flatkvRootDir)
	var beforeRollback []int64
	_ = traverseSnapshots(flatkvDir, true, func(v int64) (bool, error) {
		beforeRollback = append(beforeRollback, v)
		return false, nil
	})
	require.Contains(t, beforeRollback, int64(6))

	require.NoError(t, s.Rollback(5))

	var afterRollback []int64
	_ = traverseSnapshots(flatkvDir, true, func(v int64) (bool, error) {
		afterRollback = append(afterRollback, v)
		return false, nil
	})

	for _, v := range afterRollback {
		require.LessOrEqual(t, v, int64(5), "snapshot %d should not exist after rollback to 5", v)
	}
	require.Contains(t, afterRollback, int64(3))

	require.NoError(t, s.Close())
}

// =============================================================================
// updateCurrentSymlink
// =============================================================================

func TestUpdateCurrentSymlinkAtomic(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, updateCurrentSymlink(dir, "snapshot-00000000000000000001"))
	target1, err := os.Readlink(currentPath(dir))
	require.NoError(t, err)
	require.Equal(t, "snapshot-00000000000000000001", target1)

	require.NoError(t, updateCurrentSymlink(dir, "snapshot-00000000000000000002"))
	target2, err := os.Readlink(currentPath(dir))
	require.NoError(t, err)
	require.Equal(t, "snapshot-00000000000000000002", target2)

	_, err = os.Lstat(filepath.Join(dir, currentTmpLink))
	require.True(t, os.IsNotExist(err), "tmp symlink should be cleaned up")
}

// =============================================================================
// seekSnapshot edge cases
// =============================================================================

func TestSeekSnapshotEmptyDir(t *testing.T) {
	dir := t.TempDir()
	_, err := seekSnapshot(dir, 10)
	require.Error(t, err, "empty dir should not find any snapshot")
}

func TestSeekSnapshotExact(t *testing.T) {
	dir := t.TempDir()
	for _, v := range []int64{10, 20, 30} {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, snapshotName(v)), 0750))
	}

	v, err := seekSnapshot(dir, 30)
	require.NoError(t, err)
	require.Equal(t, int64(30), v)

	v, err = seekSnapshot(dir, 25)
	require.NoError(t, err)
	require.Equal(t, int64(20), v)

	v, err = seekSnapshot(dir, 10)
	require.NoError(t, err)
	require.Equal(t, int64(10), v)
}

// =============================================================================
// Multiple snapshots and reopen
// =============================================================================

func TestMultipleSnapshotsAndReopen(t *testing.T) {
	dir := t.TempDir()
	s := NewCommitStore(dir, nil, Config{SnapshotKeepRecent: 10})
	_, err := s.LoadVersion(0)
	require.NoError(t, err)

	var hashes [][]byte
	for i := 0; i < 3; i++ {
		commitStorageEntry(t, s, Address{byte(i + 1)}, Slot{byte(i + 1)}, []byte{byte(i + 1)})
		require.NoError(t, s.WriteSnapshot(""))
		hashes = append(hashes, s.RootHash())
	}
	require.NoError(t, s.Close())

	for i, expectedHash := range hashes {
		ver := int64(i + 1)
		s2 := NewCommitStore(dir, nil, Config{SnapshotKeepRecent: 10})
		_, err := s2.LoadVersion(ver)
		require.NoError(t, err)
		require.Equal(t, ver, s2.Version())
		require.Equal(t, expectedHash, s2.RootHash(), "hash mismatch at version %d", ver)
		require.NoError(t, s2.Close())
	}
}

// =============================================================================
// Snapshot with all key types
// =============================================================================

func TestSnapshotPreservesAllKeyTypes(t *testing.T) {
	dir := t.TempDir()
	s := NewCommitStore(dir, nil, DefaultConfig())
	_, err := s.LoadVersion(0)
	require.NoError(t, err)

	addr := Address{0xAB}
	slot := Slot{0xCD}

	pairs := []*iavl.KVPair{
		{Key: evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slot)), Value: []byte{0x11}},
		{Key: evm.BuildMemIAVLEVMKey(evm.EVMKeyNonce, addr[:]), Value: []byte{0, 0, 0, 0, 0, 0, 0, 7}},
		{Key: evm.BuildMemIAVLEVMKey(evm.EVMKeyCode, addr[:]), Value: []byte{0x60, 0x80}},
	}
	cs := &proto.NamedChangeSet{Name: "evm", Changeset: iavl.ChangeSet{Pairs: pairs}}
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	_, err = s.Commit()
	require.NoError(t, err)

	hash := s.RootHash()
	require.NoError(t, s.WriteSnapshot(""))
	require.NoError(t, s.Close())

	s2 := NewCommitStore(dir, nil, DefaultConfig())
	_, err = s2.LoadVersion(0)
	require.NoError(t, err)
	defer s2.Close()

	require.Equal(t, int64(1), s2.Version())
	require.Equal(t, hash, s2.RootHash())

	storageKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slot))
	v, ok := s2.Get(storageKey)
	require.True(t, ok)
	require.Equal(t, []byte{0x11}, v)

	nonceKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyNonce, addr[:])
	v, ok = s2.Get(nonceKey)
	require.True(t, ok)
	require.Equal(t, []byte{0, 0, 0, 0, 0, 0, 0, 7}, v)

	codeKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyCode, addr[:])
	v, ok = s2.Get(codeKey)
	require.True(t, ok)
	require.Equal(t, []byte{0x60, 0x80}, v)
}
