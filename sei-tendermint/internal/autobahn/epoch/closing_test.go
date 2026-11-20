package epoch

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestClosingEpochIsTheLastRoadOfEachEpoch(t *testing.T) {
	for _, length := range []types.RoadHeight{3, DefaultEpochLength} {
		for _, first := range []types.RoadHeight{0, 1, 2*length + 7} {
			require.Equal(t, utils.None[types.EpochIndex](), ClosingEpoch(first, length, first))
			require.Equal(t, utils.Some(types.EpochIndex(0)), ClosingEpoch(first, length, first+length-1))
			require.Equal(t, utils.None[types.EpochIndex](), ClosingEpoch(first, length, first+length))
			require.Equal(t, utils.Some(types.EpochIndex(2)), ClosingEpoch(first, length, first+3*length-1))
		}
		require.Equal(t, utils.None[types.EpochIndex](), ClosingEpoch(length, length, length-1), "a road before the first")
	}
}

func TestClosingEpochIsWhereTheRegistryAdvances(t *testing.T) {
	const first, length types.RoadHeight = 7, 3
	weights := testWeights(utils.TestRng(), 3)
	registry, err := types.GenesisRegistry(weights, first, length)
	require.NoError(t, err)
	for road := first; road < first+5*length; road++ {
		advanced, err := registry.Advance(road, weights)
		require.NoError(t, err)
		closing := ClosingEpoch(first, length, road)
		next, ok := advanced.Get()
		require.Equal(t, ok, closing.IsPresent(), "road %d", road)
		if ok {
			epochs := next.Epochs()
			require.Equal(t, utils.Some(epochs[len(epochs)-1].EpochIndex()-2), closing, "closing epoch E derives epoch E+2")
			registry = next
		}
	}
}
