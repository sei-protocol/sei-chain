package avail

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/stretchr/testify/require"
)

func TestPruneAnchorConv(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 4)

	lane := keys[0].Public()
	block := types.NewBlock(lane, 0, types.BlockHeaderHash{}, types.GenPayload(rng))
	laneQCs := map[types.LaneID]*types.LaneQC{
		lane: types.NewLaneQC(makeLaneVotes(keys, block.Header())),
	}
	commitQC := makeCommitQC(committee, keys, utils.None[*types.CommitQC](), laneQCs, utils.None[*types.AppQC]())
	appProposal := types.NewAppProposal(commitQC.GlobalRange(committee).First, commitQC.Proposal().Index(), types.GenAppHash(rng))
	appQC := types.NewAppQC(makeAppVotes(keys, appProposal))

	require.NoError(t, PruneAnchorConv.Test(&PruneAnchor{
		AppQC:    appQC,
		CommitQC: commitQC,
	}))
}
