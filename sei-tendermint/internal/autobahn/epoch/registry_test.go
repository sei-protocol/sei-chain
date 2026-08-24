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

func TestClosingEpoch(t *testing.T) {
	got, ok := ClosingEpoch(LastRoad(0)).Get()
	require.True(t, ok)
	require.Equal(t, types.EpochIndex(0), got)

	got, ok = ClosingEpoch(LastRoad(1)).Get()
	require.True(t, ok)
	require.Equal(t, types.EpochIndex(1), got)

	_, ok = ClosingEpoch(FirstRoad(1)).Get()
	require.False(t, ok)
	_, ok = ClosingEpoch(LastRoad(0) - 1).Get()
	require.False(t, ok)
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
	if _, err := r.EpochAt(FirstRoad(2)); err == nil {
		t.Fatal("EpochAt(FirstRoad(2)) expected error for unregistered epoch, got nil")
	}
}

func TestActivateEpoch_SkipsGenesisEpochOne(t *testing.T) {
	r, committee := makeRegistry(t)
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

func addFromEnd(t *testing.T, r *Registry, end types.EpochIndex, weights map[types.PublicKey]uint64) *types.Epoch {
	t.Helper()
	require.NoError(t, r.AddEpoch(end, weights))
	return r.MustEpoch(end + 2)
}

// Epoch 2 is the first committee that execution decides, so it must be absent
// until the stake at end(0) arrives, and then reflect that stake exactly.
func TestAddEpoch_FromEndStake(t *testing.T) {
	rng := utils.TestRng()
	a := types.GenSecretKey(rng)
	b := types.GenSecretKey(rng)
	committee := utils.OrPanic1(types.NewCommittee(map[types.PublicKey]uint64{
		a.Public(): 1, b.Public(): 1,
	}))
	r := utils.OrPanic1(NewRegistry(committee, 0, time.Time{}))
	_, err := r.EpochByIndex(2)
	require.Error(t, err)

	ep := addFromEnd(t, r, 0, map[types.PublicKey]uint64{b.Public(): 7})
	require.Equal(t, types.EpochIndex(2), ep.EpochIndex())
	require.Equal(t, types.EpochRange{First: 1, Next: 3}, r.Live())
	require.False(t, ep.Committee().HasReplica(a.Public()))
	require.Equal(t, uint64(7), ep.Committee().Weight(b.Public()))
	require.Equal(t, ep, r.MustEpoch(2))
}

// Joined comes from the preceding committee, so there is nothing to derive from
// until that epoch is registered.
func TestAddEpoch_RequiresPredecessor(t *testing.T) {
	rng := utils.TestRng()
	pk := types.GenSecretKey(rng).Public()
	committee := utils.OrPanic1(types.NewCommittee(map[types.PublicKey]uint64{
		pk: 1, types.GenSecretKey(rng).Public(): 1,
	}))
	r := utils.OrPanic1(NewRegistry(committee, 0, time.Time{}))
	require.Error(t, r.AddEpoch(5, map[types.PublicKey]uint64{pk: 1}))
}

// Refilling an epoch would swap a committee consensus has already verified QCs
// against, so a second AddEpoch leaves the registered one alone.
func TestAddEpoch_KeepsRegisteredEpoch(t *testing.T) {
	r, committee := makeRegistry(t)
	pk := committee.Lanes().At(0).Validator
	first := addFromEnd(t, r, 0, map[types.PublicKey]uint64{pk: 1})

	other := committee.Lanes().At(1).Validator
	require.NoError(t, r.AddEpoch(0, map[types.PublicKey]uint64{other: 5}))
	require.Equal(t, first, r.MustEpoch(2))
}

// AddEpoch is the path from execution into the registry, so the weights
// passed in are the ones the next committee is registered with.
func TestAddEpoch_Registers(t *testing.T) {
	rng := utils.TestRng()
	keeper := types.GenSecretKey(rng)
	dropped := types.GenSecretKey(rng)
	committee := utils.OrPanic1(types.NewCommittee(map[types.PublicKey]uint64{
		keeper.Public(): 1, dropped.Public(): 1,
	}))
	r := utils.OrPanic1(NewRegistry(committee, 0, time.Time{}))

	require.NoError(t, r.AddEpoch(0, map[types.PublicKey]uint64{keeper.Public(): 4}))
	ep := r.MustEpoch(2)
	require.Equal(t, uint64(4), ep.Committee().Weight(keeper.Public()))
	require.False(t, ep.Committee().HasReplica(dropped.Public()))

	require.NoError(t, r.AddEpoch(0, map[types.PublicKey]uint64{keeper.Public(): 4}))
	require.Equal(t, ep, r.MustEpoch(2))
}

// A validator that leaves and comes back gets a fresh Joined, so its lane is
// distinguishable from the one it held before leaving.
func TestActivateEpoch_RejoinTakesNewJoined(t *testing.T) {
	rng := utils.TestRng()
	a := types.GenSecretKey(rng)
	b := types.GenSecretKey(rng)
	committee := utils.OrPanic1(types.NewCommittee(map[types.PublicKey]uint64{
		a.Public(): 1, b.Public(): 1,
	}))
	r := utils.OrPanic1(NewRegistry(committee, 0, time.Time{}))

	epLeave, err := r.ActivateEpoch(
		0,
		map[types.PublicKey]uint64{b.Public(): 1},
		time.Time{}, r.FirstBlock(),
	)
	require.NoError(t, err)
	require.Equal(t, types.EpochIndex(2), epLeave.EpochIndex())
	require.False(t, epLeave.Committee().HasReplica(a.Public()))

	epJoin, err := r.ActivateEpoch(
		epLeave.EpochIndex(),
		map[types.PublicKey]uint64{a.Public(): 1, b.Public(): 1},
		time.Time{}, r.FirstBlock(),
	)
	require.NoError(t, err)
	require.Equal(t, types.EpochIndex(3), epJoin.EpochIndex())
	lane := epJoin.Committee().Lane(a.Public()).OrPanic("rejoin")
	require.Equal(t, types.EpochIndex(3), lane.Joined)
}

func TestWaitForEpoch_FastPathAndWait(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, committee := makeRegistry(t)
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
		require.Nil(t, got, "WaitForEpoch returned before AddEpoch")

		pk := committee.Lanes().At(0).Validator
		_ = addFromEnd(t, r, 0, map[types.PublicKey]uint64{pk: 1})
		synctest.Wait()
		require.NoError(t, waitErr)
		require.Equal(t, types.EpochIndex(2), got.EpochIndex())
	})
}

func TestPruneBefore_DropsIntermediateKeepsGenesis(t *testing.T) {
	r, committee := makeRegistry(t)
	pk := committee.Lanes().At(0).Validator
	_ = addFromEnd(t, r, 0, map[types.PublicKey]uint64{pk: 1})
	_ = r.MustEpoch(1)
	_ = r.MustEpoch(2)

	r.PruneBefore(2)
	require.Equal(t, types.EpochRange{First: 2, Next: 3}, r.Live())
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
