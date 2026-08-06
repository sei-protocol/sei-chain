package avail

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/memblock"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus/persist"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/stretchr/testify/require"
)

func newTestDataState(cfg *data.Config) *data.State {
	return utils.OrPanic1(data.NewState(cfg, memblock.NewBlockDB()))
}

func testSignedBlock(key types.SecretKey, lane types.LaneID, n types.BlockNumber, parent types.BlockHeaderHash, rng utils.Rng) *types.Signed[*types.LaneProposal] {
	block := types.NewBlock(lane, n, parent, types.GenPayload(rng))
	return types.Sign(key, types.NewLaneProposal(block))
}

func TestNewInnerFreshStart(t *testing.T) {
	rng := utils.TestRng()
	registry, _ := epoch.GenRegistry(rng, 4)

	i, err := newInner(newTestDataState(&data.Config{Registry: registry}), &loadedState{})
	require.NoError(t, err)

	require.Equal(t, types.RoadIndex(0), i.nextAppQC)
	require.Equal(t, types.RoadIndex(0), i.roads.first)
	require.Equal(t, types.RoadIndex(0), i.roads.next)
	require.NotNil(t, i.nextBlockToPersist)
	for lane := range registry.LatestEpoch().Committee().Lanes().All() {
		require.Equal(t, types.BlockNumber(0), i.blocks[lane].first)
		require.Equal(t, types.BlockNumber(0), i.blocks[lane].next)
		require.Equal(t, types.BlockNumber(0), i.votes[lane].first)
		require.Equal(t, types.BlockNumber(0), i.votes[lane].next)
	}
}

func TestNewInnerLoadedBlocksContiguous(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 4)
	lane := keys[0].Public()

	var parent types.BlockHeaderHash
	var bs []persist.LoadedBlock
	for n := range types.BlockNumber(3) {
		b := testSignedBlock(keys[0], lane, n, parent, rng)
		parent = b.Msg().Block().Header().Hash()
		bs = append(bs, persist.LoadedBlock{Number: n, Proposal: b})
	}

	i, err := newInner(newTestDataState(&data.Config{Registry: registry}), &loadedState{
		blocks: map[types.LaneID][]persist.LoadedBlock{lane: bs},
	})
	require.NoError(t, err)

	q := i.blocks[lane]
	require.Equal(t, types.BlockNumber(0), q.first)
	require.Equal(t, types.BlockNumber(3), q.next)
	for j, b := range bs {
		require.Equal(t, b.Proposal, q.q[types.BlockNumber(j)])
	}
	require.Equal(t, types.BlockNumber(3), i.nextBlockToPersist[lane])
	for other := range registry.LatestEpoch().Committee().Lanes().All() {
		if other != lane {
			require.Equal(t, types.BlockNumber(0), i.nextBlockToPersist[other])
		}
	}
}

func TestNewInnerLoadedBlocksEmptySlice(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 4)
	lane := keys[0].Public()

	i, err := newInner(newTestDataState(&data.Config{Registry: registry}), &loadedState{
		blocks: map[types.LaneID][]persist.LoadedBlock{lane: {}},
	})
	require.NoError(t, err)

	q := i.blocks[lane]
	require.Equal(t, types.BlockNumber(0), q.first)
	require.Equal(t, types.BlockNumber(0), q.next)
}

func TestNewInnerLoadedBlocksUnknownLane(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 4)

	unknownKey := types.GenSecretKey(rng)
	unknownLane := unknownKey.Public()
	b := testSignedBlock(unknownKey, unknownLane, 0, types.BlockHeaderHash{}, rng)

	i, err := newInner(newTestDataState(&data.Config{Registry: registry}), &loadedState{
		blocks: map[types.LaneID][]persist.LoadedBlock{unknownLane: {{Number: 0, Proposal: b}}},
	})
	require.NoError(t, err)

	for lane := range registry.LatestEpoch().Committee().Lanes().All() {
		q := i.blocks[lane]
		require.Equal(t, types.BlockNumber(0), q.first)
		require.Equal(t, types.BlockNumber(0), q.next)
	}
	_ = keys
}

func TestNewInnerLoadedBlocksMultipleLanes(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 4)
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

	i, err := newInner(newTestDataState(&data.Config{Registry: registry}), &loadedState{
		blocks: map[types.LaneID][]persist.LoadedBlock{lane0: bs0, lane1: bs1},
	})
	require.NoError(t, err)

	require.Equal(t, types.BlockNumber(2), i.blocks[lane0].next)
	require.Equal(t, types.BlockNumber(3), i.blocks[lane1].next)
	require.Equal(t, types.BlockNumber(2), i.nextBlockToPersist[lane0])
	require.Equal(t, types.BlockNumber(3), i.nextBlockToPersist[lane1])
}

func TestNewInnerLoadedCommitQCsNoAppQC(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 4)

	qcs := make([]*types.CommitQC, 3)
	prev := utils.None[*types.CommitQC]()
	for i := range qcs {
		qcs[i] = types.BuildCommitQC(registry.LatestEpoch(), keys, prev, nil)
		prev = utils.Some(qcs[i])
	}

	inner, err := newInner(newTestDataState(&data.Config{Registry: registry}), &loadedState{commitQCs: qcs})
	require.NoError(t, err)

	require.Equal(t, types.RoadIndex(0), inner.roads.first)
	require.Equal(t, types.RoadIndex(3), inner.roads.next)
	for i, qc := range qcs {
		require.NoError(t, utils.TestDiff(qc, inner.roads.q[types.RoadIndex(i)].commitQC))
	}
	require.NoError(t, utils.TestDiff(utils.Some(qcs[2]), inner.persistedCommitQC.Load()))
}

func TestNewInnerLoadedCommitQCsGapReturnsError(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 4)

	qc0 := types.BuildCommitQC(registry.LatestEpoch(), keys, utils.None[*types.CommitQC](), nil)
	qc1 := types.BuildCommitQC(registry.LatestEpoch(), keys, utils.Some(qc0), nil)
	qc2 := types.BuildCommitQC(registry.LatestEpoch(), keys, utils.Some(qc1), nil)

	_, err := newInner(newTestDataState(&data.Config{Registry: registry}), &loadedState{commitQCs: []*types.CommitQC{qc0, qc2}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "non-contiguous")
}

func TestNewInnerLoadedCommitQCsEmpty(t *testing.T) {
	rng := utils.TestRng()
	registry, _ := epoch.GenRegistry(rng, 4)

	inner, err := newInner(newTestDataState(&data.Config{Registry: registry}), &loadedState{})
	require.NoError(t, err)

	require.Equal(t, types.RoadIndex(0), inner.roads.first)
	require.Equal(t, types.RoadIndex(0), inner.roads.next)
	_, ok := inner.persistedCommitQC.Load().Get()
	require.False(t, ok)
}

func TestNewInnerLoadedBlocksGapReturnsError(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 4)
	lane := keys[0].Public()

	var bs []persist.LoadedBlock
	for _, n := range []types.BlockNumber{3, 4, 6, 7} {
		bs = append(bs, persist.LoadedBlock{Number: n, Proposal: testSignedBlock(keys[0], lane, n, types.BlockHeaderHash{}, rng)})
	}

	_, err := newInner(newTestDataState(&data.Config{Registry: registry}), &loadedState{
		blocks: map[types.LaneID][]persist.LoadedBlock{lane: bs},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "non-contiguous")
}

func TestNewInnerLoadedBlocksParentHashMismatchReturnsError(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 4)
	lane := keys[0].Public()

	var parent types.BlockHeaderHash
	b0 := testSignedBlock(keys[0], lane, 0, parent, rng)
	parent = b0.Msg().Block().Header().Hash()
	b1 := testSignedBlock(keys[0], lane, 1, parent, rng)
	b2 := testSignedBlock(keys[0], lane, 2, types.GenBlockHeaderHash(rng), rng)

	_, err := newInner(newTestDataState(&data.Config{Registry: registry}), &loadedState{
		blocks: map[types.LaneID][]persist.LoadedBlock{lane: {
			{Number: 0, Proposal: b0},
			{Number: 1, Proposal: b1},
			{Number: 2, Proposal: b2},
		}},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "parent hash mismatch")
}

func TestNewInnerLoadedBlocksOverCapacityReturnsError(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 4)
	lane := keys[0].Public()

	count := BlocksPerLane + 5
	var parent types.BlockHeaderHash
	var bs []persist.LoadedBlock
	for n := types.BlockNumber(0); n < types.BlockNumber(count); n++ {
		b := testSignedBlock(keys[0], lane, n, parent, rng)
		parent = b.Msg().Block().Header().Hash()
		bs = append(bs, persist.LoadedBlock{Number: n, Proposal: b})
	}

	_, err := newInner(newTestDataState(&data.Config{Registry: registry}), &loadedState{
		blocks: map[types.LaneID][]persist.LoadedBlock{lane: bs},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds capacity")
}
