package types_test

import (
	"errors"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

func testDuoEpochs(t *testing.T) (prev, current *types.Epoch) {
	t.Helper()
	rng := utils.TestRng()
	weights := map[types.PublicKey]uint64{}
	for range 3 {
		weights[types.GenSecretKey(rng).Public()] = 1
	}
	committee := utils.OrPanic1(types.NewCommittee(weights))
	prev = types.NewEpoch(0, types.RoadRange{First: 0, Next: 100}, utils.GenTimestamp(rng), committee, 1)
	current = types.NewEpoch(1, types.RoadRange{First: 100, Next: 200}, utils.GenTimestamp(rng), committee, 101)
	return prev, current
}

func TestNewEpochDuo_PanicsOnNonContiguousIndex(t *testing.T) {
	prev, _ := testDuoEpochs(t)
	rng := utils.TestRng()
	weights := map[types.PublicKey]uint64{types.GenSecretKey(rng).Public(): 1}
	committee := utils.OrPanic1(types.NewCommittee(weights))
	// Roads abut, but index jumps 0 → 2.
	current := types.NewEpoch(2, types.RoadRange{First: 100, Next: 200}, utils.GenTimestamp(rng), committee, 101)
	defer func() {
		if recover() == nil {
			t.Fatal("NewEpochDuo with non-contiguous indices should panic")
		}
	}()
	_ = types.NewEpochDuo(current, utils.Some(prev))
}

func TestNewEpochDuo_PanicsOnNonContiguousRoads(t *testing.T) {
	rng := utils.TestRng()
	weights := map[types.PublicKey]uint64{types.GenSecretKey(rng).Public(): 1}
	committee := utils.OrPanic1(types.NewCommittee(weights))
	prev := types.NewEpoch(0, types.OpenRoadRange(), utils.GenTimestamp(rng), committee, 1)
	current := types.NewEpoch(1, types.RoadRange{First: 100, Next: 200}, utils.GenTimestamp(rng), committee, 101)
	defer func() {
		if recover() == nil {
			t.Fatal("NewEpochDuo with non-abutting roads should panic")
		}
	}()
	_ = types.NewEpochDuo(current, utils.Some(prev))
}

func TestEpochForRoad(t *testing.T) {
	prev, current := testDuoEpochs(t)

	currentOnly := types.NewEpochDuo(current, utils.None[*types.Epoch]())
	withPrev := types.NewEpochDuo(current, utils.Some(prev))

	for _, tc := range []struct {
		name string
		w    types.EpochDuo
		road types.RoadIndex
		want *types.Epoch
		err  error
	}{
		{"current_hit", currentOnly, 150, current, nil},
		{"current_before", currentOnly, 50, nil, types.ErrRoadBeforeWindow},
		{"current_after", currentOnly, 999, nil, types.ErrRoadAfterWindow},
		{"prev_hit", withPrev, 50, prev, nil},
		{"duo_current_hit", withPrev, 150, current, nil},
		{"duo_after", withPrev, 200, nil, types.ErrRoadAfterWindow},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ep, err := tc.w.EpochForRoad(tc.road)
			if tc.err != nil {
				if !errors.Is(err, tc.err) {
					t.Fatalf("EpochForRoad(%d) = %v, want %v", tc.road, err, tc.err)
				}
				return
			}
			if err != nil {
				t.Fatalf("EpochForRoad(%d): %v", tc.road, err)
			}
			if ep != tc.want {
				t.Fatalf("EpochForRoad(%d) = %v, want %v", tc.road, ep, tc.want)
			}
		})
	}
}
