package consensus

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/avail"
	availmetrics "github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/avail/metrics"
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
	proposal := types.BuildCommitQC(ep, keys, utils.None[*types.CommitQC](), map[types.LaneID]*types.LaneQC{
		lane: types.NewLaneQC(votes),
	}).Proposal()
	tips := map[types.LaneID]avail.LaneTip{lane: {Committed: 0, Certified: 4, Held: 6}}

	var prepareVotes []*types.Signed[*types.PrepareVote]
	for _, k := range keys {
		prepareVotes = append(prepareVotes, types.Sign(k, types.NewPrepareVote(proposal)))
	}

	noProposal := inner{}
	require.Equal(t, availmetrics.LaneBlockStages{LaneQC: 2, Proposal: 4}, laneBlockStages(tips, noProposal))

	voted := inner{}
	voted.PrepareVote = utils.Some(prepareVotes[0])
	require.Equal(t, availmetrics.LaneBlockStages{LaneQC: 2, Proposal: 2, PrepareQC: 2}, laneBlockStages(tips, voted))

	prepared := inner{}
	prepared.PrepareVote = utils.Some(prepareVotes[0])
	prepared.PrepareQC = utils.Some(types.NewPrepareQC(prepareVotes))
	require.Equal(t, availmetrics.LaneBlockStages{LaneQC: 2, Proposal: 2, CommitQC: 2}, laneBlockStages(tips, prepared))
}

func TestLaneBlockStagesCountEachHeldBlockOnce(t *testing.T) {
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
	commitQC := types.BuildCommitQC(ep, keys, utils.None[*types.CommitQC](), map[types.LaneID]*types.LaneQC{
		lane: types.NewLaneQC(votes),
	})
	voted := inner{}
	voted.PrepareVote = utils.Some(types.Sign(keys[0], types.NewPrepareVote(commitQC.Proposal())))

	// The proposal arrived before this node formed any LaneQC and while it holds only blocks 0..1.
	early := map[types.LaneID]avail.LaneTip{lane: {Committed: 0, Certified: 0, Held: 2}}
	require.Equal(t, availmetrics.LaneBlockStages{PrepareQC: 2}, laneBlockStages(early, voted))

	// Consensus has committed blocks 0..3 while avail's persisted CommitQC still starts at 0.
	committed := inner{spec: types.ConsensusSpec{CommitQC: utils.Some(commitQC), Epoch: ep}}
	lagging := map[types.LaneID]avail.LaneTip{lane: {Committed: 0, Certified: 5, Held: 6}}
	require.Equal(t, availmetrics.LaneBlockStages{LaneQC: 1, Proposal: 1}, laneBlockStages(lagging, committed))
}
