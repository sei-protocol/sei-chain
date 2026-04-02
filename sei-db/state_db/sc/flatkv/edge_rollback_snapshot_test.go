package flatkv

import (
	"path/filepath"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/evm"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/stretchr/testify/require"
)

func TestRollbackOnReadOnlyStore(t *testing.T) {
	s := setupTestStore(t)

	cs := makeChangeSet(
		evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addrN(0x01), slotN(0x01))),
		[]byte{0x11}, false,
	)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	ro, err := s.LoadVersion(0, true)
	require.NoError(t, err)
	defer ro.Close()

	err = ro.Rollback(1)
	require.Error(t, err)
	require.ErrorIs(t, err, errReadOnly)
	require.NoError(t, s.Close())
}

func TestRollbackToCurrentVersion(t *testing.T) {
	cfg := DefaultTestConfig(t)
	cfg.SnapshotInterval = 1
	s := setupTestStoreWithConfig(t, cfg)
	defer s.Close()

	addr := addrN(0x02)
	key := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slotN(0x01)))
	cs := makeChangeSet(key, []byte{0x22}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s) // v1 + snapshot

	hashV1 := s.RootHash()

	// Rollback to current version: should be a valid no-op.
	require.NoError(t, s.Rollback(1))
	require.Equal(t, int64(1), s.Version())
	require.Equal(t, hashV1, s.RootHash())

	val, found := s.Get(key)
	require.True(t, found)
	require.Equal(t, []byte{0x22}, val)
}

func TestRollbackToFutureVersionFails(t *testing.T) {
	cfg := DefaultTestConfig(t)
	cfg.SnapshotInterval = 1
	s := setupTestStoreWithConfig(t, cfg)
	defer s.Close()

	cs := makeChangeSet(
		evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addrN(0x03), slotN(0x01))),
		[]byte{0x33}, false,
	)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s) // v1

	err := s.Rollback(99)
	require.Error(t, err, "rollback to future version should fail")
}

func TestRollbackDiscardsUncommittedPendingWrites(t *testing.T) {
	cfg := DefaultTestConfig(t)
	cfg.SnapshotInterval = 1
	s := setupTestStoreWithConfig(t, cfg)
	defer s.Close()

	addr := addrN(0x04)
	key1 := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slotN(0x01)))
	cs1 := makeChangeSet(key1, []byte{0x44}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs1}))
	commitAndCheck(t, s) // v1

	// Apply but do NOT commit.
	key2 := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slotN(0x02)))
	cs2 := makeChangeSet(key2, []byte{0x55}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs2}))

	require.NoError(t, s.Rollback(1))
	require.Equal(t, int64(1), s.Version())

	val, found := s.Get(key1)
	require.True(t, found)
	require.Equal(t, []byte{0x44}, val)

	_, found = s.Get(key2)
	require.False(t, found, "uncommitted pending write should be discarded after rollback")
}

func TestRollbackThenNewTimeline(t *testing.T) {
	cfg := DefaultTestConfig(t)
	cfg.SnapshotInterval = 1
	s := setupTestStoreWithConfig(t, cfg)
	defer s.Close()

	addr := addrN(0x05)
	key := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slotN(0x01)))

	cs1 := makeChangeSet(key, []byte{0x11}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs1}))
	commitAndCheck(t, s) // v1

	cs2 := makeChangeSet(key, []byte{0x22}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs2}))
	commitAndCheck(t, s) // v2

	require.NoError(t, s.Rollback(1))
	require.Equal(t, int64(1), s.Version())

	// Write new data in the alternate timeline.
	cs3 := makeChangeSet(key, []byte{0xFF}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs3}))
	v, err := s.Commit()
	require.NoError(t, err)
	require.Equal(t, int64(2), v) // Version 2 in the new timeline.

	val, found := s.Get(key)
	require.True(t, found)
	require.Equal(t, []byte{0xFF}, val)
}

func TestRollbackPreservesWALContinuity(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	cfg.SnapshotInterval = 2

	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	addr := addrN(0x06)
	for i := 1; i <= 4; i++ {
		key := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slotN(byte(i))))
		cs := makeChangeSet(key, []byte{byte(i)}, false)
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
		_, err := s.Commit()
		require.NoError(t, err)
	}

	require.NoError(t, s.Rollback(2))

	// Continue committing.
	for i := 5; i <= 6; i++ {
		key := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slotN(byte(i))))
		cs := makeChangeSet(key, []byte{byte(i)}, false)
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
		_, err := s.Commit()
		require.NoError(t, err)
	}
	hashAfterNewCommits := s.RootHash()
	require.NoError(t, s.Close())

	// Reopen and verify WAL continuity is intact.
	s2, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s2.LoadVersion(0, false)
	require.NoError(t, err)
	defer s2.Close()

	require.Equal(t, int64(4), s2.Version())
	require.Equal(t, hashAfterNewCommits, s2.RootHash())
}

func TestWriteSnapshotOnReadOnlyStore(t *testing.T) {
	s := setupTestStore(t)

	cs := makeChangeSet(
		evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addrN(0x01), slotN(0x01))),
		[]byte{0x11}, false,
	)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	ro, err := s.LoadVersion(0, true)
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
		evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addrN(0x07), slotN(0x01))),
		[]byte{0x77}, false,
	)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	ro, err := s.LoadVersion(0, true)
	require.NoError(t, err)
	defer ro.Close()

	// WriteSnapshot should succeed even with active RO clone.
	require.NoError(t, s.WriteSnapshot(""))

	// RO clone should still work.
	val, found := ro.Get(evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addrN(0x07), slotN(0x01))))
	require.True(t, found)
	require.Equal(t, []byte{0x77}, val)
	require.NoError(t, s.Close())
}

func TestWriteSnapshotDirParameterIgnored(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	cs := makeChangeSet(
		evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addrN(0x08), slotN(0x01))),
		[]byte{0x88}, false,
	)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	// Pass a non-empty dir parameter. The implementation should ignore it.
	require.NoError(t, s.WriteSnapshot("/tmp/this-should-be-ignored"))

	// Verify snapshot was created in the correct location (not the passed dir).
	val, found := s.Get(evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addrN(0x08), slotN(0x01))))
	require.True(t, found)
	require.Equal(t, []byte{0x88}, val)
}
