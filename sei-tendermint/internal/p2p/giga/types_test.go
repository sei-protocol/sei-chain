package giga

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestConv(t *testing.T) {
	rng := utils.TestRng()
	for range 10 {
		require.NoError(t, firstErr(
			LaneVoteConv.Test(types.GenSigned(rng, types.GenLaneVote(rng))),
			LaneProposalConv.Test(types.GenSigned(rng, types.GenLaneProposal(rng))),
			AppVoteConv.Test(types.GenSigned(rng, types.GenAppVote(rng))),
			StreamLaneProposalsReqConv.Test(&StreamLaneProposalsReq{LaneID: types.GenLaneID(rng), FirstHeight: types.GenLaneHeight(rng)}),
			GetBlockReqConv.Test(&GetBlockReq{Pos: types.LaneBlockPos{Height: types.GenRoadHeight(rng), Offset: rng.Uint64()}}),
			GetBlockRespConv.Test(utils.None[*types.LaneBlock]()),
			GetBlockRespConv.Test(utils.Some(types.GenLaneBlock(rng))),
			StreamFullCommitQCsReqConv.Test(&StreamFullCommitQCsReq{NextBlock: types.GenRoadHeight(rng)}),
			StreamAppQCsReqConv.Test(&StreamAppQCsReq{NextBlock: types.GenRoadHeight(rng)}),
		))
	}
}

func firstErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}
