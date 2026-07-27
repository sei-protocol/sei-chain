package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	seidbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	flatkvconfig "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss"
	sscomposite "github.com/sei-protocol/sei-chain/sei-db/state_db/ss/composite"
	"github.com/stretchr/testify/require"
)

func makeFlatKVSnapshots(t *testing.T, root string, heights ...int64) {
	t.Helper()
	for _, h := range heights {
		require.NoError(t, os.MkdirAll(filepath.Join(root, fmt.Sprintf("%s%020d", flatKVArchiveSnapshotPref, h)), 0o755))
	}
}

func makeSSCheckpoints(t *testing.T, stateStoreDir string, versions ...int64) {
	t.Helper()
	for _, v := range versions {
		dir := filepath.Join(stateStoreDir, sscomposite.CheckpointsDirName, sscomposite.CheckpointDirName(v))
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "cosmos", "pebbledb"), 0o755))
	}
}

func TestSelectStateStoreSourcePrefersNewestCheckpoint(t *testing.T) {
	stateStoreDir := t.TempDir()
	makeSSCheckpoints(t, stateStoreDir, 100, 200)

	source, name, version, err := selectStateStoreSource(stateStoreDir)
	require.NoError(t, err)
	require.Equal(t, sscomposite.CheckpointDirName(200), name)
	require.Equal(t, int64(200), version)
	require.Equal(t,
		filepath.Join(stateStoreDir, sscomposite.CheckpointsDirName, sscomposite.CheckpointDirName(200)),
		source)
}

func TestSelectStateStoreSourceFallsBackToLiveDir(t *testing.T) {
	stateStoreDir := t.TempDir()

	source, name, version, err := selectStateStoreSource(stateStoreDir)
	require.NoError(t, err)
	require.Empty(t, name)
	require.Zero(t, version)
	require.Equal(t, stateStoreDir, source)
}

func TestSelectFlatKVSnapshotAtMost(t *testing.T) {
	root := t.TempDir()
	makeFlatKVSnapshots(t, root, 100, 200, 300)

	// The state-store checkpoint's label bounds the FlatKV height from above:
	// pairing H <= label is what guarantees the restored query store is
	// complete up to the archive height.
	dir, name, height, err := selectFlatKVSnapshotAtMost(root, 250)
	require.NoError(t, err)
	require.Equal(t, int64(200), height)
	require.Equal(t, fmt.Sprintf("%s%020d", flatKVArchiveSnapshotPref, int64(200)), name)
	require.Equal(t, filepath.Join(root, name), dir)

	// Exact match is allowed.
	_, _, height, err = selectFlatKVSnapshotAtMost(root, 300)
	require.NoError(t, err)
	require.Equal(t, int64(300), height)

	// No snapshot at or below the checkpoint label: refuse rather than ship
	// an archive whose query store has holes below the archive height.
	_, _, _, err = selectFlatKVSnapshotAtMost(root, 50)
	require.Error(t, err)
}

// TestRebuildStateStoreFromFlatKV covers the single-source-of-truth restore
// path: commit cosmos-module and EVM data into a FlatKV store, rebuild a
// fresh state_store from it, and verify every logical entry is queryable at
// the archive height through the SS read API.
func TestRebuildStateStoreFromFlatKV(t *testing.T) {
	home := t.TempDir()
	flatKVRoot := filepath.Join(home, "data", "state_commit", "flatkv")

	bankKey := []byte("balances/addr1")
	bankValue := []byte("100usei")
	stakingKey := []byte("validators/val1")
	stakingValue := []byte("validator-record")
	var addr ktype.Address
	addr[19] = 0x42
	var slot ktype.Slot
	slot[31] = 0x01
	storageValue := make([]byte, 32)
	storageValue[31] = 0x07
	storageKey := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot))

	fcfg := flatkvconfig.DefaultConfig()
	fcfg.DataDir = flatKVRoot
	store, err := flatkv.NewCommitStore(t.Context(), fcfg)
	require.NoError(t, err)
	_, err = store.LoadVersion(0, false)
	require.NoError(t, err)
	require.NoError(t, store.ApplyChangeSets([]*proto.NamedChangeSet{
		{Name: "bank", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: bankKey, Value: bankValue},
		}}},
		{Name: "staking", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: stakingKey, Value: stakingValue},
		}}},
		{Name: "evm", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: storageKey, Value: storageValue},
		}}},
	}))
	height, err := store.Commit()
	require.NoError(t, err)
	require.NoError(t, store.Close())

	ssConfig := seidbconfig.DefaultStateStoreConfig()
	entries, err := rebuildStateStoreFromFlatKV(t.Context(), home, flatKVRoot, height, ssConfig)
	require.NoError(t, err)
	require.Equal(t, int64(3), entries)

	// A height mismatch must refuse to rebuild rather than mislabel versions.
	_, err = rebuildStateStoreFromFlatKV(t.Context(), home, flatKVRoot, height+1, ssConfig)
	require.Error(t, err)

	ssStore, err := ss.NewStateStore(home, ssConfig)
	require.NoError(t, err)
	defer func() { require.NoError(t, ssStore.Close()) }()

	got, err := ssStore.Get("bank", height, bankKey)
	require.NoError(t, err)
	require.Equal(t, bankValue, got)
	got, err = ssStore.Get("staking", height, stakingKey)
	require.NoError(t, err)
	require.Equal(t, stakingValue, got)
	got, err = ssStore.Get("evm", height, storageKey)
	require.NoError(t, err)
	require.Equal(t, storageValue, got)

	require.Equal(t, height, ssStore.GetLatestVersion())
	require.Equal(t, height, ssStore.GetEarliestVersion())
}

func TestArchiveHasStateStore(t *testing.T) {
	require.False(t, archiveHasStateStore(&flatKVArchiveManifest{Files: []flatKVArchiveFileEntry{
		{Path: "flatkv/snapshot-1/misc/000001.sst"},
	}}))
	require.True(t, archiveHasStateStore(&flatKVArchiveManifest{Files: []flatKVArchiveFileEntry{
		{Path: "flatkv/snapshot-1/misc/000001.sst"},
		{Path: "state_store/cosmos/pebbledb/000001.sst"},
	}}))
}
