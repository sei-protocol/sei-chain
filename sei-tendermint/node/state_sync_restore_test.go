package node

import (
	"os"
	"path/filepath"
	"testing"

	gigaconfig "github.com/sei-protocol/sei-chain/giga/config"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	flatkvconfig "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// checkpointWithRow returns the FlatKV checkpoint of a store that committed one block writing
// value at key of module.
func checkpointWithRow(t *testing.T, module string, key, value []byte) string {
	t.Helper()
	return commitRow(t, filepath.Join(t.TempDir(), "flatkv"), module, key, value)
}

// commitRow commits block 1, which writes value at key of module, to a FlatKV store in dataDir.
// It returns the FlatKV checkpoint of that block.
func commitRow(t *testing.T, dataDir string, module string, key, value []byte) string {
	t.Helper()
	cfg := flatkvconfig.DefaultTestConfig(t)
	cfg.DataDir = dataDir
	cfg.SnapshotInterval = 1
	wal, err := flatkv.OpenStateWAL(cfg)
	require.NoError(t, err)
	store, err := flatkv.NewCommitStore(t.Context(), cfg, wal)
	require.NoError(t, err)
	defer func() { require.NoError(t, store.Close()) }()
	require.NoError(t, store.LoadLatest())
	require.NoError(t, store.ApplyChangeSets(1, []*proto.NamedChangeSet{
		{Name: module, Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{{Key: key, Value: value}}}},
	}))
	version, err := store.Commit(1)
	require.NoError(t, err)
	require.NoError(t, store.FlushSnapshots())
	return filepath.Join(cfg.DataDir, flatkv.SnapshotDirName(version))
}

func TestVerifyRestoreProofReturnsTheSignedAppHash(t *testing.T) {
	fixture := utils.OrPanic1(GenStateSyncProofFixture(utils.TestRng(), 5))
	checkpoint := checkpointWithRow(t, fixture.Module, fixture.Key, fixture.Value)

	appHash, err := VerifyRestoreProof(checkpoint, 5, fixture.Proof)
	require.NoError(t, err)
	require.Equal(t, fixture.AppHash, appHash)
}

func TestVerifyRestoreProofRejectsAProofOfAnotherCommittee(t *testing.T) {
	rng := utils.TestRng()
	fixture := utils.OrPanic1(GenStateSyncProofFixture(rng, 5))
	other := utils.OrPanic1(GenStateSyncProofFixture(rng, 5))
	checkpoint := checkpointWithRow(t, fixture.Module, fixture.Key, fixture.Value)

	_, err := VerifyRestoreProof(checkpoint, 5, other.Proof)
	require.Error(t, err)
}

func TestVerifyRestoreProofRejectsARoadOutsideTheRegistry(t *testing.T) {
	fixture := utils.OrPanic1(GenStateSyncProofFixture(utils.TestRng(), 5))
	checkpoint := checkpointWithRow(t, fixture.Module, fixture.Key, fixture.Value)

	_, err := VerifyRestoreProof(checkpoint, 4, fixture.Proof)
	require.Error(t, err)
}

func TestVerifyRestoreProofRejectsACheckpointWithoutARegistry(t *testing.T) {
	fixture := utils.OrPanic1(GenStateSyncProofFixture(utils.TestRng(), 5))
	checkpoint := checkpointWithRow(t, "bank", []byte("balance"), []byte{1})

	_, err := VerifyRestoreProof(checkpoint, 5, fixture.Proof)
	require.Error(t, err)
}

func TestWriteRestoredAppHash(t *testing.T) {
	psd := t.TempDir()
	layout := AutobahnLayout{PersistentStateDir: psd, AppHashVaultDir: filepath.Join(psd, "app_hash_vault")}
	genDoc := &types.GenesisDoc{InitialHeight: 3}
	appHash := [32]byte{0xa1, 0x5e}

	require.NoError(t, WriteRestoredAppHash(layout, config.ModeFull, gigaconfig.StorageConfig{}, genDoc, 9, appHash))

	anchor, err := ReadNewestAppHashForTesting(layout, genDoc)
	require.NoError(t, err)
	require.True(t, anchor.IsRestoreAnchor())
	require.Equal(t, uint64(9), anchor.BlockHeight())
	require.Equal(t, appHash, anchor.AppHash())
	// The anchor is the first record of an empty vault only.
	require.Error(t, WriteRestoredAppHash(layout, config.ModeFull, gigaconfig.StorageConfig{}, genDoc, 9, appHash))
}

func TestWriteRestoredAppHashRejectsAHeightBelowActivation(t *testing.T) {
	psd := t.TempDir()
	layout := AutobahnLayout{PersistentStateDir: psd, AppHashVaultDir: filepath.Join(psd, "app_hash_vault")}

	err := WriteRestoredAppHash(layout, config.ModeFull, gigaconfig.StorageConfig{}, &types.GenesisDoc{InitialHeight: 10}, 9, [32]byte{1})
	require.ErrorIs(t, err, ErrBelowGigaActivation)
	_, err = os.Stat(layout.AppHashVaultDir)
	require.ErrorIs(t, err, os.ErrNotExist)
}
