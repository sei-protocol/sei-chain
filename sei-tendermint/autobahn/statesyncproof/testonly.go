package statesyncproof

import (
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
)

// EncodeForTesting returns a state sync proof file of road whose AppQC signs appHash, signed by
// keys, the keys of the committee of ep. It verifies against ep. Intended for use in tests only.
func EncodeForTesting(ep *types.Epoch, keys []types.SecretKey, road types.RoadIndex, appHash types.AppHash) ([]byte, error) {
	proposal, blocks := types.ProposalAtBlocks(ep, types.View{Index: road}, 1)
	commitVote := types.NewCommitVote(types.BuildPrepareQC(keys, proposal))
	commitVotes := make([]*types.Signed[*types.CommitVote], 0, len(keys))
	for _, k := range keys {
		commitVotes = append(commitVotes, types.Sign(k, commitVote))
	}
	commitQC := types.NewCommitQC(commitVotes)
	appVote := types.NewAppVote(types.NewAppProposal(commitQC.View(), appHash))
	appVotes := make([]*types.Signed[*types.AppVote], 0, len(keys))
	for _, k := range keys {
		appVotes = append(appVotes, types.Sign(k, appVote))
	}
	roadBlock := types.NewRoadBlock(types.NewFullCommitQC(commitQC, []*types.BlockHeader{blocks[0].Header()}), blocks)
	return Encode(roadBlock, types.NewAppQC(appVotes))
}
