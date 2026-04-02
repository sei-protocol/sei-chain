package flatkv

import (
	"path/filepath"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/evm"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/stretchr/testify/require"
)

// verifyLtHashConsistency checks that the in-memory workingLtHash matches a
// fresh full-scan of all data DBs. Used after any recovery path.
func verifyLtHashConsistency(t *testing.T, s *CommitStore) {
	t.Helper()
	expected := fullScanLtHash(t, s)
	require.Equal(t, expected.Checksum(), s.workingLtHash.Checksum(),
		"workingLtHash should match fullScanLtHash after recovery")
}

func TestCrashRecoverySkewedPerDBVersions(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	cfg.SnapshotInterval = 3

	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	addr := addrN(0x01)
	for i := 1; i <= 6; i++ {
		cs := &proto.NamedChangeSet{
			Name: "evm",
			Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
				noncePair(addr, uint64(i*10)),
			}},
		}
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
		_, err := s.Commit()
		require.NoError(t, err)
	}
	require.Equal(t, int64(6), s.Version())

	// Save the correct per-DB LtHash for accountDB before skewing version.
	savedAccountLtHash := s.perDBWorkingLtHash[accountDBDir].Clone()

	// Skew accountDB's local meta version to 4 while keeping the correct
	// LtHash. This simulates a crash where the version watermark wasn't
	// persisted but the actual data and hash are intact.
	batch := s.accountDB.NewBatch()
	require.NoError(t, writeLocalMetaToBatch(batch, 4, savedAccountLtHash))
	require.NoError(t, batch.Commit(types.WriteOptions{Sync: true}))
	_ = batch.Close()

	require.NoError(t, s.Close())

	// Reopen: loadGlobalMetadata detects version skew and catchup replays.
	s2, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s2.LoadVersion(0, false)
	require.NoError(t, err)
	defer s2.Close()

	require.Equal(t, int64(6), s2.Version())
	verifyLtHashConsistency(t, s2)

	// Data should be correct and store should accept new writes.
	cs := &proto.NamedChangeSet{
		Name: "evm",
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			noncePair(addr, 999),
		}},
	}
	require.NoError(t, s2.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	v, err := s2.Commit()
	require.NoError(t, err)
	require.Equal(t, int64(7), v)
}

func TestCrashRecoveryGlobalMetadataAheadOfDataDBs(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	cfg.SnapshotInterval = 3

	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	addr := addrN(0x02)
	for i := 1; i <= 5; i++ {
		key := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slotN(byte(i))))
		cs := makeChangeSet(key, []byte{byte(i * 11)}, false)
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
		_, err := s.Commit()
		require.NoError(t, err)
	}

	// Save the correct storageDB per-DB LtHash before skewing.
	savedStorageLtHash := s.perDBWorkingLtHash[storageDBDir].Clone()

	// Simulate crash: storageDB only flushed v3 (version watermark behind).
	batch := s.storageDB.NewBatch()
	require.NoError(t, writeLocalMetaToBatch(batch, 3, savedStorageLtHash))
	require.NoError(t, batch.Commit(types.WriteOptions{Sync: true}))
	_ = batch.Close()

	require.NoError(t, s.Close())

	s2, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s2.LoadVersion(0, false)
	require.NoError(t, err)
	defer s2.Close()

	require.Equal(t, int64(5), s2.Version())
	verifyLtHashConsistency(t, s2)

	for i := 1; i <= 5; i++ {
		key := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slotN(byte(i))))
		val, found := s2.Get(key)
		require.True(t, found, "slot %d should exist after recovery", i)
		require.Equal(t, []byte{byte(i * 11)}, val)
	}
}

func TestCrashRecoveryWALReplayLargeGap(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	cfg.SnapshotInterval = 5

	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	addr := addrN(0x03)
	for i := 1; i <= 20; i++ {
		key := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slotN(byte(i))))
		cs := makeChangeSet(key, []byte{byte(i)}, false)
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
		_, err := s.Commit()
		require.NoError(t, err)
	}
	expectedHash := s.RootHash()
	require.NoError(t, s.Close())

	// Reopen normally -- large WAL gap between snapshot and HEAD.
	s2, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s2.LoadVersion(0, false)
	require.NoError(t, err)
	defer s2.Close()

	require.Equal(t, int64(20), s2.Version())
	require.Equal(t, expectedHash, s2.RootHash())
	verifyLtHashConsistency(t, s2)

	// All 20 storage slots should be readable.
	for i := 1; i <= 20; i++ {
		key := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slotN(byte(i))))
		val, found := s2.Get(key)
		require.True(t, found, "slot %d should exist", i)
		require.Equal(t, []byte{byte(i)}, val)
	}
}

func TestCrashRecoveryEmptyWALAfterSnapshot(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)

	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	addr := addrN(0x04)
	key := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slotN(0x01)))
	cs := makeChangeSet(key, []byte{0xAA}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	_, err = s.Commit()
	require.NoError(t, err)

	require.NoError(t, s.WriteSnapshot(""))
	expectedHash := s.RootHash()
	expectedVersion := s.Version()

	// Clear the WAL entirely (simulate WAL lost after snapshot).
	require.NoError(t, s.clearChangelog())
	require.NoError(t, s.Close())

	// Reopen: should work from snapshot alone.
	s2, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s2.LoadVersion(0, false)
	require.NoError(t, err)
	defer s2.Close()

	require.Equal(t, expectedVersion, s2.Version())
	require.Equal(t, expectedHash, s2.RootHash())

	val, found := s2.Get(key)
	require.True(t, found)
	require.Equal(t, []byte{0xAA}, val)

	// Can continue committing after recovery from snapshot-only state.
	cs2 := makeChangeSet(key, []byte{0xBB}, false)
	require.NoError(t, s2.ApplyChangeSets([]*proto.NamedChangeSet{cs2}))
	v, err := s2.Commit()
	require.NoError(t, err)
	require.Equal(t, expectedVersion+1, v)
}

func TestCrashRecoveryCorruptedAccountValueInDB(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0x05)
	cs := &proto.NamedChangeSet{
		Name: "evm",
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			noncePair(addr, 42),
		}},
	}
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	_, err := s.Commit()
	require.NoError(t, err)

	// Corrupt the account value in the DB with invalid-length data.
	batch := s.accountDB.NewBatch()
	require.NoError(t, batch.Set(AccountKey(addr), []byte{0xDE, 0xAD}))
	require.NoError(t, batch.Commit(types.WriteOptions{Sync: true}))
	_ = batch.Close()

	// Next ApplyChangeSets touching this account should detect the corruption
	// during batchReadOldValues.
	cs2 := &proto.NamedChangeSet{
		Name: "evm",
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			noncePair(addr, 99),
		}},
	}
	err = s.ApplyChangeSets([]*proto.NamedChangeSet{cs2})
	require.Error(t, err, "should fail on corrupted AccountValue")
	require.Contains(t, err.Error(), "corrupted AccountValue")
}

func TestCrashRecoveryCrashAfterWALBeforeDBCommit(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	cfg.SnapshotInterval = 1

	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	addr := addrN(0x06)
	slot := slotN(0x01)
	key := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slot))
	cs := makeChangeSet(key, []byte{0x11}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	_, err = s.Commit()
	require.NoError(t, err)
	hashAfterV1 := s.RootHash()

	// Now simulate writing v2 to WAL but "crashing" before DB commit.
	cs2 := makeChangeSet(key, []byte{0x22}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs2}))

	// Write v2 to WAL manually (like Commit step 1).
	changelogEntry := proto.ChangelogEntry{
		Version:    2,
		Changesets: s.pendingChangeSets,
	}
	require.NoError(t, s.changelog.Write(changelogEntry))

	// Do NOT call commitBatches or update global metadata.
	// Reset in-memory state to v1 to simulate crash.
	s.clearPendingWrites()
	s.committedVersion = 1
	require.NoError(t, s.Close())

	// Reopen: catchup should replay v2 from WAL.
	s2, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s2.LoadVersion(0, false)
	require.NoError(t, err)
	defer s2.Close()

	require.Equal(t, int64(2), s2.Version())
	require.NotEqual(t, hashAfterV1, s2.RootHash(), "hash should differ after v2 replay")

	val, found := s2.Get(key)
	require.True(t, found)
	require.Equal(t, []byte{0x22}, val, "v2 value should be present after catchup")
	verifyLtHashConsistency(t, s2)
}

func TestCrashRecoveryLtHashConsistencyAfterAllPaths(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	cfg.SnapshotInterval = 3

	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	addr := addrN(0x07)
	for i := 1; i <= 10; i++ {
		pairs := []*proto.KVPair{
			noncePair(addr, uint64(i)),
			{
				Key:   evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slotN(byte(i)))),
				Value: []byte{byte(i)},
			},
		}
		cs := &proto.NamedChangeSet{
			Name:      "evm",
			Changeset: proto.ChangeSet{Pairs: pairs},
		}
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
		_, err := s.Commit()
		require.NoError(t, err)
	}
	verifyLtHashConsistency(t, s)
	require.NoError(t, s.Close())

	// Path 1: Normal reopen
	s2, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s2.LoadVersion(0, false)
	require.NoError(t, err)
	verifyLtHashConsistency(t, s2)

	// Path 2: Rollback to v6
	require.NoError(t, s2.Rollback(6))
	require.Equal(t, int64(6), s2.Version())
	verifyLtHashConsistency(t, s2)

	// Path 3: Continue writing after rollback
	cs := &proto.NamedChangeSet{
		Name: "evm",
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			noncePair(addr, 999),
		}},
	}
	require.NoError(t, s2.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	_, err = s2.Commit()
	require.NoError(t, err)
	verifyLtHashConsistency(t, s2)
	require.NoError(t, s2.Close())

	// Path 4: Reopen after rollback + new commit
	s3, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s3.LoadVersion(0, false)
	require.NoError(t, err)
	defer s3.Close()
	verifyLtHashConsistency(t, s3)
}

func TestCrashRecoveryCorruptLtHashBlobInMetadata(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)

	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	cs := makeChangeSet(
		evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addrN(0x01), slotN(0x01))),
		[]byte{0x11}, false,
	)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	_, err = s.Commit()
	require.NoError(t, err)

	// Write garbage to the global _meta/hash key in metadataDB.
	batch := s.metadataDB.NewBatch()
	require.NoError(t, batch.Set(metaLtHashKey, []byte{0xDE, 0xAD, 0xBE, 0xEF}))
	require.NoError(t, batch.Commit(types.WriteOptions{Sync: true}))
	_ = batch.Close()

	require.NoError(t, s.Close())

	// Reopen should fail with an LtHash unmarshal error.
	s2, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s2.LoadVersion(0, false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid LtHash size")
}

func TestCrashRecoveryCorruptLtHashBlobInPerDBMeta(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)

	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	cs := makeChangeSet(
		evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addrN(0x02), slotN(0x01))),
		[]byte{0x22}, false,
	)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	_, err = s.Commit()
	require.NoError(t, err)

	// Write garbage to accountDB's _meta/hash key.
	batch := s.accountDB.NewBatch()
	require.NoError(t, batch.Set(metaLtHashKey, []byte{0x01, 0x02, 0x03}))
	require.NoError(t, batch.Commit(types.WriteOptions{Sync: true}))
	_ = batch.Close()

	require.NoError(t, s.Close())

	// Reopen should fail with an LtHash unmarshal error from per-DB meta.
	s2, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s2.LoadVersion(0, false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid LtHash size")
}

func TestCrashRecoveryGlobalVersionOverflow(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)

	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	cs := makeChangeSet(
		evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addrN(0x03), slotN(0x01))),
		[]byte{0x33}, false,
	)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	_, err = s.Commit()
	require.NoError(t, err)

	// Write a version value that exceeds math.MaxInt64 to the global metadata.
	overflowBytes := make([]byte, 8)
	overflowBytes[0] = 0xFF // 0xFF00000000000000 > MaxInt64
	batch := s.metadataDB.NewBatch()
	require.NoError(t, batch.Set(metaVersionKey, overflowBytes))
	require.NoError(t, batch.Commit(types.WriteOptions{Sync: true}))
	_ = batch.Close()

	require.NoError(t, s.Close())

	// Reopen should fail with an overflow error.
	s2, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s2.LoadVersion(0, false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "global version overflow")
}
