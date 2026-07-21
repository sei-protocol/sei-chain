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

func TestEpochForRoad_HitsCurrentEpoch(t *testing.T) {
	_, current := testDuoEpochs(t)
	w := types.EpochDuo{Current: current}
	ep, err := w.EpochForRoad(150)
	if err != nil {
		t.Fatalf("EpochForRoad(150): %v", err)
	}
	if ep != current {
		t.Fatalf("got %v, want current", ep)
	}
}

func TestEpochForRoad_HitsPrevEpoch(t *testing.T) {
	prev, current := testDuoEpochs(t)
	w := types.EpochDuo{Prev: utils.Some(prev), Current: current}
	ep, err := w.EpochForRoad(50)
	if err != nil {
		t.Fatalf("EpochForRoad(50): %v", err)
	}
	if ep != prev {
		t.Fatalf("got %v, want prev", ep)
	}
}

func TestEpochForRoad_OutsideWindowReturnsError(t *testing.T) {
	_, current := testDuoEpochs(t)
	w := types.EpochDuo{Current: current}
	_, err := w.EpochForRoad(999)
	if !errors.Is(err, types.ErrRoadAfterWindow) {
		t.Fatalf("EpochForRoad(999) = %v, want ErrRoadAfterWindow", err)
	}
	_, err = w.EpochForRoad(50)
	if !errors.Is(err, types.ErrRoadBeforeWindow) {
		t.Fatalf("EpochForRoad(50) current-only = %v, want ErrRoadBeforeWindow", err)
	}
}

func TestEpochForRoad_OpenRangePrevDoesNotMaskCurrent(t *testing.T) {
	rng := utils.TestRng()
	weights := map[types.PublicKey]uint64{types.GenSecretKey(rng).Public(): 1}
	committee := utils.OrPanic1(types.NewCommittee(weights))
	openEpoch := types.NewEpoch(0, types.OpenRoadRange(), utils.GenTimestamp(rng), committee, 1)
	current := types.NewEpoch(1, types.RoadRange{First: 100, Next: 200}, utils.GenTimestamp(rng), committee, 101)
	w := types.EpochDuo{Prev: utils.Some(openEpoch), Current: current}
	ep, err := w.EpochForRoad(150)
	if err != nil {
		t.Fatalf("EpochForRoad(150): %v", err)
	}
	if ep.EpochIndex() != current.EpochIndex() {
		t.Fatalf("got epoch %d (Prev with OpenRoadRange masked Current), want current (%d)",
			ep.EpochIndex(), current.EpochIndex())
	}
}

func TestEpochForRoad_AbsentPrevSkipped(t *testing.T) {
	_, current := testDuoEpochs(t)
	w := types.EpochDuo{Current: current}
	_, err := w.EpochForRoad(50)
	if !errors.Is(err, types.ErrRoadBeforeWindow) {
		t.Fatalf("EpochForRoad(50) with absent Prev = %v, want ErrRoadBeforeWindow", err)
	}
}

func TestEpochForRoad_BeforeAndAfterWithPrev(t *testing.T) {
	prev, current := testDuoEpochs(t)
	w := types.EpochDuo{Prev: utils.Some(prev), Current: current}
	if _, err := w.EpochForRoad(50); err != nil {
		t.Fatalf("EpochForRoad(50) in prev: %v", err)
	}
	if _, err := w.EpochForRoad(150); err != nil {
		t.Fatalf("EpochForRoad(150) in current: %v", err)
	}
	_, err := w.EpochForRoad(200)
	if !errors.Is(err, types.ErrRoadAfterWindow) {
		t.Fatalf("EpochForRoad(200) = %v, want ErrRoadAfterWindow", err)
	}
}

func TestWindowFirst_WithPrev(t *testing.T) {
	prev, current := testDuoEpochs(t)
	w := types.EpochDuo{Prev: utils.Some(prev), Current: current}
	if got, want := w.WindowFirst(), prev.RoadRange().First; got != want {
		t.Fatalf("WindowFirst() = %d, want %d", got, want)
	}
}

func TestWindowFirst_CurrentOnly(t *testing.T) {
	_, current := testDuoEpochs(t)
	w := types.EpochDuo{Current: current}
	if got, want := w.WindowFirst(), current.RoadRange().First; got != want {
		t.Fatalf("WindowFirst() = %d, want %d", got, want)
	}
}

func TestEpochOptForRoad(t *testing.T) {
	prev, current := testDuoEpochs(t)
	w := types.EpochDuo{Prev: utils.Some(prev), Current: current}
	if ep, ok := w.EpochOptForRoad(50).Get(); !ok || ep != prev {
		t.Fatalf("EpochOptForRoad(50) = %v, want prev", ep)
	}
	if w.EpochOptForRoad(999).IsPresent() {
		t.Fatal("EpochOptForRoad(999) should be None")
	}
}

func TestCurrentForRoad(t *testing.T) {
	prev, current := testDuoEpochs(t)
	w := types.EpochDuo{Prev: utils.Some(prev), Current: current}
	if ep, ok := w.CurrentForRoad(150).Get(); !ok || ep != current {
		t.Fatalf("CurrentForRoad(150) = %v, want current", ep)
	}
	if w.CurrentForRoad(50).IsPresent() {
		t.Fatal("CurrentForRoad(50) must not admit Prev")
	}
}
