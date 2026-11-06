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

func TestNewInnerInitializesGenesisLanes(t *testing.T) {
	rng := utils.TestRng()
	registry, _ := epoch.GenRegistry(rng, 3)
	anchor := data.Anchor{Epochs: [2]*types.Epoch{registry.MustEpoch(0), registry.MustEpoch(1)}}
	i, err := newInner(anchor, registry.GenesisTimestamp(), &loadedState{})
	require.NoError(t, err)
	for lane := range anchor.Epochs[0].Committee().Lanes().All() {
		blocks, hasBlocks := i.blocks[lane]
		votes, hasVotes := i.votes[lane]
		cursor, hasCursor := i.nextBlockToPersist[lane]
		require.True(t, hasBlocks)
		require.True(t, hasVotes)
		require.True(t, hasCursor)
		require.Equal(t, types.BlockNumber(0), blocks.next)
		require.Equal(t, types.BlockNumber(0), votes.next)
		require.Equal(t, types.BlockNumber(0), cursor)
	}
}

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
	require.Equal(t, types.BlockNumber(6), i.blocks[stay].first)
	require.Equal(t, types.BlockNumber(7), i.blocks[stay].next)
	require.Equal(t, types.BlockNumber(6), i.votes[stay].first)
	require.Equal(t, types.BlockNumber(7), i.nextBlockToPersist[stay])
	require.Equal(t, stayProposal, i.blocks[stay].localTip.OrPanic("local tip"))
	require.Equal(t, leaveProposal, i.blocks[leave].q[0])
	require.Equal(t, joinProposal, i.blocks[join].q[0])
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
	require.Len(t, i.blocks, 3)
	require.Equal(t, proposal, i.blocks[join].q[0])
	stayBlocks, joinBlocks := i.blocks[stay], i.blocks[join]
	_, hasRejoin := i.blocks[rejoin]
	require.False(t, hasRejoin)

	require.Equal(t, 0, i.prune(anchor))
	require.True(t, i.blocks[stay] == stayBlocks)
	require.True(t, i.blocks[join] == joinBlocks)
	require.Equal(t, proposal, i.blocks[join].q[0])

	anchor = data.Anchor{Epochs: [2]*types.Epoch{ep2, ep3}}
	require.Equal(t, 1, i.prune(anchor))
	require.Equal(t, ep2, i.applied())
	require.Len(t, i.blocks, 3)
	require.Len(t, i.votes, 3)
	require.Len(t, i.nextBlockToPersist, 3)
	require.True(t, i.blocks[stay] == stayBlocks)
	require.True(t, i.blocks[join] == joinBlocks)
	require.Equal(t, proposal, i.blocks[join].q[0])
	_, hasLeave := i.blocks[leave]
	_, hasRejoin = i.blocks[rejoin]
	require.False(t, hasLeave)
	require.True(t, hasRejoin)
}
