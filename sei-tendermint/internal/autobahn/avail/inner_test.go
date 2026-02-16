package avail

import (
	"testing"
	"time"

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
	require.Contains(t, err.Error(), "mismatched QCs")

	// Get the inner state
	for inner := range state.inner.Lock() {
		// Now call prune with mismatched QCs directly to test the safety check
		updated, err := inner.prune(appQC1, qc0)

		require.Error(t, err)
		require.Contains(t, err.Error(), "mismatched QCs")
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

	i := newInner(committee, nil)

	require.False(t, i.latestAppQC.IsPresent())
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
		appQC:  utils.Some[*types.AppQC](appQC),
		blocks: nil,
	}

	i := newInner(committee, loaded)

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
		appQC:  utils.None[*types.AppQC](),
		blocks: nil,
	}

	i := newInner(committee, loaded)

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
	bs := map[types.BlockNumber]*types.Signed[*types.LaneProposal]{}
	for n := types.BlockNumber(5); n < 8; n++ {
		b := testSignedBlock(keys[0], lane, n, parent, rng)
		parent = b.Msg().Block().Header().Hash()
		bs[n] = b
	}

	loaded := &loadedAvailState{
		appQC:  utils.None[*types.AppQC](),
		blocks: map[types.LaneID]map[types.BlockNumber]*types.Signed[*types.LaneProposal]{lane: bs},
	}

	i := newInner(committee, loaded)

	q := i.blocks[lane]
	require.Equal(t, types.BlockNumber(5), q.first)
	require.Equal(t, types.BlockNumber(8), q.next)
	for n := types.BlockNumber(5); n < 8; n++ {
		require.Equal(t, bs[n], q.q[n])
	}

	// Votes queue should be aligned.
	vq := i.votes[lane]
	require.Equal(t, types.BlockNumber(5), vq.first)
	require.Equal(t, types.BlockNumber(5), vq.next)
}

func TestNewInnerLoadedBlocksWithGap(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 4)
	lane := keys[0].Public()

	// Blocks 3, 4, 6 (gap at 5).
	var parent types.BlockHeaderHash
	bs := map[types.BlockNumber]*types.Signed[*types.LaneProposal]{}
	for _, n := range []types.BlockNumber{3, 4, 6} {
		b := testSignedBlock(keys[0], lane, n, parent, rng)
		parent = b.Msg().Block().Header().Hash()
		bs[n] = b
	}

	loaded := &loadedAvailState{
		appQC:  utils.None[*types.AppQC](),
		blocks: map[types.LaneID]map[types.BlockNumber]*types.Signed[*types.LaneProposal]{lane: bs},
	}

	i := newInner(committee, loaded)

	// Should load only 3, 4 (stop at gap before 5).
	q := i.blocks[lane]
	require.Equal(t, types.BlockNumber(3), q.first)
	require.Equal(t, types.BlockNumber(5), q.next)
	require.Equal(t, bs[3], q.q[types.BlockNumber(3)])
	require.Equal(t, bs[4], q.q[types.BlockNumber(4)])
	_, has6 := q.q[types.BlockNumber(6)]
	require.False(t, has6, "block 6 should not be loaded (after gap)")
}

func TestNewInnerLoadedBlocksEmptyMap(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 4)
	lane := keys[0].Public()

	loaded := &loadedAvailState{
		appQC: utils.None[*types.AppQC](),
		blocks: map[types.LaneID]map[types.BlockNumber]*types.Signed[*types.LaneProposal]{
			lane: {},
		},
	}

	i := newInner(committee, loaded)

	// Empty block map should leave queue at 0.
	q := i.blocks[lane]
	require.Equal(t, types.BlockNumber(0), q.first)
	require.Equal(t, types.BlockNumber(0), q.next)
}

func TestNewInnerLoadedBlocksUnknownLane(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 4)

	// Create a lane that doesn't belong to the committee.
	unknownKey := types.GenSecretKey(rng)
	unknownLane := unknownKey.Public()

	b := testSignedBlock(unknownKey, unknownLane, 0, types.BlockHeaderHash{}, rng)
	loaded := &loadedAvailState{
		appQC: utils.None[*types.AppQC](),
		blocks: map[types.LaneID]map[types.BlockNumber]*types.Signed[*types.LaneProposal]{
			unknownLane: {0: b},
		},
	}

	i := newInner(committee, loaded)

	// Unknown lane should be silently skipped; known lanes unaffected.
	for _, lane := range committee.Lanes().All() {
		q := i.blocks[lane]
		require.Equal(t, types.BlockNumber(0), q.first)
		require.Equal(t, types.BlockNumber(0), q.next)
	}
	_ = keys // suppress unused
}

func TestNewInnerLoadedAppQCAndBlocks(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 4)
	lane := keys[0].Public()

	roadIdx := types.RoadIndex(3)
	globalNum := types.GlobalBlockNumber(10)
	appProposal := types.NewAppProposal(globalNum, roadIdx, types.GenAppHash(rng))
	appQC := types.NewAppQC(makeAppVotes(keys, appProposal))

	// Build 2 contiguous blocks: 7, 8.
	var parent types.BlockHeaderHash
	bs := map[types.BlockNumber]*types.Signed[*types.LaneProposal]{}
	for n := types.BlockNumber(7); n < 9; n++ {
		b := testSignedBlock(keys[0], lane, n, parent, rng)
		parent = b.Msg().Block().Header().Hash()
		bs[n] = b
	}

	loaded := &loadedAvailState{
		appQC: utils.Some[*types.AppQC](appQC),
		blocks: map[types.LaneID]map[types.BlockNumber]*types.Signed[*types.LaneProposal]{
			lane: bs,
		},
	}

	i := newInner(committee, loaded)

	// AppQC should be restored.
	aq, ok := i.latestAppQC.Get()
	require.True(t, ok)
	require.Equal(t, roadIdx, aq.Proposal().RoadIndex())

	// Queues advanced by AppQC.
	require.Equal(t, roadIdx+1, i.commitQCs.first)
	require.Equal(t, globalNum+1, i.appVotes.first)

	// Blocks restored.
	q := i.blocks[lane]
	require.Equal(t, types.BlockNumber(7), q.first)
	require.Equal(t, types.BlockNumber(9), q.next)

	// Votes aligned.
	vq := i.votes[lane]
	require.Equal(t, types.BlockNumber(7), vq.first)
	require.Equal(t, types.BlockNumber(7), vq.next)
}

func TestNewInnerLoadedBlocksMultipleLanes(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 4)
	lane0 := keys[0].Public()
	lane1 := keys[1].Public()

	// Lane 0: blocks 2, 3.
	bs0 := map[types.BlockNumber]*types.Signed[*types.LaneProposal]{}
	var parent0 types.BlockHeaderHash
	for n := types.BlockNumber(2); n < 4; n++ {
		b := testSignedBlock(keys[0], lane0, n, parent0, rng)
		parent0 = b.Msg().Block().Header().Hash()
		bs0[n] = b
	}

	// Lane 1: blocks 0, 1, 2.
	bs1 := map[types.BlockNumber]*types.Signed[*types.LaneProposal]{}
	var parent1 types.BlockHeaderHash
	for n := types.BlockNumber(0); n < 3; n++ {
		b := testSignedBlock(keys[1], lane1, n, parent1, rng)
		parent1 = b.Msg().Block().Header().Hash()
		bs1[n] = b
	}

	loaded := &loadedAvailState{
		appQC: utils.None[*types.AppQC](),
		blocks: map[types.LaneID]map[types.BlockNumber]*types.Signed[*types.LaneProposal]{
			lane0: bs0,
			lane1: bs1,
		},
	}

	i := newInner(committee, loaded)

	// Lane 0.
	q0 := i.blocks[lane0]
	require.Equal(t, types.BlockNumber(2), q0.first)
	require.Equal(t, types.BlockNumber(4), q0.next)

	// Lane 1.
	q1 := i.blocks[lane1]
	require.Equal(t, types.BlockNumber(0), q1.first)
	require.Equal(t, types.BlockNumber(3), q1.next)

	// Votes aligned per lane.
	require.Equal(t, types.BlockNumber(2), i.votes[lane0].first)
	require.Equal(t, types.BlockNumber(0), i.votes[lane1].first)
}
