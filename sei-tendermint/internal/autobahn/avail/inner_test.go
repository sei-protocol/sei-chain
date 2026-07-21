package avail

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/memblock"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus/persist"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	pb "github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/stretchr/testify/require"
)

func newTestDataState(cfg *data.Config) *data.State {
	return utils.OrPanic1(data.NewState(cfg, memblock.NewBlockDB()))
}

func TestPruneMismatchedIndices(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistryAt(rng, 4, 0)
	ep, err := registry.EpochAt(0)
	require.NoError(t, err)

	makeCommitQC := func(prev utils.Option[*types.CommitQC]) *types.CommitQC {
		l := keys[0].Public()
		lr := types.LaneRangeOpt(prev, l)
		b := types.NewBlock(l, lr.Next(), lr.LastHash(), types.GenPayload(rng))
		lqcs := map[types.LaneID]*types.LaneQC{
			l: types.NewLaneQC(makeLaneVotes(keys, b.Header())),
		}
		return makeCommitQC(ep, keys, prev, lqcs, utils.None[*types.AppQC]())
	}
	makeAppQC := func(qcForRange *types.CommitQC, qcForIndex *types.CommitQC) *types.AppQC {
		gr := qcForRange.GlobalRange()
		require.True(t, gr.Len() > 0)
		ap := types.NewAppProposal(gr.First, qcForIndex.Index(), types.GenAppHash(rng), 0)
		return types.NewAppQC(makeAppVotes(keys, ap))
	}

	qc0 := makeCommitQC(utils.None[*types.CommitQC]())
	qc1 := makeCommitQC(utils.Some(qc0))

	t.Logf("test State.PushAppQC")
	ds := newTestDataState(&data.Config{Registry: registry})
	state, err := NewState(keys[0], ds, utils.Some(t.TempDir()))
	require.NoError(t, err)
	require.Error(t, state.PushAppQC(t.Context(), makeAppQC(qc0, qc0), qc1), "bad range, bad index should fail")
	require.Error(t, state.PushAppQC(t.Context(), makeAppQC(qc1, qc0), qc1), "good range, bad index should fail")
	require.Error(t, state.PushAppQC(t.Context(), makeAppQC(qc0, qc1), qc1), "bad range, good index should fail")
	require.NoError(t, state.PushAppQC(t.Context(), makeAppQC(qc1, qc1), qc1), "good range, good index should succeed")

	t.Logf("test inner.prune")
	ds = newTestDataState(&data.Config{Registry: registry})
	state, err = NewState(keys[0], ds, utils.Some(t.TempDir()))
	require.NoError(t, err)
	for inner := range state.inner.Lock() {
		_, err := inner.prune(makeAppQC(qc1, qc0), qc1)
		require.Error(t, err, "good range, bad index should fail")
		require.False(t, inner.latestAppQC.IsPresent(), "latestAppQC should not have been updated")
		_, err = inner.prune(makeAppQC(qc1, qc1), qc1)
		require.NoError(t, err, "good range, good index should succeed")
	}
}

func testSignedBlock(key types.SecretKey, lane types.LaneID, n types.BlockNumber, parent types.BlockHeaderHash, rng utils.Rng) *types.Signed[*types.LaneProposal] {
	block := types.NewBlock(lane, n, parent, types.GenPayload(rng))
	return types.Sign(key, types.NewLaneProposal(block))
}

func TestNewInnerFreshStart(t *testing.T) {
	rng := utils.TestRng()
	registry, _ := epoch.GenRegistryAt(rng, 4, 0)

	i, err := newInner(registry, utils.OrPanic1(registry.DuoAt(0)), utils.None[*loadedAvailState]())
	require.NoError(t, err)

	require.False(t, i.latestAppQC.IsPresent())
	require.NotNil(t, i.lanes)
	require.Equal(t, types.RoadIndex(0), i.commitQCs.first)
	require.Equal(t, types.RoadIndex(0), i.commitQCs.next)
	require.Equal(t, registry.FirstBlock(), i.appVotes.first)
	require.Equal(t, registry.FirstBlock(), i.appVotes.next)
	for lane := range registry.LatestEpoch().Committee().Lanes().All() {
		require.Equal(t, types.BlockNumber(0), i.lanes[lane].blocks.first)
		require.Equal(t, types.BlockNumber(0), i.lanes[lane].blocks.next)
		require.Equal(t, types.BlockNumber(0), i.lanes[lane].votes.first)
		require.Equal(t, types.BlockNumber(0), i.lanes[lane].votes.next)
	}
}

func TestDecodePruneAnchorIncomplete(t *testing.T) {
	rng := utils.TestRng()
	_, keys := epoch.GenRegistryAt(rng, 4, 0)

	appProposal := types.NewAppProposal(42, 5, types.GenAppHash(rng), 0)
	appQC := types.NewAppQC(makeAppVotes(keys, appProposal))

	_, err := PruneAnchorConv.Decode(&pb.PersistedAvailPruneAnchor{
		AppQc: types.AppQCConv.Encode(appQC),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "incomplete prune anchor")
}

func TestNewInnerLoadedNoAnchor(t *testing.T) {
	rng := utils.TestRng()
	registry, _ := epoch.GenRegistryAt(rng, 4, 0)

	loaded := &loadedAvailState{}

	i, err := newInner(registry, utils.OrPanic1(registry.DuoAt(0)), utils.Some(loaded))
	require.NoError(t, err)

	// No anchor loaded, app votes should start at the registry's first block.
	require.False(t, i.latestAppQC.IsPresent())
	require.Equal(t, types.RoadIndex(0), i.commitQCs.first)
	require.Equal(t, registry.FirstBlock(), i.appVotes.first)
}

func TestNewInnerRequiresAnchorWhenEpochNonZero(t *testing.T) {
	rng := utils.TestRng()
	registry, _ := epoch.GenRegistryAt(rng, 4, 0)
	// NewRegistry already seeds epoch 1.
	duo := utils.OrPanic1(registry.DuoAt(epoch.FirstRoad(1)))
	require.Equal(t, types.EpochIndex(1), duo.Current.EpochIndex())

	_, err := newInner(registry, duo, utils.Some(&loadedAvailState{}))
	require.Error(t, err)
	_, err = newInner(registry, duo, utils.None[*loadedAvailState]())
	require.Error(t, err)
}

func TestNewInnerLoadedBlocksContiguous(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistryAt(rng, 4, 0)
	lane := keys[0].Public()

	// Build 3 contiguous blocks: 0, 1, 2.
	var parent types.BlockHeaderHash
	var bs []persist.LoadedBlock
	for n := range types.BlockNumber(3) {
		b := testSignedBlock(keys[0], lane, n, parent, rng)
		parent = b.Msg().Block().Header().Hash()
		bs = append(bs, persist.LoadedBlock{Number: n, Proposal: b})
	}

	loaded := &loadedAvailState{
		blocks: map[types.LaneID][]persist.LoadedBlock{lane: bs},
	}

	i, err := newInner(registry, utils.OrPanic1(registry.DuoAt(0)), utils.Some(loaded))
	require.NoError(t, err)

	q := i.lanes[lane].blocks
	require.Equal(t, types.BlockNumber(0), q.first)
	require.Equal(t, types.BlockNumber(3), q.next)
	for j, b := range bs {
		require.Equal(t, b.Proposal, q.q[types.BlockNumber(j)])
	}

	// nextBlockToPersist: loaded lane at q.next, other lanes at 0 (map zero-value).
	require.NotNil(t, i.lanes)
	require.Equal(t, types.BlockNumber(3), i.lanes[lane].nextBlockToPersist)
	for other := range registry.LatestEpoch().Committee().Lanes().All() {
		if other != lane {
			require.Equal(t, types.BlockNumber(0), i.lanes[other].nextBlockToPersist)
		}
	}
}

func TestNewInnerLoadedBlocksEmptySlice(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistryAt(rng, 4, 0)
	lane := keys[0].Public()

	loaded := &loadedAvailState{
		blocks: map[types.LaneID][]persist.LoadedBlock{lane: {}},
	}

	i, err := newInner(registry, utils.OrPanic1(registry.DuoAt(0)), utils.Some(loaded))
	require.NoError(t, err)

	q := i.lanes[lane].blocks
	require.Equal(t, types.BlockNumber(0), q.first)
	require.Equal(t, types.BlockNumber(0), q.next)
}

func TestNewInnerLoadedBlocksUnknownLane(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistryAt(rng, 4, 0)

	unknownKey := types.GenSecretKey(rng)
	unknownLane := unknownKey.Public()

	b := testSignedBlock(unknownKey, unknownLane, 0, types.BlockHeaderHash{}, rng)
	loaded := &loadedAvailState{
		blocks: map[types.LaneID][]persist.LoadedBlock{unknownLane: {{Number: 0, Proposal: b}}},
	}

	i, err := newInner(registry, utils.OrPanic1(registry.DuoAt(0)), utils.Some(loaded))
	require.NoError(t, err)

	for lane := range registry.LatestEpoch().Committee().Lanes().All() {
		q := i.lanes[lane].blocks
		require.Equal(t, types.BlockNumber(0), q.first)
		require.Equal(t, types.BlockNumber(0), q.next)
	}
	_ = keys
}

func TestNewInnerLoadedBlocksMultipleLanes(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistryAt(rng, 4, 0)
	lane0 := keys[0].Public()
	lane1 := keys[1].Public()

	var parent0 types.BlockHeaderHash
	var bs0 []persist.LoadedBlock
	for n := range types.BlockNumber(2) {
		b := testSignedBlock(keys[0], lane0, n, parent0, rng)
		parent0 = b.Msg().Block().Header().Hash()
		bs0 = append(bs0, persist.LoadedBlock{Number: n, Proposal: b})
	}

	var parent1 types.BlockHeaderHash
	var bs1 []persist.LoadedBlock
	for n := range types.BlockNumber(3) {
		b := testSignedBlock(keys[1], lane1, n, parent1, rng)
		parent1 = b.Msg().Block().Header().Hash()
		bs1 = append(bs1, persist.LoadedBlock{Number: n, Proposal: b})
	}

	loaded := &loadedAvailState{
		blocks: map[types.LaneID][]persist.LoadedBlock{lane0: bs0, lane1: bs1},
	}

	i, err := newInner(registry, utils.OrPanic1(registry.DuoAt(0)), utils.Some(loaded))
	require.NoError(t, err)

	q0 := i.lanes[lane0].blocks
	require.Equal(t, types.BlockNumber(0), q0.first)
	require.Equal(t, types.BlockNumber(2), q0.next)

	q1 := i.lanes[lane1].blocks
	require.Equal(t, types.BlockNumber(0), q1.first)
	require.Equal(t, types.BlockNumber(3), q1.next)

	// nextBlockToPersist reflects q.next per loaded lane.
	require.Equal(t, types.BlockNumber(2), i.lanes[lane0].nextBlockToPersist)
	require.Equal(t, types.BlockNumber(3), i.lanes[lane1].nextBlockToPersist)
}

func TestNewInnerLoadedCommitQCsNoAppQC(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistryAt(rng, 4, 0)

	// Create 3 sequential CommitQCs.
	qcs := make([]*types.CommitQC, 3)
	prev := utils.None[*types.CommitQC]()
	for i := range qcs {
		qcs[i] = makeCommitQC(registry.LatestEpoch(), keys, prev, nil, utils.None[*types.AppQC]())
		prev = utils.Some(qcs[i])
	}

	var loadedQCs []persist.LoadedCommitQC
	for i, qc := range qcs {
		loadedQCs = append(loadedQCs, persist.LoadedCommitQC{Index: types.RoadIndex(i), QC: qc})
	}

	loaded := &loadedAvailState{
		commitQCs: loadedQCs,
	}

	inner, err := newInner(registry, utils.OrPanic1(registry.DuoAt(0)), utils.Some(loaded))
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
	registry, keys := epoch.GenRegistryAt(rng, 4, 0)

	// AppQC at road index 2.
	roadIdx := types.RoadIndex(2)
	globalNum := types.GlobalBlockNumber(10)
	appProposal := types.NewAppProposal(globalNum, roadIdx, types.GenAppHash(rng), 0)
	appQC := types.NewAppQC(makeAppVotes(keys, appProposal))

	// Create 5 sequential CommitQCs (indices 0-4).
	qcs := make([]*types.CommitQC, 5)
	prev := utils.None[*types.CommitQC]()
	for i := range qcs {
		qcs[i] = makeCommitQC(registry.LatestEpoch(), keys, prev, nil, utils.None[*types.AppQC]())
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

	inner, err := newInner(registry, utils.OrPanic1(registry.DuoAt(0)), utils.Some(loaded))
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
	registry, keys := epoch.GenRegistryAt(rng, 4, 0)
	lane := keys[0].Public()

	// AppQC at road index 2.
	roadIdx := types.RoadIndex(2)
	appProposal := types.NewAppProposal(10, roadIdx, types.GenAppHash(rng), 0)
	appQC := types.NewAppQC(makeAppVotes(keys, appProposal))

	// CommitQCs 0-4.
	qcs := make([]*types.CommitQC, 5)
	prev := utils.None[*types.CommitQC]()
	for i := range qcs {
		qcs[i] = makeCommitQC(registry.LatestEpoch(), keys, prev, nil, utils.None[*types.AppQC]())
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
	for n := range types.BlockNumber(3) {
		b := testSignedBlock(keys[0], lane, n, parent, rng)
		parent = b.Msg().Block().Header().Hash()
		bs = append(bs, persist.LoadedBlock{Number: n, Proposal: b})
	}

	loaded := &loadedAvailState{
		pruneAnchor: utils.Some(&PruneAnchor{AppQC: appQC, CommitQC: qcs[2]}),
		commitQCs:   loadedQCs,
		blocks:      map[types.LaneID][]persist.LoadedBlock{lane: bs},
	}

	inner, err := newInner(registry, utils.OrPanic1(registry.DuoAt(0)), utils.Some(loaded))
	require.NoError(t, err)

	// AppQC restored.
	aq, ok := inner.latestAppQC.Get()
	require.True(t, ok)
	require.Equal(t, roadIdx, aq.Proposal().RoadIndex())

	// CommitQCs: prune pushed qcs[2], loading skipped it, added 3 and 4.
	require.Equal(t, types.RoadIndex(2), inner.commitQCs.first)
	require.Equal(t, types.RoadIndex(5), inner.commitQCs.next)

	// Blocks loaded.
	q := inner.lanes[lane].blocks
	require.Equal(t, types.BlockNumber(0), q.first)
	require.Equal(t, types.BlockNumber(3), q.next)
	require.Equal(t, types.BlockNumber(3), inner.lanes[lane].nextBlockToPersist)

	// latestCommitQC is the last loaded one.
	latest, ok := inner.latestCommitQC.Load().Get()
	require.True(t, ok)
	require.NoError(t, utils.TestDiff(qcs[4], latest))
}

func TestPruneAdvancesNextBlockToPersist(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistryAt(rng, 4, 0)
	lane := keys[0].Public()

	i, err := newInner(registry, utils.OrPanic1(registry.DuoAt(0)), utils.None[*loadedAvailState]())
	require.NoError(t, err)

	// Push blocks 0-4 on one lane.
	var parent types.BlockHeaderHash
	for n := range types.BlockNumber(5) {
		b := testSignedBlock(keys[0], lane, n, parent, rng)
		parent = b.Msg().Block().Header().Hash()
		i.lanes[lane].blocks.pushBack(b)
	}
	// Simulate partial persistence: only block 0 persisted.
	i.lanes[lane].nextBlockToPersist = 1

	// Build CommitQCs with lane ranges that reference actual blocks.
	// Each CommitQC covers one block on the lane via a LaneQC.
	qcs := make([]*types.CommitQC, 3)
	prev := utils.None[*types.CommitQC]()
	for j := range qcs {
		bn := types.BlockNumber(j)
		h := i.lanes[lane].blocks.q[bn].Msg().Block().Header()
		laneQCs := map[types.LaneID]*types.LaneQC{
			lane: types.NewLaneQC(makeLaneVotes(
				types.TestKeysWithWeight(registry.LatestEpoch().Committee(), keys, registry.LatestEpoch().Committee().LaneQuorum()),
				h,
			)),
		}
		qcs[j] = makeCommitQC(registry.LatestEpoch(), keys, prev, laneQCs, utils.None[*types.AppQC]())
		prev = utils.Some(qcs[j])
		i.commitQCs.pushBack(qcs[j])
	}

	// Verify QC@2's lane range actually covers blocks (First > 0).
	lr := qcs[2].LaneRange(lane)
	require.Greater(t, lr.First(), types.BlockNumber(0),
		"CommitQC lane range should reference blocks for this test to be meaningful")

	// AppQC at index 2 → prune will fast-forward blocks past the cursor.
	appProposal := types.NewAppProposal(10, 2, types.GenAppHash(rng), 0)
	appQC := types.NewAppQC(makeAppVotes(keys, appProposal))

	updated, err := i.prune(appQC, qcs[2])
	require.NoError(t, err)
	require.True(t, updated)

	// nextBlockToPersist must have advanced to at least the lane's new first
	// (determined by CommitQC@2's lane range). Without this fix, it would
	// stay at 1, causing the persist goroutine to busy-loop.
	laneFirst := i.lanes[lane].blocks.first
	require.Greater(t, laneFirst, types.BlockNumber(1),
		"prune should have advanced blocks.first past the old cursor")
	require.GreaterOrEqual(t, i.lanes[lane].nextBlockToPersist, laneFirst,
		"nextBlockToPersist should advance when prune moves blocks.first past it")
}

func TestNewInnerLoadedCommitQCsAllBeforeAppQCArePruned(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistryAt(rng, 4, 0)

	// Build 6 CommitQCs (indices 0-5). Anchor at index 5.
	// All stale commitQCs (0-4) were already filtered by loadPersistedState,
	// so newInner receives an empty commitQC slice.
	qcs := make([]*types.CommitQC, 6)
	prev := utils.None[*types.CommitQC]()
	for i := range qcs {
		qcs[i] = makeCommitQC(registry.LatestEpoch(), keys, prev, nil, utils.None[*types.AppQC]())
		prev = utils.Some(qcs[i])
	}

	appProposal := types.NewAppProposal(20, 5, types.GenAppHash(rng), 0)
	appQC := types.NewAppQC(makeAppVotes(keys, appProposal))

	loaded := &loadedAvailState{
		pruneAnchor: utils.Some(&PruneAnchor{AppQC: appQC, CommitQC: qcs[5]}),
	}

	inner, err := newInner(registry, utils.OrPanic1(registry.DuoAt(0)), utils.Some(loaded))
	require.NoError(t, err)

	// tipcut insert after prune places the anchor's CommitQC.
	require.Equal(t, types.RoadIndex(5), inner.commitQCs.first)
	require.Equal(t, types.RoadIndex(6), inner.commitQCs.next)
	require.NoError(t, utils.TestDiff(qcs[5], inner.commitQCs.q[5]))
}

func TestNewInnerAnchorWithNoCommitQCFiles(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistryAt(rng, 4, 0)

	// Simulate crash between anchor write and CommitQC file write:
	// anchor has AppQC@3 + CommitQC@3, but no CommitQC files on disk.
	qcs := make([]*types.CommitQC, 4)
	prev := utils.None[*types.CommitQC]()
	for i := range qcs {
		qcs[i] = makeCommitQC(registry.LatestEpoch(), keys, prev, nil, utils.None[*types.AppQC]())
		prev = utils.Some(qcs[i])
	}

	appProposal := types.NewAppProposal(20, 3, types.GenAppHash(rng), 0)
	appQC := types.NewAppQC(makeAppVotes(keys, appProposal))

	loaded := &loadedAvailState{
		pruneAnchor: utils.Some(&PruneAnchor{AppQC: appQC, CommitQC: qcs[3]}),
	}

	inner, err := newInner(registry, utils.OrPanic1(registry.DuoAt(0)), utils.Some(loaded))
	require.NoError(t, err)

	// tipcut insert after prune places the anchor's CommitQC.
	require.Equal(t, types.RoadIndex(3), inner.commitQCs.first)
	require.Equal(t, types.RoadIndex(4), inner.commitQCs.next)
	require.NoError(t, utils.TestDiff(qcs[3], inner.commitQCs.q[3]))

	// latestAppQC should be set.
	aq, ok := inner.latestAppQC.Get()
	require.True(t, ok)
	require.Equal(t, types.RoadIndex(3), aq.Proposal().RoadIndex())

	// persistedBlockStart should be initialized from the anchor's CommitQC.
	for lane := range registry.LatestEpoch().Committee().Lanes().All() {
		expected := qcs[3].LaneRange(lane).First()
		require.Equal(t, expected, inner.lanes[lane].persistedBlockStart)
	}
}

func TestNewInnerLoadedCommitQCsGapReturnsError(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistryAt(rng, 4, 0)

	qcs := make([]*types.CommitQC, 3)
	prev := utils.None[*types.CommitQC]()
	for i := range qcs {
		qcs[i] = makeCommitQC(registry.LatestEpoch(), keys, prev, nil, utils.None[*types.AppQC]())
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

	_, err := newInner(registry, utils.OrPanic1(registry.DuoAt(0)), utils.Some(loaded))
	require.Error(t, err)
	require.Contains(t, err.Error(), "non-contiguous")
}

func TestNewInnerLoadedCommitQCsEmpty(t *testing.T) {
	rng := utils.TestRng()
	registry, _ := epoch.GenRegistryAt(rng, 4, 0)

	loaded := &loadedAvailState{
		commitQCs: nil,
	}

	inner, err := newInner(registry, utils.OrPanic1(registry.DuoAt(0)), utils.Some(loaded))
	require.NoError(t, err)

	require.Equal(t, types.RoadIndex(0), inner.commitQCs.first)
	require.Equal(t, types.RoadIndex(0), inner.commitQCs.next)
	_, ok := inner.latestCommitQC.Load().Get()
	require.False(t, ok)
}

func TestNewInnerLoadedCommitQCsGapWithAppQCAnchor(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistryAt(rng, 4, 0)

	// Simulate crash scenario: disk had stale QCs [0,1,2] and a new QC at
	// index 10. loadPersistedState pre-filters stale entries, so newInner
	// only receives [10].
	qcs := make([]*types.CommitQC, 11)
	prev := utils.None[*types.CommitQC]()
	for i := range qcs {
		qcs[i] = makeCommitQC(registry.LatestEpoch(), keys, prev, nil, utils.None[*types.AppQC]())
		prev = utils.Some(qcs[i])
	}

	appProposal := types.NewAppProposal(50, 10, types.GenAppHash(rng), 0)
	appQC := types.NewAppQC(makeAppVotes(keys, appProposal))

	loadedQCs := []persist.LoadedCommitQC{
		{Index: 10, QC: qcs[10]},
	}

	loaded := &loadedAvailState{
		pruneAnchor: utils.Some(&PruneAnchor{AppQC: appQC, CommitQC: qcs[10]}),
		commitQCs:   loadedQCs,
	}

	inner, err := newInner(registry, utils.OrPanic1(registry.DuoAt(0)), utils.Some(loaded))
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
	registry, keys := epoch.GenRegistryAt(rng, 4, 0)

	// Build 6 CommitQCs (0-5). Anchor at index 3.
	// Loaded list includes stale entries [1, 2] below the anchor plus [3, 4, 5].
	// In production loadPersistedState filters these, but newInner should
	// handle them gracefully via the lqc.Index < commitQCs.next skip.
	qcs := make([]*types.CommitQC, 6)
	prev := utils.None[*types.CommitQC]()
	for i := range qcs {
		qcs[i] = makeCommitQC(registry.LatestEpoch(), keys, prev, nil, utils.None[*types.AppQC]())
		prev = utils.Some(qcs[i])
	}

	appProposal := types.NewAppProposal(20, 3, types.GenAppHash(rng), 0)
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

	inner, err := newInner(registry, utils.OrPanic1(registry.DuoAt(0)), utils.Some(loaded))
	require.NoError(t, err)

	// tipcut insert places QC@3 (next=4). Indices 1,2,3 are skipped. 4,5 pushed.
	require.Equal(t, types.RoadIndex(3), inner.commitQCs.first)
	require.Equal(t, types.RoadIndex(6), inner.commitQCs.next)
	latest, ok := inner.latestCommitQC.Load().Get()
	require.True(t, ok)
	require.NoError(t, utils.TestDiff(qcs[5], latest))
}

func TestNewInnerLoadedCommitQCsGapAfterAnchorReturnsError(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistryAt(rng, 4, 0)

	// Anchor at index 2. Loaded commitQCs are [2, 3, 5] — gap at 4.
	// After prune+tipcut insert at 2, next=3. Index 2 is skipped, 3 pushed (next=4),
	// then 5 != 4 → error.
	qcs := make([]*types.CommitQC, 6)
	prev := utils.None[*types.CommitQC]()
	for i := range qcs {
		qcs[i] = makeCommitQC(registry.LatestEpoch(), keys, prev, nil, utils.None[*types.AppQC]())
		prev = utils.Some(qcs[i])
	}

	appProposal := types.NewAppProposal(10, 2, types.GenAppHash(rng), 0)
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

	_, err := newInner(registry, utils.OrPanic1(registry.DuoAt(0)), utils.Some(loaded))
	require.Error(t, err)
	require.Contains(t, err.Error(), "non-contiguous")
}

func TestNewInnerLoadedBlocksGapReturnsError(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistryAt(rng, 4, 0)
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

	_, err := newInner(registry, utils.OrPanic1(registry.DuoAt(0)), utils.Some(loaded))
	require.Error(t, err)
	require.Contains(t, err.Error(), "non-contiguous")
}

func TestNewInnerLoadedBlocksParentHashMismatchReturnsError(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistryAt(rng, 4, 0)
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

	_, err := newInner(registry, utils.OrPanic1(registry.DuoAt(0)), utils.Some(loaded))
	require.Error(t, err)
	require.Contains(t, err.Error(), "parent hash mismatch")
}

func TestNewInnerLoadedBlocksOverCapacityReturnsError(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistryAt(rng, 4, 0)
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

	_, err := newInner(registry, utils.OrPanic1(registry.DuoAt(0)), utils.Some(loaded))
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds capacity")
}

func TestNewInnerPruneAnchorPrunesBlockQueues(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistryAt(rng, 4, 0)
	initialBlock := types.GlobalBlockNumber(0)

	// Build CommitQCs 0-2.
	qcs := make([]*types.CommitQC, 3)
	prev := utils.None[*types.CommitQC]()
	for i := range qcs {
		qcs[i] = makeCommitQC(registry.LatestEpoch(), keys, prev, nil, utils.None[*types.AppQC]())
		prev = utils.Some(qcs[i])
	}

	// AppQC at road index 2, prune anchor is CommitQC[2].
	appProposal := types.NewAppProposal(initialBlock, 2, types.GenAppHash(rng), 0)
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

	i, err := newInner(registry, utils.OrPanic1(registry.DuoAt(0)), utils.Some(loaded))
	require.NoError(t, err)

	// prune() should advance block queue first to the prune anchor's lane range.
	for l := range registry.LatestEpoch().Committee().Lanes().All() {
		expected := pruneQC.LaneRange(l).First()
		require.Equal(t, expected, i.lanes[l].blocks.first,
			"lanes[%v].blocks.first should be advanced by prune to prune anchor lane range", l)
	}
}

func TestNewInnerPruneAnchorCommitQCUsedForPrune(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistryAt(rng, 4, 0)
	initialBlock := types.GlobalBlockNumber(0)

	// Build CommitQCs 0-2.
	qcs := make([]*types.CommitQC, 3)
	prev := utils.None[*types.CommitQC]()
	for i := range qcs {
		qcs[i] = makeCommitQC(registry.LatestEpoch(), keys, prev, nil, utils.None[*types.AppQC]())
		prev = utils.Some(qcs[i])
	}

	// AppQC at road index 1, prune anchor is CommitQC[1].
	appProposal := types.NewAppProposal(initialBlock, 1, types.GenAppHash(rng), 0)
	appQC := types.NewAppQC(makeAppVotes(keys, appProposal))

	loaded := &loadedAvailState{
		pruneAnchor: utils.Some(&PruneAnchor{AppQC: appQC, CommitQC: qcs[1]}),
		commitQCs: []persist.LoadedCommitQC{
			{Index: 1, QC: qcs[1]},
			{Index: 2, QC: qcs[2]},
		},
	}

	i, err := newInner(registry, utils.OrPanic1(registry.DuoAt(0)), utils.Some(loaded))
	require.NoError(t, err)

	// prune(appQC@1, pruneQC@1) should advance commitQCs.first to 1.
	require.Equal(t, types.RoadIndex(1), i.commitQCs.first)
	// CommitQCs 1 and 2 should still be loaded.
	require.Equal(t, types.RoadIndex(3), i.commitQCs.next)
}

// TestNewInnerAppVotesFloorFromAnchorNotTipFirstBlock covers tip-based restart:
// appVotes must be floored by the prune-anchor CommitQC, not tip Current.FirstBlock
// (queue.prune only advances; a too-high bootstrap would stick).
func TestNewInnerAppVotesFloorFromAnchorNotTipFirstBlock(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistryAt(rng, 4, 0)
	ep0 := utils.OrPanic1(registry.EpochAt(0))

	qcs := make([]*types.CommitQC, 3)
	prev := utils.None[*types.CommitQC]()
	for i := range qcs {
		qcs[i] = makeCommitQC(ep0, keys, prev, nil, utils.None[*types.AppQC]())
		prev = utils.Some(qcs[i])
	}
	wantAppFirst := qcs[1].GlobalRange().First
	// Synthetic tip duo with FirstBlock above the prune floor (placeholder
	// registry epochs share genesis FirstBlock, so inflate for this invariant).
	tipFirst := wantAppFirst + 1000
	c := ep0.Committee()
	tipCurrent := types.NewEpoch(1, types.RoadRange{First: epoch.FirstRoad(1), Next: epoch.FirstRoad(2)}, ep0.FirstTimestamp(), c, tipFirst)
	tipDuo := types.EpochDuo{Prev: utils.Some(ep0), Current: tipCurrent}

	ap := types.NewAppProposal(wantAppFirst, qcs[1].Index(), types.GenAppHash(rng), 0)
	loaded := &loadedAvailState{
		pruneAnchor: utils.Some(&PruneAnchor{
			AppQC:    types.NewAppQC(makeAppVotes(keys, ap)),
			CommitQC: qcs[1],
		}),
		commitQCs: []persist.LoadedCommitQC{
			{Index: 1, QC: qcs[1]},
			{Index: 2, QC: qcs[2]},
		},
	}

	inner, err := newInner(registry, tipDuo, utils.Some(loaded))
	require.NoError(t, err)
	require.Equal(t, wantAppFirst, inner.appVotes.first,
		"appVotes must follow prune-anchor GlobalRange, not tip Current.FirstBlock")
	require.NotEqual(t, tipFirst, inner.appVotes.first)
}

func TestAdvanceEpoch_AddsLanesKeepsOld(t *testing.T) {
	rng := utils.TestRng()
	registry, _ := epoch.GenRegistryAt(rng, 4, 0)
	duo := utils.OrPanic1(registry.DuoAt(0))

	i, err := newInner(registry, duo, utils.None[*loadedAvailState]())
	require.NoError(t, err)

	// All current lanes are present after construction.
	for lane := range duo.Current.Committee().Lanes().All() {
		require.Contains(t, i.lanes, lane, "lane %v missing after newInner", lane)
	}

	// Add a lane not in the duo — until lane-expiry lands, advance must not
	// delete it (ever-growing map).
	var realLane types.LaneID
	for l := range duo.Current.Committee().Lanes().All() {
		realLane = l
		break
	}
	bogusSK := types.GenSecretKey(rng)
	bogusLane := bogusSK.Public()
	i.lanes[bogusLane] = newLaneState()

	i.advanceEpoch(duo)
	require.Contains(t, i.lanes, bogusLane, "old lanes must be retained until lane-expiry")
	require.Contains(t, i.lanes, realLane, "active lane removed incorrectly")
}

func TestAdvanceEpoch_EmptyQueuesNoop(t *testing.T) {
	rng := utils.TestRng()
	registry, _ := epoch.GenRegistryAt(rng, 4, 0)
	duo := utils.OrPanic1(registry.DuoAt(0))

	i, err := newInner(registry, duo, utils.None[*loadedAvailState]())
	require.NoError(t, err)

	// No votes in any queue; advancing to the same duo is a safe no-op that
	// keeps the current lane set intact.
	i.advanceEpoch(duo)
	for lane := range duo.Current.Committee().Lanes().All() {
		require.Contains(t, i.lanes, lane)
	}
}

func TestAdvanceEpoch_RetainsPrevEpochLanes(t *testing.T) {
	rng := utils.TestRng()
	registry, _ := epoch.GenRegistryAt(rng, 4, 0)

	// duo0: Prev=nil, Current=epoch0
	duo0 := utils.OrPanic1(registry.DuoAt(0))
	i, err := newInner(registry, duo0, utils.None[*loadedAvailState]())
	require.NoError(t, err)

	// Collect a lane from epoch0 (will become Prev after advance).
	var epoch0Lane types.LaneID
	for l := range duo0.Current.Committee().Lanes().All() {
		epoch0Lane = l
		break
	}
	require.Contains(t, i.lanes, epoch0Lane, "epoch0 lane missing before reweight")

	// duo1: Prev=epoch0, Current=epoch1
	duo1 := utils.OrPanic1(registry.DuoAt(epoch.FirstRoad(1)))
	i.advanceEpoch(duo1)

	// Epoch0 lane is now in Prev — must be retained for boundary QC collection.
	require.Contains(t, i.lanes, epoch0Lane, "Prev-epoch lane deleted prematurely; fullCommitQC needs it")
}
