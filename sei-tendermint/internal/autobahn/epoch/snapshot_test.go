package epoch

import (
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestPersistedRegistryConvRejectsEpochIndexGaps(t *testing.T) {
	rng := utils.TestRng()
	a := types.GenSecretKey(rng).Public()
	b := types.GenSecretKey(rng).Public()
	genesis := utils.OrPanic1(types.NewCommittee(map[types.PublicKey]uint64{a: 1}))
	c2 := utils.OrPanic1(genesis.DeriveNext(map[types.PublicKey]uint64{a: 1, b: 1}, 2))
	c3 := utils.OrPanic1(c2.DeriveNext(map[types.PublicKey]uint64{a: 1, b: 1}, 3))
	c4 := utils.OrPanic1(c3.DeriveNext(map[types.PublicKey]uint64{a: 1, b: 1}, 4))

	_, err := persistedRegistryConv.DecodeReq(&pb.PersistedEpochRegistry{
		Live: []*pb.Epoch{types.EpochConv.Encode(testEpoch(2, c2)), types.EpochConv.Encode(testEpoch(4, c4))},
	})
	require.Error(t, err)
	_, err = persistedRegistryConv.DecodeReq(&pb.PersistedEpochRegistry{
		Live:    []*pb.Epoch{types.EpochConv.Encode(testEpoch(2, c2))},
		Pending: types.EpochConv.Encode(testEpoch(4, c4)),
	})
	require.Error(t, err)
}

func TestNewRegistryStateRejectsBrokenDerivation(t *testing.T) {
	rng := utils.TestRng()
	a := types.GenSecretKey(rng).Public()
	b := types.GenSecretKey(rng).Public()
	genesis := utils.OrPanic1(types.NewCommittee(map[types.PublicKey]uint64{a: 1}))
	other := utils.OrPanic1(types.NewCommittee(map[types.PublicKey]uint64{a: 1, b: 1}))
	construct := func(persisted *persistedRegistry) error {
		_, err := newRegistryState(genesis, 0, time.Time{}, utils.Some(persisted))
		return err
	}

	require.Error(t, construct(utils.OrPanic1(newPersistedRegistry(
		[]*types.Epoch{testEpoch(0, genesis), testEpoch(1, genesis), testEpoch(2, other)}, utils.None[*types.Epoch](),
	))))
	require.Error(t, construct(utils.OrPanic1(newPersistedRegistry(
		[]*types.Epoch{testEpoch(0, genesis), testEpoch(1, genesis)}, utils.Some(testEpoch(2, other)),
	))))
}

func TestNewRegistryStateAcceptsDerivedChain(t *testing.T) {
	rng := utils.TestRng()
	a := types.GenSecretKey(rng).Public()
	b := types.GenSecretKey(rng).Public()
	genesis := utils.OrPanic1(types.NewCommittee(map[types.PublicKey]uint64{a: 1}))
	c2 := utils.OrPanic1(genesis.DeriveNext(map[types.PublicKey]uint64{a: 1, b: 1}, 2))
	c3 := utils.OrPanic1(c2.DeriveNext(map[types.PublicKey]uint64{a: 1, b: 1}, 3))
	construct := func(persisted *persistedRegistry) error {
		_, err := newRegistryState(genesis, 0, time.Time{}, utils.Some(persisted))
		return err
	}

	require.NoError(t, construct(utils.OrPanic1(newPersistedRegistry([]*types.Epoch{testEpoch(2, c2)}, utils.None[*types.Epoch]()))))
	require.NoError(t, construct(utils.OrPanic1(newPersistedRegistry(
		[]*types.Epoch{testEpoch(2, c2)}, utils.Some(testEpoch(3, c3)),
	))))
	require.NoError(t, construct(utils.OrPanic1(newPersistedRegistry(
		[]*types.Epoch{testEpoch(0, genesis), testEpoch(1, genesis)}, utils.Some(testEpoch(2, c2)),
	))))
}

func TestNewRegistryStateAllowsPrunedPredecessor(t *testing.T) {
	rng := utils.TestRng()
	a := types.GenSecretKey(rng).Public()
	b := types.GenSecretKey(rng).Public()
	genesis := utils.OrPanic1(types.NewCommittee(map[types.PublicKey]uint64{a: 1}))
	other := utils.OrPanic1(types.NewCommittee(map[types.PublicKey]uint64{b: 1}))
	persisted := utils.OrPanic1(newPersistedRegistry([]*types.Epoch{testEpoch(3, other)}, utils.None[*types.Epoch]()))
	_, err := newRegistryState(genesis, 0, time.Time{}, utils.Some(persisted))
	require.NoError(t, err)
}

func TestEpochConvRejectsFutureJoin(t *testing.T) {
	rng := utils.TestRng()
	a := types.GenSecretKey(rng).Public()
	b := types.GenSecretKey(rng).Public()
	genesis := utils.OrPanic1(types.NewCommittee(map[types.PublicKey]uint64{a: 1}))
	committee := utils.OrPanic1(genesis.DeriveNext(map[types.PublicKey]uint64{a: 1, b: 1}, 3))

	_, err := types.EpochConv.DecodeReq(types.EpochConv.Encode(testEpoch(2, committee)))
	require.Error(t, err)
}

func TestPersistedRegistryConvRoundTrip(t *testing.T) {
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
			committee, _, _ := genCommittee(rng, 3)
			var live []*types.Epoch
			pending := utils.None[*types.Epoch]()
			for idx := tc.prune; idx <= tc.last; idx++ {
				live = append(live, testEpoch(idx, committee))
			}
			if tc.pending {
				pending = utils.Some(testEpoch(tc.last+1, committee))
			}
			persisted := utils.OrPanic1(newPersistedRegistry(live, pending))
			require.NoError(t, persistedRegistryConv.Test(persisted))
		})
	}
}

func testEpoch(idx types.EpochIndex, committee *types.Committee) *types.Epoch {
	first := types.RoadIndex(idx) * epochLength
	return types.NewEpoch(idx, types.RoadRange{First: first, Next: first + epochLength}, committee)
}

func TestPersistedRegistryConvRoundTripArbitraryRoadRanges(t *testing.T) {
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

func testRegistryRoadRanges(committee *types.Committee) *persistedRegistry {
	return utils.OrPanic1(newPersistedRegistry(
		[]*types.Epoch{
			types.NewEpoch(3, types.RoadRange{First: 1_000_000, Next: 1_000_017}, committee),
			types.NewEpoch(4, types.RoadRange{First: 1_000_017, Next: 1_000_060}, committee),
		},
		utils.Some(types.NewEpoch(5, types.RoadRange{First: 1_000_060, Next: 1_000_100}, committee)),
	))
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
			construct := func(roads types.RoadRange) error {
				epoch := types.NewEpoch(idx, roads, committee)
				var live []*types.Epoch
				pending := utils.None[*types.Epoch]()
				if tc.pending {
					live = []*types.Epoch{
						types.NewEpoch(0, types.RoadRange{First: firstRoad, Next: firstRoad + epochLength}, committee),
						types.NewEpoch(1, types.RoadRange{First: firstRoad + epochLength, Next: firstRoad + 2*epochLength}, committee),
					}
					pending = utils.Some(epoch)
				} else {
					live = []*types.Epoch{epoch}
				}
				persisted := utils.OrPanic1(newPersistedRegistry(live, pending))
				_, err := newRegistryState(committee, firstRoad, time.Time{}, utils.Some(persisted))
				return err
			}
			first := firstRoad + types.RoadIndex(idx)*epochLength
			roads := types.RoadRange{First: first, Next: first + epochLength}
			require.NoError(t, construct(roads))
			if tc.shift {
				roads.First++
			}
			roads.Next++
			require.Error(t, construct(roads))
		})
	}
}

func TestNewPersistedRegistryRejectsInvalidEpochOrder(t *testing.T) {
	for _, tc := range []struct {
		name    string
		live    []types.EpochIndex
		pending utils.Option[types.EpochIndex]
	}{
		{name: "empty"},
		{name: "pending without live", pending: utils.Some(types.EpochIndex(2))},
		{name: "duplicate", live: []types.EpochIndex{2, 2}},
		{name: "decreasing", live: []types.EpochIndex{3, 2}},
		{name: "gap", live: []types.EpochIndex{2, 4}},
		{name: "pending duplicate", live: []types.EpochIndex{2}, pending: utils.Some(types.EpochIndex(2))},
		{name: "pending before live", live: []types.EpochIndex{2}, pending: utils.Some(types.EpochIndex(1))},
		{name: "pending gap", live: []types.EpochIndex{2}, pending: utils.Some(types.EpochIndex(4))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rng := utils.TestRng()
			committee, _, _ := genCommittee(rng, 3)
			var live []*types.Epoch
			for _, idx := range tc.live {
				live = append(live, testEpoch(idx, committee))
			}
			pending := utils.None[*types.Epoch]()
			if idx, ok := tc.pending.Get(); ok {
				pending = utils.Some(testEpoch(idx, committee))
			}
			_, err := newPersistedRegistry(live, pending)
			require.Error(t, err)
		})
	}
}

func TestNewRegistryStateRejectsEmptyPersistedRegistry(t *testing.T) {
	rng := utils.TestRng()
	committee, _, _ := genCommittee(rng, 3)
	persisted := new(persistedRegistry)
	_, err := newRegistryState(committee, 0, time.Time{}, utils.Some(persisted))
	require.Error(t, err)
}

func TestNewRegistryStateRetainsLiveEpochs(t *testing.T) {
	for _, mode := range []string{"genesis", "one live", "one live and pending"} {
		t.Run(mode, func(t *testing.T) {
			rng := utils.TestRng()
			committee, _, _ := genCommittee(rng, 3)
			persisted := utils.None[*persistedRegistry]()
			want := 2
			if mode != "genesis" {
				want = 1
				pending := utils.None[*types.Epoch]()
				if mode == "one live and pending" {
					pending = utils.Some(testEpoch(4, committee))
				}
				persisted = utils.Some(utils.OrPanic1(newPersistedRegistry([]*types.Epoch{testEpoch(3, committee)}, pending)))
			}
			state, err := newRegistryState(committee, 0, time.Time{}, persisted)
			require.NoError(t, err)
			require.Equal(t, want, len(state.live))
			require.Equal(t, types.EpochIndex(want), state.liveRange.Next-state.liveRange.First)
		})
	}
}

func TestPersistedRegistryConvRejectsEmptyLive(t *testing.T) {
	for _, withPending := range []bool{false, true} {
		name := "empty"
		if withPending {
			name = "pending only"
		}
		t.Run(name, func(t *testing.T) {
			rng := utils.TestRng()
			committee, _, _ := genCommittee(rng, 3)
			snapshot := &pb.PersistedEpochRegistry{}
			if withPending {
				snapshot.Pending = types.EpochConv.Encode(testEpoch(2, committee))
			}
			_, err := persistedRegistryConv.DecodeReq(snapshot)
			require.Error(t, err)
		})
	}
}
