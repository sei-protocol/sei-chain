package node

import (
	"errors"
	"fmt"

	"github.com/sei-protocol/sei-chain/giga/apphash"
	"github.com/sei-protocol/sei-chain/giga/apphash/vault"
	gigaconfig "github.com/sei-protocol/sei-chain/giga/config"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/evmonlyapp"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// VerifyRestoreProof returns the app hash that the AppQC of the state sync proof file proof signs
// for road height. It returns an error unless the road block and the AppQC of proof verify against
// the epoch of height in the epoch registry of the FlatKV checkpoint in checkpointDir.
func VerifyRestoreProof(checkpointDir string, height int64, proof []byte) ([32]byte, error) {
	if height < 1 {
		return [32]byte{}, fmt.Errorf("checkpoint height %d is not positive", height)
	}
	registry, err := evmonlyapp.ReadCheckpointEpochRegistry(checkpointDir)
	if err != nil {
		return [32]byte{}, err
	}
	_, appHash, err := p2p.VerifyStateSyncProof(registry, proof, atypes.RoadHeight(height))
	return appHash, err
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
