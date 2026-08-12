package flatkv

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
)

func commitStorageEntry(t *testing.T, s *CommitStore, addr ktype.Address, slot ktype.Slot, value []byte) int64 {
	t.Helper()
	padded := make([]byte, 32)
	copy(padded[32-len(value):], value)
	key := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot))
	cs := &proto.NamedChangeSet{
		Name: "evm",
		Changeset: proto.ChangeSet{
			Pairs: []*proto.KVPair{{Key: key, Value: padded}},
		},
	}
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs}))
	return commitAndCheck(t, s)
}

func TestSnapshotCreatesDir(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	err = s.LoadLatest()
	require.NoError(t, err)
	defer s.Close()

	commitStorageEntry(t, s, ktype.Address{0x01}, ktype.Slot{0x01}, []byte{0xAA})

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
	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	err = s.LoadLatest()
	require.NoError(t, err)
	defer s.Close()

	commitStorageEntry(t, s, ktype.Address{0x02}, ktype.Slot{0x02}, []byte{0xBB})

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
	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	s1, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	err = s1.LoadLatest()
	require.NoError(t, err)

	commitStorageEntry(t, s1, ktype.Address{0x10}, ktype.Slot{0x01}, []byte{0x01})
	commitStorageEntry(t, s1, ktype.Address{0x10}, ktype.Slot{0x02}, []byte{0x02})

	require.NoError(t, s1.WriteSnapshot(""))
	require.Equal(t, int64(2), s1.Version())

	commitStorageEntry(t, s1, ktype.Address{0x10}, ktype.Slot{0x03}, []byte{0x03})
	require.Equal(t, int64(3), s1.Version())

	hashAtV3 := s1.RootHash()
	require.NoError(t, s1.Close())

	// Phase 2: reopen - should catchup from v2 snapshot + WAL entry for v3
	cfg = config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	s2, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	err = s2.LoadLatest()
	require.NoError(t, err)
	defer s2.Close()

	require.Equal(t, int64(3), s2.Version())
	require.Equal(t, hashAtV3, s2.RootHash())

	// Verify data from all 3 versions is present
	key1 := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(ktype.Address{0x10}, ktype.Slot{0x01}))
	key3 := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(ktype.Address{0x10}, ktype.Slot{0x03}))
	v, ok := s2.Get(keys.EVMStoreKey, key1)
	require.True(t, ok)
	require.Equal(t, padLeft32(0x01), v)
	v, ok = s2.Get(keys.EVMStoreKey, key3)
	require.True(t, ok)
	require.Equal(t, padLeft32(0x03), v)
}

func TestCatchupUpdatesLtHash(t *testing.T) {
	dir := t.TempDir()

	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	s1, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	err = s1.LoadLatest()
	require.NoError(t, err)

	// Commit 5 versions, snapshot at v2
	commitStorageEntry(t, s1, ktype.Address{0x20}, ktype.Slot{0x01}, []byte{0x10})
	commitStorageEntry(t, s1, ktype.Address{0x20}, ktype.Slot{0x02}, []byte{0x20})
	require.NoError(t, s1.WriteSnapshot(""))

	commitStorageEntry(t, s1, ktype.Address{0x20}, ktype.Slot{0x03}, []byte{0x30})
	hashAtV3 := s1.RootHash()

	commitStorageEntry(t, s1, ktype.Address{0x20}, ktype.Slot{0x04}, []byte{0x40})
	commitStorageEntry(t, s1, ktype.Address{0x20}, ktype.Slot{0x05}, []byte{0x50})
	hashAtV5 := s1.RootHash()
	require.NoError(t, s1.Close())

	// Reopen: catchup from v2 snapshot through v3,v4,v5 via WAL
	cfg = config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	s2, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	err = s2.LoadLatest()
	require.NoError(t, err)
	defer s2.Close()

	require.Equal(t, int64(5), s2.Version())
	require.Equal(t, hashAtV5, s2.RootHash(), "LtHash after catchup must match original")

	_ = hashAtV3 // referenced for clarity but not re-checked here
}

func TestRollbackRewindsState(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	err = s.LoadLatest()
	require.NoError(t, err)

	// Commit v1..v5, snapshot at v3
	commitStorageEntry(t, s, ktype.Address{0x30}, ktype.Slot{0x01}, []byte{0x01})
	commitStorageEntry(t, s, ktype.Address{0x30}, ktype.Slot{0x02}, []byte{0x02})
	commitStorageEntry(t, s, ktype.Address{0x30}, ktype.Slot{0x03}, []byte{0x03})
	require.NoError(t, s.WriteSnapshot(""))

	commitStorageEntry(t, s, ktype.Address{0x30}, ktype.Slot{0x04}, []byte{0x04})
	hashAtV4 := s.RootHash()
	commitStorageEntry(t, s, ktype.Address{0x30}, ktype.Slot{0x05}, []byte{0x05})
	require.Equal(t, int64(5), s.Version())

	// Rollback to v4: restores from v3 snapshot, catches up to v4 via WAL
	require.NoError(t, s.Rollback(4))
	require.Equal(t, int64(4), s.Version())
	require.Equal(t, hashAtV4, s.RootHash())

	// v5's data should not exist (WAL truncated, snapshot pruned)
	key5 := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(ktype.Address{0x30}, ktype.Slot{0x05}))
	_, ok := s.Get(keys.EVMStoreKey, key5)
	require.False(t, ok, "v5 data should be gone after rollback to v4")

	// v4's data should still exist
	key4 := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(ktype.Address{0x30}, ktype.Slot{0x04}))
	v, ok := s.Get(keys.EVMStoreKey, key4)
	require.True(t, ok)
	require.Equal(t, padLeft32(0x04), v)

	require.NoError(t, s.Close())
}

func TestRollbackToSnapshotExact(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	err = s.LoadLatest()
	require.NoError(t, err)

	commitStorageEntry(t, s, ktype.Address{0x40}, ktype.Slot{0x01}, []byte{0x01})
	commitStorageEntry(t, s, ktype.Address{0x40}, ktype.Slot{0x02}, []byte{0x02})
	hashAtV2 := s.RootHash()
	require.NoError(t, s.WriteSnapshot(""))

	commitStorageEntry(t, s, ktype.Address{0x40}, ktype.Slot{0x03}, []byte{0x03})
	require.Equal(t, int64(3), s.Version())

	require.NoError(t, s.Rollback(2))
	require.Equal(t, int64(2), s.Version())
	require.Equal(t, hashAtV2, s.RootHash())

	require.NoError(t, s.Close())
}

func TestPartialSnapshotCleanup(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	err = s.LoadLatest()
	require.NoError(t, err)

	commitStorageEntry(t, s, ktype.Address{0x50}, ktype.Slot{0x01}, []byte{0x01})

	// Take a valid snapshot first
	require.NoError(t, s.WriteSnapshot(""))

	flatkvDir := filepath.Join(dir, flatkvRootDir)
	prevTarget, err := os.Readlink(currentPath(flatkvDir))
	require.NoError(t, err)

	commitStorageEntry(t, s, ktype.Address{0x50}, ktype.Slot{0x02}, []byte{0x02})

	// Sabotage: close the code database so the checkpoint fails on it. The store owns that database
	// now, so there is no handle to save and restore — the store keeps its own reference either way,
	// and the Close below simply reports the already-closed database.
	require.NoError(t, s.rawDBFor(codeDBDir).Close())

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

	// Teardown will report the database this test deliberately closed; that is expected here.
	_ = s.Close()
}

func TestMigrationFromFlatLayout(t *testing.T) {
	dir := t.TempDir()
	flatkvDir := filepath.Join(dir, flatkvRootDir)

	// Simulate the old flat layout by creating DB dirs directly
	for _, sub := range []string{accountDBDir, codeDBDir, storageDBDir, metadataDir, miscDBDir} {
		dbPath := filepath.Join(flatkvDir, sub)
		require.NoError(t, os.MkdirAll(dbPath, 0750))
		// Create an actual PebbleDB so Open works
		cfg := pebbledb.DefaultTestConfig(t)
		cfg.DataDir = dbPath
		db, err := pebbledb.Open(t.Context(), &cfg)
		require.NoError(t, err)
		require.NoError(t, db.Close())
	}

	// Ensure no current symlink exists
	_, err := os.Lstat(currentPath(flatkvDir))
	require.True(t, os.IsNotExist(err))

	// Open the store - should trigger migration
	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	err = s.LoadLatest()
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
	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	s1, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	err = s1.LoadLatest()
	require.NoError(t, err)

	commitStorageEntry(t, s1, ktype.Address{0x60}, ktype.Slot{0x01}, []byte{0x11})
	commitStorageEntry(t, s1, ktype.Address{0x60}, ktype.Slot{0x02}, []byte{0x22})
	hashAtV2 := s1.RootHash()
	require.NoError(t, s1.Close())

	// Phase 2: tamper with one DB's local meta to simulate an incomplete commit
	// (accountDB thinks it's at v1, but global says v2)
	// The working directory, not the snapshot: that is what the reopened store opens.
	flatkvDir := filepath.Join(dir, flatkvRootDir)
	accountDBPath := filepath.Join(flatkvDir, workingDirName, accountDBDir)
	require.Equal(t, cfg.AccountDBConfig.DataDir, accountDBPath,
		"the forged skew must target the directory the store opens, or this test proves nothing")
	acctCfg := pebbledb.DefaultConfig()
	acctCfg.DataDir = accountDBPath
	acctCfg.EnableMetrics = false
	db, err := pebbledb.Open(t.Context(), &acctCfg)
	require.NoError(t, err)
	require.NoError(t, db.Set(ktype.MetaVersionKey, versionToBytes(1), types.WriteOptions{Sync: true}))
	require.NoError(t, db.Close())

	// Phase 3: reopen - should detect skew and catchup
	cfg = config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	s2, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	err = s2.LoadLatest()
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

func TestReadOnlyAtTargetVersion(t *testing.T) {
	dir := t.TempDir()

	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	s1, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	err = s1.LoadLatest()
	require.NoError(t, err)

	commitStorageEntry(t, s1, ktype.Address{0x70}, ktype.Slot{0x01}, []byte{0x01})
	commitStorageEntry(t, s1, ktype.Address{0x70}, ktype.Slot{0x02}, []byte{0x02})
	require.NoError(t, s1.WriteSnapshot(""))
	commitStorageEntry(t, s1, ktype.Address{0x70}, ktype.Slot{0x03}, []byte{0x03})
	hashAtV3 := s1.RootHash()
	commitStorageEntry(t, s1, ktype.Address{0x70}, ktype.Slot{0x04}, []byte{0x04})
	require.NoError(t, s1.Close())

	// Reopen and take a read-only view at version 3.
	cfg = config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	s2, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	require.NoError(t, s2.LoadLatest())
	defer s2.Close()

	ro, err := s2.LoadVersionReadOnly(3)
	require.NoError(t, err)
	defer func() { require.NoError(t, ro.Close()) }()

	require.Equal(t, int64(3), ro.Version())
	require.Equal(t, hashAtV3, ro.RootHash())
}

// TestSnapshotThenCatchupThenVerifyCorrectness verifies that commits after a
// snapshot do not mutate the snapshot's baseline.
func TestSnapshotThenCatchupThenVerifyCorrectness(t *testing.T) {
	dir := t.TempDir()

	addr := ktype.Address{0x7A}
	slot := ktype.Slot{0x7B}
	key := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot))

	// Phase 1: build baseline at v2 and snapshot it.
	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	s1, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	err = s1.LoadLatest()
	require.NoError(t, err)

	commitStorageEntry(t, s1, addr, slot, []byte{0x01})                            // v1
	commitStorageEntry(t, s1, ktype.Address{0x7A}, ktype.Slot{0x7C}, []byte{0xAA}) // v2
	require.NoError(t, s1.WriteSnapshot(""))

	// Record baseline value at v2 for the same key.
	vAtV2, ok := s1.Get(keys.EVMStoreKey, key)
	require.True(t, ok)
	require.Equal(t, padLeft32(0x01), vAtV2)

	// Phase 2: advance state beyond the snapshot (v3..v4).
	commitStorageEntry(t, s1, addr, slot, []byte{0x03}) // v3
	commitStorageEntry(t, s1, addr, slot, []byte{0x04}) // v4
	require.Equal(t, int64(4), s1.Version())
	require.NoError(t, s1.Close())

	// Phase 3: reopen exactly at v2. If later commits had mutated the snapshot
	// baseline in place, we'd incorrectly read 0x04 here.
	cfg = config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	s2, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	require.NoError(t, s2.LoadLatest())
	ro, err := s2.LoadVersionReadOnly(2)
	require.NoError(t, err)
	gotV2, ok := ro.Get(keys.EVMStoreKey, key)
	require.True(t, ok)
	require.Equal(t, padLeft32(0x01), gotV2, "snapshot baseline should remain stable")
	require.NoError(t, ro.Close())
	require.NoError(t, s2.Close())

	// Phase 4: reopen latest again to ensure catchup/replay still reaches v4.
	cfg = config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	s3, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	err = s3.LoadLatest()
	require.NoError(t, err)
	defer s3.Close()

	require.Equal(t, int64(4), s3.Version())
	gotLatest, ok := s3.Get(keys.EVMStoreKey, key)
	require.True(t, ok)
	require.Equal(t, padLeft32(0x04), gotLatest)
}

// TestLoadVersionMixedSequence: load-old -> load-latest -> load-old-again.
// Ensures the working directory keeps snapshots immutable across mixed loads.
func TestReadOnlyAtIsUnaffectedByLoadLatest(t *testing.T) {
	dir := t.TempDir()

	addr := ktype.Address{0x80}
	slot := ktype.Slot{0x81}
	key := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot))

	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	err = s.LoadLatest()
	require.NoError(t, err)

	commitStorageEntry(t, s, addr, slot, []byte{0x01})
	commitStorageEntry(t, s, addr, slot, []byte{0x02})
	hashAtV2 := s.RootHash()
	require.NoError(t, s.WriteSnapshot(""))

	commitStorageEntry(t, s, addr, slot, []byte{0x03})
	commitStorageEntry(t, s, addr, slot, []byte{0x04})
	hashAtV4 := s.RootHash()
	require.NoError(t, s.Close())

	// Reopen at latest: the working dir is now dirty at v4, well past the v2 snapshot.
	cfg = config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	s2, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	require.NoError(t, s2.LoadLatest())
	defer func() { require.NoError(t, s2.Close()) }()
	require.Equal(t, int64(4), s2.Version())
	require.Equal(t, hashAtV4, s2.RootHash())

	requireViewAtV2 := func(what string) {
		t.Helper()
		ro, err := s2.LoadVersionReadOnly(2)
		require.NoError(t, err, what)
		defer func() { require.NoError(t, ro.Close()) }()
		require.Equal(t, int64(2), ro.Version(), what)
		require.Equal(t, hashAtV2, ro.RootHash(), what)
		v, ok := ro.Get(keys.EVMStoreKey, key)
		require.True(t, ok, what)
		require.Equal(t, padLeft32(0x02), v, what)
	}

	// A working dir left at the tip must not leak into an older view.
	requireViewAtV2("view at v2 must not see the tip the working dir sits at")

	// Committing on the primary while views are taken must not disturb the older view either.
	commitStorageEntry(t, s2, addr, slot, []byte{0x05})
	require.Equal(t, int64(5), s2.Version())
	requireViewAtV2("view at v2 must be unaffected by a concurrent commit")
}

// TestRollbackToSnapshotVersion: rollback to a version that is exactly a snapshot boundary. The WAL tail
// beyond the target is dropped (via close → offline PruneAfter → reopen) so a restart does not re-apply the
// later blocks and the store stays at the rolled-back version.
func TestRollbackToSnapshotVersion(t *testing.T) {
	dir := t.TempDir()

	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	err = s.LoadLatest()
	require.NoError(t, err)

	// Build: v1..v5, snapshot at v2.
	commitStorageEntry(t, s, ktype.Address{0x90}, ktype.Slot{0x01}, []byte{0x01})
	commitStorageEntry(t, s, ktype.Address{0x90}, ktype.Slot{0x02}, []byte{0x02})
	hashAtV2 := s.RootHash()
	require.NoError(t, s.WriteSnapshot(""))

	commitStorageEntry(t, s, ktype.Address{0x90}, ktype.Slot{0x03}, []byte{0x03})
	commitStorageEntry(t, s, ktype.Address{0x90}, ktype.Slot{0x04}, []byte{0x04})
	commitStorageEntry(t, s, ktype.Address{0x90}, ktype.Slot{0x05}, []byte{0x05})

	// Rollback to v2: lands at the v2 snapshot exactly, with the WAL tail beyond v2 pruned.
	require.NoError(t, s.Rollback(2))
	require.Equal(t, int64(2), s.Version())
	require.Equal(t, hashAtV2, s.RootHash())

	// The WAL must not hold anything above the rolled-back version, or a restart would re-apply v3..v5.
	ok, _, last, err := s.wal.GetStoredRange()
	require.NoError(t, err)
	require.True(t, !ok || last <= 2, "WAL must not retain blocks above the rolled-back version")

	// A fresh commit resumes contiguously at v3.
	commitStorageEntry(t, s, ktype.Address{0x90}, ktype.Slot{0x06}, []byte{0x06})
	require.Equal(t, int64(3), s.Version())

	// Simulate restart from the rolled-back-then-advanced state: should land at v3.
	hashAtV3 := s.RootHash()
	require.NoError(t, s.Close())
	cfg = config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	s2, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	err = s2.LoadLatest()
	require.NoError(t, err)
	defer s2.Close()

	require.Equal(t, int64(3), s2.Version())
	require.Equal(t, hashAtV3, s2.RootHash())
}

// rollbackFixture returns a store with v1..v5 committed and a snapshot at v2.
func rollbackFixture(t *testing.T) *CommitStore {
	t.Helper()
	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(t.TempDir(), flatkvRootDir)
	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	err = s.LoadLatest()
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	for i := byte(1); i <= 5; i++ {
		commitStorageEntry(t, s, ktype.Address{0x91}, ktype.Slot{i}, []byte{i})
		if i == 2 {
			require.NoError(t, s.WriteSnapshot(""))
		}
	}
	return s
}

// requireRollbackRejected asserts Rollback(target) fails up front with wantErr and leaves the store completely
// untouched: the same snapshot is current, the WAL holds the same blocks, the version has not moved, and the
// store is still usable. wantErr must name a message only the pre-flight check produces — some unreachable
// targets also fail late, after the store has been rewound, so matching the message is what distinguishes
// "refused before touching anything" from "attempted and then reported". The trailing commit catches the
// reachability check drifting below the point where Rollback closes the DBs.
func requireRollbackRejected(t *testing.T, s *CommitStore, target int64, wantErr string) {
	t.Helper()
	dir := s.flatkvDir()

	_, curBefore, err := currentSnapshotDir(dir)
	require.NoError(t, err)
	okBefore, firstBefore, lastBefore, err := s.wal.GetStoredRange()
	require.NoError(t, err)
	versionBefore := s.Version()

	rollbackErr := s.Rollback(target)
	require.Error(t, rollbackErr, "an unreachable rollback target must be rejected")
	require.Contains(t, rollbackErr.Error(), wantErr, "must be refused by the pre-flight check, not mid-flight")

	_, curAfter, err := currentSnapshotDir(dir)
	require.NoError(t, err)
	require.Equal(t, curBefore, curAfter, "a rejected rollback must not move the current snapshot")

	okAfter, firstAfter, lastAfter, err := s.wal.GetStoredRange()
	require.NoError(t, err)
	require.Equal(t, okBefore, okAfter, "a rejected rollback must not touch the WAL")
	require.Equal(t, firstBefore, firstAfter, "a rejected rollback must not prune the WAL")
	require.Equal(t, lastBefore, lastAfter, "a rejected rollback must not truncate the WAL")
	require.Equal(t, versionBefore, s.Version(), "a rejected rollback must not move the version")

	commitStorageEntry(t, s, ktype.Address{0x9F}, ktype.Slot{0xFF}, []byte{0xFF})
	require.Equal(t, versionBefore+1, s.Version(), "the store must remain usable after a rejected rollback")
}

// TestRollbackRejectsTargetBeyondWALEnd verifies a target above everything the WAL holds is refused up front,
// rather than discovered after the store has already been rewound and pruned.
func TestRollbackRejectsTargetBeyondWALEnd(t *testing.T) {
	requireRollbackRejected(t, rollbackFixture(t), 9, "the WAL only holds")
}

// rollbackFixtureEmptyWALAtV2 returns a store with v1..v2 committed, a snapshot at v2, and an emptied WAL, so
// the caller can choose exactly which blocks the WAL retains. Prune is file-granular and asynchronous, so it
// cannot be used to shape a small WAL; wiping and re-committing is deterministic.
func rollbackFixtureEmptyWALAtV2(t *testing.T) *CommitStore {
	t.Helper()
	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(t.TempDir(), flatkvRootDir)
	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	err = s.LoadLatest()
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	commitStorageEntry(t, s, ktype.Address{0x92}, ktype.Slot{0x01}, []byte{0x01})
	commitStorageEntry(t, s, ktype.Address{0x92}, ktype.Slot{0x02}, []byte{0x02})
	require.NoError(t, s.WriteSnapshot(""))
	resetWALForTest(t, s)
	return s
}

// TestRollbackRejectsTargetTheWALNoLongerCovers verifies that when the snapshot lands below the target and the
// WAL does not reach back to the blocks in between, the rollback is refused instead of landing on the snapshot
// with the intervening blocks destroyed.
func TestRollbackRejectsTargetTheWALNoLongerCovers(t *testing.T) {
	s := rollbackFixtureEmptyWALAtV2(t)

	// Commit v3, then wipe the WAL again and commit v4, so the WAL starts at 4 while the snapshot is still
	// at 2: reaching v4 would need block 3, which the WAL no longer holds.
	for _, v := range []int64{3, 4} {
		if v == 4 {
			resetWALForTest(t, s)
		}
		cs := makeChangeSet(evmStorageKey(ktype.Address{0x92}, ktype.Slot{byte(v)}), padLeft32(byte(v)), false)
		require.NoError(t, s.ApplyChangeSets(v, []*proto.NamedChangeSet{cs}))
		_, err := s.Commit(v)
		require.NoError(t, err)
	}
	var err error

	ok, first, _, err := s.wal.GetStoredRange()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(4), first, "fixture precondition: the WAL must start above the v2 snapshot")

	requireRollbackRejected(t, s, 4, "the WAL only holds")
}

// TestRollbackRejectsVersionZero verifies version 0 is refused: it means no state, so there is nothing to roll
// back to, and it is the one target that would reach PruneAfter's retains-block-zero boundary.
func TestRollbackRejectsVersionZero(t *testing.T) {
	requireRollbackRejected(t, rollbackFixture(t), 0, "nothing to roll back to")
}

// rollbackFixtureMidChainWALStart returns a store seeded to begin at block 10, so its snapshot sits at 9 and
// its WAL holds 10-12 — a store that legally started mid-chain and has no history behind its first WAL block.
func rollbackFixtureMidChainWALStart(t *testing.T) *CommitStore {
	t.Helper()
	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(t.TempDir(), flatkvRootDir)
	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	err = s.LoadLatest()
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	require.NoError(t, s.SetInitialVersion(10))
	for _, v := range []int64{10, 11, 12} {
		cs := makeChangeSet(evmStorageKey(ktype.Address{0x93}, ktype.Slot{byte(v)}), padLeft32(byte(v)), false)
		require.NoError(t, s.ApplyChangeSets(v, []*proto.NamedChangeSet{cs}))
		_, err := s.Commit(v)
		require.NoError(t, err)
	}

	base, err := seekSnapshot(s.flatkvDir(), 11)
	require.NoError(t, err)
	require.Equal(t, int64(9), base, "fixture precondition: the seeded snapshot must sit at 9")
	ok, first, last, err := s.wal.GetStoredRange()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(10), first, "fixture precondition: the WAL must begin above block 1")
	require.Equal(t, uint64(12), last)
	return s
}

// TestRollbackAcceptsMidChainWALStart verifies a target inside the WAL is reachable on a store that legally
// began mid-chain: the seeded snapshot at 9 is the baseline, and the WAL supplies 10 onwards.
func TestRollbackAcceptsMidChainWALStart(t *testing.T) {
	s := rollbackFixtureMidChainWALStart(t)

	require.NoError(t, s.Rollback(11))
	require.Equal(t, int64(11), s.Version())

	ok, first, last, err := s.wal.GetStoredRange()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(10), first, "rollback must not drop blocks at or below the target")
	require.Equal(t, uint64(11), last, "rollback must truncate the WAL to the target")
}

// TestRollbackRejectsTargetBelowMidChainWALStart verifies a target below where this store's history begins
// stays an up-front rejection: no snapshot names it and the WAL does not reach it, and PruneAfter(target)
// would drop every block the WAL holds, so discovering it after the rewind would destroy the whole history.
func TestRollbackRejectsTargetBelowMidChainWALStart(t *testing.T) {
	requireRollbackRejected(t, rollbackFixtureMidChainWALStart(t), 5, "blocks 1-5 are needed")
}

// TestRollbackToTargetPredatingWALSucceeds covers the truncate-to-empty case: the target sits exactly on a
// snapshot and below every block the WAL still holds, so pruning to it empties the WAL. No replay is needed,
// so this is reachable and must succeed.
func TestRollbackToTargetPredatingWALSucceeds(t *testing.T) {
	s := rollbackFixtureEmptyWALAtV2(t)

	// The WAL holds only blocks above the v2 snapshot, so rolling back to v2 must empty it.
	for i := byte(3); i <= 5; i++ {
		commitStorageEntry(t, s, ktype.Address{0x92}, ktype.Slot{i}, []byte{i})
	}
	ok, first, _, err := s.wal.GetStoredRange()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(3), first)

	require.NoError(t, s.Rollback(2))
	require.Equal(t, int64(2), s.Version())

	ok, _, _, err = s.wal.GetStoredRange()
	require.NoError(t, err)
	require.False(t, ok, "pruning to a target below every retained block must empty the WAL")

	// The emptied WAL accepts the next contiguous block, so the store keeps working.
	commitStorageEntry(t, s, ktype.Address{0x92}, ktype.Slot{0x06}, []byte{0x06})
	require.Equal(t, int64(3), s.Version())
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
	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(t.TempDir(), flatkvRootDir)
	cfg.SnapshotKeepRecent = 1
	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	err = s.LoadLatest()
	require.NoError(t, err)

	for i := 0; i < 5; i++ {
		commitStorageEntry(t, s, ktype.Address{byte(i + 1)}, ktype.Slot{byte(i + 1)}, []byte{byte(i + 1)})
		require.NoError(t, s.WriteSnapshot(""))
	}

	var snapshots []int64
	_ = traverseSnapshots(cfg.DataDir, true, func(v int64) (bool, error) {
		snapshots = append(snapshots, v)
		return false, nil
	})

	require.Len(t, snapshots, 2, "should keep current(5) + 1 recent")
	require.Contains(t, snapshots, int64(5))
	require.Contains(t, snapshots, int64(4))
	require.NoError(t, s.Close())
}

func TestPruneSnapshotsKeepAll(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	cfg.SnapshotKeepRecent = 100
	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	err = s.LoadLatest()
	require.NoError(t, err)
	defer s.Close()

	for i := 0; i < 3; i++ {
		commitStorageEntry(t, s, ktype.Address{byte(i + 1)}, ktype.Slot{byte(i + 1)}, []byte{byte(i + 1)})
		require.NoError(t, s.WriteSnapshot(""))
	}

	var count int
	_ = traverseSnapshots(cfg.DataDir, true, func(_ int64) (bool, error) {
		count++
		return false, nil
	})
	// 4 snapshots: initial snapshot-0 + three manual snapshots (1,2,3)
	require.Equal(t, 4, count, "all snapshots should be kept when KeepRecent is large")
}

func TestPruneSnapshotsIgnoresSnapshotsAboveCurrent(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(t.TempDir(), flatkvRootDir)
	cfg.SnapshotKeepRecent = 2
	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	require.NoError(t, s.LoadLatest())
	defer s.Close()

	// snapshot-40 is what a rollback that could not finish leaves behind. Pruning runs against a directory of
	// its own so that remnant is the only thing separating this layout from a healthy one.
	dir := t.TempDir()
	planted := []int64{10, 20, 30, 40}
	for _, v := range planted {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, snapshotName(v)), 0750))
	}

	layout := s.snapshotLayout()
	layout.dir = dir
	require.Equal(t, 0, pruneSnapshotsByCount(s.ctx, layout, 30),
		"only 10 and 20 sit below the current version, and KeepRecent=2 covers both")

	var remaining []int64
	require.NoError(t, traverseSnapshots(dir, true, func(v int64) (bool, error) {
		remaining = append(remaining, v)
		return false, nil
	}))
	require.Equal(t, planted, remaining,
		"snapshot-40 must not take a keep slot and evict snapshot-10, which rollback still needs as a base")
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

	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	err = s.LoadLatest()
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
// Rollback removes post-target snapshots
// =============================================================================

func TestRollbackRemovesPostTargetSnapshots(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	err = s.LoadLatest()
	require.NoError(t, err)

	for i := 0; i < 3; i++ {
		commitStorageEntry(t, s, ktype.Address{byte(i + 1)}, ktype.Slot{byte(i + 1)}, []byte{byte(i + 1)})
	}
	require.NoError(t, s.WriteSnapshot(""))

	for i := 3; i < 6; i++ {
		commitStorageEntry(t, s, ktype.Address{byte(i + 1)}, ktype.Slot{byte(i + 1)}, []byte{byte(i + 1)})
	}
	require.NoError(t, s.WriteSnapshot(""))

	for i := 6; i < 8; i++ {
		commitStorageEntry(t, s, ktype.Address{byte(i + 1)}, ktype.Slot{byte(i + 1)}, []byte{byte(i + 1)})
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

func TestRemoveSnapshotsAboveKeepsTargetAndBelow(t *testing.T) {
	dir := t.TempDir()
	for _, v := range []int64{3, 5, 7, 9} {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, snapshotName(v)), 0750))
	}

	require.NoError(t, removeSnapshotsAbove(dir, 5))

	var remaining []int64
	require.NoError(t, traverseSnapshots(dir, true, func(v int64) (bool, error) {
		remaining = append(remaining, v)
		return false, nil
	}))
	require.Equal(t, []int64{3, 5}, remaining, "the snapshot sitting on the target is kept")
}

func TestRemoveSnapshotsAboveReportsEveryFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, which is not stopped by the directory permissions this test relies on")
	}

	dir := t.TempDir()
	for _, v := range []int64{3, 7, 9} {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, snapshotName(v)), 0750))
	}

	// atomicRemoveDir renames the snapshot within this directory, so withholding write permission on it is
	// what makes the removal fail. Restore it before t.TempDir's own cleanup, which runs after this one.
	require.NoError(t, os.Chmod(dir, 0555))
	t.Cleanup(func() { _ = os.Chmod(dir, 0750) })

	err := removeSnapshotsAbove(dir, 5)
	require.Error(t, err, "a snapshot above the target that survives must not be reported as a clean rollback")
	require.Contains(t, err.Error(), "remove snapshot 7", "the first failure must be named")
	require.Contains(t, err.Error(), "remove snapshot 9",
		"failing on 7 must not hide 9: the caller reconciles from the whole list, not the first name")
}

// TestRollbackReportsUnremovableSnapshotWithoutRewinding pins the ordering that makes the error safe to act
// on. Removing snapshots above the target runs before the WAL is pruned, so a failure there means the
// rollback did not take effect and every caller that skips its post-rollback bookkeeping on error is right
// to — `seid rollback` most of all, since it rewinds the app before Tendermint.
func TestRollbackReportsUnremovableSnapshotWithoutRewinding(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, which is not stopped by the directory permissions this test relies on")
	}

	dir := t.TempDir()
	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	require.NoError(t, s.LoadLatest())
	defer func() { _ = s.Close() }()

	for i := 0; i < 3; i++ {
		commitStorageEntry(t, s, ktype.Address{byte(i + 1)}, ktype.Slot{byte(i + 1)}, []byte{byte(i + 1)})
	}
	require.NoError(t, s.WriteSnapshot(""))
	for i := 3; i < 6; i++ {
		commitStorageEntry(t, s, ktype.Address{byte(i + 1)}, ktype.Slot{byte(i + 1)}, []byte{byte(i + 1)})
	}
	require.NoError(t, s.WriteSnapshot("")) // snapshot-6, above the rollback target below

	// atomicRemoveDir renames snapshot-6 onto this trash name before unlinking it, so an undeletable
	// directory already sitting there fails that rename. Restore permissions before t.TempDir's own cleanup,
	// which runs after this one.
	blocker := filepath.Join(cfg.DataDir, snapshotName(6)+removingSuffix)
	require.NoError(t, os.MkdirAll(blocker, 0750))
	require.NoError(t, os.WriteFile(filepath.Join(blocker, "occupied"), []byte("x"), 0600))
	require.NoError(t, os.Chmod(blocker, 0555))
	t.Cleanup(func() { _ = os.Chmod(blocker, 0750) })

	err = s.Rollback(5)
	require.Error(t, err, "Rollback must not report success while a snapshot above the target is still on disk")
	require.Contains(t, err.Error(), "remove snapshot 6")

	require.Contains(t, walBlockNumbers(t, s), uint64(6),
		"the WAL must be untouched, so a restart can replay back to the old tail and the rollback be retried")
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
	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	cfg.SnapshotKeepRecent = 10
	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	err = s.LoadLatest()
	require.NoError(t, err)

	var hashes [][]byte
	for i := 0; i < 3; i++ {
		commitStorageEntry(t, s, ktype.Address{byte(i + 1)}, ktype.Slot{byte(i + 1)}, []byte{byte(i + 1)})
		require.NoError(t, s.WriteSnapshot(""))
		hashes = append(hashes, s.RootHash())
	}
	require.NoError(t, s.Close())

	cfg2 := config.DefaultTestConfig(t)
	cfg2.DataDir = filepath.Join(dir, flatkvRootDir)
	cfg2.SnapshotKeepRecent = 10
	s2, err := newCommitStoreWithWAL(t.Context(), cfg2)
	require.NoError(t, err)
	require.NoError(t, s2.LoadLatest())
	defer func() { require.NoError(t, s2.Close()) }()

	for i, expectedHash := range hashes {
		ver := int64(i + 1)
		ro, err := s2.LoadVersionReadOnly(ver)
		require.NoError(t, err)
		require.Equal(t, ver, ro.Version())
		require.Equal(t, expectedHash, ro.RootHash(), "hash mismatch at version %d", ver)
		require.NoError(t, ro.Close())
	}
}

// =============================================================================
// Snapshot with all key types
// =============================================================================

func TestWriteSnapshotUpdatesSnapshotBase(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	s, err := newCommitStoreWithWAL(context.Background(), cfg)
	require.NoError(t, err)
	err = s.LoadLatest()
	require.NoError(t, err)

	commitStorageEntry(t, s, ktype.Address{0xF0}, ktype.Slot{0x01}, []byte{0x01})
	commitStorageEntry(t, s, ktype.Address{0xF0}, ktype.Slot{0x02}, []byte{0x02})
	require.NoError(t, s.WriteSnapshot(""))

	flatkvDir := filepath.Join(dir, flatkvRootDir)
	workDir := filepath.Join(flatkvDir, workingDirName)

	// SNAPSHOT_BASE should now match the new snapshot, not the old one.
	data, err := os.ReadFile(filepath.Join(workDir, snapshotBaseFile))
	require.NoError(t, err)
	require.Equal(t, snapshotName(2), strings.TrimSpace(string(data)))

	// Commit more versions beyond the snapshot.
	commitStorageEntry(t, s, ktype.Address{0xF0}, ktype.Slot{0x03}, []byte{0x03})
	commitStorageEntry(t, s, ktype.Address{0xF0}, ktype.Slot{0x04}, []byte{0x04})
	commitStorageEntry(t, s, ktype.Address{0xF0}, ktype.Slot{0x05}, []byte{0x05})
	hashAtV5 := s.RootHash()
	require.NoError(t, s.Close())

	// Reopen: working dir should be reused (SNAPSHOT_BASE matches current),
	// so committedVersion should be 5 (from working dir metadata), not 2
	// (from the snapshot). Catchup should replay 0 entries.
	cfg = config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	s2, err := newCommitStoreWithWAL(context.Background(), cfg)
	require.NoError(t, err)
	err = s2.LoadLatest()
	require.NoError(t, err)
	defer s2.Close()

	require.Equal(t, int64(5), s2.Version())
	require.Equal(t, hashAtV5, s2.RootHash())
}

func TestSnapshotPreservesAllKeyTypes(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	err = s.LoadLatest()
	require.NoError(t, err)

	addr := ktype.Address{0xAB}
	slot := ktype.Slot{0xCD}

	pairs := []*proto.KVPair{
		{Key: keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot)), Value: padLeft32(0x11)},
		{Key: keys.BuildEVMKey(keys.EVMKeyNonce, addr[:]), Value: []byte{0, 0, 0, 0, 0, 0, 0, 7}},
		{Key: keys.BuildEVMKey(keys.EVMKeyCode, addr[:]), Value: []byte{0x60, 0x80}},
	}
	cs := &proto.NamedChangeSet{Name: "evm", Changeset: proto.ChangeSet{Pairs: pairs}}
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs}))
	_, err = s.Commit(s.Version() + 1)
	require.NoError(t, err)

	hash := s.RootHash()
	require.NoError(t, s.WriteSnapshot(""))
	require.NoError(t, s.Close())

	cfg = config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	s2, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	err = s2.LoadLatest()
	require.NoError(t, err)
	defer s2.Close()

	require.Equal(t, int64(1), s2.Version())
	require.Equal(t, hash, s2.RootHash())

	storageKey := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot))
	v, ok := s2.Get(keys.EVMStoreKey, storageKey)
	require.True(t, ok)
	require.Equal(t, padLeft32(0x11), v)

	nonceKey := keys.BuildEVMKey(keys.EVMKeyNonce, addr[:])
	v, ok = s2.Get(keys.EVMStoreKey, nonceKey)
	require.True(t, ok)
	require.Equal(t, []byte{0, 0, 0, 0, 0, 0, 0, 7}, v)

	codeKey := keys.BuildEVMKey(keys.EVMKeyCode, addr[:])
	v, ok = s2.Get(keys.EVMStoreKey, codeKey)
	require.True(t, ok)
	require.Equal(t, []byte{0x60, 0x80}, v)
}

// =============================================================================
// Reopen After Empty Commits (W-P1-5)
// =============================================================================

func TestReopenAfterEmptyCommits(t *testing.T) {
	dir := t.TempDir()
	dbDir := filepath.Join(dir, flatkvRootDir)

	cfg := config.DefaultConfig()
	cfg.DataDir = dbDir
	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	err = s.LoadLatest()
	require.NoError(t, err)

	for i := 0; i < 3; i++ {
		require.NoError(t, s.ApplyChangeSets(s.Version()+1, nil))
		_, err := s.Commit(s.Version() + 1)
		require.NoError(t, err)
	}

	require.Equal(t, int64(3), s.Version())
	hashBefore := s.RootHash()
	require.NoError(t, s.Close())

	cfg2 := config.DefaultConfig()
	cfg2.DataDir = dbDir
	s2, err := newCommitStoreWithWAL(context.Background(), cfg2)
	require.NoError(t, err)
	err = s2.LoadLatest()
	require.NoError(t, err)
	defer s2.Close()

	require.Equal(t, int64(3), s2.Version(), "version should be preserved after reopen")
	require.Equal(t, hashBefore, s2.RootHash(), "LtHash should be unchanged after reopen")
}

// =============================================================================
// Reopen After Deletes (W-P1-6)
// =============================================================================

func TestReopenAfterDeletes(t *testing.T) {
	dir := t.TempDir()
	dbDir := filepath.Join(dir, flatkvRootDir)

	cfg := config.DefaultConfig()
	cfg.DataDir = dbDir
	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	err = s.LoadLatest()
	require.NoError(t, err)

	addr := ktype.Address{0xEE}
	slot := ktype.Slot{0xFF}

	ch := codeHashN(0x77)
	cs := &proto.NamedChangeSet{
		Name: "evm",
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot)), Value: padLeft32(0x11)},
			{Key: keys.BuildEVMKey(keys.EVMKeyNonce, addr[:]), Value: []byte{0, 0, 0, 0, 0, 0, 0, 42}},
			{Key: keys.BuildEVMKey(keys.EVMKeyCodeHash, addr[:]), Value: ch[:]},
			{Key: keys.BuildEVMKey(keys.EVMKeyCode, addr[:]), Value: []byte{0x60, 0x80}},
		}},
	}
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs}))
	_, err = s.Commit(s.Version() + 1)
	require.NoError(t, err)

	delCS := &proto.NamedChangeSet{
		Name: "evm",
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot)), Delete: true},
			{Key: keys.BuildEVMKey(keys.EVMKeyNonce, addr[:]), Delete: true},
			{Key: keys.BuildEVMKey(keys.EVMKeyCodeHash, addr[:]), Delete: true},
			{Key: keys.BuildEVMKey(keys.EVMKeyCode, addr[:]), Delete: true},
		}},
	}
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{delCS}))
	_, err = s.Commit(s.Version() + 1)
	require.NoError(t, err)

	hashBefore := s.RootHash()
	require.NoError(t, s.Close())

	cfg2 := config.DefaultConfig()
	cfg2.DataDir = dbDir
	s2, err := newCommitStoreWithWAL(context.Background(), cfg2)
	require.NoError(t, err)
	err = s2.LoadLatest()
	require.NoError(t, err)
	defer s2.Close()

	require.Equal(t, hashBefore, s2.RootHash())

	storageKey := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot))
	_, found := s2.Get(keys.EVMStoreKey, storageKey)
	require.False(t, found, "storage should stay deleted after reopen")

	codeKey2 := keys.BuildEVMKey(keys.EVMKeyCode, addr[:])
	_, found = s2.Get(keys.EVMStoreKey, codeKey2)
	require.False(t, found, "code should stay deleted after reopen")

	// With Account Row GC, all-zero account row is physically deleted.
	nonceKey := keys.BuildEVMKey(keys.EVMKeyNonce, addr[:])
	nonceVal, found := s2.Get(keys.EVMStoreKey, nonceKey)
	require.False(t, found, "nonce should not be found after reopen (row deleted)")
	require.Nil(t, nonceVal)

	chKey := keys.BuildEVMKey(keys.EVMKeyCodeHash, addr[:])
	chVal, found := s2.Get(keys.EVMStoreKey, chKey)
	require.False(t, found, "codehash should not be found after reopen (row deleted)")
	require.Nil(t, chVal)
}

// =============================================================================
// WAL Truncation + Rollback Combo (W-P2-5)
// =============================================================================

func TestWALTruncationThenRollback(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	cfg.SnapshotInterval = 5
	cfg.SnapshotKeepRecent = 1
	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	err = s.LoadLatest()
	require.NoError(t, err)

	for i := 1; i <= 10; i++ {
		commitStorageEntry(t, s, addrN(byte(i)), slotN(byte(i)), []byte{byte(i)})
	}

	s.tryTruncateWAL()

	require.NoError(t, s.Rollback(5))
	require.Equal(t, int64(5), s.Version())

	for i := 1; i <= 5; i++ {
		key := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addrN(byte(i)), slotN(byte(i))))
		var val []byte
		var found bool
		val, found = s.Get(keys.EVMStoreKey, key)
		require.True(t, found, "key at block %d should exist after rollback to v5", i)
		require.Equal(t, padLeft32(byte(i)), val)
	}

	for i := 6; i <= 10; i++ {
		key := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addrN(byte(i)), slotN(byte(i))))
		var found bool
		_, found = s.Get(keys.EVMStoreKey, key)
		require.False(t, found, "key at block %d should NOT exist after rollback to v5", i)
	}

	require.NoError(t, s.Close())
}

// =============================================================================
// Reopen After Snapshot + Truncation (W-P2-6)
// =============================================================================

func TestReopenAfterSnapshotAndTruncation(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	cfg.SnapshotInterval = 5
	cfg.SnapshotKeepRecent = 1

	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	err = s.LoadLatest()
	require.NoError(t, err)

	for i := 1; i <= 10; i++ {
		commitStorageEntry(t, s, addrN(byte(i)), slotN(byte(i)), []byte{byte(i)})
	}

	s.tryTruncateWAL()
	hashBefore := s.RootHash()
	require.NoError(t, s.Close())

	s2, err := newCommitStoreWithWAL(context.Background(), cfg)
	require.NoError(t, err)
	err = s2.LoadLatest()
	require.NoError(t, err)
	defer s2.Close()

	require.Equal(t, int64(10), s2.Version())
	require.Equal(t, hashBefore, s2.RootHash())

	for i := 1; i <= 10; i++ {
		key := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addrN(byte(i)), slotN(byte(i))))
		var val []byte
		var found bool
		val, found = s2.Get(keys.EVMStoreKey, key)
		require.True(t, found, "key at block %d should exist after reopen", i)
		require.Equal(t, padLeft32(byte(i)), val)
	}
}

// =============================================================================
// Single DB Open Failure (W-P3-1)
// =============================================================================

func TestSingleDBOpenFailure(t *testing.T) {
	dir := t.TempDir()
	dbDir := filepath.Join(dir, flatkvRootDir)

	cfg := config.DefaultConfig()
	cfg.DataDir = dbDir
	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	err = s.LoadLatest()
	require.NoError(t, err)
	commitStorageEntry(t, s, ktype.Address{0x01}, ktype.Slot{0x01}, []byte{0xAA})
	require.NoError(t, s.WriteSnapshot(""))
	require.NoError(t, s.Close())

	workingStorage := filepath.Join(dbDir, "working", storageDBDir)
	manifests, _ := filepath.Glob(filepath.Join(workingStorage, "MANIFEST-*"))
	for _, m := range manifests {
		require.NoError(t, os.Remove(m))
	}
	snapshotStorage := filepath.Join(dbDir, snapshotName(1), storageDBDir)
	snapManifests, _ := filepath.Glob(filepath.Join(snapshotStorage, "MANIFEST-*"))
	for _, m := range snapManifests {
		require.NoError(t, os.Remove(m))
	}
	_ = os.Remove(filepath.Join(dbDir, "working", snapshotBaseFile))

	cfg2 := config.DefaultConfig()
	cfg2.DataDir = dbDir
	s2, err := newCommitStoreWithWAL(context.Background(), cfg2)
	require.NoError(t, err)
	err = s2.LoadLatest()
	require.Error(t, err, "open should fail when storageDB is corrupted in both working and snapshot")
}

// =============================================================================
// Global Metadata Corruption (W-P3-2)
// =============================================================================

func TestGlobalMetadataCorruption(t *testing.T) {
	dir := t.TempDir()
	dbDir := filepath.Join(dir, flatkvRootDir)

	cfg := config.DefaultConfig()
	cfg.DataDir = dbDir
	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	err = s.LoadLatest()
	require.NoError(t, err)
	commitStorageEntry(t, s, ktype.Address{0x01}, ktype.Slot{0x01}, []byte{0xAA})
	require.NoError(t, s.WriteSnapshot(""))
	require.NoError(t, s.Close())

	workingMeta := filepath.Join(dbDir, "working", metadataDir)
	metaCfg := pebbledb.DefaultConfig()
	metaCfg.DataDir = workingMeta
	metaCfg.EnableMetrics = false
	db, err := pebbledb.Open(context.Background(), &metaCfg)
	require.NoError(t, err)
	require.NoError(t, db.Set(ktype.MetaVersionKey, []byte{0xFF, 0xFF, 0xFF}, types.WriteOptions{Sync: true}))
	require.NoError(t, db.Close())

	snapMeta := filepath.Join(dbDir, snapshotName(1), metadataDir)
	metaCfg2 := pebbledb.DefaultConfig()
	metaCfg2.DataDir = snapMeta
	metaCfg2.EnableMetrics = false
	db2, err := pebbledb.Open(context.Background(), &metaCfg2)
	require.NoError(t, err)
	require.NoError(t, db2.Set(ktype.MetaVersionKey, []byte{0xFF, 0xFF, 0xFF}, types.WriteOptions{Sync: true}))
	require.NoError(t, db2.Close())
	_ = os.Remove(filepath.Join(dbDir, "working", snapshotBaseFile))

	cfg2 := config.DefaultConfig()
	cfg2.DataDir = dbDir
	s2, err := newCommitStoreWithWAL(context.Background(), cfg2)
	require.NoError(t, err)
	err = s2.LoadLatest()
	require.Error(t, err, "open should fail when global metadata is corrupted")
}

// =============================================================================
// WAL Directory Deleted (W-P3-5)
// =============================================================================

func TestWALDirectoryDeleted(t *testing.T) {
	dir := t.TempDir()
	dbDir := filepath.Join(dir, flatkvRootDir)

	cfg := config.DefaultConfig()
	cfg.DataDir = dbDir
	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	err = s.LoadLatest()
	require.NoError(t, err)

	commitStorageEntry(t, s, ktype.Address{0x01}, ktype.Slot{0x01}, []byte{0xAA})
	commitStorageEntry(t, s, ktype.Address{0x02}, ktype.Slot{0x02}, []byte{0xBB})
	require.NoError(t, s.WriteSnapshot(""))
	require.NoError(t, s.Close())

	walDir := filepath.Join(dbDir, changelogDir)
	require.NoError(t, os.RemoveAll(walDir))

	cfg2 := config.DefaultConfig()
	cfg2.DataDir = dbDir
	s2, err := newCommitStoreWithWAL(context.Background(), cfg2)
	require.NoError(t, err)
	err = s2.LoadLatest()
	require.NoError(t, err)
	defer s2.Close()

	require.Equal(t, int64(2), s2.Version())

	commitStorageEntry(t, s2, ktype.Address{0x03}, ktype.Slot{0x03}, []byte{0xCC})
	require.Equal(t, int64(3), s2.Version())

	key := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(ktype.Address{0x03}, ktype.Slot{0x03}))
	val, found := s2.Get(keys.EVMStoreKey, key)
	require.True(t, found)
	require.Equal(t, padLeft32(0xCC), val)
}

func TestLocalMetaCorruption(t *testing.T) {
	dir := t.TempDir()
	dbDir := filepath.Join(dir, flatkvRootDir)

	cfg := config.DefaultConfig()
	cfg.DataDir = dbDir
	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	err = s.LoadLatest()
	require.NoError(t, err)
	commitStorageEntry(t, s, ktype.Address{0x01}, ktype.Slot{0x01}, []byte{0xAA})
	require.NoError(t, s.WriteSnapshot(""))
	require.NoError(t, s.Close())

	// Corrupt accountDB meta version in working dir: write 3 garbage bytes (expected 8).
	workingAccount := filepath.Join(dbDir, "working", accountDBDir)
	acctCfg := pebbledb.DefaultConfig()
	acctCfg.DataDir = workingAccount
	acctCfg.EnableMetrics = false
	db, err := pebbledb.Open(context.Background(), &acctCfg)
	require.NoError(t, err)
	require.NoError(t, db.Set(ktype.MetaVersionKey, []byte{0xDE, 0xAD, 0xFF}, types.WriteOptions{Sync: true}))
	require.NoError(t, db.Close())

	// Same corruption in the snapshot dir.
	snapAccount := filepath.Join(dbDir, snapshotName(1), accountDBDir)
	acctCfg2 := pebbledb.DefaultConfig()
	acctCfg2.DataDir = snapAccount
	acctCfg2.EnableMetrics = false
	db2, err := pebbledb.Open(context.Background(), &acctCfg2)
	require.NoError(t, err)
	require.NoError(t, db2.Set(ktype.MetaVersionKey, []byte{0xDE, 0xAD, 0xFF}, types.WriteOptions{Sync: true}))
	require.NoError(t, db2.Close())

	// Remove SNAPSHOT_BASE to force re-clone from corrupted snapshot.
	_ = os.Remove(filepath.Join(dbDir, "working", snapshotBaseFile))

	cfg2 := config.DefaultConfig()
	cfg2.DataDir = dbDir
	s2, err := newCommitStoreWithWAL(context.Background(), cfg2)
	require.NoError(t, err)
	err = s2.LoadLatest()
	require.Error(t, err, "open should fail when meta version is corrupted")
	require.Contains(t, err.Error(), "invalid meta version length")
}

// TestWALSegmentCorruption simulates WAL data loss caused by segment corruption.
// tidwall/wal auto-truncates corrupted segments on open, so the observable effect
// is entry loss. When catchup needs those entries to reach the requested version,
// LoadVersion fails with a version mismatch.
func TestWALSegmentCorruption(t *testing.T) {
	dir := t.TempDir()
	dbDir := filepath.Join(dir, flatkvRootDir)

	cfg := config.DefaultConfig()
	cfg.DataDir = dbDir
	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	err = s.LoadLatest()
	require.NoError(t, err)

	commitStorageEntry(t, s, ktype.Address{0x01}, ktype.Slot{0x01}, []byte{0xAA}) // v1
	commitStorageEntry(t, s, ktype.Address{0x02}, ktype.Slot{0x02}, []byte{0xBB}) // v2
	require.NoError(t, s.Close())

	// Simulate crash between the stores sealing v2 and the store-wide committed version advancing:
	// rewind global version to v1 so catchup needs to replay v2 from WAL.
	workingMeta := filepath.Join(dbDir, "working", metadataDir)
	metaCfg := pebbledb.DefaultConfig()
	metaCfg.DataDir = workingMeta
	metaCfg.EnableMetrics = false
	mdb, err := pebbledb.Open(context.Background(), &metaCfg)
	require.NoError(t, err)
	require.NoError(t, mdb.Set(ktype.MetaVersionKey, versionToBytes(1), types.WriteOptions{Sync: true}))
	require.NoError(t, mdb.Close())

	// Corrupt WAL segments: tidwall/wal will auto-truncate, losing all entries.
	walDir := filepath.Join(dbDir, changelogDir)
	entries, err := os.ReadDir(walDir)
	require.NoError(t, err)
	corrupted := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		p := filepath.Join(walDir, e.Name())
		garbage := make([]byte, 128)
		for i := range garbage {
			garbage[i] = 0xFF
		}
		require.NoError(t, os.WriteFile(p, garbage, 0600))
		corrupted++
	}
	require.Greater(t, corrupted, 0, "should have found at least one WAL segment to corrupt")

	// Request version 2: global says v1, WAL auto-truncated (empty), can't catchup to v2.
	cfg2 := config.DefaultConfig()
	cfg2.DataDir = dbDir
	s2, err := newCommitStoreWithWAL(context.Background(), cfg2)
	require.NoError(t, err)
	require.Error(t, s2.LoadLatest(),
		"opening must fail loudly rather than silently skipping the corrupted WAL segment")
}

// =============================================================================
// Account Row GC Persistence Tests
// =============================================================================

func TestAccountRowDeletePersistsAfterReopen(t *testing.T) {
	dir := t.TempDir()
	dbDir := filepath.Join(dir, flatkvRootDir)

	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = dbDir

	s, err := newCommitStoreWithWAL(context.Background(), cfg)
	require.NoError(t, err)
	err = s.LoadLatest()
	require.NoError(t, err)

	addr := ktype.Address{0xE1}
	nonceKey := keys.BuildEVMKey(keys.EVMKeyNonce, addr[:])

	cs1 := &proto.NamedChangeSet{
		Name: "evm",
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: keys.BuildEVMKey(keys.EVMKeyNonce, addr[:]), Value: []byte{0, 0, 0, 0, 0, 0, 0, 5}},
			{Key: keys.BuildEVMKey(keys.EVMKeyCodeHash, addr[:]), Value: make([]byte, vtype.CodeHashLength)},
		}},
	}
	ch := vtype.CodeHash{0xAA}
	cs1.Changeset.Pairs[1].Value = ch[:]
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs1}))
	_, err = s.Commit(s.Version() + 1)
	require.NoError(t, err)

	cs2 := &proto.NamedChangeSet{
		Name: "evm",
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: keys.BuildEVMKey(keys.EVMKeyNonce, addr[:]), Delete: true},
			{Key: keys.BuildEVMKey(keys.EVMKeyCodeHash, addr[:]), Delete: true},
		}},
	}
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs2}))
	_, err = s.Commit(s.Version() + 1)
	require.NoError(t, err)

	hashBefore := s.RootHash()
	require.NoError(t, s.Close())

	s2, err := newCommitStoreWithWAL(context.Background(), cfg)
	require.NoError(t, err)
	err = s2.LoadLatest()
	require.NoError(t, err)
	defer s2.Close()

	require.Equal(t, hashBefore, s2.RootHash(), "LtHash should match after reopen")

	nonceVal, found := s2.Get(keys.EVMStoreKey, nonceKey)
	require.False(t, found, "nonce should not be found after reopen (row deleted)")
	require.Nil(t, nonceVal)
}

func TestAccountRowDeleteSurvivesWALReplay(t *testing.T) {
	dir := t.TempDir()
	dbDir := filepath.Join(dir, flatkvRootDir)

	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = dbDir

	s, err := newCommitStoreWithWAL(context.Background(), cfg)
	require.NoError(t, err)
	err = s.LoadLatest()
	require.NoError(t, err)

	addr := ktype.Address{0xE2}

	cs1 := &proto.NamedChangeSet{
		Name: "evm",
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: keys.BuildEVMKey(keys.EVMKeyNonce, addr[:]), Value: []byte{0, 0, 0, 0, 0, 0, 0, 7}},
		}},
	}
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs1}))
	_, err = s.Commit(s.Version() + 1) // v1
	require.NoError(t, err)

	cs2 := &proto.NamedChangeSet{
		Name: "evm",
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: keys.BuildEVMKey(keys.EVMKeyNonce, addr[:]), Delete: true},
		}},
	}
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs2}))
	_, err = s.Commit(s.Version() + 1) // v2
	require.NoError(t, err)

	hashAtV2 := s.RootHash()
	require.NoError(t, s.Close())

	// Simulate crash: rewind global version to v1 so catchup must replay v2
	metaCfg := pebbledb.DefaultTestConfig(t)
	metaCfg.DataDir = filepath.Join(dbDir, "working", metadataDir)
	mdb, err := pebbledb.Open(context.Background(), &metaCfg)
	require.NoError(t, err)
	versionBuf := make([]byte, 8)
	versionBuf[7] = 1 // version = 1
	require.NoError(t, mdb.Set(ktype.MetaVersionKey, versionBuf, types.WriteOptions{Sync: true}))
	require.NoError(t, mdb.Close())

	s2, err := newCommitStoreWithWAL(context.Background(), cfg)
	require.NoError(t, err)
	err = s2.LoadLatest()
	require.NoError(t, err)
	defer s2.Close()

	require.Equal(t, int64(2), s2.Version())
	require.Equal(t, hashAtV2, s2.RootHash(), "LtHash should match after WAL replay")

	nonceKey := keys.BuildEVMKey(keys.EVMKeyNonce, addr[:])
	_, found := s2.Get(keys.EVMStoreKey, nonceKey)
	require.False(t, found, "nonce should not be found after WAL replay (row deleted)")
}

func TestAccountRowDeleteAfterSnapshotRollback(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	cfg.SnapshotInterval = 1
	cfg.SnapshotKeepRecent = 2

	s, err := newCommitStoreWithWAL(context.Background(), cfg)
	require.NoError(t, err)
	err = s.LoadLatest()
	require.NoError(t, err)

	addr := ktype.Address{0xE3}
	nonceKey := keys.BuildEVMKey(keys.EVMKeyNonce, addr[:])

	cs1 := &proto.NamedChangeSet{
		Name: "evm",
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: keys.BuildEVMKey(keys.EVMKeyNonce, addr[:]), Value: []byte{0, 0, 0, 0, 0, 0, 0, 3}},
		}},
	}
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs1}))
	_, err = s.Commit(s.Version() + 1) // v1 (snapshot taken)
	require.NoError(t, err)

	nonceVal, found := s.Get(keys.EVMStoreKey, nonceKey)
	require.True(t, found)
	require.Equal(t, []byte{0, 0, 0, 0, 0, 0, 0, 3}, nonceVal)

	cs2 := &proto.NamedChangeSet{
		Name: "evm",
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: keys.BuildEVMKey(keys.EVMKeyNonce, addr[:]), Delete: true},
		}},
	}
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs2}))
	_, err = s.Commit(s.Version() + 1) // v2 (row deleted, snapshot taken)
	require.NoError(t, err)

	_, found = s.Get(keys.EVMStoreKey, nonceKey)
	require.False(t, found, "nonce should be gone at v2")

	// Rollback to v1: row should be restored
	require.NoError(t, s.Rollback(1))
	require.Equal(t, int64(1), s.Version())

	nonceVal, found = s.Get(keys.EVMStoreKey, nonceKey)
	require.True(t, found, "nonce should be restored after rollback to v1")
	require.Equal(t, []byte{0, 0, 0, 0, 0, 0, 0, 3}, nonceVal)

	require.NoError(t, s.Close())
}

func TestRollbackOnReadOnlyStore(t *testing.T) {
	s := setupTestStore(t)

	cs := makeChangeSet(
		keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addrN(0x01), slotN(0x01))),
		padLeft32(0x11), false,
	)
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	ro, err := s.LoadVersionReadOnly(0)
	require.NoError(t, err)
	defer ro.Close()

	err = ro.Rollback(1)
	require.Error(t, err)
	require.ErrorIs(t, err, errReadOnly)
	require.NoError(t, s.Close())
}

func TestRollbackToCurrentVersion(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	cfg.SnapshotInterval = 1
	s := setupTestStoreWithConfig(t, cfg)
	defer s.Close()

	addr := addrN(0x02)
	key := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slotN(0x01)))
	cs := makeChangeSet(key, padLeft32(0x22), false)
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s) // v1 + snapshot

	hashV1 := s.RootHash()

	// Rollback to current version: should be a valid no-op.
	require.NoError(t, s.Rollback(1))
	require.Equal(t, int64(1), s.Version())
	require.Equal(t, hashV1, s.RootHash())

	val, found := s.Get(keys.EVMStoreKey, key)
	require.True(t, found)
	require.Equal(t, padLeft32(0x22), val)
}

func TestRollbackToFutureVersionFails(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	cfg.SnapshotInterval = 1
	s := setupTestStoreWithConfig(t, cfg)
	defer s.Close()

	cs := makeChangeSet(
		keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addrN(0x03), slotN(0x01))),
		padLeft32(0x33), false,
	)
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s) // v1

	err := s.Rollback(99)
	require.Error(t, err, "rollback to future version should fail")
}

func TestRollbackDiscardsUncommittedPendingWrites(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	cfg.SnapshotInterval = 1
	s := setupTestStoreWithConfig(t, cfg)
	defer s.Close()

	addr := addrN(0x04)
	key1 := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slotN(0x01)))
	cs1 := makeChangeSet(key1, padLeft32(0x44), false)
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs1}))
	commitAndCheck(t, s) // v1

	// Apply but do NOT commit.
	key2 := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slotN(0x02)))
	cs2 := makeChangeSet(key2, padLeft32(0x55), false)
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs2}))

	require.NoError(t, s.Rollback(1))
	require.Equal(t, int64(1), s.Version())

	val, found := s.Get(keys.EVMStoreKey, key1)
	require.True(t, found)
	require.Equal(t, padLeft32(0x44), val)

	_, found = s.Get(keys.EVMStoreKey, key2)
	require.False(t, found, "uncommitted pending write should be discarded after rollback")
}

func TestRollbackThenNewTimeline(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	cfg.SnapshotInterval = 1
	s := setupTestStoreWithConfig(t, cfg)
	defer s.Close()

	addr := addrN(0x05)
	key := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slotN(0x01)))

	cs1 := makeChangeSet(key, padLeft32(0x11), false)
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs1}))
	commitAndCheck(t, s) // v1

	cs2 := makeChangeSet(key, padLeft32(0x22), false)
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs2}))
	commitAndCheck(t, s) // v2

	require.NoError(t, s.Rollback(1))
	require.Equal(t, int64(1), s.Version())

	// Write new data in the alternate timeline.
	cs3 := makeChangeSet(key, padLeft32(0xFF), false)
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs3}))
	v, err := s.Commit(s.Version() + 1)
	require.NoError(t, err)
	require.Equal(t, int64(2), v) // Version 2 in the new timeline.

	val, found := s.Get(keys.EVMStoreKey, key)
	require.True(t, found)
	require.Equal(t, padLeft32(0xFF), val)
}

func TestRollbackPreservesWALContinuity(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	cfg.SnapshotInterval = 2

	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	err = s.LoadLatest()
	require.NoError(t, err)

	addr := addrN(0x06)
	for i := 1; i <= 4; i++ {
		key := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slotN(byte(i))))
		cs := makeChangeSet(key, padLeft32(byte(i)), false)
		require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs}))
		_, err := s.Commit(s.Version() + 1)
		require.NoError(t, err)
	}

	require.NoError(t, s.Rollback(2))

	// Continue committing.
	for i := 5; i <= 6; i++ {
		key := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slotN(byte(i))))
		cs := makeChangeSet(key, padLeft32(byte(i)), false)
		require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs}))
		_, err := s.Commit(s.Version() + 1)
		require.NoError(t, err)
	}
	hashAfterNewCommits := s.RootHash()
	require.NoError(t, s.Close())

	// Reopen and verify WAL continuity is intact.
	s2, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	err = s2.LoadLatest()
	require.NoError(t, err)
	defer s2.Close()

	require.Equal(t, int64(4), s2.Version())
	require.Equal(t, hashAfterNewCommits, s2.RootHash())
}

func TestWriteSnapshotOnReadOnlyStore(t *testing.T) {
	s := setupTestStore(t)

	cs := makeChangeSet(
		keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addrN(0x01), slotN(0x01))),
		padLeft32(0x11), false,
	)
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	ro, err := s.LoadVersionReadOnly(0)
	require.NoError(t, err)
	defer ro.Close()

	err = ro.WriteSnapshot("")
	require.Error(t, err)
	require.ErrorIs(t, err, errReadOnly)
	require.NoError(t, s.Close())
}

func TestWriteSnapshotAtVersion0(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	err := s.WriteSnapshot("")
	require.Error(t, err, "snapshot at version 0 should fail")
	require.Contains(t, err.Error(), "cannot snapshot uncommitted store")
}

func TestWriteSnapshotWhileReadOnlyCloneActive(t *testing.T) {
	s := setupTestStore(t)

	cs := makeChangeSet(
		keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addrN(0x07), slotN(0x01))),
		padLeft32(0x77), false,
	)
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	ro, err := s.LoadVersionReadOnly(0)
	require.NoError(t, err)
	defer ro.Close()

	// WriteSnapshot should succeed even with active RO clone.
	require.NoError(t, s.WriteSnapshot(""))

	// RO clone should still work.
	val, found := ro.Get(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addrN(0x07), slotN(0x01))))
	require.True(t, found)
	require.Equal(t, padLeft32(0x77), val)
	require.NoError(t, s.Close())
}

func TestWriteSnapshotDirParameterIgnored(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	cs := makeChangeSet(
		keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addrN(0x08), slotN(0x01))),
		padLeft32(0x88), false,
	)
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	// Pass a non-empty dir parameter. The implementation should ignore it.
	require.NoError(t, s.WriteSnapshot("/tmp/this-should-be-ignored"))

	// Verify snapshot was created in the correct location (not the passed dir).
	val, found := s.Get(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addrN(0x08), slotN(0x01))))
	require.True(t, found)
	require.Equal(t, padLeft32(0x88), val)
}
