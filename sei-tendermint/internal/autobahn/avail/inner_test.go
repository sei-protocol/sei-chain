package avail

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/memblock"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
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

func TestNewInnerRestoresBlocksContiguous(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 4)
	lane := keys[0].Public()

	var parent types.BlockHeaderHash
	var bs []*types.Signed[*types.LaneProposal]
	for n := range types.BlockNumber(3) {
		b := testSignedBlock(keys[0], lane, n, parent, rng)
		parent = b.Msg().Block().Header().Hash()
		bs = append(bs, b)
	}

	i, err := newInner(newTestDataState(&data.Config{Registry: registry}), &loadedState{
		blocks: map[types.LaneID][]*types.Signed[*types.LaneProposal]{lane: bs},
	})
	require.NoError(t, err)

	q := i.blocks[lane]
	require.Equal(t, types.BlockNumber(0), q.first)
	require.Equal(t, types.BlockNumber(3), q.next)
	for j, b := range bs {
		require.Equal(t, b, q.q[types.BlockNumber(j)])
	}
	require.Equal(t, types.BlockNumber(3), i.nextBlockToPersist[lane])
	for other := range registry.LatestEpoch().Committee().Lanes().All() {
		if other != lane {
			require.Equal(t, types.BlockNumber(0), i.nextBlockToPersist[other])
		}
	}
}

func TestNewInnerRestoresBlocksEmptySlice(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 4)
	lane := keys[0].Public()

	i, err := newInner(newTestDataState(&data.Config{Registry: registry}), &loadedState{
		blocks: map[types.LaneID][]*types.Signed[*types.LaneProposal]{lane: {}},
	})
	require.NoError(t, err)

	q := i.blocks[lane]
	require.Equal(t, types.BlockNumber(0), q.first)
	require.Equal(t, types.BlockNumber(0), q.next)
}

func TestNewInnerRestoresBlocksUnknownLane(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 4)

	unknownKey := types.GenSecretKey(rng)
	unknownLane := unknownKey.Public()
	b := testSignedBlock(unknownKey, unknownLane, 0, types.BlockHeaderHash{}, rng)

	i, err := newInner(newTestDataState(&data.Config{Registry: registry}), &loadedState{
		blocks: map[types.LaneID][]*types.Signed[*types.LaneProposal]{unknownLane: {b}},
	})
	require.NoError(t, err)

	for lane := range registry.LatestEpoch().Committee().Lanes().All() {
		q := i.blocks[lane]
		require.Equal(t, types.BlockNumber(0), q.first)
		require.Equal(t, types.BlockNumber(0), q.next)
	}
	_ = keys
}

func TestNewInnerRestoresBlocksMultipleLanes(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 4)
	lane0 := keys[0].Public()
	lane1 := keys[1].Public()

	var parent0 types.BlockHeaderHash
	var bs0 []*types.Signed[*types.LaneProposal]
	for n := range types.BlockNumber(2) {
		b := testSignedBlock(keys[0], lane0, n, parent0, rng)
		parent0 = b.Msg().Block().Header().Hash()
		bs0 = append(bs0, b)
	}

	var parent1 types.BlockHeaderHash
	var bs1 []*types.Signed[*types.LaneProposal]
	for n := range types.BlockNumber(3) {
		b := testSignedBlock(keys[1], lane1, n, parent1, rng)
		parent1 = b.Msg().Block().Header().Hash()
		bs1 = append(bs1, b)
	}

	i, err := newInner(newTestDataState(&data.Config{Registry: registry}), &loadedState{
		blocks: map[types.LaneID][]*types.Signed[*types.LaneProposal]{lane0: bs0, lane1: bs1},
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

func TestNewInnerRestoresBlocksGapReturnsError(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 4)
	lane := keys[0].Public()

	var bs []*types.Signed[*types.LaneProposal]
	for _, n := range []types.BlockNumber{3, 4, 6, 7} {
		bs = append(bs, testSignedBlock(keys[0], lane, n, types.BlockHeaderHash{}, rng))
	}

	_, err := newInner(newTestDataState(&data.Config{Registry: registry}), &loadedState{
		blocks: map[types.LaneID][]*types.Signed[*types.LaneProposal]{lane: bs},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "non-contiguous")
}

func TestNewInnerRestoresBlocksParentHashMismatchReturnsError(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 4)
	lane := keys[0].Public()

	var parent types.BlockHeaderHash
	b0 := testSignedBlock(keys[0], lane, 0, parent, rng)
	parent = b0.Msg().Block().Header().Hash()
	b1 := testSignedBlock(keys[0], lane, 1, parent, rng)
	b2 := testSignedBlock(keys[0], lane, 2, types.GenBlockHeaderHash(rng), rng)

	_, err := newInner(newTestDataState(&data.Config{Registry: registry}), &loadedState{
		blocks: map[types.LaneID][]*types.Signed[*types.LaneProposal]{lane: {b0, b1, b2}},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "parent hash mismatch")
}

func TestNewInnerRestoresBlocksOverCapacityReturnsError(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 4)
	lane := keys[0].Public()

	count := BlocksPerLane + 5
	var parent types.BlockHeaderHash
	var bs []*types.Signed[*types.LaneProposal]
	for n := types.BlockNumber(0); n < types.BlockNumber(count); n++ {
		b := testSignedBlock(keys[0], lane, n, parent, rng)
		parent = b.Msg().Block().Header().Hash()
		bs = append(bs, b)
	}

	_, err := newInner(newTestDataState(&data.Config{Registry: registry}), &loadedState{
		blocks: map[types.LaneID][]*types.Signed[*types.LaneProposal]{lane: bs},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds capacity")
}
