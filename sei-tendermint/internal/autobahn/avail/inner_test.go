package avail

import (
	"context"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/memblock"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/blockstore"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus/persist"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

func newTestDataState(cfg *data.Config) *data.State {
	store := utils.OrPanic1(blockstore.New(memblock.NewBlockDB()))
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

func TestNewInner_Empty(t *testing.T) {
	rng := utils.TestRng()
	registry, _ := epoch.GenRegistry(rng, 4)

	i, err := newInner(newTestDataState(&data.Config{Registry: registry}), &loadedState{})
	require.NoError(t, err)

	require.Equal(t, types.RoadIndex(0), i.roads.first)
	require.Equal(t, types.RoadIndex(0), i.roads.next)
	_, ok := i.persistedCommitQC.Load().Get()
	require.False(t, ok)
	require.NotNil(t, i.nextBlockToPersist)
	for lane := range registry.LatestEpoch().Committee().Lanes().All() {
		require.Equal(t, types.BlockNumber(0), i.blocks[lane].first)
		require.Equal(t, types.BlockNumber(0), i.blocks[lane].next)
		require.Equal(t, types.BlockNumber(0), i.votes[lane].first)
		require.Equal(t, types.BlockNumber(0), i.votes[lane].next)
	}
}

func TestNewInner_LoadedBlocks(t *testing.T) {
	t.Run("contiguous", func(t *testing.T) {
		rng := utils.TestRng()
		registry, keys := epoch.GenRegistry(rng, 4)
		lane0 := registry.LatestEpoch().Committee().Lane(keys[0].Public()).OrPanic("keys[0]")
		ds := newTestDataState(&data.Config{Registry: registry})
		bs := contiguousBlocks(keys[0], lane0, 3, rng)
		i, err := newInner(ds, &loadedState{blocks: map[types.LaneID][]persist.LoadedBlock{lane0: bs}})
		require.NoError(t, err)
		q := i.blocks[lane0]
		require.Equal(t, types.BlockNumber(0), q.first)
		require.Equal(t, types.BlockNumber(3), q.next)
		for j, b := range bs {
			require.Equal(t, b.Proposal, q.q[types.BlockNumber(j)])
		}
		require.Equal(t, types.BlockNumber(3), i.nextBlockToPersist[lane0])
		for other := range registry.LatestEpoch().Committee().Lanes().All() {
			if other != lane0 {
				require.Equal(t, types.BlockNumber(0), i.nextBlockToPersist[other])
			}
		}
	})

	t.Run("empty slice", func(t *testing.T) {
		rng := utils.TestRng()
		registry, keys := epoch.GenRegistry(rng, 4)
		lane0 := registry.LatestEpoch().Committee().Lane(keys[0].Public()).OrPanic("keys[0]")
		i, err := newInner(newTestDataState(&data.Config{Registry: registry}), &loadedState{
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
		i, err := newInner(newTestDataState(&data.Config{Registry: registry}), &loadedState{
			blocks: map[types.LaneID][]persist.LoadedBlock{unknownLane: {{Number: 0, Proposal: b}}},
		})
		require.NoError(t, err)
		q := i.blocks[unknownLane]
		require.Equal(t, types.BlockNumber(0), q.first)
		require.Equal(t, types.BlockNumber(1), q.next)
		require.Equal(t, b, q.q[0])
		for lane := range registry.LatestEpoch().Committee().Lanes().All() {
			cq := i.blocks[lane]
			require.Equal(t, types.BlockNumber(0), cq.first)
			require.Equal(t, types.BlockNumber(0), cq.next)
		}
	})

	t.Run("multiple lanes", func(t *testing.T) {
		rng := utils.TestRng()
		registry, keys := epoch.GenRegistry(rng, 4)
		lane0 := registry.LatestEpoch().Committee().Lane(keys[0].Public()).OrPanic("keys[0]")
		lane1 := registry.LatestEpoch().Committee().Lane(keys[1].Public()).OrPanic("keys[1]")
		bs0 := contiguousBlocks(keys[0], lane0, 2, rng)
		bs1 := contiguousBlocks(keys[1], lane1, 3, rng)
		i, err := newInner(newTestDataState(&data.Config{Registry: registry}), &loadedState{
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
		lane0 := registry.LatestEpoch().Committee().Lane(keys[0].Public()).OrPanic("keys[0]")
		var bs []persist.LoadedBlock
		for _, n := range []types.BlockNumber{3, 4, 6, 7} {
			bs = append(bs, persist.LoadedBlock{Number: n, Proposal: testSignedBlock(keys[0], lane0, n, types.BlockHeaderHash{}, rng)})
		}
		_, err := newInner(newTestDataState(&data.Config{Registry: registry}), &loadedState{
			blocks: map[types.LaneID][]persist.LoadedBlock{lane0: bs},
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "non-contiguous")
	})

	t.Run("parent hash mismatch", func(t *testing.T) {
		rng := utils.TestRng()
		registry, keys := epoch.GenRegistry(rng, 4)
		lane0 := registry.LatestEpoch().Committee().Lane(keys[0].Public()).OrPanic("keys[0]")
		var parent types.BlockHeaderHash
		b0 := testSignedBlock(keys[0], lane0, 0, parent, rng)
		parent = b0.Msg().Block().Header().Hash()
		b1 := testSignedBlock(keys[0], lane0, 1, parent, rng)
		b2 := testSignedBlock(keys[0], lane0, 2, types.GenBlockHeaderHash(rng), rng)
		_, err := newInner(newTestDataState(&data.Config{Registry: registry}), &loadedState{blocks: map[types.LaneID][]persist.LoadedBlock{lane0: {
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
		lane0 := registry.LatestEpoch().Committee().Lane(keys[0].Public()).OrPanic("keys[0]")
		bs := contiguousBlocks(keys[0], lane0, BlocksPerLane+5, rng)
		_, err := newInner(newTestDataState(&data.Config{Registry: registry}), &loadedState{
			blocks: map[types.LaneID][]persist.LoadedBlock{lane0: bs},
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "exceeds capacity")
	})
}

func TestNewInner_LoadedCommitQCs(t *testing.T) {
	t.Run("contiguous", func(t *testing.T) {
		rng := utils.TestRng()
		registry, keys := epoch.GenRegistry(rng, 4)
		ds := newTestDataState(&data.Config{Registry: registry})
		qcs := make([]*types.CommitQC, 3)
		prev := utils.None[*types.CommitQC]()
		for i := range qcs {
			qcs[i] = types.BuildCommitQC(registry.LatestEpoch(), keys, prev, nil)
			prev = utils.Some(qcs[i])
		}
		inner, err := newInner(ds, &loadedState{commitQCs: qcs})
		require.NoError(t, err)
		require.Equal(t, types.RoadIndex(0), inner.roads.first)
		require.Equal(t, types.RoadIndex(3), inner.roads.next)
		for i, qc := range qcs {
			require.NoError(t, utils.TestDiff(qc, inner.roads.q[types.RoadIndex(i)].commitQC))
		}
		require.NoError(t, utils.TestDiff(utils.Some(qcs[2]), inner.persistedCommitQC.Load()))
	})

	t.Run("gap", func(t *testing.T) {
		rng := utils.TestRng()
		registry, keys := epoch.GenRegistry(rng, 4)
		ds := newTestDataState(&data.Config{Registry: registry})
		qc0 := types.BuildCommitQC(registry.LatestEpoch(), keys, utils.None[*types.CommitQC](), nil)
		qc1 := types.BuildCommitQC(registry.LatestEpoch(), keys, utils.Some(qc0), nil)
		qc2 := types.BuildCommitQC(registry.LatestEpoch(), keys, utils.Some(qc1), nil)
		_, err := newInner(ds, &loadedState{commitQCs: []*types.CommitQC{qc0, qc2}})
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

// TestAdvanceReadyEpochs_BoundaryTipUsesDataAppQC: tip at LastRoad(0) with
// applied floored to 0 (restart), data's Anchor already covers epoch 0, registry
// has epoch 1 → advance walks to 1 so ConsensusSpec republishes the tip.
// This is the avail half of the blind-Spec restore invariant: consensus may
// refuse to start if Spec stays behind a WAL tip at LastRoad(0) after catch-up.
func TestAdvanceReadyEpochs_BoundaryTipUsesDataAppQC(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 4)
	ds := newTestDataState(&data.Config{Registry: registry})

	ep0, ok := registry.EpochByIndex(0)
	require.True(t, ok)

	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBgNamed("data.Run", func() error {
			return utils.IgnoreCancel(ds.Run(ctx))
		})
		qc0, blocks := data.TestCommitQC(rng, ep0, keys, utils.None[*types.CommitQC]())
		if err := ds.PushQC(ctx, qc0, blocks); err != nil {
			return err
		}
		if err := ds.PushAppHash(ctx, qc0.QC().GlobalRange().Next-1, types.GenAppHash(rng)); err != nil {
			return err
		}
		vote, err := ds.AppVote(ctx, qc0.QC().GlobalRange().First)
		if err != nil {
			return err
		}
		if err := ds.PushAppQC(ctx, data.TestAppQC(keys, vote.Proposal())); err != nil {
			return err
		}
		_, err = ds.Anchor().Wait(ctx, func(a utils.Option[data.Anchor]) bool {
			got, ok := a.Get()
			return ok && got.CommitQC.Index() == qc0.QC().Index()
		})
		return err
	}))
	anchor, ok := ds.Anchor().Load().Get()
	require.True(t, ok)
	require.Equal(t, types.EpochIndex(0), anchor.Epoch.EpochIndex())

	registry.AdvanceIfNeeded(epoch.LastRoad(0))
	ep1, ok := registry.EpochByIndex(1)
	require.True(t, ok)

	last := epoch.LastRoad(0)
	prev := types.NewCommitQC([]*types.Signed[*types.CommitVote]{
		types.Sign(keys[0], types.NewCommitVote(types.ProposalAt(ep0, types.View{Index: last - 1, Number: 0}, ep0.FirstBlock()))),
	})
	qcLast := types.BuildCommitQC(ep0, keys, utils.Some(prev), nil)
	require.Equal(t, last, qcLast.Index())

	i := &inner{
		persistedCommitQC:  utils.NewAtomicSend(utils.None[*types.CommitQC]()),
		consensusSpec:      utils.NewAtomicSend(types.ConsensusSpec{CommitQC: utils.None[*types.CommitQC](), Epoch: ep0}),
		roads:              newQueue[types.RoadIndex, *road](),
		epoch:              utils.NewAtomicSend(ep0),
		anchorEpoch:        utils.Some(anchor.Epoch),
		blocks:             map[types.LaneID]*queue[types.BlockNumber, *types.Signed[*types.LaneProposal]]{},
		votes:              map[types.LaneID]*queue[types.BlockNumber, *blockVotes]{},
		nextBlockToPersist: map[types.LaneID]types.BlockNumber{},
	}
	for lane := range ep0.Committee().Lanes().All() {
		i.addLane(lane)
	}
	i.roads.first = last
	i.roads.next = last
	i.roads.pushBack(newRoad(qcLast, ep0))
	i.persistedCommitQC.Store(utils.Some(qcLast))
	i.epoch.Store(ep0)

	require.False(t, i.roads.q[last].appQC.IsPresent(), "road AppQC empty; prune leash is the Anchor")
	require.True(t, i.canAdvanceEpoch())
	require.NoError(t, i.advanceReadyEpochs(ds))

	require.Equal(t, ep1.EpochIndex(), i.epoch.Load().EpochIndex())
	spec := i.consensusSpec.Load()
	cqc, ok := spec.CommitQC.Get()
	require.True(t, ok)
	require.Equal(t, last, cqc.Index(), "must not walk tip back to LastRoad(0)-1")
	require.Equal(t, types.EpochIndex(1), spec.Epoch.EpochIndex())
}

func TestAdvanceReadyEpochs_MissingNextEpochErrors(t *testing.T) {
	rng := utils.TestRng()
	// Fresh registry has epochs 0 and 1; seal epoch 1 so the next lookup is 2.
	registry, keys := epoch.GenRegistry(rng, 3)
	ep1, ok := registry.EpochByIndex(1)
	require.True(t, ok)
	_, err := registry.EpochAt(epoch.FirstRoad(2))
	require.Error(t, err)

	last := epoch.LastRoad(1)
	prev := types.NewCommitQC([]*types.Signed[*types.CommitVote]{
		types.Sign(keys[0], types.NewCommitVote(types.ProposalAt(ep1, types.View{Index: last - 1, Number: 0}, ep1.FirstBlock()))),
	})
	qcLast := types.BuildCommitQC(ep1, keys, utils.Some(prev), nil)
	require.Equal(t, last, qcLast.Index())

	i := &inner{
		persistedCommitQC:  utils.NewAtomicSend(utils.None[*types.CommitQC]()),
		consensusSpec:      utils.NewAtomicSend(types.ConsensusSpec{CommitQC: utils.None[*types.CommitQC](), Epoch: ep1}),
		roads:              newQueue[types.RoadIndex, *road](),
		epoch:              utils.NewAtomicSend(ep1),
		anchorEpoch:        utils.Some(ep1),
		blocks:             map[types.LaneID]*queue[types.BlockNumber, *types.Signed[*types.LaneProposal]]{},
		votes:              map[types.LaneID]*queue[types.BlockNumber, *blockVotes]{},
		nextBlockToPersist: map[types.LaneID]types.BlockNumber{},
	}
	for lane := range ep1.Committee().Lanes().All() {
		i.addLane(lane)
	}
	i.roads.first = last
	i.roads.next = last
	i.roads.pushBack(newRoad(qcLast, ep1))
	i.persistedCommitQC.Store(utils.Some(qcLast))

	require.True(t, i.canAdvanceEpoch())
	require.Error(t, i.advanceReadyEpochs(newTestDataState(&data.Config{Registry: registry})))
}

// TestRefreshConsensusSpec_WithholdsTipUntilNextViewEpochApplied: the durable tip
// sits on LastRoad(0) while applied is still epoch 0, and the tip's predecessor is
// retained. The spec must be withheld rather than published at that predecessor —
// a node that already entered epoch 1 holds the tip and must not be rolled back.
func TestRefreshConsensusSpec_WithholdsTipUntilNextViewEpochApplied(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 4)
	registry.AdvanceIfNeeded(epoch.LastRoad(0))

	ep0, ok := registry.EpochByIndex(0)
	require.True(t, ok)
	ep1, ok := registry.EpochByIndex(1)
	require.True(t, ok)

	last := epoch.LastRoad(0)
	qcPrev := types.BuildCommitQC(ep0, keys, utils.Some(tipLink(ep0, keys[0], last-2)), nil)
	qcLast := types.BuildCommitQC(ep0, keys, utils.Some(qcPrev), nil)
	require.Equal(t, last-1, qcPrev.Index())
	require.Equal(t, last, qcLast.Index())

	i := &inner{
		persistedCommitQC:  utils.NewAtomicSend(utils.None[*types.CommitQC]()),
		consensusSpec:      utils.NewAtomicSend(types.ConsensusSpec{CommitQC: utils.None[*types.CommitQC](), Epoch: ep0}),
		roads:              newQueue[types.RoadIndex, *road](),
		epoch:              utils.NewAtomicSend(ep0),
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
