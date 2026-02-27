package flatkv

import (
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

		// Write metadata
		original := &LocalMeta{CommittedVersion: 42}
		err := db.Set(DBLocalMetaKey, MarshalLocalMeta(original), types.WriteOptions{})
		require.NoError(t, err)

		// Load it back
		loaded, err := loadLocalMeta(db)
		require.NoError(t, err)
		require.Equal(t, original.CommittedVersion, loaded.CommittedVersion)
	})

	t.Run("CorruptedMeta_ReturnsError", func(t *testing.T) {
		db := setupTestDB(t)
		defer db.Close()

		// Write invalid data (wrong size)
		err := db.Set(DBLocalMetaKey, []byte{0x01, 0x02}, types.WriteOptions{})
		require.NoError(t, err)

		// Should fail to load
		_, err = loadLocalMeta(db)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid LocalMeta size")
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
	data, err := s.storageDB.Get(DBLocalMetaKey)
	require.NoError(t, err)
	meta, err := UnmarshalLocalMeta(data)
	require.NoError(t, err)
	require.Equal(t, int64(1), meta.CommittedVersion)
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
		err := s.metadataDB.Set([]byte(MetaGlobalVersion), []byte{0x01}, types.WriteOptions{})
		require.NoError(t, err)

		// Should return error
		_, err = s.loadGlobalVersion()
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid global version length")
	})
}
