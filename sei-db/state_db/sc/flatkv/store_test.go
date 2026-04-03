package flatkv

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	commonerrors "github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/common/evm"
	"github.com/sei-protocol/sei-chain/sei-db/common/threading"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
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
		Name: "evm",
		Changeset: proto.ChangeSet{
			Pairs: []*proto.KVPair{
				{Key: key, Value: value, Delete: delete},
			},
		},
	}
}

// setupTestDB creates a temporary PebbleDB for testing
func setupTestDB(t *testing.T) types.KeyValueDB {
	t.Helper()
	cfg := pebbledb.DefaultTestConfig(t)
	cacheCfg := pebbledb.DefaultTestCacheConfig()
	db, err := pebbledb.OpenWithCache(t.Context(), &cfg, &cacheCfg,
		threading.NewAdHocPool(), threading.NewAdHocPool())
	require.NoError(t, err)
	return db
}

// setupTestStore creates a minimal test store
func setupTestStore(t *testing.T) *CommitStore {
	t.Helper()
	s, err := NewCommitStore(t.Context(), DefaultTestConfig(t))
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)
	return s
}

// setupTestStoreWithConfig creates a test store with custom config
func setupTestStoreWithConfig(t *testing.T, cfg *Config) *CommitStore {
	t.Helper()
	dir := t.TempDir()
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
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
	cfg := DefaultTestConfig(t)
	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	require.NoError(t, s.Close())
}

func TestStoreClose(t *testing.T) {
	cfg := DefaultTestConfig(t)
	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
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
	pairs := make([]*proto.KVPair, len(entries))
	for i, e := range entries {
		key := memiavlStorageKey(addr, e.slot)
		pairs[i] = &proto.KVPair{Key: key, Value: []byte{e.value}}
	}

	cs := &proto.NamedChangeSet{
		Name: "evm",
		Changeset: proto.ChangeSet{
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
		Name:      "evm",
		Changeset: proto.ChangeSet{Pairs: nil},
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
	cfg := DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	s1, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s1.LoadVersion(0, false)
	require.NoError(t, err)

	cs := makeChangeSet(key, value, false)
	require.NoError(t, s1.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s1)
	require.NoError(t, s1.Close())

	// Reopen and verify
	cfg = DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	s2, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s2.LoadVersion(0, false)
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

func TestStoreWriteSnapshotRequiresCommit(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	// Cannot snapshot at version 0 (nothing committed)
	err := s.WriteSnapshot("")
	require.Error(t, err)
	require.Contains(t, err.Error(), "uncommitted")
}

func TestStoreRollbackNoSnapshot(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	// Rollback with no snapshots should fail (no snapshot found)
	err := s.Rollback(1)
	require.Error(t, err)
}

// =============================================================================
// File lock
// =============================================================================

func TestFileLockPreventsDoubleOpen(t *testing.T) {
	dir := t.TempDir()

	cfg := DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	s1, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s1.LoadVersion(0, false)
	require.NoError(t, err)

	cfg = DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	s2, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s2.LoadVersion(0, false)
	require.Error(t, err, "second open on same dir should fail due to file lock")
	require.Contains(t, err.Error(), "file lock")

	require.NoError(t, s1.Close())

	_, err = s2.LoadVersion(0, false)
	require.NoError(t, err, "should succeed after first store releases lock")
	require.NoError(t, s2.Close())
}

// =============================================================================
// clearChangelog
// =============================================================================

func TestClearChangelog(t *testing.T) {
	cfg := DefaultTestConfig(t)
	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)
	defer s.Close()

	commitStorageEntry(t, s, Address{0x01}, Slot{0x01}, []byte{0x01})
	commitStorageEntry(t, s, Address{0x02}, Slot{0x02}, []byte{0x02})

	last, _ := s.changelog.LastOffset()
	require.Greater(t, last, uint64(0), "WAL should have entries")

	require.NoError(t, s.clearChangelog())

	require.NotNil(t, s.changelog, "changelog should be reopened")

	last, _ = s.changelog.LastOffset()
	require.Equal(t, uint64(0), last, "WAL should be empty after clear")
}

// =============================================================================
// closeDBsOnly and Close idempotent
// =============================================================================

func TestCloseDBsOnlyIdempotent(t *testing.T) {
	cfg := DefaultTestConfig(t)
	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	require.NoError(t, s.closeDBsOnly())
	require.NoError(t, s.closeDBsOnly(), "double closeDBsOnly should be safe")

	require.NoError(t, s.Close())
	require.NoError(t, s.Close(), "double Close should be safe")
}

// =============================================================================
// LoadVersion with targetVersion > WAL
// =============================================================================

func TestLoadVersionTargetBeyondWALFails(t *testing.T) {
	dir := t.TempDir()

	cfg := DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	s1, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s1.LoadVersion(0, false)
	require.NoError(t, err)

	commitStorageEntry(t, s1, Address{0x01}, Slot{0x01}, []byte{0x01})
	commitStorageEntry(t, s1, Address{0x01}, Slot{0x02}, []byte{0x02})
	require.NoError(t, s1.WriteSnapshot(""))
	require.NoError(t, s1.Close())

	cfg = DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	s2, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s2.LoadVersion(100, false)
	require.Error(t, err, "loading version beyond WAL should fail")
}

// =============================================================================
// Reopen preserves working dir optimization
// =============================================================================

func TestReopenReusesWorkingDir(t *testing.T) {
	dir := t.TempDir()

	cfg := DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	commitStorageEntry(t, s, Address{0x01}, Slot{0x01}, []byte{0x01})
	require.NoError(t, s.WriteSnapshot(""))
	require.NoError(t, s.Close())

	workDir := filepath.Join(dir, flatkvRootDir, workingDirName)
	basePath := filepath.Join(workDir, snapshotBaseFile)
	_, err = os.Stat(basePath)
	require.NoError(t, err, "SNAPSHOT_BASE should exist after close")

	cfg = DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	s2, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s2.LoadVersion(0, false)
	require.NoError(t, err)
	defer s2.Close()

	require.Equal(t, int64(1), s2.Version())
}

// =============================================================================
// walOffsetForVersion
// =============================================================================

func TestWalOffsetForVersionFastPath(t *testing.T) {
	cfg := DefaultTestConfig(t)
	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)
	defer s.Close()

	for i := 0; i < 5; i++ {
		commitStorageEntry(t, s, Address{byte(i + 1)}, Slot{byte(i + 1)}, []byte{byte(i + 1)})
	}

	for v := int64(1); v <= 5; v++ {
		off, err := s.walOffsetForVersion(v)
		require.NoError(t, err)
		require.Greater(t, off, uint64(0), "offset for version %d should be nonzero", v)

		ver, err := s.walVersionAtOffset(off)
		require.NoError(t, err)
		require.Equal(t, v, ver)
	}
}

func TestWalOffsetForVersionBeforeWAL(t *testing.T) {
	cfg := DefaultTestConfig(t)
	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)
	defer s.Close()

	for i := 0; i < 3; i++ {
		commitStorageEntry(t, s, Address{byte(i + 1)}, Slot{byte(i + 1)}, []byte{byte(i + 1)})
	}

	off, err := s.walOffsetForVersion(0)
	require.NoError(t, err)
	require.Equal(t, uint64(0), off, "version 0 predates WAL")
}

func TestWalOffsetForVersionNotFound(t *testing.T) {
	cfg := DefaultTestConfig(t)
	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)
	defer s.Close()

	commitStorageEntry(t, s, Address{0x01}, Slot{0x01}, []byte{0x01})
	commitStorageEntry(t, s, Address{0x02}, Slot{0x02}, []byte{0x02})

	_, err = s.walOffsetForVersion(10)
	require.Error(t, err, "version 10 should not be found in WAL with only 2 entries")
}

// =============================================================================
// Catchup from specific version
// =============================================================================

func TestCatchupFromSpecificVersion(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	s1, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s1.LoadVersion(0, false)
	require.NoError(t, err)

	for i := 0; i < 10; i++ {
		commitStorageEntry(t, s1, Address{byte(i + 1)}, Slot{byte(i + 1)}, []byte{byte(i + 1)})
	}
	hashAtV10 := s1.RootHash()

	require.NoError(t, s1.WriteSnapshot(""))
	require.NoError(t, s1.Close())

	cfg = DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	s2, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s2.LoadVersion(0, false)
	require.NoError(t, err)
	defer s2.Close()

	require.Equal(t, int64(10), s2.Version())
	require.Equal(t, hashAtV10, s2.RootHash())
}

// =============================================================================
// Version, RootHash basic behavior
// =============================================================================

func TestVersionStartsAtZero(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()
	require.Equal(t, int64(0), s.Version())
}

func TestRootHashIsBlake3_256(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()
	hash := s.RootHash()
	require.Len(t, hash, 32)
}

// =============================================================================
// Get returns nil for unknown keys
// =============================================================================

func TestGetUnknownKeyReturnsNil(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	v, ok := s.Get([]byte{0xFF, 0xFF, 0xFF})
	require.False(t, ok)
	require.Nil(t, v)
}

// =============================================================================
// Persistence across close/reopen
// =============================================================================

func TestPersistenceAllKeyTypes(t *testing.T) {
	dir := t.TempDir()

	addr := Address{0xAA}
	slot := Slot{0xBB}

	cfg := DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	s1, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s1.LoadVersion(0, false)
	require.NoError(t, err)

	storageKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slot))
	nonceKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyNonce, addr[:])
	codeKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyCode, addr[:])

	cs := makeChangeSet(storageKey, []byte{0x11}, false)
	require.NoError(t, s1.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	cs2 := makeChangeSet(nonceKey, []byte{0, 0, 0, 0, 0, 0, 0, 5}, false)
	require.NoError(t, s1.ApplyChangeSets([]*proto.NamedChangeSet{cs2}))
	cs3 := makeChangeSet(codeKey, []byte{0x60, 0x80}, false)
	require.NoError(t, s1.ApplyChangeSets([]*proto.NamedChangeSet{cs3}))
	commitAndCheck(t, s1)

	hash := s1.RootHash()
	require.NoError(t, s1.Close())

	cfg = DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	s2, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s2.LoadVersion(0, false)
	require.NoError(t, err)
	defer s2.Close()

	require.Equal(t, int64(1), s2.Version())
	require.Equal(t, hash, s2.RootHash())

	v, ok := s2.Get(storageKey)
	require.True(t, ok)
	require.Equal(t, []byte{0x11}, v)

	v, ok = s2.Get(nonceKey)
	require.True(t, ok)
	require.Equal(t, []byte{0, 0, 0, 0, 0, 0, 0, 5}, v)

	v, ok = s2.Get(codeKey)
	require.True(t, ok)
	require.Equal(t, []byte{0x60, 0x80}, v)
}

// =============================================================================
// ReadOnly LoadVersion Tests
// =============================================================================

func TestReadOnlyBasicLoadAndRead(t *testing.T) {
	s, err := NewCommitStore(t.Context(), DefaultTestConfig(t))
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	addr := Address{0xAA}
	slot := Slot{0xBB}
	key := memiavlStorageKey(addr, slot)
	value := []byte{0xCC}

	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{makeChangeSet(key, value, false)}))
	commitAndCheck(t, s)

	ro, err := s.LoadVersion(0, true)
	require.NoError(t, err)
	defer ro.Close()

	require.Equal(t, int64(1), ro.Version())
	got, found := ro.Get(key)
	require.True(t, found)
	require.Equal(t, value, got)
	require.NotNil(t, ro.RootHash())
	require.Len(t, ro.RootHash(), 32)
}

func TestReadOnlyLoadFromUnopenedStore(t *testing.T) {
	cfg := DefaultTestConfig(t)
	writer, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = writer.LoadVersion(0, false)
	require.NoError(t, err)

	addr := Address{0xCC}
	slot := Slot{0xDD}
	key := memiavlStorageKey(addr, slot)
	value := []byte{0xEE}

	require.NoError(t, writer.ApplyChangeSets([]*proto.NamedChangeSet{makeChangeSet(key, value, false)}))
	commitAndCheck(t, writer)
	require.NoError(t, writer.Close())

	fresh, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	ro, err := fresh.LoadVersion(0, true)
	require.NoError(t, err)
	defer ro.Close()

	require.Equal(t, int64(1), ro.Version())
	got, found := ro.Get(key)
	require.True(t, found)
	require.Equal(t, value, got)
}

func TestReadOnlyAtSpecificVersion(t *testing.T) {
	cfg := DefaultTestConfig(t)
	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	addr := Address{0x11}
	slot := Slot{0x22}
	key := memiavlStorageKey(addr, slot)

	for i := byte(1); i <= 5; i++ {
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
			makeChangeSet(key, []byte{i}, false),
		}))
		commitAndCheck(t, s)
	}

	ro, err := s.LoadVersion(3, true)
	require.NoError(t, err)
	defer ro.Close()

	require.Equal(t, int64(3), ro.Version())
	got, found := ro.Get(key)
	require.True(t, found)
	require.Equal(t, []byte{3}, got)
}

func TestReadOnlyWriteGuards(t *testing.T) {
	cfg := DefaultTestConfig(t)
	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	addr := Address{0xAA}
	slot := Slot{0xBB}
	key := memiavlStorageKey(addr, slot)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{makeChangeSet(key, []byte{1}, false)}))
	commitAndCheck(t, s)

	ro, err := s.LoadVersion(0, true)
	require.NoError(t, err)
	defer ro.Close()

	require.ErrorIs(t, ro.ApplyChangeSets(nil), errReadOnly)
	_, err = ro.Commit()
	require.ErrorIs(t, err, errReadOnly)
	require.ErrorIs(t, ro.WriteSnapshot(""), errReadOnly)
	require.ErrorIs(t, ro.Rollback(1), errReadOnly)
	_, err = ro.(*CommitStore).Importer(1)
	require.ErrorIs(t, err, errReadOnly)

	_, err = ro.LoadVersion(0, true)
	require.ErrorIs(t, err, errReadOnly)
}

func TestReadOnlyParentWritesDuringReadOnly(t *testing.T) {
	cfg := DefaultTestConfig(t)
	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	addr := Address{0xAA}
	slot := Slot{0xBB}
	key := memiavlStorageKey(addr, slot)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{makeChangeSet(key, []byte{1}, false)}))
	commitAndCheck(t, s)

	ro, err := s.LoadVersion(0, true)
	require.NoError(t, err)
	defer ro.Close()

	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{makeChangeSet(key, []byte{2}, false)}))
	commitAndCheck(t, s)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{makeChangeSet(key, []byte{3}, false)}))
	commitAndCheck(t, s)

	require.Equal(t, int64(3), s.Version())

	require.Equal(t, int64(1), ro.Version())
	got, found := ro.Get(key)
	require.True(t, found)
	require.Equal(t, []byte{1}, got)
}

func TestReadOnlyConcurrentInstances(t *testing.T) {
	cfg := DefaultTestConfig(t)
	cfg.SnapshotInterval = 2
	cfg.SnapshotKeepRecent = 10
	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	addr := Address{0x11}
	slot := Slot{0x22}
	key := memiavlStorageKey(addr, slot)

	for i := byte(1); i <= 4; i++ {
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
			makeChangeSet(key, []byte{i}, false),
		}))
		commitAndCheck(t, s)
	}

	ro1, err := s.LoadVersion(0, true)
	require.NoError(t, err)
	defer ro1.Close()

	ro2, err := s.LoadVersion(0, true)
	require.NoError(t, err)
	defer ro2.Close()

	require.Equal(t, int64(4), ro1.Version())
	require.Equal(t, int64(4), ro2.Version())

	g1, ok1 := ro1.Get(key)
	g2, ok2 := ro2.Get(key)
	require.True(t, ok1)
	require.True(t, ok2)
	require.Equal(t, []byte{4}, g1)
	require.Equal(t, []byte{4}, g2)
}

func TestReadOnlyFailureDoesNotAffectParent(t *testing.T) {
	cfg := DefaultTestConfig(t)
	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	addr := Address{0xAA}
	slot := Slot{0xBB}
	key := memiavlStorageKey(addr, slot)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{makeChangeSet(key, []byte{1}, false)}))
	commitAndCheck(t, s)

	_, err = s.LoadVersion(999, true)
	require.Error(t, err)

	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{makeChangeSet(key, []byte{2}, false)}))
	v, err := s.Commit()
	require.NoError(t, err)
	require.Equal(t, int64(2), v)

	got, found := s.Get(key)
	require.True(t, found)
	require.Equal(t, []byte{2}, got)
}

func TestReadOnlyCloseRemovesTempDir(t *testing.T) {
	cfg := DefaultTestConfig(t)
	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	addr := Address{0xAA}
	slot := Slot{0xBB}
	key := memiavlStorageKey(addr, slot)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{makeChangeSet(key, []byte{1}, false)}))
	commitAndCheck(t, s)

	roStore, err := s.LoadVersion(0, true)
	require.NoError(t, err)

	roCommit := roStore.(*CommitStore)
	tmpDir := roCommit.readOnlyWorkDir
	require.DirExists(t, tmpDir)

	require.NoError(t, roStore.Close())
	require.NoDirExists(t, tmpDir)
}

func TestCleanupOrphanedReadOnlyDirs(t *testing.T) {
	cfg := DefaultTestConfig(t)
	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	defer func() { require.NoError(t, s.Close()) }()

	fkvDir := s.flatkvDir()
	require.NoError(t, os.MkdirAll(fkvDir, 0o755))

	// Simulate orphaned dirs left by a crash.
	orphan1 := filepath.Join(fkvDir, "readonly-old1")
	orphan2 := filepath.Join(fkvDir, "readonly-old2")
	require.NoError(t, os.Mkdir(orphan1, 0o755))
	require.NoError(t, os.Mkdir(orphan2, 0o755))

	require.NoError(t, s.CleanupOrphanedReadOnlyDirs())

	require.NoDirExists(t, orphan1)
	require.NoDirExists(t, orphan2)

	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)
}

func TestCleanupOrphanedReadOnlyDirsHoldsWriterLock(t *testing.T) {
	cfg := DefaultTestConfig(t)
	s1, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	defer func() { require.NoError(t, s1.Close()) }()
	require.NoError(t, s1.CleanupOrphanedReadOnlyDirs())

	s2, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	defer func() { require.NoError(t, s2.Close()) }()

	err = s2.CleanupOrphanedReadOnlyDirs()
	require.Error(t, err)
	require.ErrorIs(t, err, commonerrors.ErrFileLockUnavailable)
	require.ErrorContains(t, err, "file already locked")
}
