package epoch

import (
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/stretchr/testify/require"
)

func makeRegistry(t *testing.T) (*Registry, *types.Committee) {
	t.Helper()
	rng := utils.TestRng()
	committee := utils.OrPanic1(types.NewCommittee(map[types.PublicKey]uint64{
		types.GenSecretKey(rng).Public(): 1,
		types.GenSecretKey(rng).Public(): 1,
		types.GenSecretKey(rng).Public(): 1,
	}))
	r := utils.OrPanic1(NewRegistry(committee, 0, time.Time{}))
	return r, committee
}

func midRoad(idx types.EpochIndex) types.RoadIndex {
	return FirstRoad(idx) + EpochLength/2
}

func TestNewRegistry_GenesisEpochBoundedRange(t *testing.T) {
	r, _ := makeRegistry(t)
	ep, err := r.EpochAt(0)
	if err != nil {
		t.Fatalf("EpochAt(0): %v", err)
	}
	rng := ep.RoadRange()
	if rng.First != 0 || rng.Next != FirstRoad(1) {
		t.Fatalf("genesis RoadRange = {%d, %d}, want {0, %d}", rng.First, rng.Next, FirstRoad(1))
	}
}

func TestEpochAt_GenesisEpoch(t *testing.T) {
	r, _ := makeRegistry(t)
	ep, err := r.EpochAt(0)
	if err != nil {
		t.Fatalf("EpochAt(0) error: %v", err)
	}
	if ep.EpochIndex() != 0 {
		t.Fatalf("EpochAt(0).EpochIndex() = %d, want 0", ep.EpochIndex())
	}
}

func TestEpochAt_WithinGenesisEpoch(t *testing.T) {
	r, _ := makeRegistry(t)
	ep, err := r.EpochAt(LastRoad(0))
	if err != nil {
		t.Fatalf("EpochAt(LastRoad(0)) error: %v", err)
	}
	if ep.EpochIndex() != 0 {
		t.Fatalf("EpochAt(LastRoad(0)).EpochIndex() = %d, want 0", ep.EpochIndex())
	}
}

func TestEpochAt_ErrorIfNotRegistered(t *testing.T) {
	r, _ := makeRegistry(t)
	_, err := r.EpochAt(FirstRoad(2))
	if err == nil {
		t.Fatal("EpochAt(FirstRoad(2)) expected error for unregistered epoch, got nil")
	}
}

func TestEpochAt_FoundAfterAdvanceIfNeeded(t *testing.T) {
	r, _ := makeRegistry(t)
	// NewRegistry already seeds {0,1}. Only the last road of epoch 0 seeds epoch 2.
	r.AdvanceIfNeeded(0)
	if _, err := r.EpochAt(FirstRoad(2)); err == nil {
		t.Fatal("AdvanceIfNeeded(0) must not seed epoch 2")
	}
	r.AdvanceIfNeeded(LastRoad(0))
	ep, err := r.EpochAt(FirstRoad(2))
	if err != nil {
		t.Fatalf("EpochAt(FirstRoad(2)) after last road of epoch 0: %v", err)
	}
	if ep.EpochIndex() != 2 {
		t.Fatalf("EpochAt(FirstRoad(2)).EpochIndex() = %d, want 2", ep.EpochIndex())
	}
}

func TestSetupInitialDuo_EmptyNoneNoneSeedsGenesisNeighbor(t *testing.T) {
	r, _ := makeRegistry(t)
	r.SetupInitialDuo(utils.None[types.RoadIndex](), utils.None[types.RoadRange]())
	for _, idx := range []types.EpochIndex{0, 1} {
		if _, err := r.EpochAt(FirstRoad(idx)); err != nil {
			t.Fatalf("EpochAt(epoch %d) after empty seeding: %v", idx, err)
		}
	}
	if _, err := r.EpochAt(FirstRoad(2)); err == nil {
		t.Fatal("EpochAt(epoch 2) should not be present from empty None/None seeding")
	}
}

func TestSetupInitialDuo_EmptyRangePanics(t *testing.T) {
	r, _ := makeRegistry(t)
	defer func() {
		if recover() == nil {
			t.Fatal("empty CommitQC range must panic")
		}
	}()
	r.SetupInitialDuo(utils.None[types.RoadIndex](), utils.Some(types.RoadRange{First: 5, Next: 5}))
}

func TestSetupInitialDuo_CommitQCOnly(t *testing.T) {
	r, _ := makeRegistry(t)
	// Single tip mid-5 + tipcut EnsureDuoAt → {4,5}.
	tip := midRoad(5)
	r.SetupInitialDuo(utils.None[types.RoadIndex](), utils.Some(types.RoadRange{First: tip, Next: tip + 1}))
	for _, idx := range []types.EpochIndex{4, 5} {
		if _, err := r.EpochAt(FirstRoad(idx)); err != nil {
			t.Fatalf("EpochAt(epoch %d) after CommitQC seeding: %v", idx, err)
		}
	}
	if _, err := r.EpochAt(FirstRoad(6)); err == nil {
		t.Fatal("EpochAt(epoch 6) should not be present from CommitQC alone")
	}
}

func TestSetupInitialDuo_CommitQCClosingSeedsNext(t *testing.T) {
	r, _ := makeRegistry(t)
	// Closing tip: {5} + tipcut FirstRoad(6) → {5,6}.
	tip := LastRoad(5)
	r.SetupInitialDuo(utils.None[types.RoadIndex](), utils.Some(types.RoadRange{First: tip, Next: tip + 1}))
	for _, idx := range []types.EpochIndex{5, 6} {
		if _, err := r.EpochAt(FirstRoad(idx)); err != nil {
			t.Fatalf("EpochAt(epoch %d) after closing CommitQC: %v", idx, err)
		}
	}
	if _, err := r.DuoAt(FirstRoad(6)); err != nil {
		t.Fatalf("DuoAt(FirstRoad(6)) after closing CommitQC: %v", err)
	}
	if _, err := r.EpochAt(FirstRoad(7)); err == nil {
		t.Fatal("EpochAt(epoch 7) should not be present from CommitQC closing alone")
	}
}

func TestSetupInitialDuo_CommitSpanFromFirst(t *testing.T) {
	r, _ := makeRegistry(t)
	// Span mid-2..mid-5 → seed epochs 2..5 only (not open from 0).
	r.SetupInitialDuo(utils.None[types.RoadIndex](), utils.Some(types.RoadRange{
		First: midRoad(2),
		Next:  midRoad(5) + 1,
	}))
	for _, idx := range []types.EpochIndex{2, 3, 4, 5} {
		if _, err := r.EpochAt(FirstRoad(idx)); err != nil {
			t.Fatalf("EpochAt(epoch %d) after commit span seeding: %v", idx, err)
		}
	}
	if _, err := r.EpochAt(FirstRoad(1)); err == nil {
		t.Fatal("EpochAt(epoch 1) should not be present when span.First is in epoch 2")
	}
	if _, err := r.EpochAt(FirstRoad(6)); err == nil {
		t.Fatal("EpochAt(epoch 6) should not be present from mid-epoch commit tipcut")
	}
}

func TestSetupInitialDuo_ExecutionExtendsForward(t *testing.T) {
	r, _ := makeRegistry(t)
	tip := midRoad(5)
	// Tip mid-5 + CommitQC tipcut → {4,5}; execution tipcut mid-5 → lastDone mid-5-1 → epoch 6.
	r.SetupInitialDuo(utils.Some(tip), utils.Some(types.RoadRange{First: tip, Next: tip + 1}))
	for _, idx := range []types.EpochIndex{4, 5, 6} {
		if _, err := r.EpochAt(FirstRoad(idx)); err != nil {
			t.Fatalf("EpochAt(epoch %d) after execution extend: %v", idx, err)
		}
	}
	if _, err := r.EpochAt(FirstRoad(7)); err == nil {
		t.Fatal("EpochAt(epoch 7) should not be present after mid-epoch execution")
	}
}

func TestSetupInitialDuo_ExecutionClosingAddsNextNext(t *testing.T) {
	r, _ := makeRegistry(t)
	tip := LastRoad(5)
	// Closing road fully executed → execution tipcut tip+1 → AdvanceIfNeeded → 6 and 7.
	r.SetupInitialDuo(utils.Some(tip+1), utils.Some(types.RoadRange{First: tip, Next: tip + 1}))
	for _, idx := range []types.EpochIndex{5, 6, 7} {
		if _, err := r.EpochAt(FirstRoad(idx)); err != nil {
			t.Fatalf("EpochAt(epoch %d) after closing execution: %v", idx, err)
		}
	}
	if _, err := r.EpochAt(FirstRoad(8)); err == nil {
		t.Fatal("EpochAt(epoch 8) should not be present from closing-road execution")
	}
}

// TestSetupInitialDuo_MidClosingRoadDoesNotSeedNextNext: execution tipcut still
// on LastRoad(E) (mid-range) must seed E+1 only — tipcut not past the road, so
// lastDone is not LastRoad(E). Mid-road crash must not unlock E+2.
func TestSetupInitialDuo_MidClosingRoadDoesNotSeedNextNext(t *testing.T) {
	r, _ := makeRegistry(t)
	tip := LastRoad(5)
	r.SetupInitialDuo(utils.Some(tip), utils.Some(types.RoadRange{First: tip, Next: tip + 1}))
	for _, idx := range []types.EpochIndex{5, 6} {
		if _, err := r.EpochAt(FirstRoad(idx)); err != nil {
			t.Fatalf("EpochAt(epoch %d) after mid-closing execution: %v", idx, err)
		}
	}
	if _, err := r.EpochAt(FirstRoad(7)); err == nil {
		t.Fatal("EpochAt(epoch 7) must not be present when LastRoad(E) is only mid-executed")
	}
}

// TestSetupInitialDuo_ExecutedSameEpochAsTipSeedsNext: CommitQC tip in epoch N;
// LastExecuted also in N ⇒ N-1 done ⇒ EnsureExecTipcut seeds N+1.
func TestSetupInitialDuo_ExecutedSameEpochAsTipSeedsNext(t *testing.T) {
	r, _ := makeRegistry(t) // {0}
	n := types.EpochIndex(1)
	tip := FirstRoad(n)
	r.SetupInitialDuo(utils.Some(tip), utils.Some(types.RoadRange{First: tip, Next: tip + 1}))
	if _, err := r.EpochAt(FirstRoad(n + 1)); err != nil {
		t.Fatalf("EpochAt(N+1) when executed in tip epoch N: %v", err)
	}
	if _, err := r.EpochAt(FirstRoad(n + 2)); err == nil {
		t.Fatal("EpochAt(N+2) should not be present from mid-N execution")
	}
}

func TestSetupInitialDuo_ExecutionPastCommitQCIgnored(t *testing.T) {
	r, _ := makeRegistry(t)
	tip := midRoad(3)
	// Commit tip mid-3; execution mid-5 past tipcut → warn, ignore.
	r.SetupInitialDuo(utils.Some(midRoad(5)), utils.Some(types.RoadRange{First: tip, Next: tip + 1}))
	for _, idx := range []types.EpochIndex{2, 3} {
		if _, err := r.EpochAt(FirstRoad(idx)); err != nil {
			t.Fatalf("EpochAt(epoch %d) from CommitQC: %v", idx, err)
		}
	}
	if _, err := r.EpochAt(FirstRoad(4)); err == nil {
		t.Fatal("EpochAt(epoch 4) should not be present when execution past CommitQC is ignored")
	}
}

func TestSetupInitialDuo_ExecutionDoesNotShrinkCommitWindow(t *testing.T) {
	r, _ := makeRegistry(t)
	// Commit span mid-2..mid-5; lagging execution mid-3 must not drop 4/5.
	r.SetupInitialDuo(utils.Some(midRoad(3)), utils.Some(types.RoadRange{
		First: midRoad(2),
		Next:  midRoad(5) + 1,
	}))
	for _, idx := range []types.EpochIndex{2, 3, 4, 5} {
		if _, err := r.EpochAt(FirstRoad(idx)); err != nil {
			t.Fatalf("EpochAt(epoch %d) must remain after lagging execution: %v", idx, err)
		}
	}
	// Lagging mid-3 EnsureExecTipcut → epoch 4 only; not 6.
	if _, err := r.EpochAt(FirstRoad(6)); err == nil {
		t.Fatal("EpochAt(epoch 6) should not be present when execution lags in epoch 3")
	}
}

func TestDuoAt_GenesisEpoch(t *testing.T) {
	r, _ := makeRegistry(t)
	duo, err := r.DuoAt(0)
	if err != nil {
		t.Fatalf("DuoAt(0) error: %v", err)
	}
	if duo.Prev.IsPresent() {
		t.Fatalf("DuoAt(0).Prev = %v, want absent for epoch 0", duo.Prev)
	}
	if duo.Current == nil || duo.Current.EpochIndex() != 0 {
		t.Fatalf("DuoAt(0).Current.EpochIndex() wrong, want 0")
	}
}

func TestDuoAt_MiddleEpoch(t *testing.T) {
	r, _ := makeRegistry(t)
	tip := midRoad(2)
	r.SetupInitialDuo(utils.None[types.RoadIndex](), utils.Some(types.RoadRange{First: tip, Next: tip + 1}))
	duo, err := r.DuoAt(FirstRoad(2))
	if err != nil {
		t.Fatalf("DuoAt(epoch 2) error: %v", err)
	}
	prev, ok := duo.Prev.Get()
	if !ok || prev.EpochIndex() != 1 {
		t.Fatalf("DuoAt(epoch 2).Prev.EpochIndex() wrong, want 1")
	}
	if duo.Current == nil || duo.Current.EpochIndex() != 2 {
		t.Fatalf("DuoAt(epoch 2).Current.EpochIndex() wrong, want 2")
	}
}

func TestDuoAt_ErrorWhenCurrentMissing(t *testing.T) {
	committee := utils.OrPanic1(types.NewCommittee(map[types.PublicKey]uint64{
		types.GenSecretKey(utils.TestRng()).Public(): 1,
	}))
	ep := types.NewEpoch(0, types.RoadRange{First: 0, Next: FirstRoad(1)}, time.Time{}, committee, 0)
	bare := &Registry{
		state: utils.NewRWMutex(&registryState{
			m:      map[types.EpochIndex]*types.Epoch{0: ep},
			latest: 0,
		}),
		highestEpoch: utils.NewAtomicSend(types.EpochIndex(0)),
	}
	_, err := bare.DuoAt(FirstRoad(1))
	if err == nil {
		t.Fatal("DuoAt(FirstRoad(1)) expected error when Current epoch not registered, got nil")
	}
}

func TestWaitForDuo_FastPathAndWait(t *testing.T) {
	r, _ := makeRegistry(t)
	// NewRegistry seeds {0,1}; DuoAt(0) is immediate.
	duo, err := r.WaitForDuo(t.Context(), 0)
	require.NoError(t, err)
	require.Equal(t, types.EpochIndex(0), duo.Current.EpochIndex())

	// Tipcut into epoch 2 needs epoch 2 registered (seeded by executing epoch 0).
	tip := FirstRoad(2)
	_, err = r.DuoAt(tip)
	require.Error(t, err)

	type result struct {
		duo types.EpochDuo
		err error
	}
	done := make(chan result, 1)
	go func() {
		duo, err := r.WaitForDuo(t.Context(), tip)
		done <- result{duo, err}
	}()
	r.AdvanceIfNeeded(LastRoad(0)) // last road of epoch 0 seeds epoch 2
	got := <-done
	require.NoError(t, got.err)
	require.Equal(t, types.EpochIndex(2), got.duo.Current.EpochIndex())
}
