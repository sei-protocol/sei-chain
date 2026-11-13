package node

import (
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/statesyncproof"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
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

// StateSyncProofForTesting returns the state sync proof file of road height whose AppQC signs
// appHash, signed by keys, the keys of the genesis validators. It verifies against the epoch
// registry that holds only the genesis epochs of genesis, which it opens under scratchDir.
// Intended for use in tests only.
func StateSyncProofForTesting(
	genesis EpochGenesis,
	keys []atypes.SecretKey,
	height int64,
	appHash atypes.AppHash,
	scratchDir string,
) ([]byte, error) {
	registry, err := p2p.OpenEpochRegistry(genesis.GenDoc, genesis.Validators, scratchDir)
	if err != nil {
		return nil, err
	}
	road := atypes.RoadIndex(height) //nolint:gosec // test heights are positive.
	ep, err := registry.EpochAt(road)
	if err != nil {
		return nil, err
	}
	proposal, blocks := atypes.ProposalAtBlocks(ep, atypes.View{Index: road}, 1)
	commitVote := atypes.NewCommitVote(atypes.BuildPrepareQC(keys, proposal))
	commitVotes := make([]*atypes.Signed[*atypes.CommitVote], 0, len(keys))
	for _, k := range keys {
		commitVotes = append(commitVotes, atypes.Sign(k, commitVote))
	}
	commitQC := atypes.NewCommitQC(commitVotes)
	appVote := atypes.NewAppVote(atypes.NewAppProposal(commitQC.View(), appHash))
	appVotes := make([]*atypes.Signed[*atypes.AppVote], 0, len(keys))
	for _, k := range keys {
		appVotes = append(appVotes, atypes.Sign(k, appVote))
	}
	roadBlock := atypes.NewRoadBlock(atypes.NewFullCommitQC(commitQC, []*atypes.BlockHeader{blocks[0].Header()}), blocks)
	return statesyncproof.Encode(roadBlock, atypes.NewAppQC(appVotes))
}
