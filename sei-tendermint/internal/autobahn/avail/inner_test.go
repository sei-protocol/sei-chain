package avail

import (
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus/persist"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/stretchr/testify/require"
)

func TestPruneMismatchedIndices(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 4)
	ds := data.NewState(&data.Config{
		Committee: committee,
	}, utils.None[data.BlockStore]())
	state, err := NewState(keys[0], ds, utils.None[string]())
	require.NoError(t, err)

	// Helper to create a CommitQC for a specific index
	makeQC := func(index types.RoadIndex, prev utils.Option[*types.CommitQC]) *types.CommitQC {
		vs := types.ViewSpec{CommitQC: prev}
		fullProposal := utils.OrPanic1(types.NewProposal(
			leaderKey(committee, keys, vs.View()),
			committee,
			vs,
			time.Now(),
			nil,
			utils.None[*types.AppQC](),
		))
		vote := types.NewCommitVote(fullProposal.Proposal().Msg())
		var votes []*types.Signed[*types.CommitVote]
		for _, k := range keys {
			votes = append(votes, types.Sign(k, vote))
		}
		return types.NewCommitQC(votes)
	}

	qc0 := makeQC(0, utils.None[*types.CommitQC]())
	_ = makeQC(1, utils.Some(qc0)) // show we can generate index 1

	// Create an AppQC for index 1 (matching qc1)
	appProposal1 := types.NewAppProposal(0, 1, types.GenAppHash(rng))
	appQC1 := types.NewAppQC(makeAppVotes(keys, appProposal1))

	// Now call PushAppQC with appQC1 (index 1) and qc0 (index 0)
	err = state.PushAppQC(appQC1, qc0)
	require.Error(t, err)

	// Get the inner state
	for inner := range state.inner.Lock() {
		// Now call prune with mismatched QCs directly to test the safety check
		updated, err := inner.prune(appQC1, qc0)

		require.Error(t, err)
		require.False(t, updated, "prune should return false for mismatched indices")
		require.False(t, inner.latestAppQC.IsPresent(), "latestAppQC should not have been updated")
	}
}

// testSignedBlock creates a signed lane proposal for a given lane, block number, and parent hash.
func testSignedBlock(key types.SecretKey, lane types.LaneID, n types.BlockNumber, parent types.BlockHeaderHash, rng utils.Rng) *types.Signed[*types.LaneProposal] {
	block := types.NewBlock(lane, n, parent, types.GenPayload(rng))
	return types.Sign(key, types.NewLaneProposal(block))
}

func TestNewInnerFreshStart(t *testing.T) {
	rng := utils.TestRng()
	committee, _ := types.GenCommittee(rng, 4)

	i := newInner(committee, utils.None[*loadedAvailState]())

	require.False(t, i.latestAppQC.IsPresent())
	require.Nil(t, i.nextBlockToPersist)
	require.Equal(t, types.RoadIndex(0), i.commitQCs.first)
	require.Equal(t, types.RoadIndex(0), i.commitQCs.next)
	require.Equal(t, types.GlobalBlockNumber(0), i.appVotes.first)
	require.Equal(t, types.GlobalBlockNumber(0), i.appVotes.next)
	for _, lane := range committee.Lanes().All() {
		require.Equal(t, types.BlockNumber(0), i.blocks[lane].first)
		require.Equal(t, types.BlockNumber(0), i.blocks[lane].next)
		require.Equal(t, types.BlockNumber(0), i.votes[lane].first)
		require.Equal(t, types.BlockNumber(0), i.votes[lane].next)
	}
}

func TestNewInnerLoadedAppQCAdvancesQueues(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 4)

	roadIdx := types.RoadIndex(5)
	globalNum := types.GlobalBlockNumber(42)
	appProposal := types.NewAppProposal(globalNum, roadIdx, types.GenAppHash(rng))
	appQC := types.NewAppQC(makeAppVotes(keys, appProposal))

	loaded := &loadedAvailState{
		appQC: utils.Some[*types.AppQC](appQC),
	}

	i := newInner(committee, utils.Some(loaded))

	// latestAppQC should be restored.
	aq, ok := i.latestAppQC.Get()
	require.True(t, ok)
	require.Equal(t, roadIdx, aq.Proposal().RoadIndex())
	require.Equal(t, globalNum, aq.Proposal().GlobalNumber())

	// commitQCs queue should skip past the loaded AppQC's road index.
	require.Equal(t, roadIdx+1, i.commitQCs.first)
	require.Equal(t, roadIdx+1, i.commitQCs.next)

	// appVotes queue should skip past the loaded AppQC's global block number.
	require.Equal(t, globalNum+1, i.appVotes.first)
	require.Equal(t, globalNum+1, i.appVotes.next)
}

func TestNewInnerLoadedAppQCNone(t *testing.T) {
	rng := utils.TestRng()
	committee, _ := types.GenCommittee(rng, 4)

	loaded := &loadedAvailState{
		appQC: utils.None[*types.AppQC](),
	}

	i := newInner(committee, utils.Some(loaded))

	// No AppQC loaded, queues should start at 0.
	require.False(t, i.latestAppQC.IsPresent())
	require.Equal(t, types.RoadIndex(0), i.commitQCs.first)
	require.Equal(t, types.GlobalBlockNumber(0), i.appVotes.first)
}

func TestNewInnerLoadedBlocksContiguous(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 4)
	lane := keys[0].Public()

	// Build 3 contiguous blocks: 5, 6, 7.
	var parent types.BlockHeaderHash
	var bs []persist.LoadedBlock
	for n := types.BlockNumber(5); n < 8; n++ {
		b := testSignedBlock(keys[0], lane, n, parent, rng)
		parent = b.Msg().Block().Header().Hash()
		bs = append(bs, persist.LoadedBlock{Number: n, Proposal: b})
	}

	loaded := &loadedAvailState{
		appQC:  utils.None[*types.AppQC](),
		blocks: map[types.LaneID][]persist.LoadedBlock{lane: bs},
	}

	i := newInner(committee, utils.Some(loaded))

	q := i.blocks[lane]
	require.Equal(t, types.BlockNumber(5), q.first)
	require.Equal(t, types.BlockNumber(8), q.next)
	for j, b := range bs {
		require.Equal(t, b.Proposal, q.q[types.BlockNumber(5)+types.BlockNumber(j)])
	}

	// Votes queue should be aligned.
	vq := i.votes[lane]
	require.Equal(t, types.BlockNumber(5), vq.first)
	require.Equal(t, types.BlockNumber(5), vq.next)

	// nextBlockToPersist: loaded lane at q.next, other lanes at 0 (map zero-value).
	require.NotNil(t, i.nextBlockToPersist)
	require.Equal(t, types.BlockNumber(8), i.nextBlockToPersist[lane])
	for _, other := range committee.Lanes().All() {
		if other != lane {
			require.Equal(t, types.BlockNumber(0), i.nextBlockToPersist[other])
		}
	}
}

func TestNewInnerLoadedBlocksEmptySlice(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 4)
	lane := keys[0].Public()

	loaded := &loadedAvailState{
		appQC:  utils.None[*types.AppQC](),
		blocks: map[types.LaneID][]persist.LoadedBlock{lane: {}},
	}

	i := newInner(committee, utils.Some(loaded))

	q := i.blocks[lane]
	require.Equal(t, types.BlockNumber(0), q.first)
	require.Equal(t, types.BlockNumber(0), q.next)
}

func TestNewInnerLoadedBlocksUnknownLane(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 4)

	unknownKey := types.GenSecretKey(rng)
	unknownLane := unknownKey.Public()

	b := testSignedBlock(unknownKey, unknownLane, 0, types.BlockHeaderHash{}, rng)
	loaded := &loadedAvailState{
		appQC:  utils.None[*types.AppQC](),
		blocks: map[types.LaneID][]persist.LoadedBlock{unknownLane: {{Number: 0, Proposal: b}}},
	}

	i := newInner(committee, utils.Some(loaded))

	for _, lane := range committee.Lanes().All() {
		q := i.blocks[lane]
		require.Equal(t, types.BlockNumber(0), q.first)
		require.Equal(t, types.BlockNumber(0), q.next)
	}
	_ = keys
}

func TestNewInnerLoadedAppQCAndBlocks(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 4)
	lane := keys[0].Public()

	roadIdx := types.RoadIndex(3)
	globalNum := types.GlobalBlockNumber(10)
	appProposal := types.NewAppProposal(globalNum, roadIdx, types.GenAppHash(rng))
	appQC := types.NewAppQC(makeAppVotes(keys, appProposal))

	var parent types.BlockHeaderHash
	var bs []persist.LoadedBlock
	for n := types.BlockNumber(7); n < 9; n++ {
		b := testSignedBlock(keys[0], lane, n, parent, rng)
		parent = b.Msg().Block().Header().Hash()
		bs = append(bs, persist.LoadedBlock{Number: n, Proposal: b})
	}

	loaded := &loadedAvailState{
		appQC:  utils.Some[*types.AppQC](appQC),
		blocks: map[types.LaneID][]persist.LoadedBlock{lane: bs},
	}

	i := newInner(committee, utils.Some(loaded))

	aq, ok := i.latestAppQC.Get()
	require.True(t, ok)
	require.Equal(t, roadIdx, aq.Proposal().RoadIndex())

	require.Equal(t, roadIdx+1, i.commitQCs.first)
	require.Equal(t, globalNum+1, i.appVotes.first)

	q := i.blocks[lane]
	require.Equal(t, types.BlockNumber(7), q.first)
	require.Equal(t, types.BlockNumber(9), q.next)

	vq := i.votes[lane]
	require.Equal(t, types.BlockNumber(7), vq.first)
	require.Equal(t, types.BlockNumber(7), vq.next)
}

func TestNewInnerLoadedBlocksMultipleLanes(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 4)
	lane0 := keys[0].Public()
	lane1 := keys[1].Public()

	var parent0 types.BlockHeaderHash
	var bs0 []persist.LoadedBlock
	for n := types.BlockNumber(2); n < 4; n++ {
		b := testSignedBlock(keys[0], lane0, n, parent0, rng)
		parent0 = b.Msg().Block().Header().Hash()
		bs0 = append(bs0, persist.LoadedBlock{Number: n, Proposal: b})
	}

	var parent1 types.BlockHeaderHash
	var bs1 []persist.LoadedBlock
	for n := types.BlockNumber(0); n < 3; n++ {
		b := testSignedBlock(keys[1], lane1, n, parent1, rng)
		parent1 = b.Msg().Block().Header().Hash()
		bs1 = append(bs1, persist.LoadedBlock{Number: n, Proposal: b})
	}

	loaded := &loadedAvailState{
		appQC:  utils.None[*types.AppQC](),
		blocks: map[types.LaneID][]persist.LoadedBlock{lane0: bs0, lane1: bs1},
	}

	i := newInner(committee, utils.Some(loaded))

	q0 := i.blocks[lane0]
	require.Equal(t, types.BlockNumber(2), q0.first)
	require.Equal(t, types.BlockNumber(4), q0.next)

	q1 := i.blocks[lane1]
	require.Equal(t, types.BlockNumber(0), q1.first)
	require.Equal(t, types.BlockNumber(3), q1.next)

	require.Equal(t, types.BlockNumber(2), i.votes[lane0].first)
	require.Equal(t, types.BlockNumber(0), i.votes[lane1].first)

	// nextBlockToPersist reflects q.next per loaded lane.
	require.Equal(t, types.BlockNumber(4), i.nextBlockToPersist[lane0])
	require.Equal(t, types.BlockNumber(3), i.nextBlockToPersist[lane1])
}
