package flatkv

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/evm"
	db_engine "github.com/sei-protocol/sei-chain/sei-db/db_engine"
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

	flatkvDir := filepath.Join(dir, "flatkv")

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

	flatkvDir := filepath.Join(dir, "flatkv")
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

	flatkvDir := filepath.Join(dir, "flatkv")
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
	tmpPath := filepath.Join(flatkvDir, snapshotName(2)+"-tmp")
	_, statErr := os.Stat(tmpPath)
	require.True(t, os.IsNotExist(statErr), "tmp dir should be cleaned up on failure")

	// Restore codeDB for proper cleanup (reopen is needed for Close to work)
	s.codeDB = savedCodeDB
	_ = s.Close()
}

func TestMigrationFromFlatLayout(t *testing.T) {
	dir := t.TempDir()
	flatkvDir := filepath.Join(dir, "flatkv")

	// Simulate the old flat layout by creating DB dirs directly
	for _, sub := range []string{accountDBDir, codeDBDir, storageDBDir, metadataDir, legacyDBDir} {
		dbPath := filepath.Join(flatkvDir, sub)
		require.NoError(t, os.MkdirAll(dbPath, 0750))
		// Create an actual PebbleDB so Open works
		db, err := pebbledb.Open(dbPath, db_engine.OpenOptions{})
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
	flatkvDir := filepath.Join(dir, "flatkv")
	snapDir, _, err := currentSnapshotDir(flatkvDir)
	require.NoError(t, err)

	accountDBPath := filepath.Join(snapDir, accountDBDir)
	db, err := pebbledb.Open(accountDBPath, db_engine.OpenOptions{})
	require.NoError(t, err)
	lagMeta := &LocalMeta{CommittedVersion: 1}
	require.NoError(t, db.Set(DBLocalMetaKey, MarshalLocalMeta(lagMeta), db_engine.WriteOptions{Sync: true}))
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
