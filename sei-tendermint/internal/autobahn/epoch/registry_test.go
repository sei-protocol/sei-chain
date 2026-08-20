package epoch

import (
	"errors"
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
	_, err := r.EpochByIndex(99)
	if err == nil {
		t.Fatal("EpochByIndex(99) succeeded, want not found")
	}
	if errors.Is(err, types.ErrPruned) {
		t.Fatal("EpochByIndex(99) returned ErrPruned, want not registered")
	}
}

func TestNewRegistry_Genesis(t *testing.T) {
	r, _ := makeRegistry(t)
	ep0, err := r.EpochByIndex(0)
	require.NoError(t, err)
	require.Equal(t, types.EpochIndex(0), ep0.EpochIndex())

	epAt, err := r.EpochAt(LastRoad(0))
	require.NoError(t, err)
	require.Equal(t, types.EpochIndex(0), epAt.EpochIndex())
	rng0 := epAt.RoadRange()
	if rng0.First != 0 || rng0.Next != FirstRoad(1) {
		t.Fatalf("epoch 0 RoadRange = {%d, %d}, want {0, %d}", rng0.First, rng0.Next, FirstRoad(1))
	}

	ep1, err := r.EpochAt(FirstRoad(1))
	require.NoError(t, err)
	rng1 := ep1.RoadRange()
	if rng1.First != FirstRoad(1) || rng1.Next != FirstRoad(2) {
		t.Fatalf("epoch 1 RoadRange = {%d, %d}, want {%d, %d}", rng1.First, rng1.Next, FirstRoad(1), FirstRoad(2))
	}
	if ep1.Committee() != ep0.Committee() {
		t.Fatal("epoch 1 must use the genesis committee")
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

func TestSetupInitialEpochs(t *testing.T) {
	for _, tc := range []struct {
		name   string
		span   utils.Option[types.RoadRange]
		want   []types.EpochIndex
		absent types.EpochIndex
	}{
		{
			name:   "empty None is no-op",
			span:   utils.None[types.RoadRange](),
			want:   []types.EpochIndex{0, 1},
			absent: 2,
		},
		{
			name:   "mid CommitQC seeds placeholder next",
			span:   utils.Some(types.RoadRange{First: midRoad(5), Next: midRoad(5) + 1}),
			want:   []types.EpochIndex{4, 5, 6},
			absent: 7,
		},
		{
			name: "commit span from first",
			span: utils.Some(types.RoadRange{
				First: midRoad(2),
				Next:  midRoad(5) + 1,
			}),
			want:   []types.EpochIndex{1, 2, 3, 4, 5, 6},
			absent: 7,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := makeRegistry(t)
			r.SetupInitialEpochs(tc.span)
			for _, idx := range tc.want {
				if _, err := r.EpochAt(FirstRoad(idx)); err != nil {
					t.Fatalf("EpochAt(epoch %d): %v", idx, err)
				}
			}
			if _, err := r.EpochAt(FirstRoad(tc.absent)); err == nil {
				t.Fatalf("EpochAt(epoch %d) should not be present", tc.absent)
			}
		})
	}
}

func TestActivateEpoch_SkipsExistingSeeds(t *testing.T) {
	r, committee := makeRegistry(t)
	r.SetupInitialEpochs(utils.None[types.RoadRange]())
	seeded := r.MustEpoch(1)
	seededCommittee := seeded.Committee()

	pk := committee.Lanes().At(0).Validator
	ep, err := r.ActivateEpoch(
		0,
		map[types.PublicKey]uint64{pk: 1},
		time.Time{},
		r.FirstBlock(),
	)
	require.NoError(t, err)
	require.Equal(t, types.EpochIndex(2), ep.EpochIndex())
	require.Equal(t, FirstRoad(2), ep.RoadRange().First)
	require.Equal(t, FirstRoad(3), ep.RoadRange().Next)
	got := r.MustEpoch(1)
	require.Equal(t, seededCommittee, got.Committee())
	_, ok := ep.Committee().Lane(pk).Get()
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
		0,
		map[types.PublicKey]uint64{b.Public(): 1},
		time.Time{}, r.FirstBlock(),
	)
	require.NoError(t, err)
	require.Equal(t, types.EpochIndex(2), epLeave.EpochIndex())
	require.False(t, epLeave.Committee().HasReplica(a.Public()))

	// Seed a genesis-committee placeholder ahead of the activated epoch. Deriving from that
	// slot would treat A as still present and keep Joined=0.
	r.AdvanceIfNeeded(LastRoad(2))
	seeded := r.MustEpoch(3)
	require.True(t, seeded.Committee().HasReplica(a.Public()))

	epJoin, err := r.ActivateEpoch(
		epLeave.EpochIndex(),
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

func TestPruneBefore_DropsIntermediateKeepsGenesis(t *testing.T) {
	r, _ := makeRegistry(t)
	r.AdvanceIfNeeded(LastRoad(0))
	r.AdvanceIfNeeded(LastRoad(1))
	_ = r.MustEpoch(1)
	_ = r.MustEpoch(2)

	r.PruneBefore(2)
	_ = r.MustEpoch(0)
	_, err := r.EpochByIndex(1)
	require.ErrorIs(t, err, types.ErrPruned)
	_ = r.MustEpoch(2)
	require.Equal(t, types.GlobalBlockNumber(0), r.FirstBlock())

	_, err = r.EpochAt(FirstRoad(1))
	require.ErrorIs(t, err, types.ErrPruned)
	_, err = r.WaitForEpoch(t.Context(), 1)
	require.ErrorIs(t, err, types.ErrPruned)

	r.PruneBefore(1) // no rewind
	_ = r.MustEpoch(2)

	pk := r.MustEpoch(0).Committee().Lanes().At(0).Validator
	ep, err := r.ActivateEpoch(
		0,
		map[types.PublicKey]uint64{pk: 1},
		time.Time{},
		r.FirstBlock(),
	)
	require.NoError(t, err)
	require.Equal(t, types.EpochIndex(3), ep.EpochIndex())
	_, err = r.EpochByIndex(1)
	require.ErrorIs(t, err, types.ErrPruned)
}
