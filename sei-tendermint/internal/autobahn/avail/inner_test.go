package avail

import (
	"fmt"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus/persist"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func newTestDataState(t testing.TB, cfg *data.Config) *data.State {
	store := blockstore.NewTestStore(t)
	return utils.OrPanic1(data.NewState(cfg, store))
}

func testSignedBlock(key types.SecretKey, lane types.LaneID, n types.BlockNumber, parent types.BlockHeaderHash, rng utils.Rng) *types.Signed[*types.LaneProposal] {
	block := types.NewBlock(lane, n, parent, types.GenPayload(rng))
	return types.Sign(key, types.NewLaneProposal(block))
}

func contiguousBlocks(key types.SecretKey, lane types.LaneID, n int, rng utils.Rng) []persist.LoadedBlock {
	var parent types.BlockHeaderHash
	bs := make([]persist.LoadedBlock, 0, n)
	for i := range types.BlockNumber(n) {
		b := testSignedBlock(key, lane, i, parent, rng)
		parent = b.Msg().Block().Header().Hash()
		bs = append(bs, persist.LoadedBlock{Number: i, Proposal: b})
	}
	return bs
}

func TestRestoreInner_Empty(t *testing.T) {
	rng := utils.TestRng()
	registry, _ := epoch.GenRegistry(rng, 4)
	ep0 := registry.MustEpoch(0)

	i, err := restoreInner(newTestDataState(t, &data.Config{Registry: registry}), &loadedState{})
	require.NoError(t, err)

	require.Equal(t, ep0.RoadRange().First, i.roads.first)
	require.Equal(t, ep0.RoadRange().First, i.roads.next)
	_, ok := i.persistedCommitQC.Load().Get()
	require.False(t, ok)
	require.NotNil(t, i.nextBlockToPersist)
	for lane := range ep0.Committee().Lanes().All() {
		require.Equal(t, types.BlockNumber(0), i.blocks[lane].first)
		require.Equal(t, types.BlockNumber(0), i.blocks[lane].next)
		require.Equal(t, types.BlockNumber(0), i.votes[lane].first)
		require.Equal(t, types.BlockNumber(0), i.votes[lane].next)
	}
}

func TestRestoreInner_LoadedBlocks(t *testing.T) {
	t.Run("contiguous", func(t *testing.T) {
		rng := utils.TestRng()
		registry, keys := epoch.GenRegistry(rng, 4)
		ep0 := registry.MustEpoch(0)
		lane0 := ep0.Committee().Lane(keys[0].Public()).OrPanic("keys[0]")
		ds := newTestDataState(t, &data.Config{Registry: registry})
		bs := contiguousBlocks(keys[0], lane0, 3, rng)
		i, err := restoreInner(ds, &loadedState{blocks: map[types.LaneID][]persist.LoadedBlock{lane0: bs}})
		require.NoError(t, err)
		q := i.blocks[lane0]
		require.Equal(t, types.BlockNumber(0), q.first)
		require.Equal(t, types.BlockNumber(3), q.next)
		for j, b := range bs {
			require.Equal(t, b.Proposal, q.q[types.BlockNumber(j)])
		}
		require.Equal(t, types.BlockNumber(3), i.nextBlockToPersist[lane0])
		for other := range ep0.Committee().Lanes().All() {
			if other != lane0 {
				require.Equal(t, types.BlockNumber(0), i.nextBlockToPersist[other])
			}
		}
	})

	t.Run("empty slice", func(t *testing.T) {
		rng := utils.TestRng()
		registry, keys := epoch.GenRegistry(rng, 4)
		lane0 := registry.MustEpoch(0).Committee().Lane(keys[0].Public()).OrPanic("keys[0]")
		i, err := restoreInner(newTestDataState(t, &data.Config{Registry: registry}), &loadedState{
			blocks: map[types.LaneID][]persist.LoadedBlock{lane0: {}},
		})
		require.NoError(t, err)
		q := i.blocks[lane0]
		require.Equal(t, types.BlockNumber(0), q.first)
		require.Equal(t, types.BlockNumber(0), q.next)
	})

	t.Run("foreign loaded lane does not touch committee queues", func(t *testing.T) {
		rng := utils.TestRng()
		registry, _ := epoch.GenRegistry(rng, 4)
		unknownKey := types.GenSecretKey(rng)
		unknownLane := types.LaneID{Validator: unknownKey.Public(), Joined: 0}
		b := testSignedBlock(unknownKey, unknownLane, 0, types.BlockHeaderHash{}, rng)
		i, err := restoreInner(newTestDataState(t, &data.Config{Registry: registry}), &loadedState{
			blocks: map[types.LaneID][]persist.LoadedBlock{unknownLane: {{Number: 0, Proposal: b}}},
		})
		require.NoError(t, err)
		_, ok := i.blocks[unknownLane]
		require.False(t, ok)
		_, ok = i.votes[unknownLane]
		require.False(t, ok)
		_, ok = i.nextBlockToPersist[unknownLane]
		require.False(t, ok)
		for lane := range registry.MustEpoch(0).Committee().Lanes().All() {
			cq := i.blocks[lane]
			require.Equal(t, types.BlockNumber(0), cq.first)
			require.Equal(t, types.BlockNumber(0), cq.next)
		}
	})

	t.Run("multiple lanes", func(t *testing.T) {
		rng := utils.TestRng()
		registry, keys := epoch.GenRegistry(rng, 4)
		ep0 := registry.MustEpoch(0)
		lane0 := ep0.Committee().Lane(keys[0].Public()).OrPanic("keys[0]")
		lane1 := ep0.Committee().Lane(keys[1].Public()).OrPanic("keys[1]")
		bs0 := contiguousBlocks(keys[0], lane0, 2, rng)
		bs1 := contiguousBlocks(keys[1], lane1, 3, rng)
		i, err := restoreInner(newTestDataState(t, &data.Config{Registry: registry}), &loadedState{
			blocks: map[types.LaneID][]persist.LoadedBlock{lane0: bs0, lane1: bs1},
		})
		require.NoError(t, err)
		require.Equal(t, types.BlockNumber(2), i.blocks[lane0].next)
		require.Equal(t, types.BlockNumber(3), i.blocks[lane1].next)
		require.Equal(t, types.BlockNumber(2), i.nextBlockToPersist[lane0])
		require.Equal(t, types.BlockNumber(3), i.nextBlockToPersist[lane1])
	})

	t.Run("gap", func(t *testing.T) {
		rng := utils.TestRng()
		registry, keys := epoch.GenRegistry(rng, 4)
		lane0 := registry.MustEpoch(0).Committee().Lane(keys[0].Public()).OrPanic("keys[0]")
		var bs []persist.LoadedBlock
		for _, n := range []types.BlockNumber{3, 4, 6, 7} {
			bs = append(bs, persist.LoadedBlock{Number: n, Proposal: testSignedBlock(keys[0], lane0, n, types.BlockHeaderHash{}, rng)})
		}
		_, err := restoreInner(newTestDataState(t, &data.Config{Registry: registry}), &loadedState{
			blocks: map[types.LaneID][]persist.LoadedBlock{lane0: bs},
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "non-contiguous")
	})

	t.Run("parent hash mismatch", func(t *testing.T) {
		rng := utils.TestRng()
		registry, keys := epoch.GenRegistry(rng, 4)
		lane0 := registry.MustEpoch(0).Committee().Lane(keys[0].Public()).OrPanic("keys[0]")
		var parent types.BlockHeaderHash
		b0 := testSignedBlock(keys[0], lane0, 0, parent, rng)
		parent = b0.Msg().Block().Header().Hash()
		b1 := testSignedBlock(keys[0], lane0, 1, parent, rng)
		b2 := testSignedBlock(keys[0], lane0, 2, types.GenBlockHeaderHash(rng), rng)
		_, err := restoreInner(newTestDataState(t, &data.Config{Registry: registry}), &loadedState{blocks: map[types.LaneID][]persist.LoadedBlock{lane0: {
			{Number: 0, Proposal: b0},
			{Number: 1, Proposal: b1},
			{Number: 2, Proposal: b2},
		}}})
		require.Error(t, err)
		require.Contains(t, err.Error(), "parent hash mismatch")
	})

	t.Run("over capacity", func(t *testing.T) {
		rng := utils.TestRng()
		registry, keys := epoch.GenRegistry(rng, 4)
		lane0 := registry.MustEpoch(0).Committee().Lane(keys[0].Public()).OrPanic("keys[0]")
		bs := contiguousBlocks(keys[0], lane0, BlocksPerLane+5, rng)
		_, err := restoreInner(newTestDataState(t, &data.Config{Registry: registry}), &loadedState{
			blocks: map[types.LaneID][]persist.LoadedBlock{lane0: bs},
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "exceeds capacity")
	})
}

func TestRestoreInner_LoadedCommitQCs(t *testing.T) {
	t.Run("contiguous", func(t *testing.T) {
		rng := utils.TestRng()
		registry, keys := epoch.GenRegistry(rng, 4)
		ep0 := registry.MustEpoch(0)
		ds := newTestDataState(t, &data.Config{Registry: registry})
		qcs := make([]*types.CommitQC, 3)
		prev := utils.None[*types.CommitQC]()
		for i := range qcs {
			qcs[i] = types.BuildCommitQC(ep0, keys, prev, nil)
			prev = utils.Some(qcs[i])
		}
		inner, err := restoreInner(ds, &loadedState{commitQCs: qcs})
		require.NoError(t, err)
		require.Equal(t, ep0.RoadRange().First, inner.roads.first)
		require.Equal(t, ep0.RoadRange().First+3, inner.roads.next)
		for i, qc := range qcs {
			require.NoError(t, utils.TestDiff(qc, inner.roads.q[ep0.RoadRange().First+types.RoadIndex(i)].commitQC))
		}
		require.NoError(t, utils.TestDiff(utils.Some(qcs[2]), inner.persistedCommitQC.Load()))
		require.Equal(t, int64(qcs[2].Index()), gathered(t, "tendermint_internal_autobahn_avail_commit_road_index", nil))
		spec := inner.consensusSpec.Load()
		require.Equal(t, types.EpochIndex(0), spec.Epoch.EpochIndex())
		got, ok := spec.CommitQC.Get()
		require.True(t, ok)
		require.NoError(t, utils.TestDiff(qcs[2], got))
	})

	t.Run("gap", func(t *testing.T) {
		rng := utils.TestRng()
		registry, keys := epoch.GenRegistry(rng, 4)
		ep0 := registry.MustEpoch(0)
		ds := newTestDataState(t, &data.Config{Registry: registry})
		qc0 := types.BuildCommitQC(ep0, keys, utils.None[*types.CommitQC](), nil)
		qc1 := types.BuildCommitQC(ep0, keys, utils.Some(qc0), nil)
		qc2 := types.BuildCommitQC(ep0, keys, utils.Some(qc1), nil)
		_, err := restoreInner(ds, &loadedState{commitQCs: []*types.CommitQC{qc0, qc2}})
		require.Error(t, err)
		require.Contains(t, err.Error(), "non-contiguous")
	})
}

func TestAddLane_ReportsNewLaneForEachMembershipPeriod(t *testing.T) {
	rng := utils.TestRng()
	a := types.GenSecretKey(rng)

	i := &inner{
		blocks:             map[types.LaneID]*queue[types.BlockNumber, *types.Signed[*types.LaneProposal]]{},
		votes:              map[types.LaneID]*queue[types.BlockNumber, *blockVotes]{},
		nextBlockToPersist: map[types.LaneID]types.BlockNumber{},
	}
	lane := types.LaneID{Validator: a.Public(), Joined: 1}
	require.True(t, i.addLane(lane))
	require.False(t, i.addLane(lane))
	require.True(t, i.addLane(types.LaneID{Validator: a.Public(), Joined: 3}))
}

// TestRefreshConsensusSpec_WithholdsTipUntilNextViewEpochApplied: the durable tip
// sits on LastRoad(0) while applied is still epoch 0, and the tip's predecessor is
// retained. The spec must be withheld rather than published at that predecessor —
// a node that already entered epoch 1 holds the tip and must not be rolled back.
func TestRefreshConsensusSpec_WithholdsTipUntilNextViewEpochApplied(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 4)

	ep0 := registry.MustEpoch(0)
	ep1 := registry.MustEpoch(1)

	last := registry.LastRoad(0)
	qcPrev := types.BuildCommitQC(ep0, keys, utils.Some(tipLink(ep0, keys[0], last-2)), nil)
	qcLast := types.BuildCommitQC(ep0, keys, utils.Some(qcPrev), nil)
	require.Equal(t, last-1, qcPrev.Index())
	require.Equal(t, last, qcLast.Index())

	i := &inner{
		persistedCommitQC:  utils.NewAtomicSend(utils.None[*types.CommitQC]()),
		consensusSpec:      utils.NewAtomicSend(types.ConsensusSpec{CommitQC: utils.None[*types.CommitQC](), Epoch: ep0}),
		roads:              newQueue[types.RoadIndex, *road](),
		blocks:             map[types.LaneID]*queue[types.BlockNumber, *types.Signed[*types.LaneProposal]]{},
		votes:              map[types.LaneID]*queue[types.BlockNumber, *blockVotes]{},
		nextBlockToPersist: map[types.LaneID]types.BlockNumber{},
	}
	i.roads.first = last - 1
	i.roads.next = last - 1
	i.roads.pushBack(newRoad(qcPrev, ep0))
	i.roads.pushBack(newRoad(qcLast, ep0))
	i.persistedCommitQC.Store(utils.Some(qcLast))

	i.refreshConsensusSpec()
	require.False(t, i.consensusSpec.Load().CommitQC.IsPresent(), "spec must be withheld, not published at the predecessor")

	i.advanceEpoch(ep1)
	spec := i.consensusSpec.Load()
	cqc, ok := spec.CommitQC.Get()
	require.True(t, ok)
	require.Equal(t, last, cqc.Index())
	require.Equal(t, types.EpochIndex(1), spec.Epoch.EpochIndex())
}

func TestPrune_LeavesAppliedToEpochAdvance(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistryThrough(rng, 4, 2)
	ep0 := registry.MustEpoch(0)
	ep1 := registry.MustEpoch(1)
	ep2 := registry.MustEpoch(2)
	i := newInner(ep0, utils.None[*types.CommitQC](), time.Time{})
	require.Equal(t, types.EpochIndex(0), i.applied().EpochIndex())

	voteProposal := types.ProposalAt(ep1, types.View{Index: registry.LastRoad(1), Number: 0})
	prev := types.NewCommitQC([]*types.Signed[*types.CommitVote]{
		types.Sign(keys[0], types.NewCommitVote(types.BuildPrepareQC(keys, voteProposal))),
	})
	qc := types.BuildCommitQC(ep2, keys, utils.Some(prev), nil)
	i.prune(data.Anchor{
		CommitQC: qc,
		AppQC:    data.TestAppQC(keys, types.NewAppProposal(qc.View(), types.AppHash{})),
		Epoch:    ep2,
	})
	require.Equal(t, types.EpochIndex(0), i.applied().EpochIndex())
	ae, ok := i.anchorEpoch.Get()
	require.True(t, ok)
	require.Equal(t, types.EpochIndex(2), ae.EpochIndex())
}

func TestNewInnerInitializesGenesis(t *testing.T) {
	rng := utils.TestRng()
	registry, _ := epoch.GenRegistry(rng, 3)
	ep0 := registry.MustEpoch(0)
	timestamp := time.Unix(42, 0)
	registry, err := epoch.NewRegistry(ep0.Committee(), 7, timestamp, utils.None[string]())
	require.NoError(t, err)
	ep := ep0
	i := newInner(ep, utils.None[*types.CommitQC](), timestamp)
	require.Equal(t, ep.RoadRange().First, i.roads.first)
	require.Equal(t, ep.RoadRange().First, i.roads.next)
	require.False(t, i.anchorEpoch.IsPresent())
	require.False(t, i.persistedCommitQC.Load().IsPresent())
	require.Equal(t, types.ConsensusSpec{Epoch: ep, GenesisTimestamp: timestamp}, i.consensusSpec.Load())
	require.Panics(t, func() { newInner(registry.MustEpoch(1), utils.None[*types.CommitQC](), timestamp) })
}

func TestNewInnerInitializesAnchor(t *testing.T) {
	for _, idx := range []types.EpochIndex{0, 1} {
		t.Run(fmt.Sprint(idx), func(t *testing.T) {
			rng := utils.TestRng()
			registry, keys := epoch.GenRegistry(rng, 3)
			ep := registry.MustEpoch(idx)
			qc := types.BuildCommitQC(ep, keys, utils.None[*types.CommitQC](), nil)
			timestamp := time.Unix(42, 0)
			i := newInner(ep, utils.Some(qc), timestamp)
			require.Equal(t, qc.Index()+1, i.roads.first)
			require.Equal(t, qc.Index()+1, i.roads.next)
			require.Equal(t, utils.Some(ep), i.anchorEpoch)
			require.Equal(t, utils.Some(qc), i.persistedCommitQC.Load())
			require.Equal(t, types.ConsensusSpec{Epoch: ep, CommitQC: utils.Some(qc), GenesisTimestamp: timestamp}, i.consensusSpec.Load())
			for lane := range ep.Committee().Lanes().All() {
				lr := qc.LaneRange(lane)
				require.Equal(t, lr.Next(), i.blocks[lane].first)
				require.Equal(t, lr.Next(), i.blocks[lane].next)
				require.Equal(t, lr.Next(), i.votes[lane].first)
				require.Equal(t, lr.Next(), i.votes[lane].next)
				require.Equal(t, lr.Next(), i.nextBlockToPersist[lane])
			}
		})
	}
}
