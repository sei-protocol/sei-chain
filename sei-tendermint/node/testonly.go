package node

import (
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p"
)

// GenesisEpochRegistryRowForTesting returns the epoch_registry row of a chain whose epoch
// registry holds only the genesis epochs of genesis. It writes registry files under scratchDir.
// Intended for use in tests only.
func GenesisEpochRegistryRowForTesting(genesis EpochGenesis, scratchDir string) ([]byte, error) {
	registry, err := p2p.OpenEpochRegistry(genesis.GenDoc, genesis.Validators, scratchDir)
	if err != nil {
		return nil, err
	}
	return registry.MarshalSnapshot(), nil
}
