package node

import (
	"errors"
	"fmt"
	"os"

	"github.com/sei-protocol/sei-chain/giga/apphash"
	"github.com/sei-protocol/sei-chain/giga/apphash/vault"
	gigaconfig "github.com/sei-protocol/sei-chain/giga/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/statesyncproof"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// StoreRestoreAnchor empties the app-hash vault of layout and stores in it the app hash that the
// AppQC of the state sync proof file proof signs, as the restore anchor of height. The vault has
// the chain ID and activation height of the node start of a home in nodeMode with storage and
// genDoc.
func StoreRestoreAnchor(
	layout AutobahnLayout,
	nodeMode string,
	storage gigaconfig.StorageConfig,
	genDoc *types.GenesisDoc,
	height int64,
	proof []byte,
) (retErr error) {
	if height < 1 {
		return fmt.Errorf("checkpoint height %d is not positive", height)
	}
	decoded, err := statesyncproof.Decode(proof, atypes.RoadHeight(height))
	if err != nil {
		return fmt.Errorf("decode the state sync proof: %w", err)
	}
	appHash := decoded.AppQC().Proposal().AppHash()
	if len(appHash) != 32 {
		return fmt.Errorf("the AppQC of height %d signs an app hash of %d bytes, want 32", height, len(appHash))
	}
	storageConfig, err := buildGigaStorageConfig(layout.PersistentStateDir, nodeMode, storage, genDoc)
	if err != nil {
		return err
	}
	cfg := *storageConfig.AppHashBuilderConfig
	if uint64(height) < cfg.GigaActivationHeight {
		return fmt.Errorf("height %d is below the giga activation height %d, so it has no app hash", height, cfg.GigaActivationHeight)
	}
	cfg.HashVaultConfig.Path = layout.AppHashVaultDir
	if err := os.RemoveAll(layout.AppHashVaultDir); err != nil {
		return fmt.Errorf("remove the app-hash vault of an earlier import: %w", err)
	}
	hashVault, err := vault.NewStandardHashVault(cfg.HashVaultConfig, cfg.ChainID, cfg.GigaActivationHeight)
	if err != nil {
		return fmt.Errorf("open the app-hash vault: %w", err)
	}
	defer func() { retErr = errors.Join(retErr, hashVault.Close()) }()
	anchor := apphash.NewRestoreAnchorAppHashData(cfg.ChainID, uint64(height), [32]byte(appHash))
	if err := hashVault.Append([]*apphash.AppHashData{anchor}); err != nil {
		return fmt.Errorf("store the restore anchor of height %d: %w", height, err)
	}
	return nil
}
