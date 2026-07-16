package flatkv

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	commonerrors "github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
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
// Basic Store Operations
// =============================================================================

func TestStoreOpenClose(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	require.NoError(t, s.Close())
}

func TestInitializeDataDirectoriesPropagatesPebbleMetrics(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.DataDir = t.TempDir()
	cfg.EnablePebbleMetrics = false
	cfg.AccountDBConfig.EnableMetrics = true
	cfg.CodeDBConfig.EnableMetrics = true
	cfg.StorageDBConfig.EnableMetrics = true
	cfg.MiscDBConfig.EnableMetrics = true
	cfg.MetadataDBConfig.EnableMetrics = true

	InitializeDataDirectories(cfg)

	require.False(t, cfg.AccountDBConfig.EnableMetrics)
	require.False(t, cfg.CodeDBConfig.EnableMetrics)
	require.False(t, cfg.StorageDBConfig.EnableMetrics)
	require.False(t, cfg.MiscDBConfig.EnableMetrics)
	require.False(t, cfg.MetadataDBConfig.EnableMetrics)
}

func TestStoreClose(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
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

	addr := ktype.Address{0xAA}
	slot := ktype.Slot{0xBB}
	key := evmStorageKey(addr, slot)

	cs := makeChangeSet(key, padLeft32(0xCC), false)

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

	addr := ktype.Address{0x11}
	slot := ktype.Slot{0x22}
	value := padLeft32(0x33)
	key := evmStorageKey(addr, slot)

	cs := makeChangeSet(key, value, false)

	// Apply but not commit - should be readable from pending writes
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	got, found := s.Get(keys.EVMStoreKey, key)
	require.True(t, found, "should be readable from pending writes")
	require.Equal(t, value, got)

	// Commit
	commitAndCheck(t, s)

	// Still should be readable after commit
	got, found = s.Get(keys.EVMStoreKey, key)
	require.True(t, found)
	require.Equal(t, value, got)
}

func TestStoreMultipleWrites(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := ktype.Address{0x44}
	entries := []struct {
		slot  ktype.Slot
		value byte
	}{
		{ktype.Slot{0x01}, 0xAA},
		{ktype.Slot{0x02}, 0xBB},
		{ktype.Slot{0x03}, 0xCC},
	}

	// Create multiple pairs in one changeset
	pairs := make([]*proto.KVPair, len(entries))
	for i, e := range entries {
		key := evmStorageKey(addr, e.slot)
		pairs[i] = &proto.KVPair{Key: key, Value: padLeft32(e.value)}
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
		key := evmStorageKey(addr, e.slot)
		got, found := s.Get(keys.EVMStoreKey, key)
		require.True(t, found)
		require.Equal(t, padLeft32(e.value), got)
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

// TestStoreApplyRejectsEmptyModuleName guards against a non-EVM changeset with
// an empty Name. An empty module folds into the physical key as "/"+key,
// whose per-module meta key ("_meta/x:/hash") ParseModuleLtHashKey rejects on
// reload — silently accepting it here would make the store permanently
// unopenable rather than failing fast at Apply time.
func TestStoreApplyRejectsEmptyModuleName(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	cs := &proto.NamedChangeSet{
		Name:      "",
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{{Key: []byte("k"), Value: []byte("v")}}},
	}

	err := s.ApplyChangeSets([]*proto.NamedChangeSet{cs})
	require.ErrorContains(t, err, "empty module name")
}

func TestStoreClearsPendingAfterCommit(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := ktype.Address{0xAA}
	slot := ktype.Slot{0xBB}
	key := evmStorageKey(addr, slot)

	cs := makeChangeSet(key, padLeft32(0xCC), false)
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

	addr := ktype.Address{0x88}
	slot := ktype.Slot{0x99}
	key := evmStorageKey(addr, slot)

	// Version 1
	cs1 := makeChangeSet(key, padLeft32(0x01), false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs1}))
	commitAndCheck(t, s)

	require.Equal(t, int64(1), s.Version())

	// Version 2 with updated value
	cs2 := makeChangeSet(key, padLeft32(0x02), false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs2}))
	commitAndCheck(t, s)

	require.Equal(t, int64(2), s.Version())

	// Latest value should be from version 2
	got, found := s.Get(keys.EVMStoreKey, key)
	require.True(t, found)
	require.Equal(t, padLeft32(0x02), got)
}

func TestStorePersistence(t *testing.T) {
	dir := t.TempDir()

	addr := ktype.Address{0xDD}
	slot := ktype.Slot{0xEE}
	value := padLeft32(0xFF)
	key := evmStorageKey(addr, slot)

	// Write and close
	cfg := config.DefaultTestConfig(t)
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
	cfg = config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	s2, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s2.LoadVersion(0, false)
	require.NoError(t, err)
	defer s2.Close()

	got, found := s2.Get(keys.EVMStoreKey, key)
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
	addr := ktype.Address{0xAB}
	slot := ktype.Slot{0xCD}
	key := evmStorageKey(addr, slot)

	cs := makeChangeSet(key, padLeft32(0xEF), false)
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
	addr := ktype.Address{0xEE}
	slot := ktype.Slot{0xFF}
	key := evmStorageKey(addr, slot)

	cs := makeChangeSet(key, padLeft32(0x11), false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))

	// Working hash should change
	hash2 := s.RootHash()
	require.NotEqual(t, hash1, hash2, "hash should change after ApplyChangeSets")
}

func TestStoreRootHashStableAfterCommit(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := ktype.Address{0x12}
	slot := ktype.Slot{0x34}
	key := evmStorageKey(addr, slot)

	cs := makeChangeSet(key, padLeft32(0x56), false)
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

	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	s1, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s1.LoadVersion(0, false)
	require.NoError(t, err)

	cfg = config.DefaultTestConfig(t)
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
	cfg := config.DefaultTestConfig(t)
	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)
	defer s.Close()

	commitStorageEntry(t, s, ktype.Address{0x01}, ktype.Slot{0x01}, []byte{0x01})
	commitStorageEntry(t, s, ktype.Address{0x02}, ktype.Slot{0x02}, []byte{0x02})

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
	cfg := config.DefaultTestConfig(t)
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

	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	s1, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s1.LoadVersion(0, false)
	require.NoError(t, err)

	commitStorageEntry(t, s1, ktype.Address{0x01}, ktype.Slot{0x01}, []byte{0x01})
	commitStorageEntry(t, s1, ktype.Address{0x01}, ktype.Slot{0x02}, []byte{0x02})
	require.NoError(t, s1.WriteSnapshot(""))
	require.NoError(t, s1.Close())

	cfg = config.DefaultTestConfig(t)
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

	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	commitStorageEntry(t, s, ktype.Address{0x01}, ktype.Slot{0x01}, []byte{0x01})
	require.NoError(t, s.WriteSnapshot(""))
	require.NoError(t, s.Close())

	workDir := filepath.Join(dir, flatkvRootDir, workingDirName)
	basePath := filepath.Join(workDir, snapshotBaseFile)
	_, err = os.Stat(basePath)
	require.NoError(t, err, "SNAPSHOT_BASE should exist after close")

	cfg = config.DefaultTestConfig(t)
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
	cfg := config.DefaultTestConfig(t)
	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)
	defer s.Close()

	for i := 0; i < 5; i++ {
		commitStorageEntry(t, s, ktype.Address{byte(i + 1)}, ktype.Slot{byte(i + 1)}, []byte{byte(i + 1)})
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
	cfg := config.DefaultTestConfig(t)
	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)
	defer s.Close()

	for i := 0; i < 3; i++ {
		commitStorageEntry(t, s, ktype.Address{byte(i + 1)}, ktype.Slot{byte(i + 1)}, []byte{byte(i + 1)})
	}

	off, err := s.walOffsetForVersion(0)
	require.NoError(t, err)
	require.Equal(t, uint64(0), off, "version 0 predates WAL")
}

func TestWalOffsetForVersionNotFound(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)
	defer s.Close()

	commitStorageEntry(t, s, ktype.Address{0x01}, ktype.Slot{0x01}, []byte{0x01})
	commitStorageEntry(t, s, ktype.Address{0x02}, ktype.Slot{0x02}, []byte{0x02})

	_, err = s.walOffsetForVersion(10)
	require.Error(t, err, "version 10 should not be found in WAL with only 2 entries")
}

// =============================================================================
// Catchup from specific version
// =============================================================================

func TestCatchupFromSpecificVersion(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	s1, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s1.LoadVersion(0, false)
	require.NoError(t, err)

	for i := 0; i < 10; i++ {
		commitStorageEntry(t, s1, ktype.Address{byte(i + 1)}, ktype.Slot{byte(i + 1)}, []byte{byte(i + 1)})
	}
	hashAtV10 := s1.RootHash()

	require.NoError(t, s1.WriteSnapshot(""))
	require.NoError(t, s1.Close())

	cfg = config.DefaultTestConfig(t)
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
// Get returns nil for missing keys, errors for unsupported key types
// =============================================================================

func TestGetMissingKeyReturnsNil(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	v, ok := s.Get(keys.EVMStoreKey, []byte{0xFF, 0xFF, 0xFF})
	require.False(t, ok)
	require.Nil(t, v)
}

func TestGetUnsupportedKeyType_Strict(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	val, found := s.Get(keys.EVMStoreKey, []byte{})
	require.False(t, found)
	require.Nil(t, val)
}

func TestGetUnsupportedKeyType_NonStrict(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	s := setupTestStoreWithConfig(t, cfg)
	defer s.Close()

	val, found := s.Get(keys.EVMStoreKey, []byte{})
	require.False(t, found)
	require.Nil(t, val)
}

// =============================================================================
// Persistence across close/reopen
// =============================================================================

func TestPersistenceAllKeyTypes(t *testing.T) {
	dir := t.TempDir()

	addr := ktype.Address{0xAA}
	slot := ktype.Slot{0xBB}

	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	s1, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s1.LoadVersion(0, false)
	require.NoError(t, err)

	storageKey := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot))
	nonceKey := keys.BuildEVMKey(keys.EVMKeyNonce, addr[:])
	codeKey := keys.BuildEVMKey(keys.EVMKeyCode, addr[:])

	cs := makeChangeSet(storageKey, padLeft32(0x11), false)
	require.NoError(t, s1.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	cs2 := makeChangeSet(nonceKey, []byte{0, 0, 0, 0, 0, 0, 0, 5}, false)
	require.NoError(t, s1.ApplyChangeSets([]*proto.NamedChangeSet{cs2}))
	cs3 := makeChangeSet(codeKey, []byte{0x60, 0x80}, false)
	require.NoError(t, s1.ApplyChangeSets([]*proto.NamedChangeSet{cs3}))
	commitAndCheck(t, s1)

	hash := s1.RootHash()
	require.NoError(t, s1.Close())

	cfg = config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	s2, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s2.LoadVersion(0, false)
	require.NoError(t, err)
	defer s2.Close()

	require.Equal(t, int64(1), s2.Version())
	require.Equal(t, hash, s2.RootHash())

	v, ok := s2.Get(keys.EVMStoreKey, storageKey)
	require.True(t, ok)
	require.Equal(t, padLeft32(0x11), v)

	v, ok = s2.Get(keys.EVMStoreKey, nonceKey)
	require.True(t, ok)
	require.Equal(t, []byte{0, 0, 0, 0, 0, 0, 0, 5}, v)

	v, ok = s2.Get(keys.EVMStoreKey, codeKey)
	require.True(t, ok)
	require.Equal(t, []byte{0x60, 0x80}, v)
}

// =============================================================================
// ReadOnly LoadVersion Tests
// =============================================================================

func TestReadOnlyBasicLoadAndRead(t *testing.T) {
	s, err := NewCommitStore(t.Context(), config.DefaultTestConfig(t))
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	addr := ktype.Address{0xAA}
	slot := ktype.Slot{0xBB}
	key := evmStorageKey(addr, slot)
	value := padLeft32(0xCC)

	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{makeChangeSet(key, value, false)}))
	commitAndCheck(t, s)

	ro, err := s.LoadVersion(0, true)
	require.NoError(t, err)
	defer ro.Close()

	require.Equal(t, int64(1), ro.Version())
	got, found := ro.Get(keys.EVMStoreKey, key)
	require.True(t, found)
	require.Equal(t, value, got)
	require.NotNil(t, ro.RootHash())
	require.Len(t, ro.RootHash(), 32)
}

func TestReadOnlyLoadFromUnopenedStore(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	writer, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = writer.LoadVersion(0, false)
	require.NoError(t, err)

	addr := ktype.Address{0xCC}
	slot := ktype.Slot{0xDD}
	key := evmStorageKey(addr, slot)
	value := padLeft32(0xEE)

	require.NoError(t, writer.ApplyChangeSets([]*proto.NamedChangeSet{makeChangeSet(key, value, false)}))
	commitAndCheck(t, writer)
	require.NoError(t, writer.Close())

	fresh, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	ro, err := fresh.LoadVersion(0, true)
	require.NoError(t, err)
	defer ro.Close()

	require.Equal(t, int64(1), ro.Version())
	got, found := ro.Get(keys.EVMStoreKey, key)
	require.True(t, found)
	require.Equal(t, value, got)
}

func TestReadOnlyAtSpecificVersion(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	addr := ktype.Address{0x11}
	slot := ktype.Slot{0x22}
	key := evmStorageKey(addr, slot)

	for i := byte(1); i <= 5; i++ {
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
			makeChangeSet(key, padLeft32(i), false),
		}))
		commitAndCheck(t, s)
	}

	ro, err := s.LoadVersion(3, true)
	require.NoError(t, err)
	defer ro.Close()

	require.Equal(t, int64(3), ro.Version())
	got, found := ro.Get(keys.EVMStoreKey, key)
	require.True(t, found)
	require.Equal(t, padLeft32(3), got)
}

func TestReadOnlyWriteGuards(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	addr := ktype.Address{0xAA}
	slot := ktype.Slot{0xBB}
	key := evmStorageKey(addr, slot)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{makeChangeSet(key, padLeft32(1), false)}))
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
	cfg := config.DefaultTestConfig(t)
	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	addr := ktype.Address{0xAA}
	slot := ktype.Slot{0xBB}
	key := evmStorageKey(addr, slot)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{makeChangeSet(key, padLeft32(1), false)}))
	commitAndCheck(t, s)

	ro, err := s.LoadVersion(0, true)
	require.NoError(t, err)
	defer ro.Close()

	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{makeChangeSet(key, padLeft32(2), false)}))
	commitAndCheck(t, s)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{makeChangeSet(key, padLeft32(3), false)}))
	commitAndCheck(t, s)

	require.Equal(t, int64(3), s.Version())

	require.Equal(t, int64(1), ro.Version())
	got, found := ro.Get(keys.EVMStoreKey, key)
	require.True(t, found)
	require.Equal(t, padLeft32(1), got)
}

func TestReadOnlyConcurrentInstances(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	cfg.SnapshotInterval = 2
	cfg.SnapshotKeepRecent = 10
	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	addr := ktype.Address{0x11}
	slot := ktype.Slot{0x22}
	key := evmStorageKey(addr, slot)

	for i := byte(1); i <= 4; i++ {
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
			makeChangeSet(key, padLeft32(i), false),
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

	g1, ok1 := ro1.Get(keys.EVMStoreKey, key)
	g2, ok2 := ro2.Get(keys.EVMStoreKey, key)
	require.True(t, ok1)
	require.True(t, ok2)
	require.Equal(t, padLeft32(4), g1)
	require.Equal(t, padLeft32(4), g2)
}

func TestReadOnlyFailureDoesNotAffectParent(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	addr := ktype.Address{0xAA}
	slot := ktype.Slot{0xBB}
	key := evmStorageKey(addr, slot)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{makeChangeSet(key, padLeft32(1), false)}))
	commitAndCheck(t, s)

	_, err = s.LoadVersion(999, true)
	require.Error(t, err)

	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{makeChangeSet(key, padLeft32(2), false)}))
	v, err := s.Commit()
	require.NoError(t, err)
	require.Equal(t, int64(2), v)

	got, found := s.Get(keys.EVMStoreKey, key)
	require.True(t, found)
	require.Equal(t, padLeft32(2), got)
}

func TestReadOnlyCloseRemovesTempDir(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	addr := ktype.Address{0xAA}
	slot := ktype.Slot{0xBB}
	key := evmStorageKey(addr, slot)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{makeChangeSet(key, padLeft32(1), false)}))
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
	cfg := config.DefaultTestConfig(t)
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
	cfg := config.DefaultTestConfig(t)
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

func TestLoadVersionReload(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)

	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	addr := addrN(0x01)
	key := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slotN(0x01)))
	cs := makeChangeSet(key, padLeft32(0x11), false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	expectedHash := s.RootHash()

	// Re-call LoadVersion(0, false) on the same store: should close and reopen.
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	require.Equal(t, int64(1), s.Version())
	require.Equal(t, expectedHash, s.RootHash())

	val, found := s.Get(keys.EVMStoreKey, key)
	require.True(t, found)
	require.Equal(t, padLeft32(0x11), val)
	require.NoError(t, s.Close())
}

func TestLoadVersionReadOnlyVersion0(t *testing.T) {
	s := setupTestStore(t)

	addr := addrN(0x02)
	key := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slotN(0x01)))
	cs := makeChangeSet(key, padLeft32(0x22), false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	// Version 0 in read-only means "latest committed".
	ro, err := s.LoadVersion(0, true)
	require.NoError(t, err)
	defer ro.Close()

	require.Equal(t, int64(1), ro.Version())
	val, found := ro.Get(keys.EVMStoreKey, key)
	require.True(t, found)
	require.Equal(t, padLeft32(0x22), val)
	require.NoError(t, s.Close())
}

func TestLoadVersionReadOnlyDoesNotSeePending(t *testing.T) {
	s := setupTestStore(t)

	addr := addrN(0x03)
	key := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slotN(0x01)))
	cs := makeChangeSet(key, padLeft32(0x33), false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	// Apply a new changeset without committing.
	key2 := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slotN(0x02)))
	cs2 := makeChangeSet(key2, padLeft32(0x44), false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs2}))

	// RO should not see the uncommitted write.
	ro, err := s.LoadVersion(0, true)
	require.NoError(t, err)
	defer ro.Close()

	_, found := ro.Get(keys.EVMStoreKey, key2)
	require.False(t, found, "read-only store should not see uncommitted data")

	// But committed data should be visible.
	val, found := ro.Get(keys.EVMStoreKey, key)
	require.True(t, found)
	require.Equal(t, padLeft32(0x33), val)
	require.NoError(t, s.Close())
}

func TestLoadVersionEmptyWAL(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultTestConfig(t)
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
	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)

	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	addr := addrN(0x10)
	key := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slotN(0x01)))
	cs := makeChangeSet(key, padLeft32(0x11), false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	// Apply but do NOT commit.
	key2 := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slotN(0x02)))
	cs2 := makeChangeSet(key2, padLeft32(0x22), false)
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

	val, found := s2.Get(keys.EVMStoreKey, key)
	require.True(t, found, "committed data should persist")
	require.Equal(t, padLeft32(0x11), val)

	_, found = s2.Get(keys.EVMStoreKey, key2)
	require.False(t, found, "uncommitted data should be lost")
}

func TestCloseDuringConcurrentReadOnlyClone(t *testing.T) {
	s := setupTestStore(t)

	addr := addrN(0x11)
	key := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slotN(0x01)))
	cs := makeChangeSet(key, padLeft32(0xAA), false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	ro, err := s.LoadVersion(0, true)
	require.NoError(t, err)

	// Close parent while RO is still open.
	require.NoError(t, s.Close())

	// RO should still function.
	val, found := ro.Get(keys.EVMStoreKey, key)
	require.True(t, found, "RO clone should remain functional after parent close")
	require.Equal(t, padLeft32(0xAA), val)

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
	key := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slotN(0x01)))
	cs := makeChangeSet(key, padLeft32(0xBB), false)
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
	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	cfg.SnapshotInterval = 2

	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	addr := addrN(0x20)
	for i := 1; i <= 5; i++ {
		key := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slotN(byte(i))))
		cs := makeChangeSet(key, padLeft32(byte(i)), false)
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
	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	cfg.SnapshotInterval = 2

	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	addr := addrN(0x21)
	var hashes [6][]byte
	for i := 1; i <= 5; i++ {
		key := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slotN(byte(i))))
		cs := makeChangeSet(key, padLeft32(byte(i)), false)
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

func TestCrashRecoverySkewedPerDBVersions(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultTestConfig(t)
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
	require.NoError(t, writeLocalMetaToBatch(batch, 4, savedAccountLtHash, s.perDBModuleWorkingLtHash[accountDBDir], s.perDBModuleWorkingStats[accountDBDir]))
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
	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	cfg.SnapshotInterval = 3

	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	addr := addrN(0x02)
	for i := 1; i <= 5; i++ {
		key := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slotN(byte(i))))
		cs := makeChangeSet(key, padLeft32(byte(i*11)), false)
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
		_, err := s.Commit()
		require.NoError(t, err)
	}

	// Save the correct storageDB per-DB LtHash before skewing.
	savedStorageLtHash := s.perDBWorkingLtHash[storageDBDir].Clone()

	// Simulate crash: storageDB only flushed v3 (version watermark behind).
	batch := s.storageDB.NewBatch()
	require.NoError(t, writeLocalMetaToBatch(batch, 3, savedStorageLtHash, s.perDBModuleWorkingLtHash[storageDBDir], s.perDBModuleWorkingStats[storageDBDir]))
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
		key := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slotN(byte(i))))
		val, found := s2.Get(keys.EVMStoreKey, key)
		require.True(t, found, "slot %d should exist after recovery", i)
		require.Equal(t, padLeft32(byte(i*11)), val)
	}
}

func TestCrashRecoveryWALReplayLargeGap(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	cfg.SnapshotInterval = 5

	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	addr := addrN(0x03)
	for i := 1; i <= 20; i++ {
		key := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slotN(byte(i))))
		cs := makeChangeSet(key, padLeft32(byte(i)), false)
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
		key := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slotN(byte(i))))
		val, found := s2.Get(keys.EVMStoreKey, key)
		require.True(t, found, "slot %d should exist", i)
		require.Equal(t, padLeft32(byte(i)), val)
	}
}

func TestCrashRecoveryEmptyWALAfterSnapshot(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)

	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	addr := addrN(0x04)
	key := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slotN(0x01)))
	cs := makeChangeSet(key, padLeft32(0xAA), false)
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

	val, found := s2.Get(keys.EVMStoreKey, key)
	require.True(t, found)
	require.Equal(t, padLeft32(0xAA), val)

	// Can continue committing after recovery from snapshot-only state.
	cs2 := makeChangeSet(key, padLeft32(0xBB), false)
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
	require.NoError(t, batch.Set(accountPhysKey(addr), []byte{0xDE, 0xAD}))
	require.NoError(t, batch.Commit(types.WriteOptions{Sync: true}))
	_ = batch.Close()

	// Next ApplyChangeSets touching this account should detect the corruption
	// when deserializing the old account value (deserializeAccountOld).
	cs2 := &proto.NamedChangeSet{
		Name:      "evm",
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{noncePair(addr, 99)}},
	}
	err = s.ApplyChangeSets([]*proto.NamedChangeSet{cs2})
	require.Error(t, err, "should fail on corrupted AccountValue")
	require.Contains(t, err.Error(), "unsupported serialization version")
}

func TestCrashRecoveryCrashAfterWALBeforeDBCommit(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	cfg.SnapshotInterval = 1

	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	addr := addrN(0x06)
	slot := slotN(0x01)
	key := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot))
	cs := makeChangeSet(key, padLeft32(0x11), false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	_, err = s.Commit()
	require.NoError(t, err)
	hashAfterV1 := s.RootHash()

	// Now simulate writing v2 to WAL but "crashing" before DB commit.
	cs2 := makeChangeSet(key, padLeft32(0x22), false)
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

	val, found := s2.Get(keys.EVMStoreKey, key)
	require.True(t, found)
	require.Equal(t, padLeft32(0x22), val, "v2 value should be present after catchup")
	verifyLtHashConsistency(t, s2)
}

func TestCrashRecoveryLtHashConsistencyAfterAllPaths(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultTestConfig(t)
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
				Key:   keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slotN(byte(i)))),
				Value: padLeft32(byte(i)),
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
	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)

	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	cs := makeChangeSet(
		keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addrN(0x01), slotN(0x01))),
		padLeft32(0x11), false,
	)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	_, err = s.Commit()
	require.NoError(t, err)

	// Write garbage to the global _meta/hash key in metadataDB.
	batch := s.metadataDB.NewBatch()
	require.NoError(t, batch.Set(ktype.MetaLtHashKey, []byte{0xDE, 0xAD, 0xBE, 0xEF}))
	require.NoError(t, batch.Commit(types.WriteOptions{Sync: true}))
	_ = batch.Close()

	require.NoError(t, s.Close())

	// Reopen should fail with an LtHash unmarshal error.
	s2, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	defer s2.Close()
	_, err = s2.LoadVersion(0, false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid LtHash size")
}

func TestCrashRecoveryCorruptLtHashBlobInPerDBMeta(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)

	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	cs := makeChangeSet(
		keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addrN(0x02), slotN(0x01))),
		padLeft32(0x22), false,
	)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	_, err = s.Commit()
	require.NoError(t, err)

	// Write garbage to accountDB's _meta/hash key.
	batch := s.accountDB.NewBatch()
	require.NoError(t, batch.Set(ktype.MetaLtHashKey, []byte{0x01, 0x02, 0x03}))
	require.NoError(t, batch.Commit(types.WriteOptions{Sync: true}))
	_ = batch.Close()

	require.NoError(t, s.Close())

	// Reopen should fail with an LtHash unmarshal error from per-DB meta.
	s2, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	defer s2.Close()
	_, err = s2.LoadVersion(0, false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid LtHash size")
}

func TestCrashRecoveryGlobalVersionOverflow(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)

	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	cs := makeChangeSet(
		keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addrN(0x03), slotN(0x01))),
		padLeft32(0x33), false,
	)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	_, err = s.Commit()
	require.NoError(t, err)

	// Write a version value that exceeds math.MaxInt64 to the global metadata.
	overflowBytes := make([]byte, 8)
	overflowBytes[0] = 0xFF // 0xFF00000000000000 > MaxInt64
	batch := s.metadataDB.NewBatch()
	require.NoError(t, batch.Set(ktype.MetaVersionKey, overflowBytes))
	require.NoError(t, batch.Commit(types.WriteOptions{Sync: true}))
	_ = batch.Close()

	require.NoError(t, s.Close())

	// Reopen should fail with an overflow error.
	s2, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	defer s2.Close()
	_, err = s2.LoadVersion(0, false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "global version overflow")
}

func TestInitializeDataDirectories(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.DataDir = "/base/flatkv"
	cfg.AccountDBConfig.DataDir = ""
	cfg.CodeDBConfig.DataDir = ""
	cfg.StorageDBConfig.DataDir = ""
	cfg.MiscDBConfig.DataDir = ""
	cfg.MetadataDBConfig.DataDir = ""

	InitializeDataDirectories(cfg)

	require.Equal(t, "/base/flatkv/working/account", cfg.AccountDBConfig.DataDir)
	require.Equal(t, "/base/flatkv/working/code", cfg.CodeDBConfig.DataDir)
	require.Equal(t, "/base/flatkv/working/storage", cfg.StorageDBConfig.DataDir)
	require.Equal(t, "/base/flatkv/working/misc", cfg.MiscDBConfig.DataDir)
	require.Equal(t, "/base/flatkv/working/metadata", cfg.MetadataDBConfig.DataDir)
}

func TestInitializeDataDirectoriesPreservesExisting(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.DataDir = "/base/flatkv"
	cfg.AccountDBConfig.DataDir = "/custom/account"

	InitializeDataDirectories(cfg)

	require.Equal(t, "/custom/account", cfg.AccountDBConfig.DataDir,
		"existing DataDir should not be overwritten")
	require.Equal(t, "/base/flatkv/working/code", cfg.CodeDBConfig.DataDir,
		"empty DataDir should be populated")
}
