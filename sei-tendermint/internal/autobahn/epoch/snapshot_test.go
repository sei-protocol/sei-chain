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
	live := []*types.Epoch{testEpoch(0, 0, committee), testEpoch(1, 0, committee)}
	require.NoError(t, persistedRegistryConv.Test(testPersistedRegistry(live, utils.None[*types.Epoch]())))
	require.NoError(t, persistedRegistryConv.Test(testPersistedRegistry(live, utils.Some(testEpoch(2, 0, committee)))))
	require.NoError(t, persistedRegistryConv.Test(testPersistedRegistry(live[1:], utils.None[*types.Epoch]())))

	live = []*types.Epoch{testEpoch(3, 7, committee), testEpoch(4, 7, committee)}
	require.NoError(t, persistedRegistryConv.Test(testPersistedRegistry(live, utils.None[*types.Epoch]())))
	require.NoError(t, persistedRegistryConv.Test(testPersistedRegistry(live, utils.Some(testEpoch(5, 7, committee)))))

	live = []*types.Epoch{
		types.NewEpoch(3, types.RoadRange{First: 1_000_000, Next: 1_000_017}, committee),
		types.NewEpoch(4, types.RoadRange{First: 1_000_017, Next: 1_000_060}, committee),
	}
	pending := types.NewEpoch(5, types.RoadRange{First: 1_000_060, Next: 1_000_100}, committee)
	require.NoError(t, persistedRegistryConv.Test(testPersistedRegistry(live, utils.Some(pending))))
}

func TestPersistedRegistryConvRejectsInvalidSnapshots(t *testing.T) {
	rng := utils.TestRng()
	committee, keys, _ := genCommittee(rng, 3)
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
	reject(&pb.PersistedEpochRegistry{Pending: first})
	reject(&pb.PersistedEpochRegistry{Live: []*pb.Epoch{first, record(2, 30, 40)}})
	reject(&pb.PersistedEpochRegistry{Live: []*pb.Epoch{first, record(1, 30, 40)}})
	reject(&pb.PersistedEpochRegistry{Live: []*pb.Epoch{first, record(4, 30, 40)}})
	reject(&pb.PersistedEpochRegistry{Live: []*pb.Epoch{record(^types.EpochIndex(0), 20, 30)}})
	reject(&pb.PersistedEpochRegistry{Live: []*pb.Epoch{first}, Pending: record(2, 30, 40)})
	reject(&pb.PersistedEpochRegistry{Live: []*pb.Epoch{first}, Pending: record(1, 30, 40)})
	reject(&pb.PersistedEpochRegistry{Live: []*pb.Epoch{first}, Pending: record(4, 30, 40)})

	malformed := record(2, 20, 30)
	malformed.Index = nil
	reject(&pb.PersistedEpochRegistry{Live: []*pb.Epoch{malformed}})
	malformed = record(2, 20, 30)
	malformed.Committee = nil
	reject(&pb.PersistedEpochRegistry{Live: []*pb.Epoch{malformed}})
	malformed = record(2, 20, 30)
	malformed.FirstRoad = nil
	reject(&pb.PersistedEpochRegistry{Live: []*pb.Epoch{malformed}})
	malformed = record(2, 20, 30)
	malformed.NextRoad = nil
	reject(&pb.PersistedEpochRegistry{Live: []*pb.Epoch{malformed}})
	malformed = record(3, 30, 40)
	malformed.NextRoad = nil
	reject(&pb.PersistedEpochRegistry{Live: []*pb.Epoch{first}, Pending: malformed})
	future := utils.OrPanic1(committee.DeriveNext(map[types.PublicKey]uint64{keys[0].Public(): 1, types.GenSecretKey(rng).Public(): 1}, 3))
	malformed = record(2, 20, 30)
	malformed.Committee = types.CommitteeConv.Encode(future)
	reject(&pb.PersistedEpochRegistry{Live: []*pb.Epoch{malformed}})

	reject(&pb.PersistedEpochRegistry{Live: []*pb.Epoch{record(2, 20, 20)}})
	reject(&pb.PersistedEpochRegistry{Live: []*pb.Epoch{record(2, 31, 30)}})
	reject(&pb.PersistedEpochRegistry{Live: []*pb.Epoch{first, record(3, 31, 40)}})
	reject(&pb.PersistedEpochRegistry{Live: []*pb.Epoch{first, record(3, 29, 40)}})
	reject(&pb.PersistedEpochRegistry{Live: []*pb.Epoch{first}, Pending: record(3, 31, 40)})
	reject(&pb.PersistedEpochRegistry{Live: []*pb.Epoch{first}, Pending: record(3, 29, 40)})
}
