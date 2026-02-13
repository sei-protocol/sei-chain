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
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/types"
	"github.com/sei-protocol/sei-chain/sei-db/wal"
	"github.com/cosmos/iavl"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

// newCompositeStateStoreWithStores is a test helper that creates a composite store
// from pre-created stores without triggering auto-recovery or pruning.
func newCompositeStateStoreWithStores(
	cosmosStore types.StateStore,
	evmStore *evm.EVMStateStore,
	ssConfig config.StateStoreConfig,
	log logger.Logger,
) *CompositeStateStore {
	return &CompositeStateStore{
		cosmosStore: cosmosStore,
		evmStore:    evmStore,
		config:      ssConfig,
		logger:      log,
	}
}

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
	ssConfig.WriteMode = config.DualWrite
	ssConfig.ReadMode = config.EVMFirstRead
	ssConfig.EVMDBDirectory = filepath.Join(dir, "evm_ss")

	evmStore, err := evm.NewEVMStateStore(ssConfig.EVMDBDirectory, log)
	require.NoError(t, err)
	defer evmStore.Close()

	// Create composite store using test helper (no auto-recovery)
	compositeStore := newCompositeStateStoreWithStores(cosmosStore, evmStore, ssConfig, log)
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
	ssConfig.WriteMode = config.DualWrite
	ssConfig.ReadMode = config.EVMFirstRead
	ssConfig.EVMDBDirectory = filepath.Join(dir, "evm_ss")

	evmStore, err := evm.NewEVMStateStore(ssConfig.EVMDBDirectory, log)
	require.NoError(t, err)

	// Create composite store using test helper - EVM store starts at version 0
	compositeStore := newCompositeStateStoreWithStores(cosmosStore, evmStore, ssConfig, log)
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

// TestConstructorRecoversStalEVM verifies Bug 2 fix:
// NewCompositeStateStore itself recovers EVM_SS when it's behind Cosmos_SS,
// using RecoverCompositeStateStore (not the old recoverFromWAL).
func TestConstructorRecoversStalEVM(t *testing.T) {
	dir, err := os.MkdirTemp("", "constructor_recovery_test")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	log := logger.NewNopLogger()

	ssConfig := config.DefaultStateStoreConfig()
	ssConfig.Backend = "pebbledb"
	dbHome := utils.GetStateStorePath(dir, ssConfig.Backend)

	ssConfig.WriteMode = config.DualWrite
	ssConfig.ReadMode = config.EVMFirstRead
	ssConfig.EVMDBDirectory = filepath.Join(dir, "evm_ss")

	// Step 1: Create cosmos store directly and write 5 versions
	cosmosStore, err := mvcc.OpenDB(dbHome, ssConfig)
	require.NoError(t, err)

	addr := make([]byte, 20)
	addr[0] = 0x55
	slot := make([]byte, 32)
	evmKey := append(evmtypes.StateKeyPrefix, append(addr, slot...)...)

	for v := int64(1); v <= 5; v++ {
		err := cosmosStore.ApplyChangesetSync(v, []*proto.NamedChangeSet{
			{
				Name: evm.EVMStoreKey,
				Changeset: iavl.ChangeSet{
					Pairs: []*iavl.KVPair{
						{Key: evmKey, Value: []byte{byte(v)}},
					},
				},
			},
		})
		require.NoError(t, err)
		require.NoError(t, cosmosStore.SetLatestVersion(v))
	}
	cosmosStore.Close()

	// Step 2: Write matching WAL entries so recovery can replay
	changelogPath := utils.GetChangelogPath(dbHome)
	walLog, err := wal.NewChangelogWAL(log, changelogPath, wal.Config{})
	require.NoError(t, err)
	for v := int64(1); v <= 5; v++ {
		require.NoError(t, walLog.Write(proto.ChangelogEntry{
			Version: v,
			Changesets: []*proto.NamedChangeSet{
				{
					Name: evm.EVMStoreKey,
					Changeset: iavl.ChangeSet{
						Pairs: []*iavl.KVPair{
							{Key: evmKey, Value: []byte{byte(v)}},
						},
					},
				},
			},
		}))
	}
	walLog.Close()

	// Step 3: Open via NewCompositeStateStore -- EVM_SS starts at v0, Cosmos at v5.
	// The constructor must detect this and replay WAL to catch EVM up.
	compositeStore, err := NewCompositeStateStore(ssConfig, dir, log)
	require.NoError(t, err)
	defer compositeStore.Close()

	// EVM store should now be caught up to version 5
	require.Equal(t, int64(5), compositeStore.evmStore.GetLatestVersion(),
		"EVM_SS should be caught up to Cosmos version after constructor recovery")

	// Data should be readable from EVM_SS directly
	evmVal, err := compositeStore.evmStore.Get(evmKey, 5)
	require.NoError(t, err)
	require.Equal(t, []byte{5}, evmVal,
		"EVM_SS should have data after constructor recovery")
}
