package flatkv

import (
	"path/filepath"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/evm"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/stretchr/testify/require"
)

func TestLoadVersionReload(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)

	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	addr := addrN(0x01)
	key := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slotN(0x01)))
	cs := makeChangeSet(key, []byte{0x11}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	expectedHash := s.RootHash()

	// Re-call LoadVersion(0, false) on the same store: should close and reopen.
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	require.Equal(t, int64(1), s.Version())
	require.Equal(t, expectedHash, s.RootHash())

	val, found := s.Get(key)
	require.True(t, found)
	require.Equal(t, []byte{0x11}, val)
	require.NoError(t, s.Close())
}

func TestLoadVersionReadOnlyVersion0(t *testing.T) {
	s := setupTestStore(t)

	addr := addrN(0x02)
	key := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slotN(0x01)))
	cs := makeChangeSet(key, []byte{0x22}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	// Version 0 in read-only means "latest committed".
	ro, err := s.LoadVersion(0, true)
	require.NoError(t, err)
	defer ro.Close()

	require.Equal(t, int64(1), ro.Version())
	val, found := ro.Get(key)
	require.True(t, found)
	require.Equal(t, []byte{0x22}, val)
	require.NoError(t, s.Close())
}

func TestLoadVersionReadOnlyDoesNotSeePending(t *testing.T) {
	s := setupTestStore(t)

	addr := addrN(0x03)
	key := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slotN(0x01)))
	cs := makeChangeSet(key, []byte{0x33}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	// Apply a new changeset without committing.
	key2 := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slotN(0x02)))
	cs2 := makeChangeSet(key2, []byte{0x44}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs2}))

	// RO should not see the uncommitted write.
	ro, err := s.LoadVersion(0, true)
	require.NoError(t, err)
	defer ro.Close()

	_, found := ro.Get(key2)
	require.False(t, found, "read-only store should not see uncommitted data")

	// But committed data should be visible.
	val, found := ro.Get(key)
	require.True(t, found)
	require.Equal(t, []byte{0x33}, val)
	require.NoError(t, s.Close())
}

func TestLoadVersionEmptyWAL(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)

	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	// Fresh store with no commits: WAL is empty.
	require.Equal(t, int64(0), s.Version())
	require.NotNil(t, s.RootHash())
	require.Len(t, s.RootHash(), 32)
	require.NoError(t, s.Close())
}

func TestCloseWithPendingUncommittedWrites(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)

	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	addr := addrN(0x10)
	key := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slotN(0x01)))
	cs := makeChangeSet(key, []byte{0x11}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	// Apply but do NOT commit.
	key2 := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slotN(0x02)))
	cs2 := makeChangeSet(key2, []byte{0x22}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs2}))

	// Close should succeed even with pending writes.
	require.NoError(t, s.Close())

	// Reopen: uncommitted data should be lost.
	s2, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s2.LoadVersion(0, false)
	require.NoError(t, err)
	defer s2.Close()

	require.Equal(t, int64(1), s2.Version())

	val, found := s2.Get(key)
	require.True(t, found, "committed data should persist")
	require.Equal(t, []byte{0x11}, val)

	_, found = s2.Get(key2)
	require.False(t, found, "uncommitted data should be lost")
}

func TestCloseDuringConcurrentReadOnlyClone(t *testing.T) {
	s := setupTestStore(t)

	addr := addrN(0x11)
	key := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slotN(0x01)))
	cs := makeChangeSet(key, []byte{0xAA}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	ro, err := s.LoadVersion(0, true)
	require.NoError(t, err)

	// Close parent while RO is still open.
	require.NoError(t, s.Close())

	// RO should still function.
	val, found := ro.Get(key)
	require.True(t, found, "RO clone should remain functional after parent close")
	require.Equal(t, []byte{0xAA}, val)

	require.NoError(t, ro.Close())
}

func TestCloseErrorAggregation(t *testing.T) {
	s := setupTestStore(t)

	// Normal close should aggregate no errors.
	require.NoError(t, s.Close())

	// Second close should also be fine (idempotent).
	require.NoError(t, s.Close())
}

func TestRootHashAndVersionAfterClose(t *testing.T) {
	s := setupTestStore(t)

	addr := addrN(0x12)
	key := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slotN(0x01)))
	cs := makeChangeSet(key, []byte{0xBB}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	require.NoError(t, s.Close())

	// Version and RootHash access in-memory fields, should not panic.
	require.Equal(t, int64(1), s.Version())
	require.NotNil(t, s.RootHash())
	require.Len(t, s.RootHash(), 32)
}

func TestCatchupWithEmptyWAL(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	// Store has no commits: catchup with empty WAL should be a no-op.
	require.NoError(t, s.catchup(0))
	require.Equal(t, int64(0), s.Version())
}

func TestCatchupSkipsAlreadyCommittedEntries(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	cfg.SnapshotInterval = 2

	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	addr := addrN(0x20)
	for i := 1; i <= 5; i++ {
		key := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slotN(byte(i))))
		cs := makeChangeSet(key, []byte{byte(i)}, false)
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
		_, err := s.Commit()
		require.NoError(t, err)
	}
	hashV5 := s.RootHash()
	require.NoError(t, s.Close())

	// Reopen: catchup should replay only entries after the committed version
	// and skip already-committed entries.
	s2, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s2.LoadVersion(0, false)
	require.NoError(t, err)
	defer s2.Close()

	require.Equal(t, int64(5), s2.Version())
	require.Equal(t, hashV5, s2.RootHash())
}

func TestCatchupTargetVersionMiddleOfWAL(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	cfg.SnapshotInterval = 2

	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	addr := addrN(0x21)
	var hashes [6][]byte
	for i := 1; i <= 5; i++ {
		key := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slotN(byte(i))))
		cs := makeChangeSet(key, []byte{byte(i)}, false)
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
		_, err := s.Commit()
		require.NoError(t, err)
		hashes[i] = s.RootHash()
	}
	require.NoError(t, s.Close())

	// Open at v3 (middle of WAL).
	s2, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s2.LoadVersion(3, false)
	require.NoError(t, err)
	defer s2.Close()

	require.Equal(t, int64(3), s2.Version())
	require.Equal(t, hashes[3], s2.RootHash())
}

func TestWalOffsetForVersionNilChangelog(t *testing.T) {
	s := setupTestStore(t)
	savedChangelog := s.changelog
	s.changelog = nil

	_, err := s.walOffsetForVersion(1)
	require.Error(t, err)
	require.Contains(t, err.Error(), "changelog not open")

	s.changelog = savedChangelog
	require.NoError(t, s.Close())
}
