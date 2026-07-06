package flatkv

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
)

// =============================================================================
// LocalMeta and Global Metadata
// =============================================================================

func TestLoadLocalMeta(t *testing.T) {
	t.Run("NewDB_ReturnsDefault", func(t *testing.T) {
		db := setupTestDB(t)
		defer db.Close()

		meta, err := loadLocalMeta(db)
		require.NoError(t, err)
		require.NotNil(t, meta)
		require.Equal(t, int64(0), meta.CommittedVersion)
	})

	t.Run("ExistingMeta_LoadsCorrectly", func(t *testing.T) {
		db := setupTestDB(t)
		defer db.Close()

		require.NoError(t, db.Set(ktype.MetaVersionKey, versionToBytes(42), types.WriteOptions{}))

		// Load it back
		loaded, err := loadLocalMeta(db)
		require.NoError(t, err)
		require.Equal(t, int64(42), loaded.CommittedVersion)
		require.Nil(t, loaded.LtHash)
	})

	t.Run("CorruptedVersion_ReturnsError", func(t *testing.T) {
		db := setupTestDB(t)
		defer db.Close()

		require.NoError(t, db.Set(ktype.MetaVersionKey, []byte{0x01, 0x02}, types.WriteOptions{}))

		_, err := loadLocalMeta(db)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid meta version length")
	})
}

func TestStoreCommitBatchesUpdatesLocalMeta(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := ktype.Address{0x12}
	slot := ktype.Slot{0x34}
	key := evmStorageKey(addr, slot)

	cs := makeChangeSet(key, padLeft32(0x56), false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	v := commitAndCheck(t, s)
	require.Equal(t, int64(1), v)

	// LocalMeta should be updated
	require.Equal(t, int64(1), s.localMeta[storageDBDir].CommittedVersion)

	// Verify it's persisted in DB
	data, err := s.storageDB.Get(ktype.MetaVersionKey)
	require.NoError(t, err)
	require.Equal(t, int64(1), int64(binary.BigEndian.Uint64(data)))
}

func TestStoreMetadataOperations(t *testing.T) {
	t.Run("LoadGlobalVersion_NewDB", func(t *testing.T) {
		s := setupTestStore(t)
		defer s.Close()

		version, err := s.loadGlobalVersion()
		require.NoError(t, err)
		require.Equal(t, int64(0), version)
	})

	t.Run("LoadGlobalLtHash_NewDB", func(t *testing.T) {
		s := setupTestStore(t)
		defer s.Close()

		hash, err := s.loadGlobalLtHash()
		require.NoError(t, err)
		require.Nil(t, hash)
	})

	t.Run("CommitGlobalMetadata_RoundTrip", func(t *testing.T) {
		s := setupTestStore(t)
		defer s.Close()

		// Commit metadata
		expectedVersion := int64(100)
		expectedHash := lthash.New()

		err := s.commitGlobalMetadata(expectedVersion, expectedHash)
		require.NoError(t, err)

		// Load it back
		version, err := s.loadGlobalVersion()
		require.NoError(t, err)
		require.Equal(t, expectedVersion, version)

		hash, err := s.loadGlobalLtHash()
		require.NoError(t, err)
		require.NotNil(t, hash)
		require.Equal(t, expectedHash.Marshal(), hash.Marshal())
	})

	t.Run("CommitGlobalMetadata_Atomicity", func(t *testing.T) {
		s := setupTestStore(t)
		defer s.Close()

		// Commit multiple times
		for v := int64(1); v <= 10; v++ {
			hash := lthash.New()
			err := s.commitGlobalMetadata(v, hash)
			require.NoError(t, err)

			// Verify immediately
			version, err := s.loadGlobalVersion()
			require.NoError(t, err)
			require.Equal(t, v, version)
		}
	})

	t.Run("LoadGlobalVersion_InvalidData", func(t *testing.T) {
		s := setupTestStore(t)
		defer s.Close()

		// Write invalid data (wrong size)
		err := s.metadataDB.Set(ktype.MetaVersionKey, []byte{0x01}, types.WriteOptions{})
		require.NoError(t, err)

		// Should return error
		_, err = s.loadGlobalVersion()
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid global version length")
	})
}

// =============================================================================
// SetInitialVersion
// =============================================================================

func TestSetInitialVersion_HappyPath(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	require.NoError(t, s.SetInitialVersion(100))
	require.Equal(t, int64(99), s.committedVersion)
	target, err := os.Readlink(currentPath(s.flatkvDir()))
	require.NoError(t, err)
	require.Equal(t, snapshotName(99), target)

	addr := ktype.Address{0xAA}
	slot := ktype.Slot{0xBB}
	cs := makeChangeSet(evmStorageKey(addr, slot), padLeft32(0xCC), false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))

	v, err := s.Commit()
	require.NoError(t, err)
	require.Equal(t, int64(100), v, "first Commit after SetInitialVersion(100) must produce version 100")
	require.Equal(t, int64(100), s.Version())
}

func TestSetInitialVersion_GenesisSkipsSeededSnapshot(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	require.NoError(t, s.SetInitialVersion(1))
	require.Equal(t, int64(0), s.committedVersion)
	target, err := os.Readlink(currentPath(s.flatkvDir()))
	require.NoError(t, err)
	require.Equal(t, snapshotName(0), target)

	addr := ktype.Address{0xAA}
	slot := ktype.Slot{0xBB}
	cs := makeChangeSet(evmStorageKey(addr, slot), padLeft32(0xCC), false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))

	v, err := s.Commit()
	require.NoError(t, err)
	require.Equal(t, int64(1), v, "first Commit after SetInitialVersion(1) must produce version 1")
}

func TestSetInitialVersion_PersistsEarliestVersion(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)
	require.Equal(t, int64(0), s.EarliestVersion(),
		"a fresh store has no earliest-version record")

	require.NoError(t, s.SetInitialVersion(100))
	require.Equal(t, int64(99), s.EarliestVersion())
	require.NoError(t, s.Close())

	reopened, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = reopened.LoadVersion(0, false)
	require.NoError(t, err)
	defer reopened.Close()
	require.Equal(t, int64(99), reopened.EarliestVersion(),
		"the earliest-version record must survive reopen")
}

func TestSetInitialVersion_RejectsAfterCommit(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := ktype.Address{0x01}
	slot := ktype.Slot{0x02}
	cs := makeChangeSet(evmStorageKey(addr, slot), padLeft32(0x03), false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	_, err := s.Commit()
	require.NoError(t, err)

	err = s.SetInitialVersion(50)
	require.Error(t, err)
	require.Contains(t, err.Error(), "fresh store")
}

func TestSetInitialVersion_RejectsReadOnly(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := ktype.Address{0x01}
	slot := ktype.Slot{0x02}
	cs := makeChangeSet(evmStorageKey(addr, slot), padLeft32(0x03), false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	_, err := s.Commit()
	require.NoError(t, err)

	roStore, err := s.LoadVersion(0, true)
	require.NoError(t, err)
	defer roStore.Close()

	err = roStore.SetInitialVersion(50)
	require.Error(t, err)
	require.ErrorIs(t, err, errReadOnly)
}

func TestSetInitialVersion_RejectsNonPositive(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	require.Error(t, s.SetInitialVersion(0))
	require.Error(t, s.SetInitialVersion(-1))
	require.Equal(t, int64(0), s.committedVersion, "rejected calls must not mutate state")
}

func TestSetInitialVersion_SurvivesReopen(t *testing.T) {
	dir := t.TempDir()
	dbDir := filepath.Join(dir, flatkvRootDir)

	cfg := config.DefaultConfig()
	cfg.DataDir = dbDir
	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	require.NoError(t, s.SetInitialVersion(100))
	require.NoError(t, s.Close())

	cfg2 := config.DefaultConfig()
	cfg2.DataDir = dbDir
	s2, err := NewCommitStore(context.Background(), cfg2)
	require.NoError(t, err)
	_, err = s2.LoadVersion(0, false)
	require.NoError(t, err)
	defer s2.Close()

	require.Equal(t, int64(99), s2.committedVersion,
		"persisted committedVersion must equal initialVersion-1 after reopen")

	addr := ktype.Address{0xDD}
	slot := ktype.Slot{0xEE}
	cs := makeChangeSet(evmStorageKey(addr, slot), padLeft32(0xFF), false)
	require.NoError(t, s2.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	v, err := s2.Commit()
	require.NoError(t, err)
	require.Equal(t, int64(100), v,
		"first Commit after reopen must produce initialVersion")
}

func TestSetInitialVersion_RollbackBelowSeededVersionFails(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	require.NoError(t, s.SetInitialVersion(100))

	addr := ktype.Address{0x77}
	slot := ktype.Slot{0x88}
	cs := makeChangeSet(evmStorageKey(addr, slot), padLeft32(0x01), false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	_, err := s.Commit()
	require.NoError(t, err)
	require.Equal(t, int64(100), s.Version())

	err = s.Rollback(50)
	require.Error(t, err,
		"rollback below initialVersion-1 must fail; nothing exists before the seeded baseline")
}

// =============================================================================
// Global Metadata Persistence After Commit + Reopen
// =============================================================================

func TestGlobalMetadataPersistence(t *testing.T) {
	dir := t.TempDir()
	dbDir := filepath.Join(dir, flatkvRootDir)

	cfg := config.DefaultConfig()
	cfg.DataDir = dbDir
	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	commitStorageEntry(t, s, ktype.Address{0x01}, ktype.Slot{0x01}, []byte{0xAA})
	commitStorageEntry(t, s, ktype.Address{0x02}, ktype.Slot{0x02}, []byte{0xBB})

	globalVer, err := s.loadGlobalVersion()
	require.NoError(t, err)
	require.Equal(t, int64(2), globalVer)

	globalHash, err := s.loadGlobalLtHash()
	require.NoError(t, err)
	require.Equal(t, s.committedLtHash.Checksum(), globalHash.Checksum())

	expectedHash := s.committedLtHash.Checksum()
	require.NoError(t, s.Close())

	cfg2 := config.DefaultConfig()
	cfg2.DataDir = dbDir
	s2, err := NewCommitStore(context.Background(), cfg2)
	require.NoError(t, err)
	_, err = s2.LoadVersion(0, false)
	require.NoError(t, err)
	defer s2.Close()

	require.Equal(t, int64(2), s2.committedVersion)
	require.Equal(t, expectedHash, s2.committedLtHash.Checksum(),
		"global LtHash should survive reopen")
}

// =============================================================================
// GetLatestVersion (free-standing helper + method)
// =============================================================================

func TestGetLatestVersionFreshDirReturnsZero(t *testing.T) {
	dir := t.TempDir()
	v, err := GetLatestVersion(filepath.Join(dir, flatkvRootDir))
	require.NoError(t, err)
	require.Equal(t, int64(0), v,
		"never-opened flatkv dir must report version 0, not an error")
}

func TestGetLatestVersionAfterCommitsReadsWorkingMeta(t *testing.T) {
	dir := t.TempDir()
	dbDir := filepath.Join(dir, flatkvRootDir)

	cfg := config.DefaultConfig()
	cfg.DataDir = dbDir
	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	commitStorageEntry(t, s, ktype.Address{0x01}, ktype.Slot{0x01}, []byte{0xAA})
	commitStorageEntry(t, s, ktype.Address{0x02}, ktype.Slot{0x02}, []byte{0xBB})
	commitStorageEntry(t, s, ktype.Address{0x03}, ktype.Slot{0x03}, []byte{0xCC})

	require.NoError(t, s.Close())

	v, err := GetLatestVersion(dbDir)
	require.NoError(t, err)
	require.Equal(t, int64(3), v,
		"helper must read MetaVersionKey from working/metadata after a clean close")
}

func TestGetLatestVersionMissingKeyReturnsZero(t *testing.T) {
	dir := t.TempDir()
	dbDir := filepath.Join(dir, flatkvRootDir)

	cfg := config.DefaultConfig()
	cfg.DataDir = dbDir
	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)
	require.NoError(t, s.Close())

	v, err := GetLatestVersion(dbDir)
	require.NoError(t, err)
	require.Equal(t, int64(0), v,
		"opened-then-closed-with-no-commits flatkv must report version 0")
}

func TestCommitStoreGetLatestVersionReturnsInMemoryWhenLoaded(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	v, err := s.GetLatestVersion()
	require.NoError(t, err)
	require.Equal(t, int64(0), v)

	commitStorageEntry(t, s, ktype.Address{0x01}, ktype.Slot{0x01}, []byte{0xAA})
	commitStorageEntry(t, s, ktype.Address{0x02}, ktype.Slot{0x02}, []byte{0xBB})

	v, err = s.GetLatestVersion()
	require.NoError(t, err)
	require.Equal(t, int64(2), v,
		"method on an open store must return the in-memory committed version")
}

func TestCommitStoreGetLatestVersionFallsBackToDiskWhenUnloaded(t *testing.T) {
	dir := t.TempDir()
	dbDir := filepath.Join(dir, flatkvRootDir)

	cfg := config.DefaultConfig()
	cfg.DataDir = dbDir
	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)
	commitStorageEntry(t, s, ktype.Address{0x01}, ktype.Slot{0x01}, []byte{0xAA})
	commitStorageEntry(t, s, ktype.Address{0x02}, ktype.Slot{0x02}, []byte{0xBB})
	require.NoError(t, s.Close())

	cfg2 := config.DefaultConfig()
	cfg2.DataDir = dbDir
	s2, err := NewCommitStore(context.Background(), cfg2)
	require.NoError(t, err)
	defer s2.Close()

	v, err := s2.GetLatestVersion()
	require.NoError(t, err)
	require.Equal(t, int64(2), v,
		"method on a not-yet-opened store must fall through to the on-disk helper")
}
