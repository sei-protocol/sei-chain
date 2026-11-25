package node

import (
	"errors"
	"fmt"

	"github.com/sei-protocol/sei-chain/giga/apphash"
	"github.com/sei-protocol/sei-chain/giga/apphash/vault"
	gigaconfig "github.com/sei-protocol/sei-chain/giga/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/statesyncproof"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/evmonlyapp"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// errRestoreNeedsNextEpoch is returned for a restore whose checkpoint holds no epoch after the
// epoch of the restored road.
var errRestoreNeedsNextEpoch = errors.New("the epoch registry of the FlatKV checkpoint holds no epoch after the epoch of the restored road, so a BlockDB cannot start at it")

// VerifyRestoreProof returns the app hash that the AppQC of the state sync proof file proof signs
// for road height. It returns an error unless the epoch registry of the FlatKV checkpoint in
// checkpointDir holds the epoch of height and the epoch after it, and proof verifies against the
// epoch of height.
func VerifyRestoreProof(checkpointDir string, height int64, proof []byte) ([32]byte, error) {
	if height < 1 {
		return [32]byte{}, fmt.Errorf("checkpoint height %d is not positive", height)
	}
	_, appHash, err := verifyRestoreProof(checkpointDir, atypes.RoadHeight(height), proof)
	return appHash, err
}

// verifyRestoreProof returns the state sync proof file proof of road and the app hash that its
// AppQC signs. It returns an error unless the epoch registry of the FlatKV checkpoint in
// checkpointDir holds the epoch of road and the epoch after it, and proof verifies against the
// epoch of road.
func verifyRestoreProof(checkpointDir string, road atypes.RoadHeight, proof []byte) (*statesyncproof.Proof, [32]byte, error) {
	registry, err := evmonlyapp.ReadCheckpointEpochRegistry(checkpointDir)
	if err != nil {
		return nil, [32]byte{}, err
	}
	// data.NewState anchors a BlockDB that starts at road on these two epochs.
	if _, ok := registry.ForAnchor(road); !ok {
		return nil, [32]byte{}, fmt.Errorf("%w; import a checkpoint of another height", errRestoreNeedsNextEpoch)
	}
	return statesyncproof.DecodeAndVerify(registry, proof, road)
}

// ErrBelowGigaActivation is returned for a restore height below the giga activation height, which
// has no app hash.
var ErrBelowGigaActivation = errors.New("height is below the giga activation height, so it has no app hash")

// RequireRestoreHeight returns ErrBelowGigaActivation unless height is at or above the giga
// activation height of the chain that genDoc starts.
func RequireRestoreHeight(genDoc *types.GenesisDoc, height int64) error {
	activationHeight, err := gigaActivationHeight(genDoc)
	if err != nil {
		return err
	}
	if height < 1 || uint64(height) < activationHeight { //nolint:gosec // height is positive.
		return fmt.Errorf("%w: height %d, activation height %d", ErrBelowGigaActivation, height, activationHeight)
	}
	return nil
}

// WriteRestoredAppHash writes appHash, the app hash of the restored height, as the restore anchor
// of the empty app-hash vault of layout. mode, storage and genDoc are the node mode, the
// [giga.storage] settings and the genesis doc that the node starts with.
func WriteRestoredAppHash(
	layout AutobahnLayout,
	mode string,
	storage gigaconfig.StorageConfig,
	genDoc *types.GenesisDoc,
	height int64,
	appHash [32]byte,
) (retErr error) {
	if err := RequireRestoreHeight(genDoc, height); err != nil {
		return err
	}
	storageConfig, err := buildGigaStorageConfig(layout.PersistentStateDir, mode, storage, genDoc)
	if err != nil {
		return fmt.Errorf("build Autobahn storage config: %w", err)
	}
	cfg := *storageConfig.AppHashBuilderConfig
	cfg.HashVaultConfig.Path = layout.AppHashVaultDir
	hashVault, err := vault.NewStandardHashVault(cfg.HashVaultConfig, cfg.ChainID, cfg.GigaActivationHeight)
	if err != nil {
		return fmt.Errorf("open the app-hash vault: %w", err)
	}
	defer func() { retErr = errors.Join(retErr, hashVault.Close()) }()
	anchor := apphash.NewRestoreAnchorAppHashData(cfg.ChainID, uint64(height), appHash) //nolint:gosec // height is positive.
	if err := hashVault.Append([]*apphash.AppHashData{anchor}); err != nil {
		return fmt.Errorf("store the restore anchor of height %d: %w", height, err)
	}
	return nil
}
