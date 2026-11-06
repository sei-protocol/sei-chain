package consensus

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/avail"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestLaneBlockStagesFollowTheCurrentProposal(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	ep := registry.MustEpoch(0)
	lane := ep.Committee().Lane(keys[0].Public()).OrPanic("lane")

	// Blocks 0..1 of the lane are in the proposal, 2..3 are certified, and 4..5 lack a LaneQC.
	parent := types.GenBlockHeaderHash(rng)
	var tip *types.BlockHeader
	for n := range types.BlockNumber(2) {
		tip = types.NewBlock(lane, n, parent, types.GenPayload(rng)).Header()
		parent = tip.Hash()
	}
	var votes []*types.Signed[*types.LaneVote]
	for _, k := range keys {
		votes = append(votes, types.Sign(k, types.NewLaneVote(tip)))
	}
	vs := types.ViewSpec{ConsensusSpec: types.ConsensusSpec{Epoch: ep}}
	proposal := utils.OrPanic1(types.NewProposalForTesting(ep.Committee(), vs, vs.NextTimestamp(), map[types.LaneID]*types.LaneQC{
		lane: types.NewLaneQC(votes),
	}, types.GenSignature(rng))).Proposal().Msg()
	tips := map[types.LaneID]avail.LaneTip{lane: {WithCommitQC: 0, WithLaneQC: 4, Held: 6}}

	var prepareVotes []*types.Signed[*types.PrepareVote]
	for _, k := range keys {
		prepareVotes = append(prepareVotes, types.Sign(k, types.NewPrepareVote(proposal)))
	}

	noProposal := inner{}
	require.Equal(t, laneBlockStages{laneQC: 2, proposal: 4}, noProposal.laneBlockStages(tips))

	voted := inner{}
	voted.PrepareVote = utils.Some(prepareVotes[0])
	require.Equal(t, laneBlockStages{laneQC: 2, proposal: 2, prepareQC: 2}, voted.laneBlockStages(tips))

	prepared := inner{}
	prepared.PrepareVote = utils.Some(prepareVotes[0])
	prepared.PrepareQC = utils.Some(types.NewPrepareQC(prepareVotes))
	require.Equal(t, laneBlockStages{laneQC: 2, proposal: 2, commitQC: 2}, prepared.laneBlockStages(tips))
}

func TestLaneBlockStagesPartitionThePositionsPastTheCommittedTip(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	ep := registry.MustEpoch(0)
	lane := ep.Committee().Lane(keys[0].Public()).OrPanic("lane")

	// The proposal covers blocks 0..3 of the lane.
	parent := types.GenBlockHeaderHash(rng)
	var tip *types.BlockHeader
	for n := range types.BlockNumber(4) {
		tip = types.NewBlock(lane, n, parent, types.GenPayload(rng)).Header()
		parent = tip.Hash()
	}
	var votes []*types.Signed[*types.LaneVote]
	for _, k := range keys {
		votes = append(votes, types.Sign(k, types.NewLaneVote(tip)))
	}
	vs := types.ViewSpec{ConsensusSpec: types.ConsensusSpec{Epoch: ep}}
	proposal := utils.OrPanic1(types.NewProposalForTesting(ep.Committee(), vs, vs.NextTimestamp(), map[types.LaneID]*types.LaneQC{
		lane: types.NewLaneQC(votes),
	}, types.GenSignature(rng))).Proposal().Msg()
	voted := inner{}
	voted.PrepareVote = utils.Some(types.Sign(keys[0], types.NewPrepareVote(proposal)))

	// The proposal arrived before this replica formed any LaneQC or received blocks 2..3, which still
	// count once, in the proposal.
	early := map[types.LaneID]avail.LaneTip{lane: {WithCommitQC: 0, WithLaneQC: 0, Held: 2}}
	require.Equal(t, laneBlockStages{prepareQC: 4}, voted.laneBlockStages(early))

	// Past the proposal, block 4 has a LaneQC and block 5 is only held.
	ahead := map[types.LaneID]avail.LaneTip{lane: {WithCommitQC: 0, WithLaneQC: 5, Held: 6}}
	require.Equal(t, laneBlockStages{prepareQC: 4, proposal: 1, laneQC: 1}, voted.laneBlockStages(ahead))
}
