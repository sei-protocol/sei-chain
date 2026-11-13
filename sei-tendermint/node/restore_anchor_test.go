package node

import (
	"testing"

	"github.com/sei-protocol/sei-chain/giga/apphash/builder"
	"github.com/sei-protocol/sei-chain/giga/apphash/vault"
	gigaconfig "github.com/sei-protocol/sei-chain/giga/config"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// TestStoreRestoreAnchorIsPublishedBySetupAtItsHeight opens the app hash builder with the
// storage config of node startup, and requires setup at the anchor's height to publish it.
func TestStoreRestoreAnchorIsPublishedBySetupAtItsHeight(t *testing.T) {
	conf := layoutConfig(t, t.TempDir())
	storage := gigaconfig.DefaultConfig.Storage
	layout, err := ResolveAutobahnLayout(conf, storage, genesisAt(3))
	require.NoError(t, err)
	genesis, err := ResolveEpochGenesis(conf, genesisAt(3))
	require.NoError(t, err)
	appHash := [32]byte{1, 2, 3}
	require.NoError(t, StoreRestoreAnchor(layout, genesis, 10, appHash))

	fc, _, err := loadAutobahnCommittee(conf.AutobahnConfigFile)
	require.NoError(t, err)
	_, storageConfig, err := autobahnStorageConfig(conf.RootDir, conf.Mode, fc, storage, genesisAt(3))
	require.NoError(t, err)
	cfg := *storageConfig.AppHashBuilderConfig
	hashVault, err := vault.NewStandardHashVault(cfg.HashVaultConfig, cfg.ChainID, cfg.GigaActivationHeight)
	require.NoError(t, err)
	b, err := builder.NewStandardAppHashBuilder(t.Context(), cfg, hashVault)
	require.NoError(t, err)
	require.NoError(t, b.SetupComplete(t.Context(), 10))
	got, err := b.GetAppHash(t.Context(), 10)
	require.NoError(t, err)
	require.True(t, got.IsRestoreAnchor())
	require.Equal(t, appHash, got.AppHash())
	require.NoError(t, b.Close())
	require.NoError(t, hashVault.Close())
}

func TestStoreRestoreAnchorRefusesAVaultThatHoldsARecord(t *testing.T) {
	conf := layoutConfig(t, t.TempDir())
	layout, err := ResolveAutobahnLayout(conf, gigaconfig.DefaultConfig.Storage, genesisAt(1))
	require.NoError(t, err)
	genesis, err := ResolveEpochGenesis(conf, genesisAt(1))
	require.NoError(t, err)
	require.NoError(t, StoreRestoreAnchor(layout, genesis, 10, [32]byte{1}))
	require.Error(t, StoreRestoreAnchor(layout, genesis, 10, [32]byte{1}))
}

func TestStoreRestoreAnchorRefusesAHeightBelowTheGenesis(t *testing.T) {
	conf := layoutConfig(t, t.TempDir())
	layout, err := ResolveAutobahnLayout(conf, gigaconfig.DefaultConfig.Storage, genesisAt(5))
	require.NoError(t, err)
	genesis, err := ResolveEpochGenesis(conf, genesisAt(5))
	require.NoError(t, err)
	require.Error(t, StoreRestoreAnchor(layout, genesis, 4, [32]byte{1}))
}

// proofGenesis returns a chain of keys as its genesis validators.
func proofGenesis(keys []atypes.SecretKey) EpochGenesis {
	validators := make([]atypes.PublicKey, 0, len(keys))
	for _, k := range keys {
		validators = append(validators, k.Public())
	}
	return EpochGenesis{GenDoc: &types.GenesisDoc{ChainID: "chain-a", InitialHeight: 1}, Validators: validators}
}

func TestVerifyStateSyncProofReturnsTheSignedAppHash(t *testing.T) {
	rng := utils.TestRng()
	keys := utils.GenSliceN(rng, 3, atypes.GenSecretKey)
	genesis := proofGenesis(keys)
	appHash := [32]byte{7, 8, 9}
	proof, err := StateSyncProofForTesting(genesis, keys, 12, appHash[:], t.TempDir())
	require.NoError(t, err)

	got, err := VerifyStateSyncProof(genesis, t.TempDir(), 12, proof)
	require.NoError(t, err)
	require.Equal(t, appHash, got)
}

func TestVerifyStateSyncProofRefusesAProofOfAnotherCommittee(t *testing.T) {
	rng := utils.TestRng()
	keys := utils.GenSliceN(rng, 3, atypes.GenSecretKey)
	others := utils.GenSliceN(rng, 3, atypes.GenSecretKey)
	proof, err := StateSyncProofForTesting(proofGenesis(others), others, 12, make(atypes.AppHash, 32), t.TempDir())
	require.NoError(t, err)

	_, err = VerifyStateSyncProof(proofGenesis(keys), t.TempDir(), 12, proof)
	require.Error(t, err)
}

func TestVerifyStateSyncProofRefusesAProofOfAnotherHeight(t *testing.T) {
	rng := utils.TestRng()
	keys := utils.GenSliceN(rng, 3, atypes.GenSecretKey)
	genesis := proofGenesis(keys)
	proof, err := StateSyncProofForTesting(genesis, keys, 12, make(atypes.AppHash, 32), t.TempDir())
	require.NoError(t, err)

	_, err = VerifyStateSyncProof(genesis, t.TempDir(), 13, proof)
	require.Error(t, err)
}

func TestVerifyStateSyncProofRefusesAnAppHashThatIsNot32Bytes(t *testing.T) {
	rng := utils.TestRng()
	keys := utils.GenSliceN(rng, 3, atypes.GenSecretKey)
	genesis := proofGenesis(keys)
	proof, err := StateSyncProofForTesting(genesis, keys, 12, atypes.AppHash{1, 2, 3}, t.TempDir())
	require.NoError(t, err)

	_, err = VerifyStateSyncProof(genesis, t.TempDir(), 12, proof)
	require.Error(t, err)
}
