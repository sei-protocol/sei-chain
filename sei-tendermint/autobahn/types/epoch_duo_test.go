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
	prev = types.NewEpoch(0, types.RoadRange{First: 0, Next: 100}, committee)
	current = types.NewEpoch(1, types.RoadRange{First: 100, Next: 200}, committee)
	return prev, current
}

func TestNewEpochDuo_PanicsOnNonContiguousIndex(t *testing.T) {
	prev, _ := testDuoEpochs(t)
	rng := utils.TestRng()
	weights := map[types.PublicKey]uint64{types.GenSecretKey(rng).Public(): 1}
	committee := utils.OrPanic1(types.NewCommittee(weights))
	// Roads abut, but index jumps 0 → 2.
	current := types.NewEpoch(2, types.RoadRange{First: 100, Next: 200}, committee)
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
	prev := types.NewEpoch(0, types.OpenRoadRange(), committee)
	current := types.NewEpoch(1, types.RoadRange{First: 100, Next: 200}, committee)
	defer func() {
		if recover() == nil {
			t.Fatal("NewEpochDuo with non-abutting roads should panic")
		}
	}()
	_ = types.NewEpochDuo(current, utils.Some(prev))
}

func TestNewEpochDuo_PanicsOnPrevCurrentMismatch(t *testing.T) {
	prev, current := testDuoEpochs(t)
	t.Run("prev_absent_with_current_gt_0", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatal("expected panic")
			}
		}()
		_ = types.NewEpochDuo(current, utils.None[*types.Epoch]())
	})
	t.Run("prev_present_with_epoch_0", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatal("expected panic")
			}
		}()
		_ = types.NewEpochDuo(prev, utils.Some(prev))
	})
}

func TestEpochForRoad(t *testing.T) {
	prev, current := testDuoEpochs(t)
	rng := utils.TestRng()
	weights := map[types.PublicKey]uint64{types.GenSecretKey(rng).Public(): 1}
	committee := utils.OrPanic1(types.NewCommittee(weights))
	// Prev absent only for epoch 0.
	ep0 := types.NewEpoch(0, types.RoadRange{First: 0, Next: 100}, committee)

	currentOnly := types.NewEpochDuo(ep0, utils.None[*types.Epoch]())
	withPrev := types.NewEpochDuo(current, utils.Some(prev))

	for _, tc := range []struct {
		name string
		w    types.EpochDuo
		road types.RoadIndex
		want *types.Epoch
		err  error
	}{
		{"ep0_hit", currentOnly, 50, ep0, nil},
		{"ep0_after", currentOnly, 100, nil, types.ErrRoadAfterWindow},
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

func TestEpochDuo_RoadStatus(t *testing.T) {
	prev, current := testDuoEpochs(t)
	withPrev := types.NewEpochDuo(current, utils.Some(prev))
	ep0Only := types.NewEpochDuo(prev, utils.None[*types.Epoch]())

	for _, tc := range []struct {
		name string
		w    types.EpochDuo
		road types.RoadIndex
		cur  types.RoadStatus
		duo  types.RoadStatus
	}{
		{"prev_road", withPrev, 50, types.RoadStale, types.RoadReady},
		{"current_road", withPrev, 150, types.RoadReady, types.RoadReady},
		{"future_road", withPrev, 200, types.RoadFuture, types.RoadFuture},
		{"ep0_ready", ep0Only, 50, types.RoadReady, types.RoadReady},
		{"ep0_future", ep0Only, 100, types.RoadFuture, types.RoadFuture},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.w.RoadStatusCurrent(tc.road); got != tc.cur {
				t.Fatalf("RoadStatusCurrent(%d) = %v, want %v", tc.road, got, tc.cur)
			}
			if got := tc.w.RoadStatusDuo(tc.road); got != tc.duo {
				t.Fatalf("RoadStatusDuo(%d) = %v, want %v", tc.road, got, tc.duo)
			}
		})
	}
}
