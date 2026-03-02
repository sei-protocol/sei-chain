package composite

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/logger"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/backend"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/cosmos"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/evm"
	"github.com/sei-protocol/sei-chain/sei-db/wal"
	iavl "github.com/sei-protocol/sei-chain/sei-iavl"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

func newCompositeStateStoreWithStores(
	cosmosStore types.StateStore,
	evmStore types.StateStore,
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

	ssConfig := config.DefaultStateStoreConfig()
	ssConfig.Backend = "pebbledb"
	dbHome := utils.GetStateStorePath(dir, ssConfig.Backend)
	mvccDB, err := backend.ResolveBackend(ssConfig.Backend)(dbHome, ssConfig)
	require.NoError(t, err)
	cosmosStore := cosmos.NewCosmosStateStore(mvccDB)
	defer cosmosStore.Close()

	ssConfig.WriteMode = config.DualWrite
	ssConfig.ReadMode = config.EVMFirstRead
	ssConfig.EVMDBDirectory = filepath.Join(dir, "evm_ss")

	evmStore, err := evm.NewEVMStateStore(ssConfig.EVMDBDirectory, ssConfig, log)
	require.NoError(t, err)
	defer evmStore.Close()

	compositeStore := newCompositeStateStoreWithStores(cosmosStore, evmStore, ssConfig, log)
	defer compositeStore.Close()

	changelogDir := filepath.Join(dir, "changelog")
	walLog, err := wal.NewChangelogWAL(log, changelogDir, wal.Config{})
	require.NoError(t, err)

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

	err = RecoverCompositeStateStore(log, changelogDir, compositeStore)
	require.NoError(t, err)

	cosmosVal, err := compositeStore.cosmosStore.Get(evm.EVMStoreKey, 5, evmKey)
	require.NoError(t, err)
	require.Equal(t, evmValue, cosmosVal)

	evmVal, err := compositeStore.Get(evm.EVMStoreKey, 5, evmKey)
	require.NoError(t, err)
	require.Equal(t, evmValue, evmVal)

	require.Equal(t, int64(5), compositeStore.GetLatestVersion())
}

func TestSyncEVMStoreBehind(t *testing.T) {
	dir, err := os.MkdirTemp("", "composite_sync_test")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	log := logger.NewNopLogger()

	ssConfig := config.DefaultStateStoreConfig()
	ssConfig.Backend = "pebbledb"
	dbHome := utils.GetStateStorePath(dir, ssConfig.Backend)
	mvccDB, err := backend.ResolveBackend(ssConfig.Backend)(dbHome, ssConfig)
	require.NoError(t, err)
	cosmosStore := cosmos.NewCosmosStateStore(mvccDB)

	addr := make([]byte, 20)
	slot := make([]byte, 32)
	evmKey := append(evmtypes.StateKeyPrefix, append(addr, slot...)...)

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

	ssConfig.WriteMode = config.DualWrite
	ssConfig.ReadMode = config.EVMFirstRead
	ssConfig.EVMDBDirectory = filepath.Join(dir, "evm_ss")

	evmStore, err := evm.NewEVMStateStore(ssConfig.EVMDBDirectory, ssConfig, log)
	require.NoError(t, err)

	compositeStore := newCompositeStateStoreWithStores(cosmosStore, evmStore, ssConfig, log)
	defer compositeStore.Close()

	require.Equal(t, int64(0), compositeStore.evmStore.GetLatestVersion())
	require.Equal(t, int64(10), compositeStore.cosmosStore.GetLatestVersion())

	err = RecoverCompositeStateStore(log, changelogDir, compositeStore)
	require.NoError(t, err)

	require.Equal(t, int64(10), compositeStore.evmStore.GetLatestVersion())

	val, err := compositeStore.evmStore.Get("evm", 10, evmKey)
	require.NoError(t, err)
	require.Equal(t, []byte{10}, val)
}

func TestExtractEVMChanges(t *testing.T) {
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
			Name: "bank",
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{
					{Key: nonEvmKey, Value: []byte("bank_val")},
				},
			},
		},
	}

	evmCS := filterEVMChangesets(changesets)
	require.Len(t, evmCS, 1)
	require.Equal(t, evm.EVMStoreKey, evmCS[0].Name)
	require.Len(t, evmCS[0].Changeset.Pairs, 2)
}

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

	mvccDB, err := backend.ResolveBackend(ssConfig.Backend)(dbHome, ssConfig)
	require.NoError(t, err)

	addr := make([]byte, 20)
	addr[0] = 0x55
	slot := make([]byte, 32)
	evmKey := append(evmtypes.StateKeyPrefix, append(addr, slot...)...)

	for v := int64(1); v <= 5; v++ {
		err := mvccDB.ApplyChangesetSync(v, []*proto.NamedChangeSet{
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
		require.NoError(t, mvccDB.SetLatestVersion(v))
	}
	mvccDB.Close()

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

	compositeStore, err := NewCompositeStateStore(ssConfig, dir, log)
	require.NoError(t, err)
	defer compositeStore.Close()

	require.Equal(t, int64(5), compositeStore.evmStore.GetLatestVersion(),
		"EVM_SS should be caught up to Cosmos version after constructor recovery")

	evmVal, err := compositeStore.evmStore.Get("evm", 5, evmKey)
	require.NoError(t, err)
	require.Equal(t, []byte{5}, evmVal)
}
