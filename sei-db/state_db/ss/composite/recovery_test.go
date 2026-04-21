package composite

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/backend"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/cosmos"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/evm"
	"github.com/sei-protocol/sei-chain/sei-db/wal"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func newCompositeStateStoreWithStores(
	cosmosStore types.StateStore,
	evmStore types.StateStore,
	ssConfig config.StateStoreConfig,
) *CompositeStateStore {
	return &CompositeStateStore{
		cosmosStore: cosmosStore,
		evmStore:    evmStore,
		config:      ssConfig,
	}
}

// TestEVMSSDirectoryCheck verifies the pre-open guard: a populated Cosmos SS
// alongside a missing or empty EVM SS dir must abort NewCompositeStateStore.
func TestEVMSSDirectoryCheck(t *testing.T) {
	dir := t.TempDir()

	ssConfig := config.DefaultStateStoreConfig()
	ssConfig.Backend = "pebbledb"
	dbHome := utils.GetStateStorePath(dir, ssConfig.Backend)
	mvccDB, err := backend.ResolveBackend(ssConfig.Backend)(dbHome, ssConfig)
	require.NoError(t, err)
	cosmosStore := cosmos.NewCosmosStateStore(mvccDB)

	// Populate Cosmos SS so GetLatestVersion() > 0.
	require.NoError(t, cosmosStore.ApplyChangesetSync(10, []*proto.NamedChangeSet{
		{Name: "bank", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("k"), Value: []byte("v")},
		}}},
	}))
	require.NoError(t, cosmosStore.SetLatestVersion(10))
	require.NoError(t, cosmosStore.Close())

	// Missing EVM SS dir while Cosmos SS has history → reject.
	ssConfig.EVMSplit = true
	ssConfig.EVMDBDirectory = filepath.Join(dir, "evm_ss_missing")
	_, err = NewCompositeStateStore(ssConfig, dir)
	require.Error(t, err)
	require.Contains(t, err.Error(), "EVM SS directory")
	require.Contains(t, err.Error(), "does not exist")

	// Empty EVM SS dir while Cosmos SS has history → also reject.
	emptyDir := filepath.Join(dir, "evm_ss_empty")
	require.NoError(t, os.MkdirAll(emptyDir, 0o755))
	ssConfig.EVMDBDirectory = emptyDir
	_, err = NewCompositeStateStore(ssConfig, dir)
	require.Error(t, err)
	require.Contains(t, err.Error(), "is empty")
}

// TestEVMSSPostRecoveryEarliestMismatch verifies the post-recovery guard:
// diverging earliest versions between Cosmos SS and EVM SS must abort startup.
func TestEVMSSPostRecoveryEarliestMismatch(t *testing.T) {
	cosmos := &fakeStateStore{latest: 100, earliest: 50}
	evm := &fakeStateStore{latest: 100, earliest: 75}
	cs := newCompositeStateStoreWithStores(cosmos, evm, config.StateStoreConfig{EVMSplit: true})
	err := cs.validateEVMSSPostRecovery()
	require.Error(t, err)
	require.Contains(t, err.Error(), "earliest version")

	// Matching earliest → pass.
	evm.earliest = 50
	require.NoError(t, cs.validateEVMSSPostRecovery())

	// Both zero → pass (fresh DBs).
	cosmos.earliest = 0
	evm.earliest = 0
	require.NoError(t, cs.validateEVMSSPostRecovery())
}

// fakeStateStore is a minimal types.StateStore used only for the earliest/latest
// version probes in validation tests.
type fakeStateStore struct {
	types.StateStore
	latest, earliest int64
}

func (f *fakeStateStore) GetLatestVersion() int64   { return f.latest }
func (f *fakeStateStore) GetEarliestVersion() int64 { return f.earliest }

func TestRecoverCompositeStateStore(t *testing.T) {
	dir, err := os.MkdirTemp("", "composite_recovery_test")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	ssConfig := config.DefaultStateStoreConfig()
	ssConfig.Backend = "pebbledb"
	dbHome := utils.GetStateStorePath(dir, ssConfig.Backend)
	mvccDB, err := backend.ResolveBackend(ssConfig.Backend)(dbHome, ssConfig)
	require.NoError(t, err)
	cosmosStore := cosmos.NewCosmosStateStore(mvccDB)
	defer cosmosStore.Close()

	ssConfig.EVMSplit = true
	ssConfig.EVMDBDirectory = filepath.Join(dir, "evm_ss")

	evmStore, err := evm.NewEVMStateStore(ssConfig.EVMDBDirectory, ssConfig)
	require.NoError(t, err)
	defer evmStore.Close()

	compositeStore := newCompositeStateStoreWithStores(cosmosStore, evmStore, ssConfig)
	defer compositeStore.Close()

	changelogDir := filepath.Join(dir, "changelog")
	walLog, err := wal.NewChangelogWAL(changelogDir, wal.Config{})
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
					Changeset: proto.ChangeSet{
						Pairs: []*proto.KVPair{
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

	err = RecoverCompositeStateStore(changelogDir, compositeStore)
	require.NoError(t, err)

	// Under EVMSplit=true, EVM data lives exclusively in the EVM store.
	evmVal, err := compositeStore.Get(evm.EVMStoreKey, 5, evmKey)
	require.NoError(t, err)
	require.Equal(t, evmValue, evmVal)

	evmStoreVal, err := compositeStore.evmStore.Get(evm.EVMStoreKey, 5, evmKey)
	require.NoError(t, err)
	require.Equal(t, evmValue, evmStoreVal)

	require.Equal(t, int64(5), compositeStore.GetLatestVersion())
}

func TestSyncEVMStoreBehind(t *testing.T) {
	dir, err := os.MkdirTemp("", "composite_sync_test")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	ssConfig := config.DefaultStateStoreConfig()
	ssConfig.Backend = "pebbledb"
	dbHome := utils.GetStateStorePath(dir, ssConfig.Backend)
	mvccDB, err := backend.ResolveBackend(ssConfig.Backend)(dbHome, ssConfig)
	require.NoError(t, err)
	cosmosStore := cosmos.NewCosmosStateStore(mvccDB)

	addr := make([]byte, 20)
	slot := make([]byte, 32)
	evmKey := append(evmtypes.StateKeyPrefix, append(addr, slot...)...)

	// Seed cosmos store directly to simulate a node that previously ran with
	// everything in cosmos, then switched to split mode. The WAL still contains
	// the EVM entries, so recovery should catch up the EVM sub-store.
	for version := int64(1); version <= 10; version++ {
		changeset := []*proto.NamedChangeSet{
			{
				Name: evm.EVMStoreKey,
				Changeset: proto.ChangeSet{
					Pairs: []*proto.KVPair{
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
	walLog, err := wal.NewChangelogWAL(changelogDir, wal.Config{})
	require.NoError(t, err)

	for version := int64(1); version <= 10; version++ {
		entry := proto.ChangelogEntry{
			Version: version,
			Changesets: []*proto.NamedChangeSet{
				{
					Name: evm.EVMStoreKey,
					Changeset: proto.ChangeSet{
						Pairs: []*proto.KVPair{
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

	ssConfig.EVMSplit = true
	ssConfig.EVMDBDirectory = filepath.Join(dir, "evm_ss")

	evmStore, err := evm.NewEVMStateStore(ssConfig.EVMDBDirectory, ssConfig)
	require.NoError(t, err)

	compositeStore := newCompositeStateStoreWithStores(cosmosStore, evmStore, ssConfig)
	defer compositeStore.Close()

	require.Equal(t, int64(0), compositeStore.evmStore.GetLatestVersion())
	require.Equal(t, int64(10), compositeStore.cosmosStore.GetLatestVersion())

	err = RecoverCompositeStateStore(changelogDir, compositeStore)
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
			Changeset: proto.ChangeSet{
				Pairs: []*proto.KVPair{
					{Key: storageKey, Value: []byte("storage_val")},
					{Key: nonceKey, Value: []byte("nonce_val")},
				},
			},
		},
		{
			Name: "bank",
			Changeset: proto.ChangeSet{
				Pairs: []*proto.KVPair{
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
