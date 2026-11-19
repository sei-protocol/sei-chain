package epoch

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestPersistedRegistryConv(t *testing.T) {
	rng := utils.TestRng()
	committee, _, _ := genCommittee(rng, 3)
	live := []*types.Epoch{testEpoch(3, 7, committee), testEpoch(4, 7, committee)}
	require.NoError(t, persistedRegistryConv.Test(testPersistedRegistry(live)))
}

func TestPersistedRegistryConvRejectsInvalidSnapshots(t *testing.T) {
	rng := utils.TestRng()
	committee, _, _ := genCommittee(rng, 3)
	record := func(idx types.EpochIndex, first, next types.RoadIndex) *pb.Epoch {
		return types.EpochConv.Encode(types.NewEpoch(idx, types.RoadRange{First: first, Next: next}, committee))
	}
	reject := func(snapshot *pb.PersistedEpochRegistry) {
		t.Helper()
		_, err := persistedRegistryConv.Decode(snapshot)
		require.Error(t, err)
	}
	first := record(2, 20, 30)
	var missing *pb.PersistedEpochRegistry
	reject(missing)
	reject(&pb.PersistedEpochRegistry{})
	reject(&pb.PersistedEpochRegistry{LiveV2: []*pb.Epoch{first, record(2, 30, 40)}})
	reject(&pb.PersistedEpochRegistry{LiveV2: []*pb.Epoch{first, record(1, 30, 40)}})
	reject(&pb.PersistedEpochRegistry{LiveV2: []*pb.Epoch{first, record(4, 30, 40)}})
	reject(&pb.PersistedEpochRegistry{LiveV2: []*pb.Epoch{record(^types.EpochIndex(0), 20, 30)}})

	malformed := record(2, 20, 30)
	malformed.Index = nil
	reject(&pb.PersistedEpochRegistry{LiveV2: []*pb.Epoch{malformed}})
	malformed = record(3, 30, 40)
	malformed.NextRoad = nil
	reject(&pb.PersistedEpochRegistry{LiveV2: []*pb.Epoch{first, malformed}})

	reject(&pb.PersistedEpochRegistry{LiveV2: []*pb.Epoch{record(2, 20, 20)}})
	reject(&pb.PersistedEpochRegistry{LiveV2: []*pb.Epoch{record(2, 31, 30)}})
	reject(&pb.PersistedEpochRegistry{LiveV2: []*pb.Epoch{first, record(3, 31, 40)}})
	reject(&pb.PersistedEpochRegistry{LiveV2: []*pb.Epoch{first, record(3, 29, 40)}})
}
