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
	dbm "github.com/tendermint/tm-db"
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

// TestEVMSSDirectoryCheck: populated Cosmos SS + missing/empty EVM SS dir must abort startup.
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

// TestEVMSSPreRecoveryAfterStateSync: state-sync restore only sets earliestVersion on
// the cosmos SS; latestVersion stays 0 until the first post-sync block commit. The guard
// must still fire against an empty EVM SS in this window.
func TestEVMSSPreRecoveryAfterStateSync(t *testing.T) {
	cosmos := &fakeStateStore{latest: 0, earliest: 100}
	evm := &fakeStateStore{latest: 0, earliest: 0}
	cs := newCompositeStateStoreWithStores(cosmos, evm, config.StateStoreConfig{EVMSplit: true})
	err := cs.validateEVMSSPreRecovery()
	require.Error(t, err)
	require.Contains(t, err.Error(), "EVM SS is empty")
}

// TestEVMSSPostRecoveryEarliestMismatch: diverging earliest versions are allowed
// because the composite reports the highest member floor.
func TestEVMSSPostRecoveryEarliestMismatch(t *testing.T) {
	cosmos := &fakeStateStore{latest: 100, earliest: 50}
	evm := &fakeStateStore{latest: 100, earliest: 75}
	cs := newCompositeStateStoreWithStores(cosmos, evm, config.StateStoreConfig{EVMSplit: true})
	cs.validateEVMSSPostRecovery()
	require.Equal(t, int64(75), cs.GetEarliestVersion())

	// Matching earliest → pass.
	evm.earliest = 50
	cs.validateEVMSSPostRecovery()
	require.Equal(t, int64(50), cs.GetEarliestVersion())

	// Both zero → pass (fresh DBs).
	cosmos.earliest = 0
	evm.earliest = 0
	cs.validateEVMSSPostRecovery()
	require.Zero(t, cs.GetEarliestVersion())
}

func TestCompositeGetEarliestVersionReportsHighestMemberFloor(t *testing.T) {
	cosmos := &fakeStateStore{latest: 100, earliest: 50}
	evm := &fakeStateStore{latest: 100, earliest: 75}
	cs := newCompositeStateStoreWithStores(cosmos, evm, config.StateStoreConfig{EVMSplit: true})
	require.Equal(t, int64(75), cs.GetEarliestVersion())

	cosmos.earliest = 90
	require.Equal(t, int64(90), cs.GetEarliestVersion())

	cs.evmStore = nil
	require.Equal(t, int64(90), cs.GetEarliestVersion())
}

// TestCompositeReadsBelowFloorDoNotError pins the read contract that keeps a
// prune racing an in-flight query from crashing the node: the cosmos KVStore
// wrapper panics on any read error, so a version below the reported floor must
// route through and report absence instead of returning an error.
func TestCompositeReadsBelowFloorDoNotError(t *testing.T) {
	cosmos := &fakeStateStore{latest: 100, earliest: 50}
	evmStore := &fakeStateStore{latest: 100, earliest: 75}
	cs := newCompositeStateStoreWithStores(cosmos, evmStore, config.StateStoreConfig{EVMSplit: true})
	require.Equal(t, int64(75), cs.GetEarliestVersion())

	value, err := cs.Get("bank", 74, []byte("key"))
	require.NoError(t, err)
	require.Nil(t, value)

	has, err := cs.Has(evm.EVMStoreKey, 74, []byte("key"))
	require.NoError(t, err)
	require.False(t, has)

	_, err = cs.Iterator("bank", 74, nil, nil)
	require.NoError(t, err)

	_, err = cs.ReverseIterator(evm.EVMStoreKey, 74, nil, nil)
	require.NoError(t, err)

	require.Equal(t, 4, cosmos.reads+evmStore.reads, "every read must reach its routed member")
}

// fakeStateStore stubs latest/earliest and absent reads for validator tests.
type fakeStateStore struct {
	types.StateStore
	latest, earliest int64
	reads            int
}

func (f *fakeStateStore) GetLatestVersion() int64   { return f.latest }
func (f *fakeStateStore) GetEarliestVersion() int64 { return f.earliest }

func (f *fakeStateStore) Get(string, int64, []byte) ([]byte, error) {
	f.reads++
	return nil, nil
}

func (f *fakeStateStore) Has(string, int64, []byte) (bool, error) {
	f.reads++
	return false, nil
}

func (f *fakeStateStore) Iterator(string, int64, []byte, []byte) (dbm.Iterator, error) {
	f.reads++
	return nil, nil
}

func (f *fakeStateStore) ReverseIterator(string, int64, []byte, []byte) (dbm.Iterator, error) {
	f.reads++
	return nil, nil
}

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
