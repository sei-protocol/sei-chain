package epoch

import (
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestPersistedRegistryConvValidatesShape(t *testing.T) {
	rng := utils.TestRng()
	a := types.GenSecretKey(rng).Public()
	b := types.GenSecretKey(rng).Public()
	genesis := utils.OrPanic1(types.NewCommittee(map[types.PublicKey]uint64{a: 1}))
	c2 := utils.OrPanic1(genesis.DeriveNext(map[types.PublicKey]uint64{a: 1, b: 1}, 2))
	c3 := utils.OrPanic1(c2.DeriveNext(map[types.PublicKey]uint64{a: 1, b: 1}, 3))
	c4 := utils.OrPanic1(c3.DeriveNext(map[types.PublicKey]uint64{a: 1, b: 1}, 4))
	restore := func(snapshot *pb.PersistedEpochRegistry) error {
		persisted, err := persistedRegistryConv.DecodeReq(snapshot)
		if err != nil {
			return err
		}
		_, err = newRegistryState(genesis, 0, time.Time{}, utils.Some(persisted))
		return err
	}

	require.Error(t, restore(&pb.PersistedEpochRegistry{
		Live: []*pb.Epoch{testEpoch(2, c2), testEpoch(4, c4)},
	}))
	require.Error(t, restore(&pb.PersistedEpochRegistry{
		Live:    []*pb.Epoch{testEpoch(2, c2)},
		Pending: testEpoch(4, c4),
	}))
}

func TestNewRegistryStateRejectsBrokenDerivation(t *testing.T) {
	rng := utils.TestRng()
	a := types.GenSecretKey(rng).Public()
	b := types.GenSecretKey(rng).Public()
	genesis := utils.OrPanic1(types.NewCommittee(map[types.PublicKey]uint64{a: 1}))
	other := utils.OrPanic1(types.NewCommittee(map[types.PublicKey]uint64{a: 1, b: 1}))
	restore := func(snapshot *pb.PersistedEpochRegistry) error {
		persisted, err := persistedRegistryConv.DecodeReq(snapshot)
		if err != nil {
			return err
		}
		_, err = newRegistryState(genesis, 0, time.Time{}, utils.Some(persisted))
		return err
	}

	require.Error(t, restore(&pb.PersistedEpochRegistry{
		Live: []*pb.Epoch{testEpoch(0, genesis), testEpoch(1, genesis), testEpoch(2, other)},
	}))
	require.Error(t, restore(&pb.PersistedEpochRegistry{
		Live:    []*pb.Epoch{testEpoch(0, genesis), testEpoch(1, genesis)},
		Pending: testEpoch(2, other),
	}))
}

func TestNewRegistryStateAcceptsDerivedChain(t *testing.T) {
	rng := utils.TestRng()
	a := types.GenSecretKey(rng).Public()
	b := types.GenSecretKey(rng).Public()
	genesis := utils.OrPanic1(types.NewCommittee(map[types.PublicKey]uint64{a: 1}))
	c2 := utils.OrPanic1(genesis.DeriveNext(map[types.PublicKey]uint64{a: 1, b: 1}, 2))
	c3 := utils.OrPanic1(c2.DeriveNext(map[types.PublicKey]uint64{a: 1, b: 1}, 3))
	restore := func(snapshot *pb.PersistedEpochRegistry) error {
		persisted, err := persistedRegistryConv.DecodeReq(snapshot)
		if err != nil {
			return err
		}
		_, err = newRegistryState(genesis, 0, time.Time{}, utils.Some(persisted))
		return err
	}

	require.NoError(t, restore(&pb.PersistedEpochRegistry{
		Live: []*pb.Epoch{testEpoch(2, c2)},
	}))
	require.NoError(t, restore(&pb.PersistedEpochRegistry{
		Live:    []*pb.Epoch{testEpoch(2, c2)},
		Pending: testEpoch(3, c3),
	}))
	require.NoError(t, restore(&pb.PersistedEpochRegistry{
		Live:    []*pb.Epoch{testEpoch(0, genesis), testEpoch(1, genesis)},
		Pending: testEpoch(2, c2),
	}))
}

func TestNewRegistryStateAllowsUnchainedFirstLive(t *testing.T) {
	rng := utils.TestRng()
	a := types.GenSecretKey(rng).Public()
	b := types.GenSecretKey(rng).Public()
	genesis := utils.OrPanic1(types.NewCommittee(map[types.PublicKey]uint64{a: 1}))
	other := utils.OrPanic1(types.NewCommittee(map[types.PublicKey]uint64{b: 1}))
	persisted, err := persistedRegistryConv.DecodeReq(&pb.PersistedEpochRegistry{
		Live: []*pb.Epoch{testEpoch(3, other)},
	})
	require.NoError(t, err)
	_, err = newRegistryState(genesis, 0, time.Time{}, utils.Some(persisted))
	require.NoError(t, err)
}

func TestDecodeEpochRejectsFutureJoin(t *testing.T) {
	rng := utils.TestRng()
	a := types.GenSecretKey(rng).Public()
	b := types.GenSecretKey(rng).Public()
	genesis := utils.OrPanic1(types.NewCommittee(map[types.PublicKey]uint64{a: 1}))
	committee := utils.OrPanic1(genesis.DeriveNext(map[types.PublicKey]uint64{a: 1, b: 1}, 3))

	_, err := types.EpochConv.DecodeReq(testEpoch(2, committee))
	require.Error(t, err)
}

func TestPersistedRegistryConv(t *testing.T) {
	for _, tc := range []struct {
		name    string
		last    types.EpochIndex
		pending bool
		prune   types.EpochIndex
	}{
		{name: "genesis", last: 1},
		{name: "genesis and pending", last: 1, pending: true},
		{name: "genesis partially pruned", last: 1, prune: 1},
		{name: "live", last: 5},
		{name: "live and pending", last: 5, pending: true},
		{name: "pruned", last: 5, prune: 4},
		{name: "pruned and pending", last: 5, prune: 4, pending: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rng := utils.TestRng()
			r, _ := GenRegistryThrough(rng, 3, tc.last)
			if tc.pending {
				committee := r.MustEpoch(tc.last).Committee()
				weights := map[types.PublicKey]uint64{}
				for lane := range committee.Lanes().All() {
					weights[lane.Validator] = committee.Weight(lane.Validator)
				}
				require.NoError(t, r.StageEpoch(tc.last-1, weights))
			}
			if tc.prune > 0 {
				require.NoError(t, r.PruneBefore(tc.prune))
			}
			for state := range r.state.Lock() {
				require.NoError(t, persistedRegistryConv.Test(state.persistedRegistry))
			}
		})
	}
}

// Equal compares persisted committees and the presence of a pending epoch.
func (s persistedRegistry) Equal(other persistedRegistry) bool {
	pending, ok := s.pending.Get()
	otherPending, otherOK := other.pending.Get()
	return utils.TestEqual(s.live, other.live) && ok == otherOK &&
		(!ok || utils.TestEqual(pending, otherPending))
}

func testEpoch(idx types.EpochIndex, committee *types.Committee) *pb.Epoch {
	first := types.RoadIndex(idx) * epochLength
	return types.EpochConv.Encode(types.NewEpoch(idx, types.RoadRange{First: first, Next: first + epochLength}, committee))
}

func TestPersistedRegistryConvRoadRanges(t *testing.T) {
	rng := utils.TestRng()
	committee, _, _ := genCommittee(rng, 3)
	persisted := testRegistryRoadRanges(committee)
	require.NoError(t, persistedRegistryConv.Test(persisted))
}

func TestPersistedRegistryConvRejectsMissingRoads(t *testing.T) {
	rng := utils.TestRng()
	committee, _, _ := genCommittee(rng, 3)
	_, err := persistedRegistryConv.DecodeReq(&pb.PersistedEpochRegistry{
		Live: []*pb.Epoch{{Index: new(uint64(2)), Committee: types.CommitteeConv.Encode(committee)}},
	})
	require.Error(t, err)
}

func TestPersistedRegistryConvRejectsInvalidRoadRanges(t *testing.T) {
	for _, tc := range []struct {
		name  string
		first types.RoadIndex
		next  types.RoadIndex
	}{
		{name: "empty", first: 10, next: 10},
		{name: "reversed", first: 11, next: 10},
		{name: "gap", first: 21, next: 30},
		{name: "overlap", first: 19, next: 30},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rng := utils.TestRng()
			committee, _, _ := genCommittee(rng, 3)
			first := types.EpochConv.Encode(types.NewEpoch(3, types.RoadRange{First: 10, Next: 20}, committee))
			second := types.EpochConv.Encode(types.NewEpoch(4, types.RoadRange{First: tc.first, Next: tc.next}, committee))
			_, err := persistedRegistryConv.DecodeReq(&pb.PersistedEpochRegistry{Live: []*pb.Epoch{first, second}})
			require.Error(t, err)
			_, err = persistedRegistryConv.DecodeReq(&pb.PersistedEpochRegistry{Live: []*pb.Epoch{first}, Pending: second})
			require.Error(t, err)
		})
	}
}

func testRegistryRoadRanges(committee *types.Committee) persistedRegistry {
	return persistedRegistry{
		live: map[types.EpochIndex]*types.Epoch{
			3: types.NewEpoch(3, types.RoadRange{First: 1_000_000, Next: 1_000_017}, committee),
			4: types.NewEpoch(4, types.RoadRange{First: 1_000_017, Next: 1_000_060}, committee),
		},
		pending: utils.Some(types.NewEpoch(5, types.RoadRange{First: 1_000_060, Next: 1_000_100}, committee)),
	}
}

func TestNewRegistryStateRejectsUnexpectedRoadRanges(t *testing.T) {
	for _, tc := range []struct {
		name    string
		pending bool
		shift   bool
	}{
		{name: "live origin", shift: true},
		{name: "live length"},
		{name: "pending length", pending: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rng := utils.TestRng()
			committee, _, _ := genCommittee(rng, 3)
			firstRoad := types.RoadIndex(7)
			idx := types.EpochIndex(3)
			if tc.pending {
				idx = 2
			}
			first := firstRoad + types.RoadIndex(idx)*epochLength
			record := types.EpochConv.Encode(types.NewEpoch(idx, types.RoadRange{First: first, Next: first + epochLength}, committee))
			snapshot := &pb.PersistedEpochRegistry{}
			if tc.pending {
				snapshot.Live = []*pb.Epoch{
					types.EpochConv.Encode(types.NewEpoch(0, types.RoadRange{First: firstRoad, Next: firstRoad + epochLength}, committee)),
					types.EpochConv.Encode(types.NewEpoch(1, types.RoadRange{First: firstRoad + epochLength, Next: firstRoad + 2*epochLength}, committee)),
				}
				snapshot.Pending = record
			} else {
				snapshot.Live = []*pb.Epoch{record}
			}
			persisted, err := persistedRegistryConv.DecodeReq(snapshot)
			require.NoError(t, err)
			_, err = newRegistryState(committee, firstRoad, time.Time{}, utils.Some(persisted))
			require.NoError(t, err)
			if tc.shift {
				record.FirstRoad = new(record.GetFirstRoad() + 1)
			}
			record.NextRoad = new(record.GetNextRoad() + 1)
			persisted, err = persistedRegistryConv.DecodeReq(snapshot)
			require.NoError(t, err)
			_, err = newRegistryState(committee, firstRoad, time.Time{}, utils.Some(persisted))
			require.Error(t, err)
		})
	}
}

func TestNewRegistryStateRejectsPendingWithoutLiveEpochs(t *testing.T) {
	rng := utils.TestRng()
	genesis, _, weights := genCommittee(rng, 3)
	committee, err := genesis.DeriveNext(weights, 2)
	require.NoError(t, err)
	persisted, err := persistedRegistryConv.DecodeReq(&pb.PersistedEpochRegistry{
		Pending: testEpoch(2, committee),
	})
	require.NoError(t, err)
	_, err = newRegistryState(genesis, 0, time.Time{}, utils.Some(persisted))
	require.Error(t, err)
}

func TestNewRegistryStateRejectsEmptyPersistedRegistry(t *testing.T) {
	rng := utils.TestRng()
	genesis, _, _ := genCommittee(rng, 3)
	persisted, err := persistedRegistryConv.DecodeReq(&pb.PersistedEpochRegistry{})
	require.NoError(t, err)
	_, err = newRegistryState(genesis, 0, time.Time{}, utils.Some(persisted))
	require.Error(t, err)
}
