package flatkv

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/evm"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	iavl "github.com/sei-protocol/sei-chain/sei-iavl/proto"
)

// =============================================================================
// Interface Compliance Tests
// =============================================================================

// TestCommitStoreImplementsStore verifies that CommitStore implements flatkv.Store
func TestCommitStoreImplementsStore(t *testing.T) {
	// Compile-time check is in store.go: var _ Store = (*CommitStore)(nil)
	// This test verifies runtime behavior of interface methods

	s := setupTestStore(t)
	defer s.Close()

	// Verify Store interface methods
	require.Equal(t, int64(0), s.Version())
	require.NotNil(t, s.RootHash())
	require.Len(t, s.RootHash(), 32)
}

// =============================================================================
// Test Helpers
// =============================================================================

// memiavlStorageKey builds a memiavl-format storage key for testing external API.
func memiavlStorageKey(addr Address, slot Slot) []byte {
	internal := StorageKey(addr, slot)
	return evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, internal)
}

// makeChangeSet creates a changeset
func makeChangeSet(key, value []byte, delete bool) *proto.NamedChangeSet {
	return &proto.NamedChangeSet{
		Name: "test",
		Changeset: iavl.ChangeSet{
			Pairs: []*iavl.KVPair{
				{Key: key, Value: value, Delete: delete},
			},
		},
	}
}

// setupTestDB creates a temporary PebbleDB for testing
func setupTestDB(t *testing.T) types.KeyValueDB {
	t.Helper()
	dir := t.TempDir()
	db, err := pebbledb.Open(dir, types.OpenOptions{})
	require.NoError(t, err)
	return db
}

// setupTestStore creates a minimal test store
func setupTestStore(t *testing.T) *CommitStore {
	t.Helper()
	dir := t.TempDir()
	s := NewCommitStore(dir, nil, DefaultConfig())
	_, err := s.LoadVersion(0)
	require.NoError(t, err)
	return s
}

// setupTestStoreWithConfig creates a test store with custom config
func setupTestStoreWithConfig(t *testing.T, cfg Config) *CommitStore {
	t.Helper()
	dir := t.TempDir()
	s := NewCommitStore(dir, nil, cfg)
	_, err := s.LoadVersion(0)
	require.NoError(t, err)
	return s
}

// commitAndCheck commits and asserts no error, returns the version
func commitAndCheck(t *testing.T, s *CommitStore) int64 {
	t.Helper()
	v, err := s.Commit()
	require.NoError(t, err)
	return v
}

// =============================================================================
// Basic Store Operations
// =============================================================================

func TestStoreOpenClose(t *testing.T) {
	dir := t.TempDir()
	s := NewCommitStore(dir, nil, DefaultConfig())
	_, err := s.LoadVersion(0)
	require.NoError(t, err)

	require.NoError(t, s.Close())
}

func TestStoreClose(t *testing.T) {
	dir := t.TempDir()
	s := NewCommitStore(dir, nil, DefaultConfig())
	_, err := s.LoadVersion(0)
	require.NoError(t, err)

	// Close should succeed
	require.NoError(t, s.Close())

	// Double close should not panic (idempotent)
	require.NoError(t, s.Close())
}

// =============================================================================
// Apply and Commit
// =============================================================================

func TestStoreCommitVersionAutoIncrement(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := Address{0xAA}
	slot := Slot{0xBB}
	key := memiavlStorageKey(addr, slot)

	cs := makeChangeSet(key, []byte{0xCC}, false)

	// Initial version is 0
	require.Equal(t, int64(0), s.Version())

	// First commit should return version 1
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	v1, err := s.Commit()
	require.NoError(t, err)
	require.Equal(t, int64(1), v1)
	require.Equal(t, int64(1), s.Version())

	// Second commit should return version 2
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	v2, err := s.Commit()
	require.NoError(t, err)
	require.Equal(t, int64(2), v2)
	require.Equal(t, int64(2), s.Version())

	// Third commit should return version 3
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	v3, err := s.Commit()
	require.NoError(t, err)
	require.Equal(t, int64(3), v3)
	require.Equal(t, int64(3), s.Version())
}

func TestStoreApplyAndCommit(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := Address{0x11}
	slot := Slot{0x22}
	value := []byte{0x33}
	key := memiavlStorageKey(addr, slot)

	cs := makeChangeSet(key, value, false)

	// Apply but not commit - should be readable from pending writes
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	got, found := s.Get(key)
	require.True(t, found, "should be readable from pending writes")
	require.Equal(t, value, got)

	// Commit
	commitAndCheck(t, s)

	// Still should be readable after commit
	got, found = s.Get(key)
	require.True(t, found)
	require.Equal(t, value, got)
}

func TestStoreMultipleWrites(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := Address{0x44}
	entries := []struct {
		slot  Slot
		value byte
	}{
		{Slot{0x01}, 0xAA},
		{Slot{0x02}, 0xBB},
		{Slot{0x03}, 0xCC},
	}

	// Create multiple pairs in one changeset
	pairs := make([]*iavl.KVPair, len(entries))
	for i, e := range entries {
		key := memiavlStorageKey(addr, e.slot)
		pairs[i] = &iavl.KVPair{Key: key, Value: []byte{e.value}}
	}

	cs := &proto.NamedChangeSet{
		Name: "test",
		Changeset: iavl.ChangeSet{
			Pairs: pairs,
		},
	}

	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	// Verify all entries
	for _, e := range entries {
		key := memiavlStorageKey(addr, e.slot)
		got, found := s.Get(key)
		require.True(t, found)
		require.Equal(t, []byte{e.value}, got)
	}
}

func TestStoreEmptyChangesets(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	// Empty changeset should not cause issues
	emptyCS := &proto.NamedChangeSet{
		Name:      "empty",
		Changeset: iavl.ChangeSet{Pairs: nil},
	}

	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{emptyCS}))
	commitAndCheck(t, s)

	require.Equal(t, int64(1), s.Version())
}

func TestStoreClearsPendingAfterCommit(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := Address{0xAA}
	slot := Slot{0xBB}
	key := memiavlStorageKey(addr, slot)

	cs := makeChangeSet(key, []byte{0xCC}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))

	// Should have pending writes
	require.Len(t, s.storageWrites, 1)
	require.Len(t, s.pendingChangeSets, 1)

	commitAndCheck(t, s)

	// Should be cleared after commit
	require.Len(t, s.storageWrites, 0)
	require.Len(t, s.pendingChangeSets, 0)
}

// =============================================================================
// Versioning and Persistence
// =============================================================================

func TestStoreVersioning(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := Address{0x88}
	slot := Slot{0x99}
	key := memiavlStorageKey(addr, slot)

	// Version 1
	cs1 := makeChangeSet(key, []byte{0x01}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs1}))
	commitAndCheck(t, s)

	require.Equal(t, int64(1), s.Version())

	// Version 2 with updated value
	cs2 := makeChangeSet(key, []byte{0x02}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs2}))
	commitAndCheck(t, s)

	require.Equal(t, int64(2), s.Version())

	// Latest value should be from version 2
	got, found := s.Get(key)
	require.True(t, found)
	require.Equal(t, []byte{0x02}, got)
}

func TestStorePersistence(t *testing.T) {
	dir := t.TempDir()

	addr := Address{0xDD}
	slot := Slot{0xEE}
	value := []byte{0xFF}
	key := memiavlStorageKey(addr, slot)

	// Write and close
	s1 := NewCommitStore(dir, nil, DefaultConfig())
	_, err := s1.LoadVersion(0)
	require.NoError(t, err)

	cs := makeChangeSet(key, value, false)
	require.NoError(t, s1.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s1)
	require.NoError(t, s1.Close())

	// Reopen and verify
	s2 := NewCommitStore(dir, nil, DefaultConfig())
	_, err = s2.LoadVersion(0)
	require.NoError(t, err)
	defer s2.Close()

	got, found := s2.Get(key)
	require.True(t, found)
	require.Equal(t, value, got)

	require.Equal(t, int64(1), s2.Version())
}

// =============================================================================
// RootHash (LtHash)
// =============================================================================

func TestStoreRootHashChanges(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	// Initial hash
	hash1 := s.RootHash()
	require.NotNil(t, hash1)
	require.Equal(t, 32, len(hash1)) // Blake3-256

	// Apply changeset
	addr := Address{0xAB}
	slot := Slot{0xCD}
	key := memiavlStorageKey(addr, slot)

	cs := makeChangeSet(key, []byte{0xEF}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))

	// Working hash should change
	hash2 := s.RootHash()
	require.NotEqual(t, hash1, hash2)

	commitAndCheck(t, s)

	// Committed hash should match working hash
	hash3 := s.RootHash()
	require.Equal(t, hash2, hash3)
}

func TestStoreRootHashChangesOnApply(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	// Initial hash
	hash1 := s.RootHash()
	require.NotNil(t, hash1)
	require.Equal(t, 32, len(hash1)) // Blake3-256

	// Apply changeset
	addr := Address{0xEE}
	slot := Slot{0xFF}
	key := memiavlStorageKey(addr, slot)

	cs := makeChangeSet(key, []byte{0x11}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))

	// Working hash should change
	hash2 := s.RootHash()
	require.NotEqual(t, hash1, hash2, "hash should change after ApplyChangeSets")
}

func TestStoreRootHashStableAfterCommit(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := Address{0x12}
	slot := Slot{0x34}
	key := memiavlStorageKey(addr, slot)

	cs := makeChangeSet(key, []byte{0x56}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))

	// Get working hash
	workingHash := s.RootHash()

	commitAndCheck(t, s)

	// Committed hash should match working hash
	committedHash := s.RootHash()
	require.Equal(t, workingHash, committedHash)
}

// =============================================================================
// Lifecycle (WriteSnapshot, Rollback)
// =============================================================================

func TestStoreWriteSnapshotNotImplemented(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	err := s.WriteSnapshot(t.TempDir())
	require.Error(t, err)
	require.Contains(t, err.Error(), "not implemented")
}

func TestStoreRollbackNoOp(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	// Rollback is currently a no-op - doesn't error
	err := s.Rollback(1)
	require.NoError(t, err)
}
