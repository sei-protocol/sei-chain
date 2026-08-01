package types_test

import (
	"errors"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

func testDuoEpochs(t *testing.T, rng utils.Rng) (prev, current *types.Epoch) {
	t.Helper()
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
	rng := utils.TestRng()
	prev, _ := testDuoEpochs(t, rng)
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
	rng := utils.TestRng()
	prev, current := testDuoEpochs(t, rng)
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

func TestByRoad(t *testing.T) {
	rng := utils.TestRng()
	prev, current := testDuoEpochs(t, rng)
	weights := map[types.PublicKey]uint64{types.GenSecretKey(rng).Public(): 1}
	committee := utils.OrPanic1(types.NewCommittee(weights))
	// Prev absent only for epoch 0.
	ep0 := types.NewEpoch(0, types.RoadRange{First: 0, Next: 100}, committee)
	// Contiguous duo starting past road 0 so a behind-window road is possible.
	latePrev := types.NewEpoch(0, types.RoadRange{First: 100, Next: 200}, committee)
	lateCurrent := types.NewEpoch(1, types.RoadRange{First: 200, Next: 300}, committee)

	currentOnly := types.NewEpochDuo(ep0, utils.None[*types.Epoch]())
	withPrev := types.NewEpochDuo(current, utils.Some(prev))
	lateDuo := types.NewEpochDuo(lateCurrent, utils.Some(latePrev))

	for _, tc := range []struct {
		name    string
		w       types.EpochDuo
		road    types.RoadIndex
		want    *types.Epoch
		wantErr error // nil = success; errors.Is match when non-nil
		anyErr  bool  // future roads: non-nil err, not a typed sentinel
	}{
		{"ep0_hit", currentOnly, 50, ep0, nil, false},
		{"ep0_after", currentOnly, 100, nil, nil, true},
		{"prev_hit", withPrev, 50, prev, nil, false},
		{"duo_current_hit", withPrev, 150, current, nil, false},
		{"duo_after", withPrev, 200, nil, nil, true},
		{"behind", lateDuo, 50, nil, types.ErrPruned, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ep, err := tc.w.ByRoad(tc.road)
			if tc.anyErr {
				if err == nil {
					t.Fatalf("ByRoad(%d) = nil err, want non-nil", tc.road)
				}
				return
			}
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("ByRoad(%d) = %v, want %v", tc.road, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ByRoad(%d): %v", tc.road, err)
			}
			if ep != tc.want {
				t.Fatalf("ByRoad(%d) = %v, want %v", tc.road, ep, tc.want)
			}
		})
	}
}
