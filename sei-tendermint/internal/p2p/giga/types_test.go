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
			StreamLaneProposalsReqConv.Test(&StreamLaneProposalsReq{LaneID: types.GenLaneID(rng), FirstBlockNumber: types.GenBlockNumber(rng)}),
			GetBlockReqConv.Test(&GetBlockReq{Pos: types.LaneBlockPos{Index: types.GenRoadIndex(rng), Offset: rng.Uint64()}}),
			GetBlockRespConv.Test(utils.None[*types.Block]()),
			GetBlockRespConv.Test(utils.Some(types.GenBlock(rng))),
			StreamFullCommitQCsReqConv.Test(&StreamFullCommitQCsReq{NextBlock: types.GenRoadIndex(rng)}),
			StreamAppQCsReqConv.Test(&StreamAppQCsReq{NextBlock: types.GenRoadIndex(rng)}),
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
