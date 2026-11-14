package node

import (
	"errors"
	"fmt"

	"github.com/sei-protocol/sei-chain/giga/apphash"
	"github.com/sei-protocol/sei-chain/giga/apphash/vault"
	seidbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p"
)

// VerifyStateSyncProof returns the app hash of height that the state sync proof file data
// carries, after it verifies the road block and the AppQC of height against the epoch registry
// that genesis opens under stateDir.
func VerifyStateSyncProof(genesis EpochGenesis, stateDir string, height int64, data []byte) ([32]byte, error) {
	if height < 1 {
		return [32]byte{}, fmt.Errorf("checkpoint height %d is not positive", height)
	}
	registry, err := p2p.OpenEpochRegistry(genesis.GenDoc, genesis.Validators, stateDir)
	if err != nil {
		return [32]byte{}, fmt.Errorf("open the epoch registry: %w", err)
	}
	_, appHash, err := p2p.VerifyStateSyncProof(registry, data, atypes.RoadIndex(height)) //nolint:gosec // height is positive.
	return appHash, err
}

// StoreRestoreAnchor stores appHash as the restore anchor of height, the first record of the
// empty app-hash vault of layout, on the chain that genesis starts.
func StoreRestoreAnchor(layout AutobahnLayout, genesis EpochGenesis, height int64, appHash [32]byte) (retErr error) {
	activationHeight, err := gigaActivationHeight(genesis.GenDoc)
	if err != nil {
		return err
	}
	if height < 1 || uint64(height) < activationHeight { //nolint:gosec // height is positive.
		return fmt.Errorf("height %d is below the giga activation height %d, so it has no app hash", height, activationHeight)
	}
	storageConfig, err := seidbconfig.AutobahnStorageConfig(layout.PersistentStateDir)
	if err != nil {
		return err
	}
	cfg := *storageConfig.AppHashBuilderConfig
	cfg.HashVaultConfig.Path = layout.AppHashVaultDir
	hashVault, err := vault.NewStandardHashVault(cfg.HashVaultConfig, cfg.ChainID, activationHeight)
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
