package composite

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/logger"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb/mvcc"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/evm"
	"github.com/sei-protocol/sei-chain/sei-db/wal"
	iavl "github.com/sei-protocol/sei-chain/sei-iavl"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

func TestRecoverCompositeStateStore(t *testing.T) {
	dir, err := os.MkdirTemp("", "composite_recovery_test")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	log := logger.NewNopLogger()

	// Create cosmos store directly (without pruning for testing)
	ssConfig := config.DefaultStateStoreConfig()
	ssConfig.Backend = "pebbledb"
	dbHome := utils.GetStateStorePath(dir, ssConfig.Backend)
	cosmosStore, err := mvcc.OpenDB(dbHome, ssConfig)
	require.NoError(t, err)
	defer cosmosStore.Close()

	// Create EVM store directly
	evmConfig := config.EVMStateStoreConfig{
		Enable:      true,
		EnableRead:  true,
		EnableWrite: true,
		DBDirectory: filepath.Join(dir, "evm_ss"),
	}
	evmStore, err := evm.NewEVMStateStore(evmConfig.DBDirectory)
	require.NoError(t, err)
	defer evmStore.Close()

	// Create composite store using test helper (no auto-recovery)
	compositeStore := newCompositeStateStoreWithStores(cosmosStore, evmStore, ssConfig, evmConfig, log)
	defer compositeStore.Close()

	// Create WAL and write some entries
	changelogDir := filepath.Join(dir, "changelog")
	walLog, err := wal.NewChangelogWAL(log, changelogDir, wal.Config{})
	require.NoError(t, err)

	// Create test data - EVM storage key
	addr := make([]byte, 20)
	for i := range addr {
		addr[i] = byte(i)
	}
	slot := make([]byte, 32)
	for i := range slot {
		slot[i] = byte(i + 100)
	}
	evmKey := append(evmtypes.StateKeyPrefix, append(addr, slot...)...)
	evmValue := []byte("test_value")

	// Write WAL entries
	for version := int64(1); version <= 5; version++ {
		entry := proto.ChangelogEntry{
			Version: version,
			Changesets: []*proto.NamedChangeSet{
				{
					Name: evm.EVMStoreKey,
					Changeset: iavl.ChangeSet{
						Pairs: []*iavl.KVPair{
							{Key: evmKey, Value: evmValue},
						},
					},
				},
			},
		}
		err := walLog.Write(entry)
		require.NoError(t, err)
	}
	walLog.Close()

	// Run recovery
	err = RecoverCompositeStateStore(log, changelogDir, compositeStore)
	require.NoError(t, err)

	// Verify data was recovered to both stores
	// Check cosmos store
	cosmosVal, err := compositeStore.cosmosStore.Get(evm.EVMStoreKey, 5, evmKey)
	require.NoError(t, err)
	require.Equal(t, evmValue, cosmosVal)

	// Check EVM store (via composite)
	evmVal, err := compositeStore.Get(evm.EVMStoreKey, 5, evmKey)
	require.NoError(t, err)
	require.Equal(t, evmValue, evmVal)

	// Verify versions
	require.Equal(t, int64(5), compositeStore.GetLatestVersion())
}

func TestSyncEVMStoreBehind(t *testing.T) {
	dir, err := os.MkdirTemp("", "composite_sync_test")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	log := logger.NewNopLogger()

	// Create cosmos store directly
	ssConfig := config.DefaultStateStoreConfig()
	ssConfig.Backend = "pebbledb"
	dbHome := utils.GetStateStorePath(dir, ssConfig.Backend)
	cosmosStore, err := mvcc.OpenDB(dbHome, ssConfig)
	require.NoError(t, err)

	// Create test EVM key
	addr := make([]byte, 20)
	slot := make([]byte, 32)
	evmKey := append(evmtypes.StateKeyPrefix, append(addr, slot...)...)

	// Write directly to cosmos store (simulating EVM store being behind)
	for version := int64(1); version <= 10; version++ {
		changeset := []*proto.NamedChangeSet{
			{
				Name: evm.EVMStoreKey,
				Changeset: iavl.ChangeSet{
					Pairs: []*iavl.KVPair{
						{Key: evmKey, Value: []byte{byte(version)}},
					},
				},
			},
		}
		err := cosmosStore.ApplyChangesetSync(version, changeset)
		require.NoError(t, err)
		err = cosmosStore.SetLatestVersion(version)
		require.NoError(t, err)
	}

	// Create WAL with same entries
	changelogDir := filepath.Join(dir, "changelog")
	walLog, err := wal.NewChangelogWAL(log, changelogDir, wal.Config{})
	require.NoError(t, err)

	for version := int64(1); version <= 10; version++ {
		entry := proto.ChangelogEntry{
			Version: version,
			Changesets: []*proto.NamedChangeSet{
				{
					Name: evm.EVMStoreKey,
					Changeset: iavl.ChangeSet{
						Pairs: []*iavl.KVPair{
							{Key: evmKey, Value: []byte{byte(version)}},
						},
					},
				},
			},
		}
		err := walLog.Write(entry)
		require.NoError(t, err)
	}
	walLog.Close()

	// Create EVM store (fresh, at version 0)
	evmConfig := config.EVMStateStoreConfig{
		Enable:      true,
		EnableRead:  true,
		EnableWrite: true,
		DBDirectory: filepath.Join(dir, "evm_ss"),
	}
	evmStore, err := evm.NewEVMStateStore(evmConfig.DBDirectory)
	require.NoError(t, err)

	// Create composite store using test helper - EVM store starts at version 0
	compositeStore := newCompositeStateStoreWithStores(cosmosStore, evmStore, ssConfig, evmConfig, log)
	defer compositeStore.Close()

	// Verify EVM store is behind
	require.Equal(t, int64(0), compositeStore.evmStore.GetLatestVersion())
	require.Equal(t, int64(10), compositeStore.cosmosStore.GetLatestVersion())

	// Run recovery - should sync EVM store
	err = RecoverCompositeStateStore(log, changelogDir, compositeStore)
	require.NoError(t, err)

	// Verify EVM store is now caught up
	require.Equal(t, int64(10), compositeStore.evmStore.GetLatestVersion())

	// Verify data in EVM store
	val, err := compositeStore.evmStore.Get(evmKey, 10)
	require.NoError(t, err)
	require.Equal(t, []byte{10}, val)
}

func TestExtractEVMChanges(t *testing.T) {
	// Create test keys
	addr := make([]byte, 20)
	slot := make([]byte, 32)
	storageKey := append(evmtypes.StateKeyPrefix, append(addr, slot...)...)
	nonceKey := append(evmtypes.NonceKeyPrefix, addr...)
	nonEvmKey := []byte("some_other_key")

	changesets := []*proto.NamedChangeSet{
		{
			Name: evm.EVMStoreKey,
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{
					{Key: storageKey, Value: []byte("storage_val")},
					{Key: nonceKey, Value: []byte("nonce_val")},
				},
			},
		},
		{
			Name: "bank", // non-EVM module
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{
					{Key: nonEvmKey, Value: []byte("bank_val")},
				},
			},
		},
	}

	evmChanges := extractEVMChangesFromChangesets(changesets)

	// Should have storage and nonce changes
	require.Len(t, evmChanges, 2)
	require.Len(t, evmChanges[evm.StoreStorage], 1)
	require.Len(t, evmChanges[evm.StoreNonce], 1)

	// Verify keys are stripped of prefix
	require.Equal(t, append(addr, slot...), evmChanges[evm.StoreStorage][0].Key)
	require.Equal(t, addr, evmChanges[evm.StoreNonce][0].Key)
}
