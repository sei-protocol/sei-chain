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
	r := utils.OrPanic1(NewRegistry(committee, 0, time.Time{}, utils.None[string]()))
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
	ep, err := r.WaitForEpoch(t.Context(), 0)
	require.NoError(t, err)
	require.Equal(t, types.EpochIndex(0), ep.EpochIndex())
	ep, err = r.WaitForEpoch(t.Context(), 1)
	require.NoError(t, err)
	require.Equal(t, types.EpochIndex(1), ep.EpochIndex())
}

func addFromEnd(t *testing.T, r *Registry, end types.EpochIndex, weights map[types.PublicKey]uint64) *types.Epoch {
	t.Helper()
	require.NoError(t, r.StageAndActivate(end, weights))
	return r.MustEpoch(end + 2)
}

func TestStageEpoch_FromEndStake(t *testing.T) {
	rng := utils.TestRng()
	a := types.GenSecretKey(rng)
	b := types.GenSecretKey(rng)
	committee := utils.OrPanic1(types.NewCommittee(map[types.PublicKey]uint64{
		a.Public(): 1, b.Public(): 1,
	}))
	r := utils.OrPanic1(NewRegistry(committee, 0, time.Time{}, utils.None[string]()))
	genesis := r.MustEpoch(1).Committee()
	_, err := r.EpochByIndex(2)
	require.Error(t, err)

	ep := addFromEnd(t, r, 0, map[types.PublicKey]uint64{b.Public(): 7})
	require.Equal(t, types.EpochIndex(2), ep.EpochIndex())
	require.Equal(t, FirstRoad(2), ep.RoadRange().First)
	require.Equal(t, FirstRoad(3), ep.RoadRange().Next)
	require.Equal(t, genesis, r.MustEpoch(1).Committee())
	require.False(t, ep.Committee().HasReplica(a.Public()))
	require.Equal(t, uint64(7), ep.Committee().Weight(b.Public()))
	require.Equal(t, ep, r.MustEpoch(2))

	require.NoError(t, r.StageAndActivate(0, map[types.PublicKey]uint64{b.Public(): 7, a.Public(): 0}))
	require.Equal(t, ep, r.MustEpoch(2))
}

func TestStageEpoch_RequiresNext(t *testing.T) {
	rng := utils.TestRng()
	pk := types.GenSecretKey(rng).Public()
	committee := utils.OrPanic1(types.NewCommittee(map[types.PublicKey]uint64{
		pk: 1, types.GenSecretKey(rng).Public(): 1,
	}))
	r := utils.OrPanic1(NewRegistry(committee, 0, time.Time{}, utils.None[string]()))
	require.Error(t, r.StageEpoch(5, map[types.PublicKey]uint64{pk: 1}))
}

func TestStageEpoch_RefusesWhileUnconfirmed(t *testing.T) {
	r, committee := makeRegistry(t)
	pk := committee.Lanes().At(0).Validator
	weights := map[types.PublicKey]uint64{pk: 1}
	require.NoError(t, r.StageEpoch(0, weights))

	require.Error(t, r.StageEpoch(1, weights))
	require.Equal(t, utils.Some(types.EpochIndex(2)), r.Pending())

	require.NoError(t, r.StageEpoch(0, weights))
	require.Equal(t, utils.Some(types.EpochIndex(2)), r.Pending())

	require.NoError(t, r.ActivateEpoch(2))
	require.NoError(t, r.StageEpoch(1, weights))
	require.Equal(t, utils.Some(types.EpochIndex(3)), r.Pending())
}

func TestStageEpoch_KeepsRegisteredEpoch(t *testing.T) {
	r, committee := makeRegistry(t)
	pk := committee.Lanes().At(0).Validator
	first := addFromEnd(t, r, 0, map[types.PublicKey]uint64{pk: 1})

	other := committee.Lanes().At(1).Validator
	require.Error(t, r.StageAndActivate(0, map[types.PublicKey]uint64{other: 5}))
	require.Equal(t, first, r.MustEpoch(2))
}

func TestStageEpoch_RejoinTakesNewJoined(t *testing.T) {
	rng := utils.TestRng()
	a := types.GenSecretKey(rng)
	b := types.GenSecretKey(rng)
	committee := utils.OrPanic1(types.NewCommittee(map[types.PublicKey]uint64{
		a.Public(): 1, b.Public(): 1,
	}))
	r := utils.OrPanic1(NewRegistry(committee, 0, time.Time{}, utils.None[string]()))

	epLeave := addFromEnd(t, r, 0, map[types.PublicKey]uint64{b.Public(): 1})
	require.Equal(t, types.EpochIndex(2), epLeave.EpochIndex())
	require.False(t, epLeave.Committee().HasReplica(a.Public()))

	epJoin := addFromEnd(t, r, 1, map[types.PublicKey]uint64{a.Public(): 1, b.Public(): 1})
	require.Equal(t, types.EpochIndex(3), epJoin.EpochIndex())
	lane := epJoin.Committee().Lane(a.Public()).OrPanic("rejoin")
	require.Equal(t, types.EpochIndex(3), lane.Joined)
}

func TestStageEpoch_PendingIsInvisible(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, committee := makeRegistry(t)
		pk := committee.Lanes().At(0).Validator
		require.NoError(t, r.StageEpoch(0, map[types.PublicKey]uint64{pk: 1}))
		require.Equal(t, utils.Some(types.EpochIndex(2)), r.Pending())

		_, err := r.EpochByIndex(2)
		require.Error(t, err)
		_, err = r.EpochAt(FirstRoad(2))
		require.Error(t, err)

		var got *types.Epoch
		go func() { got, _ = r.WaitForEpoch(t.Context(), 2) }()
		synctest.Wait()
		require.Nil(t, got, "WaitForEpoch returned for a staged committee")

		require.NoError(t, r.ActivateEpoch(2))
		synctest.Wait()
		require.Equal(t, types.EpochIndex(2), got.EpochIndex())
	})
}

func TestActivateEpoch_IdempotentAndRefusesUnstaged(t *testing.T) {
	r, committee := makeRegistry(t)
	pk := committee.Lanes().At(0).Validator
	require.Error(t, r.ActivateEpoch(2))

	require.NoError(t, r.StageEpoch(0, map[types.PublicKey]uint64{pk: 1}))
	require.NoError(t, r.ActivateEpoch(2))
	ep := r.MustEpoch(2)
	require.NoError(t, r.ActivateEpoch(2))
	require.Equal(t, ep, r.MustEpoch(2))
}

func TestPruneBefore_KeepsLatestAndPending(t *testing.T) {
	r, committee := makeRegistry(t)
	pk := committee.Lanes().At(0).Validator
	weights := map[types.PublicKey]uint64{pk: 1}
	for endEpoch := types.EpochIndex(0); endEpoch < 4; endEpoch++ {
		require.NoError(t, r.StageAndActivate(endEpoch, weights))
	}
	require.NoError(t, r.StageEpoch(4, weights))

	require.NoError(t, r.PruneBefore(10)) // clamped to the latest live epoch
	_ = r.MustEpoch(5)
	require.Equal(t, utils.Some(types.EpochIndex(6)), r.Pending())
	require.NoError(t, r.ActivateEpoch(6))
}

func TestPruneBefore_DropsDerivedKeepsGenesis(t *testing.T) {
	r, committee := makeRegistry(t)
	pk := committee.Lanes().At(0).Validator
	weights := map[types.PublicKey]uint64{pk: 1}
	_ = addFromEnd(t, r, 0, weights)
	_ = addFromEnd(t, r, 1, weights)
	_ = addFromEnd(t, r, 2, weights)

	require.NoError(t, r.PruneBefore(4))
	_, err := r.EpochByIndex(2)
	require.ErrorIs(t, err, types.ErrPruned)
	_, err = r.EpochByIndex(3)
	require.ErrorIs(t, err, types.ErrPruned)
	_ = r.MustEpoch(0)
	_ = r.MustEpoch(1)
	_ = r.MustEpoch(4)
	require.Equal(t, types.GlobalBlockNumber(0), r.FirstBlock())

	_, err = r.EpochAt(FirstRoad(2))
	require.ErrorIs(t, err, types.ErrPruned)
	_, err = r.WaitForEpoch(t.Context(), 2)
	require.ErrorIs(t, err, types.ErrPruned)

	require.NoError(t, r.PruneBefore(1)) // no rewind
	_ = r.MustEpoch(4)

	ep := addFromEnd(t, r, 3, weights)
	require.Equal(t, types.EpochIndex(5), ep.EpochIndex())
	require.NoError(t, r.PruneBefore(5))
	_, err = r.EpochByIndex(4)
	require.ErrorIs(t, err, types.ErrPruned)
	_ = r.MustEpoch(5)

	require.NoError(t, r.PruneBefore(10)) // latest live epoch is retained
	_ = r.MustEpoch(5)
	_ = r.MustEpoch(0)
	_ = r.MustEpoch(1)
}

func TestNewRegistry_RestoresPrunedSnapshot(t *testing.T) {
	rng := utils.TestRng()
	a := types.GenSecretKey(rng).Public()
	b := types.GenSecretKey(rng).Public()
	genesis := utils.OrPanic1(types.NewCommittee(map[types.PublicKey]uint64{a: 1, b: 1}))
	dir := utils.Some(t.TempDir())

	r1 := utils.OrPanic1(NewRegistry(genesis, 7, time.Unix(10, 0), dir))
	require.NoError(t, r1.StageAndActivate(0, map[types.PublicKey]uint64{b: 2}))
	require.NoError(t, r1.StageAndActivate(1, map[types.PublicKey]uint64{a: 3, b: 2}))
	require.NoError(t, r1.StageAndActivate(2, map[types.PublicKey]uint64{a: 4, b: 2}))
	require.NoError(t, r1.PruneBefore(3))

	r2 := utils.OrPanic1(NewRegistry(genesis, 7, time.Unix(10, 0), dir))

	_ = r2.MustEpoch(1)
	_, err := r2.EpochByIndex(2)
	require.ErrorIs(t, err, types.ErrPruned)
	ep3 := r2.MustEpoch(3)
	ep4 := r2.MustEpoch(4)
	require.Equal(t, utils.None[types.EpochIndex](), r2.Pending())
	require.Equal(t, uint64(3), ep3.Committee().Weight(a))
	require.Equal(t, types.EpochIndex(3), ep3.Committee().Lane(a).OrPanic("a in epoch 3").Joined)
	require.Equal(t, types.EpochIndex(3), ep4.Committee().Lane(a).OrPanic("a in epoch 4").Joined)
}

func TestNewRegistry_RestoresSnapshotPending(t *testing.T) {
	rng := utils.TestRng()
	a := types.GenSecretKey(rng).Public()
	genesis := utils.OrPanic1(types.NewCommittee(map[types.PublicKey]uint64{a: 1}))
	dir := utils.Some(t.TempDir())

	r1 := utils.OrPanic1(NewRegistry(genesis, 7, time.Unix(10, 0), dir))
	require.NoError(t, r1.StageEpoch(0, map[types.PublicKey]uint64{a: 2}))

	r2 := utils.OrPanic1(NewRegistry(genesis, 7, time.Unix(10, 0), dir))
	_ = r2.MustEpoch(1)
	require.Equal(t, utils.Some(types.EpochIndex(2)), r2.Pending())
	_, err := r2.EpochByIndex(2)
	require.Error(t, err)
	require.False(t, errors.Is(err, types.ErrPruned))

	require.NoError(t, r2.ActivateEpoch(2))
	require.Equal(t, uint64(2), r2.MustEpoch(2).Committee().Weight(a))
	require.Equal(t, utils.None[types.EpochIndex](), r2.Pending())

	r3 := utils.OrPanic1(NewRegistry(genesis, 7, time.Unix(10, 0), dir))
	require.Equal(t, uint64(2), r3.MustEpoch(2).Committee().Weight(a))
	require.Equal(t, utils.None[types.EpochIndex](), r3.Pending())
}

func TestNewRegistry_RestoresLatestAndPendingAfterPrune(t *testing.T) {
	rng := utils.TestRng()
	a := types.GenSecretKey(rng).Public()
	genesis := utils.OrPanic1(types.NewCommittee(map[types.PublicKey]uint64{a: 1}))
	dir := utils.Some(t.TempDir())
	weights := map[types.PublicKey]uint64{a: 2}

	r1 := utils.OrPanic1(NewRegistry(genesis, 7, time.Unix(10, 0), dir))
	for endEpoch := types.EpochIndex(0); endEpoch < 4; endEpoch++ {
		require.NoError(t, r1.StageAndActivate(endEpoch, weights))
	}
	require.NoError(t, r1.StageEpoch(4, weights))
	require.NoError(t, r1.PruneBefore(10))

	r2 := utils.OrPanic1(NewRegistry(genesis, 7, time.Unix(10, 0), dir))
	_ = r2.MustEpoch(5)
	require.Equal(t, utils.Some(types.EpochIndex(6)), r2.Pending())
	require.NoError(t, r2.ActivateEpoch(6))
	require.Equal(t, uint64(2), r2.MustEpoch(6).Committee().Weight(a))
}
