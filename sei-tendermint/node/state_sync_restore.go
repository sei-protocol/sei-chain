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

// VerifyRestoreProof returns the app hash that the AppQC of the state sync proof file proof signs
// for road height. It returns an error unless the road block and the AppQC of proof verify against
// the epoch of height in the epoch registry of the FlatKV checkpoint in checkpointDir.
func VerifyRestoreProof(checkpointDir string, height int64, proof []byte) ([32]byte, error) {
	if height < 1 {
		return [32]byte{}, fmt.Errorf("checkpoint height %d is not positive", height)
	}
	road := atypes.RoadHeight(height)
	registry, err := evmonlyapp.ReadCheckpointEpochRegistry(checkpointDir)
	if err != nil {
		return [32]byte{}, err
	}
	ep, err := epochOfRoad(registry, road)
	if err != nil {
		return [32]byte{}, err
	}
	decoded, err := statesyncproof.Decode(proof, road)
	if err != nil {
		return [32]byte{}, fmt.Errorf("decode the state sync proof file of road %d: %w", road, err)
	}
	if err := decoded.Verify(ep); err != nil {
		return [32]byte{}, fmt.Errorf("verify the state sync proof file of road %d against epoch %d: %w", road, ep.EpochIndex(), err)
	}
	appHash := decoded.AppQC().Proposal().AppHash()
	if len(appHash) != 32 {
		return [32]byte{}, fmt.Errorf("the AppQC of road %d signs an app hash of %d bytes, want 32", road, len(appHash))
	}
	return [32]byte(appHash), nil
}

// epochOfRoad returns the epoch of registry that holds road.
func epochOfRoad(registry *atypes.EpochRegistry, road atypes.RoadHeight) (*atypes.Epoch, error) {
	for _, ep := range registry.Epochs() {
		if ep.RoadRange().Has(road) {
			return ep, nil
		}
	}
	return nil, fmt.Errorf("the epoch registry of the checkpoint holds no epoch of road %d", road)
}

// StoreRestoreAnchor stores appHash as the app hash of height, the first record of the empty
// app-hash vault of layout. mode, storage and genDoc are the node mode, the [giga.storage] settings
// and the genesis doc that the node starts with.
func StoreRestoreAnchor(
	layout AutobahnLayout,
	mode string,
	storage gigaconfig.StorageConfig,
	genDoc *types.GenesisDoc,
	height int64,
	appHash [32]byte,
) (retErr error) {
	storageConfig, err := buildGigaStorageConfig(layout.PersistentStateDir, mode, storage, genDoc)
	if err != nil {
		return fmt.Errorf("build Autobahn storage config: %w", err)
	}
	cfg := *storageConfig.AppHashBuilderConfig
	if height < 1 || uint64(height) < cfg.GigaActivationHeight { //nolint:gosec // height is positive.
		return fmt.Errorf("height %d is below the giga activation height %d, so it has no app hash", height, cfg.GigaActivationHeight)
	}
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
