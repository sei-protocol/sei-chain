package epoch

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

const testFirstRoad types.RoadHeight = 1

// testWeights returns weights for n validators with distinct keys, all weight 1000.
func testWeights(rng utils.Rng, n int) map[types.PublicKey]uint64 {
	weights := map[types.PublicKey]uint64{}
	for _, sk := range utils.GenSliceN(rng, n, types.GenSecretKey) {
		weights[sk.Public()] = 1000
	}
	return weights
}

func testRegistry(t *testing.T, weights map[types.PublicKey]uint64, length types.RoadHeight) *TestRegistry {
	t.Helper()
	registry, err := types.GenesisRegistry(weights, testFirstRoad, length)
	require.NoError(t, err)
	return newTestRegistry(registry)
}

func TestGenesisRegistryHoldsEpochsZeroAndOne(t *testing.T) {
	rng := utils.TestRng()
	weights := testWeights(rng, 3)
	registry, err := types.GenesisRegistry(weights, testFirstRoad, 10)
	require.NoError(t, err)
	epochs := registry.Epochs()
	require.Len(t, epochs, 2)
	require.Equal(t, types.RoadRange{First: 1, Next: 11}, epochs[0].RoadRange())
	require.Equal(t, types.RoadRange{First: 11, Next: 21}, epochs[1].RoadRange())
	require.Equal(t, types.EpochIndex(0), epochs[0].EpochIndex())
	require.Equal(t, types.EpochIndex(1), epochs[1].EpochIndex())
}

func TestGenesisRegistryRejects(t *testing.T) {
	_, err := types.GenesisRegistry(testWeights(utils.TestRng(), 1), testFirstRoad, 0)
	require.Error(t, err)
	_, err = types.GenesisRegistry(map[types.PublicKey]uint64{}, testFirstRoad, 10)
	require.Error(t, err)
}

func TestAdvanceOnlyDerivesAtTheRoadBeforeTheBoundary(t *testing.T) {
	rng := utils.TestRng()
	weights := testWeights(rng, 3)
	registry := utils.OrPanic1(types.GenesisRegistry(weights, testFirstRoad, 10))
	// Epoch 2 is derived at LastRoad(0) = 10, the road before epoch 1's boundary.
	for road := types.RoadHeight(1); road < 10; road++ {
		next, err := registry.Advance(road, weights)
		require.NoError(t, err)
		require.False(t, next.IsPresent(), "road %d", road)
	}
	next, err := registry.Advance(10, weights)
	require.NoError(t, err)
	require.True(t, next.IsPresent())
	advanced := next.OrPanic("registry")
	got := advanced.Epochs()
	require.Len(t, got, 3)
	require.Equal(t, types.EpochIndex(2), got[2].EpochIndex())
	require.Equal(t, types.RoadRange{First: 21, Next: 31}, got[2].RoadRange())
	round, err := Decode(Encode(advanced))
	require.NoError(t, err)
	require.Equal(t, got[2].EpochIndex(), round.Epochs()[2].EpochIndex())
	require.Equal(t, got[2].RoadRange(), round.Epochs()[2].RoadRange())
}

func TestAdvanceKeepsTheThreeNewestEpochs(t *testing.T) {
	rng := utils.TestRng()
	weights := testWeights(rng, 3)
	r := testRegistry(t, weights, 10)
	for target := types.EpochIndex(2); target <= 5; target++ {
		require.NoError(t, r.TestPushEpoch(target-2, weights))
	}
	epochs := r.Epochs()
	require.Len(t, epochs, 3)
	require.Equal(t, types.EpochIndex(3), epochs[0].EpochIndex())
	require.Equal(t, types.EpochIndex(5), epochs[2].EpochIndex())
}

func TestAdvanceRejects(t *testing.T) {
	rng := utils.TestRng()
	weights := testWeights(rng, 3)
	registry := utils.OrPanic1(types.GenesisRegistry(weights, testFirstRoad, 10))
	_, err := registry.Advance(11, weights)
	require.Error(t, err)
	_, err = registry.Advance(10, map[types.PublicKey]uint64{})
	require.Error(t, err)
}

func TestDecodeRejectsInvalidRows(t *testing.T) {
	rng := utils.TestRng()
	committee := utils.OrPanic1(types.NewCommittee(testWeights(rng, 3)))
	record := func(idx types.EpochIndex, first, next types.RoadHeight) *pb.Epoch {
		return types.EpochConv.Encode(types.NewEpoch(idx, types.RoadRange{First: first, Next: next}, committee))
	}
	reject := func(name string, live ...*pb.Epoch) {
		t.Helper()
		_, err := Decode(protoutils.Marshal(&pb.EpochRegistry{Epochs: live}))
		require.Error(t, err, name)
	}
	reject("empty")
	reject("gap in index", record(2, 20, 30), record(4, 30, 40))
	reject("noncontiguous roads", record(2, 20, 30), record(3, 31, 40))
	reject("first index is not the next epoch", record(2, 20, 30), record(2, 30, 40))
	malformed := record(2, 20, 30)
	malformed.Index = nil
	reject("missing index field", malformed)
	_, err := Decode([]byte{0xff})
	require.Error(t, err)
	extra := utils.OrPanic1(types.NewCommittee(testWeights(rng, 1)))
	reject("committee does not follow", record(2, 20, 30), types.EpochConv.Encode(types.NewEpoch(3, types.RoadRange{First: 30, Next: 40}, extra)))
}
