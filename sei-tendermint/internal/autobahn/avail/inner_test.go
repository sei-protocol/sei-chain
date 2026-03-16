package avail

import (
	"testing"
	"time"

	pb "github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"

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

	makeQC := func(_ types.RoadIndex, prev utils.Option[*types.CommitQC]) *types.CommitQC {
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

	// Create an AppQC for index 1
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
		require.False(t, updated, "prune should not update for mismatched indices")
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

	i, err := newInner(committee, utils.None[*loadedAvailState]())
	require.NoError(t, err)

	require.False(t, i.latestAppQC.IsPresent())
	require.NotNil(t, i.nextBlockToPersist)
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

func TestDecodePruneAnchorIncomplete(t *testing.T) {
	rng := utils.TestRng()
	_, keys := types.GenCommittee(rng, 4)

	appProposal := types.NewAppProposal(42, 5, types.GenAppHash(rng))
	appQC := types.NewAppQC(makeAppVotes(keys, appProposal))

	_, err := PruneAnchorConv.Decode(&pb.PersistedAvailPruneAnchor{
		AppQc: types.AppQCConv.Encode(appQC),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "incomplete prune anchor")
}

func TestNewInnerLoadedNoAnchor(t *testing.T) {
	rng := utils.TestRng()
	committee, _ := types.GenCommittee(rng, 4)

	loaded := &loadedAvailState{}

	i, err := newInner(committee, utils.Some(loaded))
	require.NoError(t, err)

	// No anchor loaded, queues should start at 0.
	require.False(t, i.latestAppQC.IsPresent())
	require.Equal(t, types.RoadIndex(0), i.commitQCs.first)
	require.Equal(t, types.GlobalBlockNumber(0), i.appVotes.first)
}

func TestNewInnerLoadedBlocksContiguous(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 4)
	lane := keys[0].Public()

	// Build 3 contiguous blocks: 0, 1, 2.
	var parent types.BlockHeaderHash
	var bs []persist.LoadedBlock
	for n := types.BlockNumber(0); n < 3; n++ {
		b := testSignedBlock(keys[0], lane, n, parent, rng)
		parent = b.Msg().Block().Header().Hash()
		bs = append(bs, persist.LoadedBlock{Number: n, Proposal: b})
	}

	loaded := &loadedAvailState{
		blocks: map[types.LaneID][]persist.LoadedBlock{lane: bs},
	}

	i, err := newInner(committee, utils.Some(loaded))
	require.NoError(t, err)

	q := i.blocks[lane]
	require.Equal(t, types.BlockNumber(0), q.first)
	require.Equal(t, types.BlockNumber(3), q.next)
	for j, b := range bs {
		require.Equal(t, b.Proposal, q.q[types.BlockNumber(j)])
	}

	// nextBlockToPersist: loaded lane at q.next, other lanes at 0 (map zero-value).
	require.NotNil(t, i.nextBlockToPersist)
	require.Equal(t, types.BlockNumber(3), i.nextBlockToPersist[lane])
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
		blocks: map[types.LaneID][]persist.LoadedBlock{lane: {}},
	}

	i, err := newInner(committee, utils.Some(loaded))
	require.NoError(t, err)

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
		blocks: map[types.LaneID][]persist.LoadedBlock{unknownLane: {{Number: 0, Proposal: b}}},
	}

	i, err := newInner(committee, utils.Some(loaded))
	require.NoError(t, err)

	for _, lane := range committee.Lanes().All() {
		q := i.blocks[lane]
		require.Equal(t, types.BlockNumber(0), q.first)
		require.Equal(t, types.BlockNumber(0), q.next)
	}
	_ = keys
}

func TestNewInnerLoadedBlocksMultipleLanes(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 4)
	lane0 := keys[0].Public()
	lane1 := keys[1].Public()

	var parent0 types.BlockHeaderHash
	var bs0 []persist.LoadedBlock
	for n := types.BlockNumber(0); n < 2; n++ {
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
		blocks: map[types.LaneID][]persist.LoadedBlock{lane0: bs0, lane1: bs1},
	}

	i, err := newInner(committee, utils.Some(loaded))
	require.NoError(t, err)

	q0 := i.blocks[lane0]
	require.Equal(t, types.BlockNumber(0), q0.first)
	require.Equal(t, types.BlockNumber(2), q0.next)

	q1 := i.blocks[lane1]
	require.Equal(t, types.BlockNumber(0), q1.first)
	require.Equal(t, types.BlockNumber(3), q1.next)

	// nextBlockToPersist reflects q.next per loaded lane.
	require.Equal(t, types.BlockNumber(2), i.nextBlockToPersist[lane0])
	require.Equal(t, types.BlockNumber(3), i.nextBlockToPersist[lane1])
}

func TestNewInnerLoadedCommitQCsNoAppQC(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 4)

	// Create 3 sequential CommitQCs.
	qcs := make([]*types.CommitQC, 3)
	prev := utils.None[*types.CommitQC]()
	for i := range qcs {
		qcs[i] = makeCommitQC(rng, committee, keys, prev, nil, utils.None[*types.AppQC]())
		prev = utils.Some(qcs[i])
	}

	var loadedQCs []persist.LoadedCommitQC
	for i, qc := range qcs {
		loadedQCs = append(loadedQCs, persist.LoadedCommitQC{Index: types.RoadIndex(i), QC: qc})
	}

	loaded := &loadedAvailState{
		commitQCs: loadedQCs,
	}

	inner, err := newInner(committee, utils.Some(loaded))
	require.NoError(t, err)

	// Without anchor, commitQCs.first = 0. All 3 should be restored.
	require.Equal(t, types.RoadIndex(0), inner.commitQCs.first)
	require.Equal(t, types.RoadIndex(3), inner.commitQCs.next)
	for i, qc := range qcs {
		require.NoError(t, utils.TestDiff(qc, inner.commitQCs.q[types.RoadIndex(i)]))
	}

	// latestCommitQC should be set to the last loaded one.
	latest, ok := inner.latestCommitQC.Load().Get()
	require.True(t, ok)
	require.NoError(t, utils.TestDiff(qcs[2], latest))
}

func TestNewInnerLoadedCommitQCsWithAppQC(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 4)

	// AppQC at road index 2.
	roadIdx := types.RoadIndex(2)
	globalNum := types.GlobalBlockNumber(10)
	appProposal := types.NewAppProposal(globalNum, roadIdx, types.GenAppHash(rng))
	appQC := types.NewAppQC(makeAppVotes(keys, appProposal))

	// Create 5 sequential CommitQCs (indices 0-4).
	qcs := make([]*types.CommitQC, 5)
	prev := utils.None[*types.CommitQC]()
	for i := range qcs {
		qcs[i] = makeCommitQC(rng, committee, keys, prev, nil, utils.None[*types.AppQC]())
		prev = utils.Some(qcs[i])
	}

	// Pre-filtered: only commitQCs >= anchor road index (2).
	loadedQCs := []persist.LoadedCommitQC{
		{Index: 2, QC: qcs[2]},
		{Index: 3, QC: qcs[3]},
		{Index: 4, QC: qcs[4]},
	}

	loaded := &loadedAvailState{
		pruneAnchor: utils.Some(&PruneAnchor{AppQC: appQC, CommitQC: qcs[2]}),
		commitQCs:   loadedQCs,
	}

	inner, err := newInner(committee, utils.Some(loaded))
	require.NoError(t, err)

	// latestAppQC should be set by prune.
	aq, ok := inner.latestAppQC.Get()
	require.True(t, ok)
	require.Equal(t, roadIdx, aq.Proposal().RoadIndex())

	// inner.prune(appQC@2, commitQC@2) sets commitQCs.first = 2.
	// Indices 2, 3 and 4 remain; earlier ones are pruned.
	require.Equal(t, types.RoadIndex(2), inner.commitQCs.first)
	require.Equal(t, types.RoadIndex(5), inner.commitQCs.next)
	require.NoError(t, utils.TestDiff(qcs[2], inner.commitQCs.q[2]))
	require.NoError(t, utils.TestDiff(qcs[3], inner.commitQCs.q[3]))
	require.NoError(t, utils.TestDiff(qcs[4], inner.commitQCs.q[4]))

	// latestCommitQC should be the last restored one (index 4).
	latest, ok := inner.latestCommitQC.Load().Get()
	require.True(t, ok)
	require.NoError(t, utils.TestDiff(qcs[4], latest))
}

func TestNewInnerLoadedAllThree(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 4)
	lane := keys[0].Public()

	// AppQC at road index 2.
	roadIdx := types.RoadIndex(2)
	appProposal := types.NewAppProposal(10, roadIdx, types.GenAppHash(rng))
	appQC := types.NewAppQC(makeAppVotes(keys, appProposal))

	// CommitQCs 0-4.
	qcs := make([]*types.CommitQC, 5)
	prev := utils.None[*types.CommitQC]()
	for i := range qcs {
		qcs[i] = makeCommitQC(rng, committee, keys, prev, nil, utils.None[*types.AppQC]())
		prev = utils.Some(qcs[i])
	}
	// Pre-filtered: only commitQCs >= anchor road index (2).
	loadedQCs := []persist.LoadedCommitQC{
		{Index: 2, QC: qcs[2]},
		{Index: 3, QC: qcs[3]},
		{Index: 4, QC: qcs[4]},
	}

	// Blocks 0-2 on one lane (nil laneQCs → lr.First()=0 after prune).
	var parent types.BlockHeaderHash
	var bs []persist.LoadedBlock
	for n := types.BlockNumber(0); n < 3; n++ {
		b := testSignedBlock(keys[0], lane, n, parent, rng)
		parent = b.Msg().Block().Header().Hash()
		bs = append(bs, persist.LoadedBlock{Number: n, Proposal: b})
	}

	loaded := &loadedAvailState{
		pruneAnchor: utils.Some(&PruneAnchor{AppQC: appQC, CommitQC: qcs[2]}),
		commitQCs:   loadedQCs,
		blocks:      map[types.LaneID][]persist.LoadedBlock{lane: bs},
	}

	inner, err := newInner(committee, utils.Some(loaded))
	require.NoError(t, err)

	// AppQC restored.
	aq, ok := inner.latestAppQC.Get()
	require.True(t, ok)
	require.Equal(t, roadIdx, aq.Proposal().RoadIndex())

	// CommitQCs: prune pushed qcs[2], loading skipped it, added 3 and 4.
	require.Equal(t, types.RoadIndex(2), inner.commitQCs.first)
	require.Equal(t, types.RoadIndex(5), inner.commitQCs.next)

	// Blocks loaded.
	q := inner.blocks[lane]
	require.Equal(t, types.BlockNumber(0), q.first)
	require.Equal(t, types.BlockNumber(3), q.next)
	require.Equal(t, types.BlockNumber(3), inner.nextBlockToPersist[lane])

	// latestCommitQC is the last loaded one.
	latest, ok := inner.latestCommitQC.Load().Get()
	require.True(t, ok)
	require.NoError(t, utils.TestDiff(qcs[4], latest))
}

func TestPruneAdvancesNextBlockToPersist(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 4)
	lane := keys[0].Public()

	i, err := newInner(committee, utils.None[*loadedAvailState]())
	require.NoError(t, err)

	// Push blocks 0-4 on one lane.
	var parent types.BlockHeaderHash
	for n := types.BlockNumber(0); n < 5; n++ {
		b := testSignedBlock(keys[0], lane, n, parent, rng)
		parent = b.Msg().Block().Header().Hash()
		i.blocks[lane].pushBack(b)
	}
	// Simulate partial persistence: only block 0 persisted.
	i.nextBlockToPersist[lane] = 1

	// Build CommitQCs with lane ranges that reference actual blocks.
	// Each CommitQC covers one block on the lane via a LaneQC.
	qcs := make([]*types.CommitQC, 3)
	prev := utils.None[*types.CommitQC]()
	for j := range qcs {
		bn := types.BlockNumber(j)
		h := i.blocks[lane].q[bn].Msg().Block().Header()
		laneQCs := map[types.LaneID]*types.LaneQC{
			lane: types.NewLaneQC(makeLaneVotes(keys, h)[:committee.LaneQuorum()]),
		}
		qcs[j] = makeCommitQC(rng, committee, keys, prev, laneQCs, utils.None[*types.AppQC]())
		prev = utils.Some(qcs[j])
		i.commitQCs.pushBack(qcs[j])
	}

	// Verify QC@2's lane range actually covers blocks (First > 0).
	lr := qcs[2].LaneRange(lane)
	require.Greater(t, lr.First(), types.BlockNumber(0),
		"CommitQC lane range should reference blocks for this test to be meaningful")

	// AppQC at index 2 → prune will fast-forward blocks past the cursor.
	appProposal := types.NewAppProposal(10, 2, types.GenAppHash(rng))
	appQC := types.NewAppQC(makeAppVotes(keys, appProposal))

	updated, err := i.prune(appQC, qcs[2])
	require.NoError(t, err)
	require.True(t, updated)

	// nextBlockToPersist must have advanced to at least the lane's new first
	// (determined by CommitQC@2's lane range). Without this fix, it would
	// stay at 1, causing the persist goroutine to busy-loop.
	laneFirst := i.blocks[lane].first
	require.Greater(t, laneFirst, types.BlockNumber(1),
		"prune should have advanced blocks.first past the old cursor")
	require.GreaterOrEqual(t, i.nextBlockToPersist[lane], laneFirst,
		"nextBlockToPersist should advance when prune moves blocks.first past it")
}

func TestNewInnerLoadedCommitQCsAllBeforeAppQCArePruned(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 4)

	// Build 6 CommitQCs (indices 0-5). Anchor at index 5.
	// All stale commitQCs (0-4) were already filtered by loadPersistedState,
	// so newInner receives an empty commitQC slice.
	qcs := make([]*types.CommitQC, 6)
	prev := utils.None[*types.CommitQC]()
	for i := range qcs {
		qcs[i] = makeCommitQC(rng, committee, keys, prev, nil, utils.None[*types.AppQC]())
		prev = utils.Some(qcs[i])
	}

	appProposal := types.NewAppProposal(20, 5, types.GenAppHash(rng))
	appQC := types.NewAppQC(makeAppVotes(keys, appProposal))

	loaded := &loadedAvailState{
		pruneAnchor: utils.Some(&PruneAnchor{AppQC: appQC, CommitQC: qcs[5]}),
	}

	inner, err := newInner(committee, utils.Some(loaded))
	require.NoError(t, err)

	// prune() pushes the anchor's CommitQC into the queue.
	require.Equal(t, types.RoadIndex(5), inner.commitQCs.first)
	require.Equal(t, types.RoadIndex(6), inner.commitQCs.next)
	require.NoError(t, utils.TestDiff(qcs[5], inner.commitQCs.q[5]))
}

func TestNewInnerAnchorWithNoCommitQCFiles(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 4)

	// Simulate crash between anchor write and CommitQC file write:
	// anchor has AppQC@3 + CommitQC@3, but no CommitQC files on disk.
	qcs := make([]*types.CommitQC, 4)
	prev := utils.None[*types.CommitQC]()
	for i := range qcs {
		qcs[i] = makeCommitQC(rng, committee, keys, prev, nil, utils.None[*types.AppQC]())
		prev = utils.Some(qcs[i])
	}

	appProposal := types.NewAppProposal(20, 3, types.GenAppHash(rng))
	appQC := types.NewAppQC(makeAppVotes(keys, appProposal))

	loaded := &loadedAvailState{
		pruneAnchor: utils.Some(&PruneAnchor{AppQC: appQC, CommitQC: qcs[3]}),
	}

	inner, err := newInner(committee, utils.Some(loaded))
	require.NoError(t, err)

	// prune() should push the anchor's CommitQC into the queue.
	require.Equal(t, types.RoadIndex(3), inner.commitQCs.first)
	require.Equal(t, types.RoadIndex(4), inner.commitQCs.next)
	require.NoError(t, utils.TestDiff(qcs[3], inner.commitQCs.q[3]))

	// latestAppQC should be set.
	aq, ok := inner.latestAppQC.Get()
	require.True(t, ok)
	require.Equal(t, types.RoadIndex(3), aq.Proposal().RoadIndex())

	// persistedBlockStart should be initialized from the anchor's CommitQC.
	for _, lane := range committee.Lanes().All() {
		expected := qcs[3].LaneRange(lane).First()
		require.Equal(t, expected, inner.persistedBlockStart[lane])
	}
}

func TestNewInnerLoadedCommitQCsGapReturnsError(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 4)

	qcs := make([]*types.CommitQC, 3)
	prev := utils.None[*types.CommitQC]()
	for i := range qcs {
		qcs[i] = makeCommitQC(rng, committee, keys, prev, nil, utils.None[*types.AppQC]())
		prev = utils.Some(qcs[i])
	}

	// Gap: indices 0, 1, 3 (missing 2). Since the anchor is persisted first,
	// a gap in committed QCs is a bug — newInner should return an error.
	loadedQCs := []persist.LoadedCommitQC{
		{Index: 0, QC: qcs[0]},
		{Index: 1, QC: qcs[1]},
		{Index: 3, QC: qcs[2]},
	}

	loaded := &loadedAvailState{
		commitQCs: loadedQCs,
	}

	_, err := newInner(committee, utils.Some(loaded))
	require.Error(t, err)
	require.Contains(t, err.Error(), "non-contiguous")
}

func TestNewInnerLoadedCommitQCsEmpty(t *testing.T) {
	rng := utils.TestRng()
	committee, _ := types.GenCommittee(rng, 4)

	loaded := &loadedAvailState{
		commitQCs: nil,
	}

	inner, err := newInner(committee, utils.Some(loaded))
	require.NoError(t, err)

	require.Equal(t, types.RoadIndex(0), inner.commitQCs.first)
	require.Equal(t, types.RoadIndex(0), inner.commitQCs.next)
	_, ok := inner.latestCommitQC.Load().Get()
	require.False(t, ok)
}

func TestNewInnerLoadedCommitQCsGapWithAppQCAnchor(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 4)

	// Simulate crash scenario: disk had stale QCs [0,1,2] and a new QC at
	// index 10. loadPersistedState pre-filters stale entries, so newInner
	// only receives [10].
	qcs := make([]*types.CommitQC, 11)
	prev := utils.None[*types.CommitQC]()
	for i := range qcs {
		qcs[i] = makeCommitQC(rng, committee, keys, prev, nil, utils.None[*types.AppQC]())
		prev = utils.Some(qcs[i])
	}

	appProposal := types.NewAppProposal(50, 10, types.GenAppHash(rng))
	appQC := types.NewAppQC(makeAppVotes(keys, appProposal))

	loadedQCs := []persist.LoadedCommitQC{
		{Index: 10, QC: qcs[10]},
	}

	loaded := &loadedAvailState{
		pruneAnchor: utils.Some(&PruneAnchor{AppQC: appQC, CommitQC: qcs[10]}),
		commitQCs:   loadedQCs,
	}

	inner, err := newInner(committee, utils.Some(loaded))
	require.NoError(t, err)

	// Only QC@10 loaded.
	require.Equal(t, types.RoadIndex(10), inner.commitQCs.first)
	require.Equal(t, types.RoadIndex(11), inner.commitQCs.next)
	require.NoError(t, utils.TestDiff(qcs[10], inner.commitQCs.q[10]))

	latest, ok := inner.latestCommitQC.Load().Get()
	require.True(t, ok)
	require.NoError(t, utils.TestDiff(qcs[10], latest))

	// AppQC should be applied via prune.
	aq, ok := inner.latestAppQC.Get()
	require.True(t, ok)
	require.Equal(t, types.RoadIndex(10), aq.Proposal().RoadIndex())
}

func TestNewInnerLoadedCommitQCsBelowAnchorSkipped(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 4)

	// Build 6 CommitQCs (0-5). Anchor at index 3.
	// Loaded list includes stale entries [1, 2] below the anchor plus [3, 4, 5].
	// In production loadPersistedState filters these, but newInner should
	// handle them gracefully via the lqc.Index < commitQCs.next skip.
	qcs := make([]*types.CommitQC, 6)
	prev := utils.None[*types.CommitQC]()
	for i := range qcs {
		qcs[i] = makeCommitQC(rng, committee, keys, prev, nil, utils.None[*types.AppQC]())
		prev = utils.Some(qcs[i])
	}

	appProposal := types.NewAppProposal(20, 3, types.GenAppHash(rng))
	appQC := types.NewAppQC(makeAppVotes(keys, appProposal))

	loadedQCs := []persist.LoadedCommitQC{
		{Index: 1, QC: qcs[1]},
		{Index: 2, QC: qcs[2]},
		{Index: 3, QC: qcs[3]},
		{Index: 4, QC: qcs[4]},
		{Index: 5, QC: qcs[5]},
	}

	loaded := &loadedAvailState{
		pruneAnchor: utils.Some(&PruneAnchor{AppQC: appQC, CommitQC: qcs[3]}),
		commitQCs:   loadedQCs,
	}

	inner, err := newInner(committee, utils.Some(loaded))
	require.NoError(t, err)

	// prune(3) pushes QC@3 (next=4). Indices 1,2,3 are skipped. 4,5 pushed.
	require.Equal(t, types.RoadIndex(3), inner.commitQCs.first)
	require.Equal(t, types.RoadIndex(6), inner.commitQCs.next)
	latest, ok := inner.latestCommitQC.Load().Get()
	require.True(t, ok)
	require.NoError(t, utils.TestDiff(qcs[5], latest))
}

func TestNewInnerLoadedCommitQCsGapAfterAnchorReturnsError(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 4)

	// Anchor at index 2. Loaded commitQCs are [2, 3, 5] — gap at 4.
	// After prune(2), next=3. Index 2 is skipped, 3 pushed (next=4),
	// then 5 != 4 → error.
	qcs := make([]*types.CommitQC, 6)
	prev := utils.None[*types.CommitQC]()
	for i := range qcs {
		qcs[i] = makeCommitQC(rng, committee, keys, prev, nil, utils.None[*types.AppQC]())
		prev = utils.Some(qcs[i])
	}

	appProposal := types.NewAppProposal(10, 2, types.GenAppHash(rng))
	appQC := types.NewAppQC(makeAppVotes(keys, appProposal))

	loadedQCs := []persist.LoadedCommitQC{
		{Index: 2, QC: qcs[2]},
		{Index: 3, QC: qcs[3]},
		{Index: 5, QC: qcs[5]},
	}

	loaded := &loadedAvailState{
		pruneAnchor: utils.Some(&PruneAnchor{AppQC: appQC, CommitQC: qcs[2]}),
		commitQCs:   loadedQCs,
	}

	_, err := newInner(committee, utils.Some(loaded))
	require.Error(t, err)
	require.Contains(t, err.Error(), "non-contiguous")
}

func TestNewInnerLoadedBlocksGapReturnsError(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 4)
	lane := keys[0].Public()

	// Blocks 3, 4, 6, 7 with no anchor — queue starts at 0, so block 3
	// fails the contiguity check immediately (expected 0, got 3).
	var parent types.BlockHeaderHash
	var bs []persist.LoadedBlock
	for _, n := range []types.BlockNumber{3, 4, 6, 7} {
		b := testSignedBlock(keys[0], lane, n, parent, rng)
		parent = b.Msg().Block().Header().Hash()
		bs = append(bs, persist.LoadedBlock{Number: n, Proposal: b})
	}

	loaded := &loadedAvailState{
		blocks: map[types.LaneID][]persist.LoadedBlock{lane: bs},
	}

	_, err := newInner(committee, utils.Some(loaded))
	require.Error(t, err)
	require.Contains(t, err.Error(), "non-contiguous")
}

func TestNewInnerLoadedBlocksParentHashMismatchReturnsError(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 4)
	lane := keys[0].Public()

	// Build blocks 0, 1 with correct chaining, then block 2 with wrong parent.
	var parent types.BlockHeaderHash
	b0 := testSignedBlock(keys[0], lane, 0, parent, rng)
	parent = b0.Msg().Block().Header().Hash()
	b1 := testSignedBlock(keys[0], lane, 1, parent, rng)
	wrongParent := types.GenBlockHeaderHash(rng)
	b2 := testSignedBlock(keys[0], lane, 2, wrongParent, rng)

	bs := []persist.LoadedBlock{
		{Number: 0, Proposal: b0},
		{Number: 1, Proposal: b1},
		{Number: 2, Proposal: b2},
	}

	loaded := &loadedAvailState{
		blocks: map[types.LaneID][]persist.LoadedBlock{lane: bs},
	}

	_, err := newInner(committee, utils.Some(loaded))
	require.Error(t, err)
	require.Contains(t, err.Error(), "parent hash mismatch")
}

func TestNewInnerLoadedBlocksOverCapacityReturnsError(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 4)
	lane := keys[0].Public()

	// Build BlocksPerLane + 5 contiguous blocks — more than the lane capacity.
	// Since runtime enforces the capacity limit, exceeding it on disk indicates
	// corruption or a bug.
	count := BlocksPerLane + 5
	var parent types.BlockHeaderHash
	var bs []persist.LoadedBlock
	for n := types.BlockNumber(0); n < types.BlockNumber(count); n++ {
		b := testSignedBlock(keys[0], lane, n, parent, rng)
		parent = b.Msg().Block().Header().Hash()
		bs = append(bs, persist.LoadedBlock{Number: n, Proposal: b})
	}

	loaded := &loadedAvailState{
		blocks: map[types.LaneID][]persist.LoadedBlock{lane: bs},
	}

	_, err := newInner(committee, utils.Some(loaded))
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds capacity")
}

func TestNewInnerPruneAnchorPrunesBlockQueues(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 4)

	// Build CommitQCs 0-2.
	qcs := make([]*types.CommitQC, 3)
	prev := utils.None[*types.CommitQC]()
	for i := range qcs {
		qcs[i] = makeCommitQC(rng, committee, keys, prev, nil, utils.None[*types.AppQC]())
		prev = utils.Some(qcs[i])
	}

	// AppQC at road index 2, prune anchor is CommitQC[2].
	appProposal := types.NewAppProposal(0, 2, types.GenAppHash(rng))
	appQC := types.NewAppQC(makeAppVotes(keys, appProposal))
	pruneQC := qcs[2]

	lane := keys[0].Public()

	// Persist some blocks starting at the lane range for the prune CommitQC.
	lrFirst := pruneQC.LaneRange(lane).First()
	var parent types.BlockHeaderHash
	var bs []persist.LoadedBlock
	for n := lrFirst; n < lrFirst+3; n++ {
		b := testSignedBlock(keys[0], lane, n, parent, rng)
		parent = b.Msg().Block().Header().Hash()
		bs = append(bs, persist.LoadedBlock{Number: n, Proposal: b})
	}

	loaded := &loadedAvailState{
		pruneAnchor: utils.Some(&PruneAnchor{AppQC: appQC, CommitQC: pruneQC}),
		commitQCs: []persist.LoadedCommitQC{
			{Index: 2, QC: qcs[2]},
		},
		blocks: map[types.LaneID][]persist.LoadedBlock{lane: bs},
	}

	i, err := newInner(committee, utils.Some(loaded))
	require.NoError(t, err)

	// prune() should advance block queue first to the prune anchor's lane range.
	for _, l := range committee.Lanes().All() {
		expected := pruneQC.LaneRange(l).First()
		require.Equal(t, expected, i.blocks[l].first,
			"blocks[%v].first should be advanced by prune to prune anchor lane range", l)
	}
}

func TestNewInnerPruneAnchorCommitQCUsedForPrune(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 4)

	// Build CommitQCs 0-2.
	qcs := make([]*types.CommitQC, 3)
	prev := utils.None[*types.CommitQC]()
	for i := range qcs {
		qcs[i] = makeCommitQC(rng, committee, keys, prev, nil, utils.None[*types.AppQC]())
		prev = utils.Some(qcs[i])
	}

	// AppQC at road index 1, prune anchor is CommitQC[1].
	appProposal := types.NewAppProposal(0, 1, types.GenAppHash(rng))
	appQC := types.NewAppQC(makeAppVotes(keys, appProposal))

	loaded := &loadedAvailState{
		pruneAnchor: utils.Some(&PruneAnchor{AppQC: appQC, CommitQC: qcs[1]}),
		commitQCs: []persist.LoadedCommitQC{
			{Index: 1, QC: qcs[1]},
			{Index: 2, QC: qcs[2]},
		},
	}

	i, err := newInner(committee, utils.Some(loaded))
	require.NoError(t, err)

	// prune(appQC@1, pruneQC@1) should advance commitQCs.first to 1.
	require.Equal(t, types.RoadIndex(1), i.commitQCs.first)
	// CommitQCs 1 and 2 should still be loaded.
	require.Equal(t, types.RoadIndex(3), i.commitQCs.next)
}
