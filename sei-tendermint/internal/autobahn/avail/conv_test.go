package avail

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestPruneAnchorConv(t *testing.T) {
	rng := utils.TestRng()
	registry, keys, _ := epoch.GenRegistry(rng, 4)

	lane := keys[0].Public()
	block := types.NewBlock(lane, 0, types.BlockHeaderHash{}, types.GenPayload(rng))
	laneQCs := map[types.LaneID]*types.LaneQC{
		lane: types.NewLaneQC(makeLaneVotes(keys, block.Header())),
	}
	commitQC := makeCommitQC(registry.LatestEpoch(), keys, utils.None[*types.CommitQC](), laneQCs, utils.None[*types.AppQC]())
	appProposal := types.NewAppProposal(commitQC.GlobalRange().First, commitQC.Proposal().Index(), types.GenAppHash(rng), commitQC.Proposal().EpochIndex())
	appQC := types.NewAppQC(makeAppVotes(keys, appProposal))

	anchor := &PruneAnchor{AppQC: appQC, CommitQC: commitQC}
	pb1 := PruneAnchorConv.Encode(anchor)
	decoded, err := PruneAnchorConv.Decode(pb1)
	require.NoError(t, err)
	require.True(t, proto.Equal(pb1, PruneAnchorConv.Encode(decoded)))
}
