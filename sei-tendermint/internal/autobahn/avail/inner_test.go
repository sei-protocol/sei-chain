package avail

import (
	"fmt"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/memblock"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus/persist"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func newTestDataState(cfg *data.Config) *data.State {
	store := utils.OrPanic1(blockstore.New(memblock.NewBlockDB()))
	return utils.OrPanic1(data.NewState(cfg, store))
}

func newTestDataStateAtRoad(t *testing.T, rng utils.Rng, registry *epoch.Registry, keys []types.SecretKey, road types.RoadIndex) *data.State {
	t.Helper()
	ep := utils.OrPanic1(registry.EpochAt(road))
	proposal, blocks := types.ProposalAtBlocks(ep, types.View{Index: road}, 1)
	prepare := types.BuildPrepareQC(keys, proposal)
	votes := make([]*types.Signed[*types.CommitVote], len(keys))
	for n, key := range keys {
		votes[n] = types.Sign(key, types.NewCommitVote(prepare))
	}
	qc := types.NewCommitQC(votes)
	full := types.NewFullCommitQC(qc, []*types.BlockHeader{blocks[0].Header()})
	store := utils.OrPanic1(blockstore.New(memblock.NewBlockDB()))
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	require.NoError(t, store.WriteBlock(types.NewRoadBlock(full, blocks)))
	app := types.NewAppProposal(qc.View(), types.GenAppHash(rng))
	require.NoError(t, store.WriteAppProposal(app))
	require.NoError(t, store.WriteAppQC(data.TestAppQC(keys, app)))
	require.NoError(t, store.Flush())
	return utils.OrPanic1(data.NewState(&data.Config{Registry: registry}, store))
}

func testSignedBlock(key types.SecretKey, lane types.LaneID, n types.BlockNumber, parent types.BlockHeaderHash, rng utils.Rng) *types.Signed[*types.LaneProposal] {
	block := types.NewBlock(lane, n, parent, types.GenPayload(rng))
	return types.Sign(key, types.NewLaneProposal(block))
}

func blockHash(b persist.LoadedBlock) types.BlockHeaderHash {
	return b.Proposal.Msg().Block().Header().Hash()
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

func TestBlockQueueRetainsLast(t *testing.T) {
	rng := utils.TestRng()
	key := types.GenSecretKey(rng)
	lane := types.LaneID{Validator: key.Public(), Joined: 0}
	blocks := contiguousBlocks(key, lane, 3, rng)
	q := newBlockQueue()
	for _, b := range blocks {
		q.pushBack(b.Proposal)
	}

	q.prune(2, blockHash(blocks[1]))
	require.Equal(t, types.BlockNumber(2), q.first)
	require.Equal(t, types.BlockNumber(3), q.next)
	require.Equal(t, utils.Some(blocks[2].Proposal), q.localTip)
	// Block 2 is still active and carries the chain, so nothing below first is needed.
	require.Equal(t, types.BlockNumber(2), q.retentionFloor())

	q.prune(3, blockHash(blocks[2]))
	require.Equal(t, types.BlockNumber(3), q.first)
	require.Equal(t, types.BlockNumber(3), q.next)
	require.Equal(t, utils.Some(blocks[2].Proposal), q.localTip)
	require.Equal(t, types.BlockNumber(2), q.retentionFloor())

	q.prune(5, types.GenBlockHeaderHash(rng))
	require.Equal(t, types.BlockNumber(5), q.first)
	require.Equal(t, types.BlockNumber(5), q.next)
	require.Equal(t, utils.None[*types.Signed[*types.LaneProposal]](), q.localTip)
	require.Equal(t, types.BlockNumber(5), q.retentionFloor())
}

func TestInnerPruneViaQCRetainsLast(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	ep := registry.MustEpoch(0)
	lane := ep.Committee().Lane(keys[0].Public()).OrPanic("lane")
	blocks := contiguousBlocks(keys[0], lane, 3, rng)
	i := utils.OrPanic1(newInner(data.TestAnchor(ep), time.Time{}, &loadedState{}))
	for _, b := range blocks {
		i.lanes[lane].blocks.pushBack(b.Proposal)
	}

	last := blocks[len(blocks)-1].Proposal.Msg().Block().Header()
	qc := types.BuildCommitQC(ep, keys, utils.None[*types.CommitQC](), map[types.LaneID]*types.LaneQC{
		lane: types.NewLaneQC(makeLaneVotes(keys, last)),
	})
	lr := qc.LaneRange(lane)
	require.Equal(t, types.BlockNumber(3), lr.Next())
	require.Equal(t, last.Hash(), lr.LastHash())

	i.setAnchor(data.Anchor{First: utils.Some(data.AnchorFirst{
		CommitQC: qc,
		AppQC:    data.TestAppQC(keys, types.NewAppProposal(qc.View(), types.AppHash{})),
	}), Epochs: data.TestAnchor(ep).Epochs})
	q := i.lanes[lane].blocks
	require.Equal(t, types.BlockNumber(3), q.first)
	require.Equal(t, types.BlockNumber(3), q.next)
	require.Equal(t, utils.Some(blocks[2].Proposal), q.localTip)
	require.Equal(t, types.BlockNumber(2), q.retentionFloor())
	require.Equal(t, types.BlockNumber(2), i.lanes[lane].nextBlockToPersist)
	p, ok := q.unpersistedLocalTip(i.lanes[lane].nextBlockToPersist).Get()
	require.True(t, ok)
	require.Equal(t, blocks[2].Proposal, p)
}

func TestInnerPruneKeepsLastAheadOfQC(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	ep := registry.MustEpoch(0)
	lane := ep.Committee().Lane(keys[0].Public()).OrPanic("lane")
	blocks := contiguousBlocks(keys[0], lane, 3, rng)
	i := utils.OrPanic1(newInner(data.TestAnchor(ep), time.Time{}, &loadedState{}))
	for _, b := range blocks {
		i.lanes[lane].blocks.pushBack(b.Proposal)
	}

	qc := types.BuildCommitQC(ep, keys, utils.None[*types.CommitQC](), map[types.LaneID]*types.LaneQC{
		lane: types.NewLaneQC(makeLaneVotes(keys, blocks[0].Proposal.Msg().Block().Header())),
	})
	require.Equal(t, types.BlockNumber(1), qc.LaneRange(lane).Next())

	i.setAnchor(data.Anchor{First: utils.Some(data.AnchorFirst{
		CommitQC: qc,
		AppQC:    data.TestAppQC(keys, types.NewAppProposal(qc.View(), types.AppHash{})),
	}), Epochs: data.TestAnchor(ep).Epochs})
	q := i.lanes[lane].blocks
	require.Equal(t, types.BlockNumber(1), q.first)
	require.Equal(t, types.BlockNumber(3), q.next)
	require.Equal(t, utils.Some(blocks[2].Proposal), q.localTip)
}

func TestBlockQueueLastSurvivesRestart(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	ep0 := registry.MustEpoch(0)
	key := keys[0]
	lane := ep0.Committee().Lane(key.Public()).OrPanic("lane")
	block := contiguousBlocks(key, lane, 1, rng)[0]
	dir := t.TempDir()

	persister, _, err := persist.NewBlockPersister(utils.Some(dir))
	require.NoError(t, err)
	require.NoError(t, persister.PruneAndPersist(
		lane,
		0,
		[]*types.Signed[*types.LaneProposal]{block.Proposal},
	))
	q := newBlockQueue()
	q.pushBack(block.Proposal)
	q.prune(1, blockHash(block))
	require.NoError(t, persister.PruneAndPersist(lane, q.retentionFloor(), nil))
	require.NoError(t, persister.Close())

	persister, loaded, err := persist.NewBlockPersister(utils.Some(dir))
	require.NoError(t, err)
	require.NoError(t, persister.Close())
	i := utils.OrPanic1(newInner(data.TestAnchor(ep0), time.Time{}, &loadedState{}))
	i.lanes[lane].blocks.prune(1, blockHash(block))
	require.NoError(t, i.restoreBlocks(loaded))
	localTip, ok := i.lanes[lane].blocks.localTip.Get()
	require.True(t, ok)
	require.Equal(t, block.Proposal.Msg().Block().Header().Hash(), localTip.Msg().Block().Header().Hash())
}

func TestBlockQueueUnflushedLocalTipSurvivesRestart(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	ep0 := registry.MustEpoch(0)
	key := keys[0]
	lane := ep0.Committee().Lane(key.Public()).OrPanic("lane")
	block := contiguousBlocks(key, lane, 1, rng)[0]
	dir := t.TempDir()

	persister, _, err := persist.NewBlockPersister(utils.Some(dir))
	require.NoError(t, err)
	q := newBlockQueue()
	q.pushBack(block.Proposal)
	q.prune(1, blockHash(block))

	p, ok := q.unpersistedLocalTip(0).Get()
	require.True(t, ok)
	require.Equal(t, block.Proposal, p)
	require.NoError(t, persister.PruneAndPersist(
		lane,
		q.retentionFloor(),
		[]*types.Signed[*types.LaneProposal]{p},
	))
	require.False(t, q.unpersistedLocalTip(1).IsPresent())
	require.NoError(t, persister.Close())

	persister, loaded, err := persist.NewBlockPersister(utils.Some(dir))
	require.NoError(t, err)
	require.NoError(t, persister.Close())
	i := utils.OrPanic1(newInner(data.TestAnchor(ep0), time.Time{}, &loadedState{}))
	i.lanes[lane].blocks.prune(1, blockHash(block))
	require.NoError(t, i.restoreBlocks(loaded))
	localTip, ok := i.lanes[lane].blocks.localTip.Get()
	require.True(t, ok)
	require.Equal(t, block.Proposal.Msg().Block().Header().Hash(), localTip.Msg().Block().Header().Hash())
}

func TestRestoreInner_LoadedBlocks(t *testing.T) {
	t.Run("contiguous", func(t *testing.T) {
		rng := utils.TestRng()
		registry, keys := epoch.GenRegistry(rng, 4)
		ep0 := registry.MustEpoch(0)
		lane0 := ep0.Committee().Lane(keys[0].Public()).OrPanic("keys[0]")
		ds := newTestDataState(&data.Config{Registry: registry})
		bs := contiguousBlocks(keys[0], lane0, 3, rng)
		i, err := newInner(ds.Anchor().Load(), ds.GenesisTimestamp(), &loadedState{blocks: map[types.LaneID][]persist.LoadedBlock{lane0: bs}})
		require.NoError(t, err)
		q := i.lanes[lane0].blocks
		require.Equal(t, types.BlockNumber(0), q.first)
		require.Equal(t, types.BlockNumber(3), q.next)
		for j, b := range bs {
			require.Equal(t, b.Proposal, q.q[types.BlockNumber(j)])
		}
		require.Equal(t, types.BlockNumber(3), i.lanes[lane0].nextBlockToPersist)
		for other := range ep0.Committee().Lanes().All() {
			if other != lane0 {
				require.Equal(t, types.BlockNumber(0), i.lanes[other].nextBlockToPersist)
			}
		}
	})

	t.Run("empty slice", func(t *testing.T) {
		rng := utils.TestRng()
		registry, keys := epoch.GenRegistry(rng, 4)
		lane0 := registry.MustEpoch(0).Committee().Lane(keys[0].Public()).OrPanic("keys[0]")
		i, err := newInner(data.Anchor{Epochs: [2]*types.Epoch{registry.MustEpoch(0), registry.MustEpoch(1)}}, registry.GenesisTimestamp(), &loadedState{
			blocks: map[types.LaneID][]persist.LoadedBlock{lane0: {}},
		})
		require.NoError(t, err)
		q := i.lanes[lane0].blocks
		require.Equal(t, types.BlockNumber(0), q.first)
		require.Equal(t, types.BlockNumber(0), q.next)
	})

	t.Run("local tip below anchor", func(t *testing.T) {
		rng := utils.TestRng()
		registry, keys := epoch.GenRegistry(rng, 4)
		ep0 := registry.MustEpoch(0)
		lane := ep0.Committee().Lane(keys[0].Public()).OrPanic("keys[0]")
		blocks := contiguousBlocks(keys[0], lane, 2, rng)
		i := utils.OrPanic1(newInner(data.TestAnchor(ep0), time.Time{}, &loadedState{}))
		i.lanes[lane].blocks.prune(2, blockHash(blocks[1]))

		err := i.restoreBlocks(map[types.LaneID][]persist.LoadedBlock{lane: blocks})
		require.NoError(t, err)
		q := i.lanes[lane].blocks
		require.Equal(t, types.BlockNumber(2), q.first)
		require.Equal(t, types.BlockNumber(2), q.next)
		require.Equal(t, utils.Some(blocks[1].Proposal), q.localTip)
		require.Equal(t, types.BlockNumber(1), q.retentionFloor())
		require.Equal(t, types.BlockNumber(2), i.lanes[lane].nextBlockToPersist)
	})

	t.Run("after prune the live block extends the certified tip", func(t *testing.T) {
		rng := utils.TestRng()
		registry, keys := epoch.GenRegistry(rng, 4)
		ep0 := registry.MustEpoch(0)
		lane := ep0.Committee().Lane(keys[0].Public()).OrPanic("keys[0]")
		tip := types.GenBlockHeaderHash(rng)
		// The leftover sits below first. The live block names the certified tip.
		old := testSignedBlock(keys[0], lane, 0, types.BlockHeaderHash{}, rng)
		require.NotEqual(t, tip, old.Msg().Block().Header().Hash())
		live := testSignedBlock(keys[0], lane, 1, tip, rng)
		i := utils.OrPanic1(newInner(data.TestAnchor(ep0), time.Time{}, &loadedState{}))
		i.lanes[lane].blocks.prune(1, tip)

		err := i.restoreBlocks(map[types.LaneID][]persist.LoadedBlock{lane: {
			{Number: 0, Proposal: old},
			{Number: 1, Proposal: live},
		}})
		require.NoError(t, err)
		q := i.lanes[lane].blocks
		require.Equal(t, types.BlockNumber(1), q.first)
		require.Equal(t, types.BlockNumber(2), q.next)
		require.Equal(t, utils.Some(live), q.localTip)
	})

	t.Run("after prune the local block is kept", func(t *testing.T) {
		rng := utils.TestRng()
		registry, keys := epoch.GenRegistry(rng, 4)
		ep0 := registry.MustEpoch(0)
		lane := ep0.Committee().Lane(keys[0].Public()).OrPanic("keys[0]")
		tip := types.GenBlockHeaderHash(rng)
		old := testSignedBlock(keys[0], lane, 0, types.BlockHeaderHash{}, rng)
		live := testSignedBlock(keys[0], lane, 1, types.GenBlockHeaderHash(rng), rng)
		require.NotEqual(t, tip, live.Msg().Block().Header().ParentHash())
		i := utils.OrPanic1(newInner(data.TestAnchor(ep0), time.Time{}, &loadedState{}))
		i.lanes[lane].blocks.prune(1, tip)

		err := i.restoreBlocks(map[types.LaneID][]persist.LoadedBlock{lane: {
			{Number: 0, Proposal: old},
			{Number: 1, Proposal: live},
		}})
		require.NoError(t, err)
		q := i.lanes[lane].blocks
		require.Equal(t, types.BlockNumber(1), q.first)
		require.Equal(t, types.BlockNumber(2), q.next)
		require.Equal(t, utils.Some(live), q.localTip)
	})

	t.Run("foreign loaded lane is ignored", func(t *testing.T) {
		rng := utils.TestRng()
		registry, _ := epoch.GenRegistry(rng, 4)
		unknownKey := types.GenSecretKey(rng)
		unknownLane := types.LaneID{Validator: unknownKey.Public(), Joined: 0}
		b := testSignedBlock(unknownKey, unknownLane, 0, types.BlockHeaderHash{}, rng)
		i, err := newInner(data.Anchor{Epochs: [2]*types.Epoch{registry.MustEpoch(0), registry.MustEpoch(1)}}, registry.GenesisTimestamp(), &loadedState{
			blocks: map[types.LaneID][]persist.LoadedBlock{unknownLane: {{Number: 0, Proposal: b}}},
		})
		require.NoError(t, err)
		_, ok := i.lanes[unknownLane]
		require.False(t, ok)
		for lane := range registry.MustEpoch(0).Committee().Lanes().All() {
			cq := i.lanes[lane].blocks
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
		i, err := newInner(data.Anchor{Epochs: [2]*types.Epoch{registry.MustEpoch(0), registry.MustEpoch(1)}}, registry.GenesisTimestamp(), &loadedState{
			blocks: map[types.LaneID][]persist.LoadedBlock{lane0: bs0, lane1: bs1},
		})
		require.NoError(t, err)
		require.Equal(t, types.BlockNumber(2), i.lanes[lane0].blocks.next)
		require.Equal(t, types.BlockNumber(3), i.lanes[lane1].blocks.next)
		require.Equal(t, types.BlockNumber(2), i.lanes[lane0].nextBlockToPersist)
		require.Equal(t, types.BlockNumber(3), i.lanes[lane1].nextBlockToPersist)
	})

	t.Run("gap", func(t *testing.T) {
		rng := utils.TestRng()
		registry, keys := epoch.GenRegistry(rng, 4)
		lane0 := registry.MustEpoch(0).Committee().Lane(keys[0].Public()).OrPanic("keys[0]")
		var bs []persist.LoadedBlock
		for _, n := range []types.BlockNumber{3, 4, 6, 7} {
			bs = append(bs, persist.LoadedBlock{Number: n, Proposal: testSignedBlock(keys[0], lane0, n, types.BlockHeaderHash{}, rng)})
		}
		_, err := newInner(data.Anchor{Epochs: [2]*types.Epoch{registry.MustEpoch(0), registry.MustEpoch(1)}}, registry.GenesisTimestamp(), &loadedState{
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
		_, err := newInner(data.Anchor{Epochs: [2]*types.Epoch{registry.MustEpoch(0), registry.MustEpoch(1)}}, registry.GenesisTimestamp(), &loadedState{blocks: map[types.LaneID][]persist.LoadedBlock{lane0: {
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
		_, err := newInner(data.Anchor{Epochs: [2]*types.Epoch{registry.MustEpoch(0), registry.MustEpoch(1)}}, registry.GenesisTimestamp(), &loadedState{
			blocks: map[types.LaneID][]persist.LoadedBlock{lane0: bs},
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "exceeds capacity")
	})
}

func TestPruneKeepsAnchorTipAndLocalBlock(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 4)
	ep := registry.MustEpoch(0)
	committee := ep.Committee()
	lane := committee.Lane(keys[0].Public()).OrPanic("lane")
	other := committee.Lane(keys[1].Public()).OrPanic("other")
	tip := types.NewBlock(lane, 0, types.BlockHeaderHash{}, types.GenPayload(rng)).Header()
	qc1 := types.BuildCommitQC(ep, keys, utils.None[*types.CommitQC](), map[types.LaneID]*types.LaneQC{
		lane: types.NewLaneQC(makeLaneVotes(keys, tip)),
	})
	otherHeader := types.NewBlock(other, 0, types.BlockHeaderHash{}, types.GenPayload(rng)).Header()
	qc2 := types.BuildCommitQC(ep, keys, utils.Some(qc1), map[types.LaneID]*types.LaneQC{
		other: types.NewLaneQC(makeLaneVotes(keys, otherHeader)),
	})
	require.Equal(t, uint64(0), qc2.LaneRange(lane).Len())
	require.Equal(t, tip.Hash(), qc2.LaneRange(lane).LastHash())

	i := utils.OrPanic1(newInner(data.TestAnchor(ep), time.Time{}, &loadedState{}))
	i.setAnchor(data.Anchor{First: utils.Some(data.AnchorFirst{
		CommitQC: qc2,
		AppQC:    data.TestAppQC(keys, types.NewAppProposal(qc2.View(), types.AppHash{})),
	}), Epochs: data.TestAnchor(ep).Epochs})
	// A newer road QC is ahead of this queue. ProduceLocalBlock still extends
	// the anchor tip until a later AppQC prune advances parentOfFirstLaneBlock.
	later := types.NewBlock(lane, qc2.LaneRange(lane).Next(), tip.Hash(), types.GenPayload(rng)).Header()
	qc3 := types.BuildCommitQC(ep, keys, utils.Some(qc2), map[types.LaneID]*types.LaneQC{
		lane: types.NewLaneQC(makeLaneVotes(keys, later)),
	})
	i.roads.pushBack(newRoad(qc3, ep))
	require.NotEqual(t, tip.Hash(), qc3.LaneRange(lane).LastHash())
	require.Equal(t, tip.Hash(), i.lanes[lane].blocks.parentHash())

	// The block already in the WAL is this lane's local block, even when its
	// parent is not the certified tip. The suffix stays until AppQC prunes it.
	local := testSignedBlock(keys[0], lane, qc2.LaneRange(lane).Next(), types.GenBlockHeaderHash(rng), rng)
	require.NotEqual(t, tip.Hash(), local.Msg().Block().Header().ParentHash())
	require.NoError(t, i.restoreBlocks(map[types.LaneID][]persist.LoadedBlock{lane: {{
		Number:   local.Msg().Block().Header().BlockNumber(),
		Proposal: local,
	}}}))
	q := i.lanes[lane].blocks
	require.Equal(t, local, q.q[q.first])
}

func TestRestoreInner_LoadedCommitQCs(t *testing.T) {
	t.Run("contiguous", func(t *testing.T) {
		rng := utils.TestRng()
		registry, keys := epoch.GenRegistry(rng, 4)
		ep0 := registry.MustEpoch(0)
		ds := newTestDataState(&data.Config{Registry: registry})
		qcs := make([]*types.CommitQC, 3)
		prev := utils.None[*types.CommitQC]()
		for i := range qcs {
			qcs[i] = types.BuildCommitQC(ep0, keys, prev, nil)
			prev = utils.Some(qcs[i])
		}
		inner, err := newInner(ds.Anchor().Load(), ds.GenesisTimestamp(), &loadedState{commitQCs: qcs})
		require.NoError(t, err)
		require.Equal(t, ep0.RoadRange().First, inner.roads.first)
		require.Equal(t, ep0.RoadRange().First+3, inner.roads.next)
		for i, qc := range qcs {
			require.NoError(t, utils.TestDiff(qc, inner.roads.q[ep0.RoadRange().First+types.RoadIndex(i)].commitQC))
		}
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
		ds := newTestDataState(&data.Config{Registry: registry})
		qc0 := types.BuildCommitQC(ep0, keys, utils.None[*types.CommitQC](), nil)
		qc1 := types.BuildCommitQC(ep0, keys, utils.Some(qc0), nil)
		qc2 := types.BuildCommitQC(ep0, keys, utils.Some(qc1), nil)
		_, err := newInner(ds.Anchor().Load(), ds.GenesisTimestamp(), &loadedState{commitQCs: []*types.CommitQC{qc0, qc2}})
		require.Error(t, err)
		require.Contains(t, err.Error(), "non-contiguous")
	})
}

func TestNewInnerAdvancesAtPersistedSeal(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 4)
	ep0, ep1 := registry.MustEpoch(0), registry.MustEpoch(1)
	anchor := testAnchorAt(ep0, ep1, keys, registry.LastRoad(0)-1)
	qcLast := types.BuildCommitQC(ep0, keys, anchor.CommitQC(), nil)
	i, err := newInner(anchor, registry.GenesisTimestamp(), &loadedState{commitQCs: []*types.CommitQC{qcLast}})
	require.NoError(t, err)
	spec := i.consensusSpec.Load()
	require.Equal(t, utils.Some(qcLast), spec.CommitQC)
	require.Equal(t, ep1, spec.Epoch)
}

func TestPruneAdvancesAppliedEpoch(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistryThrough(rng, 4, 2)
	ep0 := registry.MustEpoch(0)
	ep1 := registry.MustEpoch(1)
	ep2 := registry.MustEpoch(2)
	i := utils.OrPanic1(newInner(data.TestAnchor(ep0), time.Time{}, &loadedState{}))
	require.Equal(t, types.EpochIndex(0), i.applied().EpochIndex())

	voteProposal := types.ProposalAt(ep1, types.View{Index: registry.LastRoad(1), Number: 0})
	prev := types.NewCommitQC([]*types.Signed[*types.CommitVote]{
		types.Sign(keys[0], types.NewCommitVote(types.BuildPrepareQC(keys, voteProposal))),
	})
	qc := types.BuildCommitQC(ep2, keys, utils.Some(prev), nil)
	i.setAnchor(data.Anchor{First: utils.Some(data.AnchorFirst{
		CommitQC: qc,
		AppQC:    data.TestAppQC(keys, types.NewAppProposal(qc.View(), types.AppHash{})),
	}), Epochs: data.TestAnchor(ep2).Epochs})
	require.Equal(t, types.EpochIndex(2), i.applied().EpochIndex())
	ae := i.anchor.Epochs[0]
	require.Equal(t, types.EpochIndex(2), ae.EpochIndex())
}

func TestPrune_WithoutFirst(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 2)
	weights := map[types.PublicKey]uint64{keys[0].Public(): 1}
	require.NoError(t, registry.TestPushEpoch(0, weights))
	require.NoError(t, registry.TestPushEpoch(1, weights))
	i := utils.OrPanic1(newInner(data.Anchor{Epochs: [2]*types.Epoch{registry.MustEpoch(0), registry.MustEpoch(1)}}, time.Time{}, &loadedState{}))
	closedLane := i.applied().Committee().Lane(keys[1].Public()).OrPanic("genesis lane")
	anchor := data.Anchor{Epochs: [2]*types.Epoch{registry.MustEpoch(2), registry.MustEpoch(3)}}

	i.setAnchor(anchor)
	require.Equal(t, anchor, i.anchor)
	require.Equal(t, anchor.Epochs[0].RoadRange().First, i.roads.first)
	require.Equal(t, i.roads.first, i.roads.next)
	require.False(t, i.consensusSpec.Load().CommitQC.IsPresent())
	_, ok := i.lanes[closedLane]
	require.False(t, ok)
}

func TestNewInnerInitializesGenesis(t *testing.T) {
	rng := utils.TestRng()
	registry, _ := epoch.GenRegistry(rng, 3)
	ep0 := registry.MustEpoch(0)
	timestamp := time.Unix(42, 0)
	registry, err := epoch.NewRegistry(ep0.Committee(), 7, timestamp, utils.None[string]())
	require.NoError(t, err)
	ep := ep0
	i := utils.OrPanic1(newInner(data.TestAnchor(ep), timestamp, &loadedState{}))
	require.Equal(t, ep.RoadRange().First, i.roads.first)
	require.Equal(t, ep.RoadRange().First, i.roads.next)
	require.False(t, i.anchor.First.IsPresent())
	require.False(t, i.consensusSpec.Load().CommitQC.IsPresent())
	require.Equal(t, types.ConsensusSpec{Epoch: ep, GenesisTimestamp: timestamp}, i.consensusSpec.Load())
	require.NotNil(t, i.lanes)
	for lane := range ep.Committee().Lanes().All() {
		ls, ok := i.lanes[lane]
		require.True(t, ok)
		blocks, votes, cursor := ls.blocks, ls.votes, ls.nextBlockToPersist
		require.Equal(t, types.BlockNumber(0), blocks.first)
		require.Equal(t, types.BlockNumber(0), blocks.next)
		require.Equal(t, types.BlockNumber(0), votes.first)
		require.Equal(t, types.BlockNumber(0), votes.next)
		require.Equal(t, types.BlockNumber(0), cursor)
	}
	require.Equal(t, registry.MustEpoch(1), utils.OrPanic1(newInner(data.TestAnchor(registry.MustEpoch(1)), timestamp, &loadedState{})).applied())
}

func TestRestoreInnerInitializesAnchor(t *testing.T) {
	for _, idx := range []types.EpochIndex{0, 1} {
		t.Run(fmt.Sprint(idx), func(t *testing.T) {
			rng := utils.TestRng()
			registry, keys := epoch.GenRegistry(rng, 3)
			ep := registry.MustEpoch(idx)
			qc := types.BuildCommitQC(ep, keys, utils.None[*types.CommitQC](), nil)
			timestamp := time.Unix(42, 0)
			i := utils.OrPanic1(newInner(data.Anchor{First: utils.Some(data.AnchorFirst{CommitQC: qc, AppQC: data.TestAppQC(keys, types.NewAppProposal(qc.View(), nil))}), Epochs: data.TestAnchor(ep).Epochs}, timestamp, &loadedState{}))
			require.Equal(t, qc.Index()+1, i.roads.first)
			require.Equal(t, qc.Index()+1, i.roads.next)
			require.Equal(t, ep, i.anchor.Epochs[0])
			require.Equal(t, types.ConsensusSpec{Epoch: ep, CommitQC: utils.Some(qc), GenesisTimestamp: timestamp}, i.consensusSpec.Load())
			for lane := range ep.Committee().Lanes().All() {
				lr := qc.LaneRange(lane)
				require.Equal(t, lr.Next(), i.lanes[lane].blocks.first)
				require.Equal(t, lr.Next(), i.lanes[lane].blocks.next)
				require.Equal(t, lr.LastHash(), i.lanes[lane].blocks.parentHash())
				require.Equal(t, lr.Next(), i.lanes[lane].votes.first)
				require.Equal(t, lr.Next(), i.lanes[lane].votes.next)
				require.Equal(t, lr.Next(), i.lanes[lane].nextBlockToPersist)
			}
		})
	}
}
