package avail

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus/persist"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestNewInnerInitializesBoundaryLanes(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 2)
	joiner := types.GenSecretKey(rng)
	require.NoError(t, registry.TestPushEpoch(0, map[types.PublicKey]uint64{keys[0].Public(): 1, joiner.Public(): 1}))
	ep := types.NewEpoch(1, types.RoadRange{First: 10, Next: 11}, registry.MustEpoch(1).Committee())
	next := types.NewEpoch(2, types.RoadRange{First: 11, Next: 13}, registry.MustEpoch(2).Committee())
	stay := ep.Committee().Lane(keys[0].Public()).OrPanic("staying lane")
	leave := ep.Committee().Lane(keys[1].Public()).OrPanic("departing lane")
	join := next.Committee().Lane(joiner.Public()).OrPanic("joining lane")
	committed := types.NewBlock(stay, 5, types.BlockHeaderHash{}, &types.Payload{})
	qc := types.BuildCommitQC(ep, keys, utils.None[*types.CommitQC](), map[types.LaneID]*types.LaneQC{
		stay: data.TestLaneQC(keys, committed.Header()),
	})
	anchor := data.Anchor{
		First:  utils.Some(data.AnchorFirst{CommitQC: qc, AppQC: data.TestAppQC(keys, types.NewAppProposal(qc.View(), nil))}),
		Epochs: [2]*types.Epoch{ep, next},
	}
	committedProposal := types.Sign(keys[0], types.NewLaneProposal(committed))
	stayProposal := types.Sign(keys[0], types.NewLaneProposal(types.NewBlock(stay, 6, committed.Header().Hash(), &types.Payload{})))
	leaveProposal := types.Sign(keys[1], types.NewLaneProposal(types.NewBlock(leave, 0, types.BlockHeaderHash{}, &types.Payload{})))
	joinProposal := types.Sign(joiner, types.NewLaneProposal(types.NewBlock(join, 0, types.BlockHeaderHash{}, &types.Payload{})))
	i, err := newInner(anchor, registry.GenesisTimestamp(), &loadedState{blocks: map[types.LaneID][]persist.LoadedBlock{
		stay:  {{Number: 5, Proposal: committedProposal}, {Number: 6, Proposal: stayProposal}},
		leave: {{Number: 0, Proposal: leaveProposal}},
		join:  {{Number: 0, Proposal: joinProposal}},
	}})
	require.NoError(t, err)
	require.Equal(t, next, i.applied())
	require.Equal(t, types.BlockNumber(5), i.lanes[stay].blocks.first)
	require.Equal(t, types.BlockNumber(7), i.lanes[stay].blocks.next)
	require.Equal(t, types.BlockNumber(6), i.lanes[stay].votes.first)
	require.Equal(t, types.BlockNumber(7), i.lanes[stay].nextBlockToPersist)
	require.Equal(t, stayProposal, i.lanes[stay].blocks.q[6])
	require.Equal(t, leaveProposal, i.lanes[leave].blocks.q[0])
	require.Equal(t, joinProposal, i.lanes[join].blocks.q[0])
}

func TestPruneMaintainsBothAnchorCommittees(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 2)
	joiner := types.GenSecretKey(rng)
	require.NoError(t, registry.TestPushEpoch(0, map[types.PublicKey]uint64{keys[0].Public(): 1, joiner.Public(): 1}))
	require.NoError(t, registry.TestPushEpoch(1, map[types.PublicKey]uint64{keys[1].Public(): 1, joiner.Public(): 1}))
	ep1, ep2, ep3 := registry.MustEpoch(1), registry.MustEpoch(2), registry.MustEpoch(3)
	stay := ep1.Committee().Lane(keys[0].Public()).OrPanic("staying lane")
	leave := ep1.Committee().Lane(keys[1].Public()).OrPanic("departing lane")
	join := ep2.Committee().Lane(joiner.Public()).OrPanic("joining lane")
	rejoin := ep3.Committee().Lane(keys[1].Public()).OrPanic("rejoining lane")
	proposal := types.Sign(joiner, types.NewLaneProposal(types.NewBlock(join, 0, types.BlockHeaderHash{}, &types.Payload{})))
	anchor := data.Anchor{Epochs: [2]*types.Epoch{ep1, ep2}}
	i, err := newInner(anchor, registry.GenesisTimestamp(), &loadedState{blocks: map[types.LaneID][]persist.LoadedBlock{
		join: {{Number: 0, Proposal: proposal}},
	}})
	require.NoError(t, err)
	require.Equal(t, ep1, i.applied())
	require.Len(t, i.lanes, 3)
	require.Equal(t, proposal, i.lanes[join].blocks.q[0])
	stayBlocks, joinBlocks := i.lanes[stay].blocks, i.lanes[join].blocks
	_, hasRejoin := i.lanes[rejoin]
	require.False(t, hasRejoin)

	i.setAnchor(anchor)
	require.True(t, i.lanes[stay].blocks == stayBlocks)
	require.True(t, i.lanes[join].blocks == joinBlocks)
	require.Equal(t, proposal, i.lanes[join].blocks.q[0])

	anchor = data.Anchor{Epochs: [2]*types.Epoch{ep2, ep3}}
	i.setAnchor(anchor)
	require.Equal(t, ep2, i.applied())
	require.Len(t, i.lanes, 3)
	require.True(t, i.lanes[stay].blocks == stayBlocks)
	require.True(t, i.lanes[join].blocks == joinBlocks)
	require.Equal(t, proposal, i.lanes[join].blocks.q[0])
	_, hasLeave := i.lanes[leave]
	_, hasRejoin = i.lanes[rejoin]
	require.False(t, hasLeave)
	require.True(t, hasRejoin)
}

func TestRestartAcceptsFinalRoadCommitQC(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	ep := registry.MustEpoch(0)
	// Anchor: LastRoad(0)-1. The restored spec must point to LastRoad(0), still in epoch 0.
	ds := newTestDataStateAtRoad(t, rng, registry, keys, registry.LastRoad(0)-1)
	state, err := NewState(keys[0], ds, utils.None[string]())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, state.Close()) })
	spec := state.ConsensusSpec().Load()
	require.Equal(t, ep, spec.Epoch)
	require.Equal(t, registry.LastRoad(0), spec.Index())
	// Receiving the final CommitQC must not advance the spec before it is persisted.
	last := types.BuildCommitQC(ep, keys, ds.Anchor().Load().CommitQC(), nil)
	require.NoError(t, last.Verify(ep))
	require.NoError(t, state.PushCommitQC(t.Context(), last))
	require.Equal(t, last.Index()+1, nextRoad(state))
	require.Equal(t, ep, state.ConsensusSpec().Load().Epoch)
	// Persistence moves the spec to LastRoad(0)+1 in the already available epoch 1.
	state.markCommitQCsPersisted(last)
	spec, err = state.ConsensusSpec().Wait(t.Context(), func(spec types.ConsensusSpec) bool {
		return spec.Epoch.EpochIndex() == 1
	})
	require.NoError(t, err)
	require.Equal(t, utils.Some(last), spec.CommitQC)
	require.True(t, spec.Epoch.RoadRange().Has(spec.Index()))
}
