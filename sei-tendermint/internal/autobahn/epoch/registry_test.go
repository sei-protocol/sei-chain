package epoch

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
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

func TestRegistry_EpochByIndex_UnknownReturnsNotFound(t *testing.T) {
	r, _ := makeRegistry(t)
	if _, ok := r.EpochByIndex(99); ok {
		t.Fatal("EpochByIndex(99) returned ok, want not found")
	}
}

func TestRegistry_EpochByIndex_GenesisFound(t *testing.T) {
	r, _ := makeRegistry(t)
	ep, ok := r.EpochByIndex(0)
	if !ok {
		t.Fatal("EpochByIndex(0) not found")
	}
	if ep.EpochIndex() != 0 {
		t.Fatalf("EpochIndex() = %d, want 0", ep.EpochIndex())
	}
}

func TestNewRegistry_GenesisEpochBoundedRange(t *testing.T) {
	r, _ := makeRegistry(t)
	ep0, err := r.EpochAt(0)
	if err != nil {
		t.Fatalf("EpochAt(0): %v", err)
	}
	rng0 := ep0.RoadRange()
	if rng0.First != 0 || rng0.Next != FirstRoad(1) {
		t.Fatalf("epoch 0 RoadRange = {%d, %d}, want {0, %d}", rng0.First, rng0.Next, FirstRoad(1))
	}
	ep1, err := r.EpochAt(FirstRoad(1))
	if err != nil {
		t.Fatalf("EpochAt(FirstRoad(1)): %v", err)
	}
	rng1 := ep1.RoadRange()
	if rng1.First != FirstRoad(1) || rng1.Next != FirstRoad(2) {
		t.Fatalf("epoch 1 RoadRange = {%d, %d}, want {%d, %d}", rng1.First, rng1.Next, FirstRoad(1), FirstRoad(2))
	}
	if ep1.Committee() != ep0.Committee() {
		t.Fatal("epoch 1 must use the genesis committee")
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
	if _, err := r.EpochAt(FirstRoad(1)); err != nil {
		t.Fatalf("epoch 1 must be present from NewRegistry: %v", err)
	}
	r.AdvanceIfNeeded(0)
	if _, err := r.EpochAt(FirstRoad(2)); err == nil {
		t.Fatal("AdvanceIfNeeded(0) must not seed epoch 2")
	}
	r.AdvanceIfNeeded(LastRoad(0))
	ep, err := r.EpochAt(FirstRoad(1))
	if err != nil {
		t.Fatalf("EpochAt(FirstRoad(1)) after last road of epoch 0: %v", err)
	}
	if ep.EpochIndex() != 1 {
		t.Fatalf("EpochAt(FirstRoad(1)).EpochIndex() = %d, want 1", ep.EpochIndex())
	}
	if _, err := r.EpochAt(FirstRoad(2)); err == nil {
		t.Fatal("AdvanceIfNeeded must not seed epoch 2")
	}
}

func TestSetupInitialEpochs_EmptyNoneIsNoOp(t *testing.T) {
	r, _ := makeRegistry(t)
	r.SetupInitialEpochs(utils.None[types.RoadRange]())
	for _, idx := range []types.EpochIndex{0, 1} {
		if _, err := r.EpochAt(FirstRoad(idx)); err != nil {
			t.Fatalf("EpochAt(epoch %d) after empty None: %v", idx, err)
		}
	}
	if _, err := r.EpochAt(FirstRoad(2)); err == nil {
		t.Fatal("EpochAt(epoch 2) should not be present from empty None")
	}
}

func TestSetupInitialEpochs_CommitQCMidSeedsPlaceholderNext(t *testing.T) {
	r, _ := makeRegistry(t)
	tip := midRoad(5)
	r.SetupInitialEpochs(utils.Some(types.RoadRange{First: tip, Next: tip + 1}))
	for _, idx := range []types.EpochIndex{4, 5, 6} {
		if _, err := r.EpochAt(FirstRoad(idx)); err != nil {
			t.Fatalf("EpochAt(epoch %d) after CommitQC seeding: %v", idx, err)
		}
	}
	if _, err := r.EpochAt(FirstRoad(7)); err == nil {
		t.Fatal("EpochAt(epoch 7) should not be present from mid-epoch CommitQC")
	}
}

func TestSetupInitialEpochs_CommitQCClosingSeedsNext(t *testing.T) {
	r, _ := makeRegistry(t)
	tip := LastRoad(5)
	r.SetupInitialEpochs(utils.Some(types.RoadRange{First: tip, Next: tip + 1}))
	for _, idx := range []types.EpochIndex{4, 5, 6} {
		if _, err := r.EpochAt(FirstRoad(idx)); err != nil {
			t.Fatalf("EpochAt(epoch %d) after closing CommitQC: %v", idx, err)
		}
	}
	if _, err := r.EpochAt(FirstRoad(7)); err == nil {
		t.Fatal("EpochAt(epoch 7) should not be present past windowLast+1")
	}
}

func TestSetupInitialEpochs_CommitSpanFromFirst(t *testing.T) {
	r, _ := makeRegistry(t)
	r.SetupInitialEpochs(utils.Some(types.RoadRange{
		First: midRoad(2),
		Next:  midRoad(5) + 1,
	}))
	for _, idx := range []types.EpochIndex{1, 2, 3, 4, 5, 6} {
		if _, err := r.EpochAt(FirstRoad(idx)); err != nil {
			t.Fatalf("EpochAt(epoch %d) after commit span seeding: %v", idx, err)
		}
	}
	if _, err := r.EpochAt(FirstRoad(7)); err == nil {
		t.Fatal("EpochAt(epoch 7) should not be present past placeholder windowLast+1")
	}
}

func TestActivateEpoch_SkipsExistingSeeds(t *testing.T) {
	r, committee := makeRegistry(t)
	r.SetupInitialEpochs(utils.None[types.RoadRange]())
	require.Equal(t, types.EpochIndex(0), r.LatestEpoch().EpochIndex())
	seeded, ok := r.EpochByIndex(1)
	require.True(t, ok)
	seededCommittee := seeded.Committee()

	pk := committee.Lanes().At(0).Validator
	ep, err := r.ActivateEpoch(
		map[types.PublicKey]uint64{pk: 1},
		time.Time{},
		r.FirstBlock(),
	)
	require.NoError(t, err)
	require.Equal(t, types.EpochIndex(2), ep.EpochIndex())
	require.Equal(t, types.EpochIndex(2), r.LatestEpoch().EpochIndex())
	require.Equal(t, FirstRoad(2), ep.RoadRange().First)
	require.Equal(t, FirstRoad(3), ep.RoadRange().Next)
	got, ok := r.EpochByIndex(1)
	require.True(t, ok)
	require.Equal(t, seededCommittee, got.Committee())
	_, ok = ep.Committee().Lane(pk).Get()
	require.True(t, ok)
	require.Equal(t, 1, ep.Committee().Lanes().Len())
}

func TestActivateEpoch_RejoinJoinedFromLatestNotPlaceholder(t *testing.T) {
	rng := utils.TestRng()
	a := types.GenSecretKey(rng)
	b := types.GenSecretKey(rng)
	committee := utils.OrPanic1(types.NewCommittee(map[types.PublicKey]uint64{
		a.Public(): 1, b.Public(): 1,
	}))
	r := utils.OrPanic1(NewRegistry(committee, 0, time.Time{}))
	r.SetupInitialEpochs(utils.None[types.RoadRange]())

	epLeave, err := r.ActivateEpoch(
		map[types.PublicKey]uint64{b.Public(): 1},
		time.Time{}, r.FirstBlock(),
	)
	require.NoError(t, err)
	require.Equal(t, types.EpochIndex(2), epLeave.EpochIndex())
	require.False(t, epLeave.Committee().HasReplica(a.Public()))

	// Seed a genesis-committee placeholder ahead of latest. Deriving from that
	// slot would treat A as still present and keep Joined=0.
	r.AdvanceIfNeeded(LastRoad(2))
	seeded, ok := r.EpochByIndex(3)
	require.True(t, ok)
	require.True(t, seeded.Committee().HasReplica(a.Public()))

	epJoin, err := r.ActivateEpoch(
		map[types.PublicKey]uint64{a.Public(): 1, b.Public(): 1},
		time.Time{}, r.FirstBlock(),
	)
	require.NoError(t, err)
	require.Equal(t, types.EpochIndex(4), epJoin.EpochIndex())
	lane := epJoin.Committee().Lane(a.Public()).OrPanic("rejoin")
	require.Equal(t, types.EpochIndex(4), lane.Joined)
}

func TestWaitForEpoch_FastPathAndWait(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, _ := makeRegistry(t)
		ep, err := r.WaitForEpoch(t.Context(), 0)
		require.NoError(t, err)
		require.Equal(t, types.EpochIndex(0), ep.EpochIndex())

		ep, err = r.WaitForEpoch(t.Context(), 1)
		require.NoError(t, err)
		require.Equal(t, types.EpochIndex(1), ep.EpochIndex())

		_, err = r.EpochAt(FirstRoad(2))
		require.Error(t, err)

		var got *types.Epoch
		var waitErr error
		go func() {
			got, waitErr = r.WaitForEpoch(t.Context(), 2)
		}()
		synctest.Wait()
		require.Nil(t, got, "WaitForEpoch returned before AdvanceIfNeeded")

		r.AdvanceIfNeeded(LastRoad(1))
		synctest.Wait()
		require.NoError(t, waitErr)
		require.Equal(t, types.EpochIndex(2), got.EpochIndex())
	})
}
