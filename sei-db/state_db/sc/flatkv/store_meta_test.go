package flatkv

import (
	"context"
	"encoding/binary"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
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

		db.Set(metaVersionKey, versionToBytes(42))

		// Load it back
		loaded, err := loadLocalMeta(db)
		require.NoError(t, err)
		require.Equal(t, int64(42), loaded.CommittedVersion)
		require.Nil(t, loaded.LtHash)
	})

	t.Run("CorruptedVersion_ReturnsError", func(t *testing.T) {
		db := setupTestDB(t)
		defer db.Close()

		db.Set(metaVersionKey, []byte{0x01, 0x02})

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
	data, found, err := s.storageDB.Get(metaVersionKey, true)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, int64(1), int64(binary.BigEndian.Uint64(data)))
}

func TestStoreMetadataOperations(t *testing.T) {
	t.Run("LoadLocalMeta_NewMetadataDB", func(t *testing.T) {
		s := setupTestStore(t)
		defer s.Close()

		meta, err := loadLocalMeta(s.metadataDB)
		require.NoError(t, err)
		require.Equal(t, int64(0), meta.CommittedVersion)
		require.Nil(t, meta.LtHash)
	})

	t.Run("MetadataDB_RoundTrip_ViaCommitBatches", func(t *testing.T) {
		s := setupTestStore(t)
		defer s.Close()

		addr := Address{0x12}
		slot := Slot{0x34}
		key := memiavlStorageKey(addr, slot)
		cs := makeChangeSet(key, []byte{0x56}, false)
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
		v := commitAndCheck(t, s)
		require.Equal(t, int64(1), v)

		meta, err := loadLocalMeta(s.metadataDB)
		require.NoError(t, err)
		require.Equal(t, int64(1), meta.CommittedVersion)
		require.NotNil(t, meta.LtHash)
		require.Equal(t, s.committedLtHash.Marshal(), meta.LtHash.Marshal())
	})

	t.Run("MetadataDB_MultipleCommits", func(t *testing.T) {
		s := setupTestStore(t)
		defer s.Close()

		for v := int64(1); v <= 10; v++ {
			cs := makeChangeSet(
				memiavlStorageKey(Address{byte(v)}, Slot{byte(v)}),
				[]byte{byte(v)}, false)
			require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
			commitAndCheck(t, s)

			meta, err := loadLocalMeta(s.metadataDB)
			require.NoError(t, err)
			require.Equal(t, v, meta.CommittedVersion)
		}
	})

	t.Run("LoadLocalMeta_InvalidData", func(t *testing.T) {
		s := setupTestStore(t)
		defer s.Close()

		s.metadataDB.Set(metaVersionKey, []byte{0x01})

		_, err := loadLocalMeta(s.metadataDB)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid meta version length")
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

	meta, err := loadLocalMeta(s.metadataDB)
	require.NoError(t, err)
	require.Equal(t, int64(2), meta.CommittedVersion)
	require.NotNil(t, meta.LtHash)
	require.Equal(t, s.committedLtHash.Checksum(), meta.LtHash.Checksum())

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
