package flatkv

import (
	"context"
	"encoding/binary"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
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

		require.NoError(t, db.Set(metaVersionKey, versionToBytes(42), types.WriteOptions{}))

		// Load it back
		loaded, err := loadLocalMeta(db)
		require.NoError(t, err)
		require.Equal(t, int64(42), loaded.CommittedVersion)
		require.Nil(t, loaded.LtHash)
	})

	t.Run("CorruptedVersion_ReturnsError", func(t *testing.T) {
		db := setupTestDB(t)
		defer db.Close()

		require.NoError(t, db.Set(metaVersionKey, []byte{0x01, 0x02}, types.WriteOptions{}))

		_, err := loadLocalMeta(db)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid meta version length")
	})
}

func TestStoreCommitBatchesUpdatesLocalMeta(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := Address{0x12}
	slot := Slot{0x34}
	key := memiavlStorageKey(addr, slot)

	cs := makeChangeSet(key, []byte{0x56}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	v := commitAndCheck(t, s)
	require.Equal(t, int64(1), v)

	// LocalMeta should be updated
	require.Equal(t, int64(1), s.localMeta[storageDBDir].CommittedVersion)

	// Verify it's persisted in DB
	data, err := s.storageDB.Get(metaVersionKey)
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
		err := s.metadataDB.Set(metaVersionKey, []byte{0x01}, types.WriteOptions{})
		require.NoError(t, err)

		// Should return error
		_, err = s.loadGlobalVersion()
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid global version length")
	})
}

// =============================================================================
// Global Metadata Persistence After Commit + Reopen
// =============================================================================

func TestGlobalMetadataPersistence(t *testing.T) {
	dir := t.TempDir()
	dbDir := filepath.Join(dir, flatkvRootDir)

	cfg := DefaultConfig()
	cfg.DataDir = dbDir
	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)

	commitStorageEntry(t, s, Address{0x01}, Slot{0x01}, []byte{0xAA})
	commitStorageEntry(t, s, Address{0x02}, Slot{0x02}, []byte{0xBB})

	globalVer, err := s.loadGlobalVersion()
	require.NoError(t, err)
	require.Equal(t, int64(2), globalVer)

	globalHash, err := s.loadGlobalLtHash()
	require.NoError(t, err)
	require.Equal(t, s.committedLtHash.Checksum(), globalHash.Checksum())

	expectedHash := s.committedLtHash.Checksum()
	require.NoError(t, s.Close())

	cfg2 := DefaultConfig()
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
