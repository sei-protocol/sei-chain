package epoch

import (
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

type errPersister struct{ err error }

func (e errPersister) Persist(*pb.PersistedEpochRegistry) error { return e.err }

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
	rng := utils.TestRng()
	r, _ := GenRegistry(rng, 3)
	got, ok := r.ClosingEpoch(r.LastRoad(0)).Get()
	require.True(t, ok)
	require.Equal(t, types.EpochIndex(0), got)

	got, ok = r.ClosingEpoch(r.LastRoad(1)).Get()
	require.True(t, ok)
	require.Equal(t, types.EpochIndex(1), got)

	_, ok = r.ClosingEpoch(r.EpochFirstRoad(1)).Get()
	require.False(t, ok)
	_, ok = r.ClosingEpoch(r.LastRoad(0) - 1).Get()
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

	epAt, err := r.EpochAt(r.LastRoad(0))
	require.NoError(t, err)
	require.Equal(t, types.EpochIndex(0), epAt.EpochIndex())
	rng0 := epAt.RoadRange()
	if rng0.First != 0 || rng0.Next != r.EpochFirstRoad(1) {
		t.Fatalf("epoch 0 RoadRange = {%d, %d}, want {0, %d}", rng0.First, rng0.Next, r.EpochFirstRoad(1))
	}

	ep1, err := r.EpochAt(r.EpochFirstRoad(1))
	require.NoError(t, err)
	rng1 := ep1.RoadRange()
	if rng1.First != r.EpochFirstRoad(1) || rng1.Next != r.EpochFirstRoad(2) {
		t.Fatalf("epoch 1 RoadRange = {%d, %d}, want {%d, %d}", rng1.First, rng1.Next, r.EpochFirstRoad(1), r.EpochFirstRoad(2))
	}
	if ep1.Committee() != ep0.Committee() {
		t.Fatal("epoch 1 must use the genesis committee")
	}
	if _, err := r.EpochAt(r.EpochFirstRoad(2)); err == nil {
		t.Fatal("EpochAt(r.EpochFirstRoad(2)) expected error for unregistered epoch, got nil")
	}
	ep, err := r.WaitForEpoch(t.Context(), 0)
	require.NoError(t, err)
	require.Equal(t, types.EpochIndex(0), ep.EpochIndex())
	ep, err = r.WaitForEpoch(t.Context(), 1)
	require.NoError(t, err)
	require.Equal(t, types.EpochIndex(1), ep.EpochIndex())
}

func TestNewRegistry_GenesisRoadOrigin(t *testing.T) {
	rng := utils.TestRng()
	committee, _, weights := genCommittee(rng, 3)
	firstRoad := 2*epochLength + 7
	r := utils.OrPanic1(NewRegistry(committee, firstRoad, time.Time{}, utils.None[string]()))

	require.Equal(t, firstRoad, r.FirstRoad())
	require.Equal(t, types.RoadRange{First: firstRoad, Next: firstRoad + epochLength}, r.MustEpoch(0).RoadRange())
	_, err := r.EpochAt(firstRoad - 1)
	require.ErrorIs(t, err, types.ErrPruned)
	ep, err := r.EpochAt(firstRoad)
	require.NoError(t, err)
	require.Equal(t, types.EpochIndex(0), ep.EpochIndex())
	require.Equal(t, types.RoadRange{First: firstRoad + epochLength, Next: firstRoad + 2*epochLength}, r.MustEpoch(1).RoadRange())
	require.Equal(t, firstRoad+2*epochLength, r.EpochFirstRoad(2))
	require.Equal(t, firstRoad+2*epochLength-1, r.LastRoad(1))
	require.Equal(t, types.EpochIndex(1), r.ClosingEpoch(r.LastRoad(1)).OrPanic("missing closing epoch"))
	require.Equal(t, types.EpochIndex(1), r.IndexForRoad(firstRoad+epochLength))
	require.NoError(t, r.StageAndActivate(0, weights))
	require.Equal(t, types.RoadRange{First: firstRoad + 2*epochLength, Next: firstRoad + 3*epochLength}, r.MustEpoch(2).RoadRange())
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
	require.Equal(t, r.EpochFirstRoad(2), ep.RoadRange().First)
	require.Equal(t, r.EpochFirstRoad(3), ep.RoadRange().Next)
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
		_, err = r.EpochAt(r.EpochFirstRoad(2))
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

func TestPruneBefore_DropsGenesisAndDerived(t *testing.T) {
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
	_, err = r.EpochByIndex(0)
	require.ErrorIs(t, err, types.ErrPruned)
	_, err = r.EpochByIndex(1)
	require.ErrorIs(t, err, types.ErrPruned)
	_ = r.MustEpoch(4)
	require.Equal(t, types.RoadIndex(0), r.FirstRoad())
	require.Equal(t, types.RoadIndex(0), r.FirstRoad())

	_, err = r.EpochAt(r.EpochFirstRoad(2))
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
	_, err = r.EpochByIndex(0)
	require.ErrorIs(t, err, types.ErrPruned)
	_, err = r.EpochByIndex(1)
	require.ErrorIs(t, err, types.ErrPruned)
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

	_, err := r2.EpochByIndex(1)
	require.ErrorIs(t, err, types.ErrPruned)
	_, err = r2.EpochByIndex(2)
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

func TestCommit_PersistFailureLeavesMemoryUnchanged(t *testing.T) {
	r, committee := makeRegistry(t)
	pk := committee.Lanes().At(0).Validator
	weights := map[types.PublicKey]uint64{pk: 1}
	ok := r.persister
	disk := errors.New("disk")

	r.persister = errPersister{err: disk}
	require.ErrorIs(t, r.StageEpoch(0, weights), disk)
	require.Equal(t, utils.None[types.EpochIndex](), r.Pending())

	r.persister = ok
	require.NoError(t, r.StageEpoch(0, weights))
	require.Equal(t, utils.Some(types.EpochIndex(2)), r.Pending())

	r.persister = errPersister{err: disk}
	require.ErrorIs(t, r.ActivateEpoch(2), disk)
	require.Equal(t, utils.Some(types.EpochIndex(2)), r.Pending())
	_, err := r.EpochByIndex(2)
	require.Error(t, err)

	r.persister = ok
	require.NoError(t, r.ActivateEpoch(2))
	_ = r.MustEpoch(2)
	require.NoError(t, r.StageAndActivate(1, weights))
	require.NoError(t, r.StageAndActivate(2, weights))

	r.persister = errPersister{err: disk}
	require.ErrorIs(t, r.PruneBefore(10), disk)
	_ = r.MustEpoch(2)

	r.persister = ok
	require.NoError(t, r.PruneBefore(10))
	_ = r.MustEpoch(4)
}

func TestNewRegistryRestoresRoadRanges(t *testing.T) {
	rng := utils.TestRng()
	committee, _, weights := genCommittee(rng, 3)
	dir := utils.Some(t.TempDir())
	firstRoad := types.RoadIndex(7)
	persisted := persistedRegistry{
		live: map[types.EpochIndex]*types.Epoch{
			3: types.NewEpoch(3, types.RoadRange{First: firstRoad + 3*epochLength, Next: firstRoad + 4*epochLength}, committee),
			4: types.NewEpoch(4, types.RoadRange{First: firstRoad + 4*epochLength, Next: firstRoad + 5*epochLength}, committee),
		},
		pending: utils.Some(types.NewEpoch(5, types.RoadRange{First: firstRoad + 5*epochLength, Next: firstRoad + 6*epochLength}, committee)),
	}
	persister, _, err := openEpochSnapshot(dir)
	require.NoError(t, err)
	require.NoError(t, persister.Persist(persistedRegistryConv.Encode(persisted)))
	timestamp := time.Unix(10, 0)
	r, err := NewRegistry(committee, firstRoad, timestamp, dir)
	require.NoError(t, err)
	for idx, epoch := range persisted.live {
		got := r.MustEpoch(idx)
		require.Equal(t, epoch.RoadRange(), got.RoadRange())
		require.Equal(t, epoch.RoadRange().First, r.EpochFirstRoad(idx))
		require.Equal(t, epoch.RoadRange().Next-1, r.LastRoad(idx))
		require.Equal(t, idx, r.IndexForRoad(epoch.RoadRange().First))
		require.Equal(t, utils.Some(idx), r.ClosingEpoch(epoch.RoadRange().Next-1))
		at, err := r.EpochAt(epoch.RoadRange().First)
		require.NoError(t, err)
		require.Equal(t, idx, at.EpochIndex())
	}
	_, err = r.EpochAt(firstRoad + 3*epochLength - 1)
	require.ErrorIs(t, err, types.ErrPruned)
	require.Equal(t, timestamp, r.GenesisTimestamp())
	require.Equal(t, firstRoad+6*epochLength, r.EpochFirstRoad(6))
	require.Equal(t, types.EpochIndex(6), r.IndexForRoad(firstRoad+6*epochLength))
	require.NoError(t, r.ActivateEpoch(5))
	require.Equal(t, persisted.pending.OrPanic("pending epoch").RoadRange(), r.MustEpoch(5).RoadRange())
	require.NoError(t, r.StageAndActivate(4, weights))
	require.Equal(t, types.RoadRange{First: firstRoad + 6*epochLength, Next: firstRoad + 7*epochLength}, r.MustEpoch(6).RoadRange())
	r, err = NewRegistry(committee, firstRoad, timestamp, dir)
	require.NoError(t, err)
	require.Equal(t, persisted.pending.OrPanic("pending epoch").RoadRange(), r.MustEpoch(5).RoadRange())
	require.Equal(t, types.RoadRange{First: firstRoad + 6*epochLength, Next: firstRoad + 7*epochLength}, r.MustEpoch(6).RoadRange())
}

func TestGenesisEpochsPersistUntilPruned(t *testing.T) {
	rng := utils.TestRng()
	committee, _, weights := genCommittee(rng, 3)
	dir := utils.Some(t.TempDir())
	timestamp := time.Unix(10, 0)
	r := utils.OrPanic1(NewRegistry(committee, 7, timestamp, dir))
	for state := range r.state.Lock() {
		require.Equal(t, 2, len(state.live))
		for idx := range 2 {
			require.Equal(t, types.EpochIndex(idx), state.live[types.EpochIndex(idx)].EpochIndex())
		}
	}
	checkPersisted := func(want ...types.EpochIndex) {
		t.Helper()
		_, loaded, err := openEpochSnapshot(dir)
		require.NoError(t, err)
		persisted, err := persistedRegistryConv.DecodeReq(loaded.OrPanic("persisted registry"))
		require.NoError(t, err)
		require.Equal(t, len(want), len(persisted.live))
		for _, idx := range want {
			_, ok := persisted.live[idx]
			require.True(t, ok)
		}
	}
	require.NoError(t, r.StageEpoch(0, weights))
	checkPersisted(0, 1)
	r = utils.OrPanic1(NewRegistry(committee, 7, timestamp, dir))
	require.NoError(t, r.PruneBefore(1))
	checkPersisted(1)
	r = utils.OrPanic1(NewRegistry(committee, 7, timestamp, dir))
	require.NoError(t, r.ActivateEpoch(2))
	checkPersisted(1, 2)
	require.NoError(t, r.PruneBefore(2))
	checkPersisted(2)
	r = utils.OrPanic1(NewRegistry(committee, 7, timestamp, dir))
	for idx := range 2 {
		_, err := r.EpochByIndex(types.EpochIndex(idx))
		require.ErrorIs(t, err, types.ErrPruned)
		_, err = r.EpochAt(r.EpochFirstRoad(types.EpochIndex(idx)))
		require.ErrorIs(t, err, types.ErrPruned)
		_, err = r.WaitForEpoch(t.Context(), types.EpochIndex(idx))
		require.ErrorIs(t, err, types.ErrPruned)
	}
	require.Equal(t, types.RoadIndex(7), r.FirstRoad())
	require.Equal(t, timestamp, r.GenesisTimestamp())
	require.NoError(t, r.StageEpoch(1, weights))
	checkPersisted(2)
}

func TestActivateEpochRejectsDifferentStagedEpoch(t *testing.T) {
	rng := utils.TestRng()
	r, keys := GenRegistry(rng, 1)
	require.NoError(t, r.StageEpoch(0, map[types.PublicKey]uint64{keys[0].Public(): 1}))
	require.Error(t, r.ActivateEpoch(3))
	require.Equal(t, utils.Some(types.EpochIndex(2)), r.Pending())
	_, err := r.EpochByIndex(2)
	require.Error(t, err)
	require.NoError(t, r.ActivateEpoch(2))
}
