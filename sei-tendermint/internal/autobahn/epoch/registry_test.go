package epoch

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"testing/synctest"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

func TestRegistryGenesisAndLookups(t *testing.T) {
	for _, first := range []types.RoadIndex{0, 2*epochLength + 7} {
		t.Run(fmt.Sprint(first), func(t *testing.T) {
			rng := utils.TestRng()
			committee, _, _ := genCommittee(rng, 3)
			timestamp := time.Unix(10, 0)
			r, err := NewRegistry(committee, first, timestamp, utils.None[string]())
			require.NoError(t, err)
			epochs := []*types.Epoch{testEpoch(0, first, committee), testEpoch(1, first, committee)}
			require.Equal(t, testPersistedRegistry(epochs), r.stateRecv.Load())
			require.Equal(t, timestamp, r.GenesisTimestamp())
			for _, want := range epochs {
				idx, roads := want.EpochIndex(), want.RoadRange()
				got, err := r.Epoch(idx)
				require.NoError(t, err)
				require.Equal(t, want, got)
				got, err = r.WaitForEpoch(t.Context(), idx)
				require.NoError(t, err)
				require.Equal(t, want, got)
				for _, road := range []types.RoadIndex{roads.First, roads.Next - 1} {
					got, err = r.EpochAt(road)
					require.NoError(t, err)
					require.Equal(t, want, got)
					require.Equal(t, idx, r.IndexForRoad(road))
				}
				require.Equal(t, roads.Next-1, r.LastRoad(idx))
				require.Equal(t, utils.Some(idx), r.ClosingEpoch(roads.Next-1))
				require.Equal(t, utils.None[types.EpochIndex](), r.ClosingEpoch(roads.First))
			}
			_, err = r.Epoch(2)
			require.ErrorIs(t, err, types.ErrNotFound)
			_, err = r.EpochAt(epochs[1].RoadRange().Next)
			require.ErrorIs(t, err, types.ErrNotFound)
			require.Equal(t, types.EpochIndex(2), r.IndexForRoad(epochs[1].RoadRange().Next))
			require.Equal(t, first+3*epochLength-1, r.LastRoad(2))
			if first > 0 {
				_, err = r.EpochAt(first - 1)
				require.ErrorIs(t, err, types.ErrPruned)
				require.Equal(t, utils.None[types.EpochIndex](), r.ClosingEpoch(first-1))
			}
		})
	}
}

func TestRegistryPushAndImmutableSnapshots(t *testing.T) {
	rng := utils.TestRng()
	genesis, keys, _ := genCommittee(rng, 3)
	a, b := keys[0].Public(), keys[1].Public()
	weights := map[types.PublicKey]uint64{b: 7}
	rejoin := map[types.PublicKey]uint64{a: 1, b: 7}
	c2 := utils.OrPanic1(genesis.DeriveNext(weights, 2))
	c3 := utils.OrPanic1(c2.DeriveNext(rejoin, 3))
	e0, e1 := testEpoch(0, 0, genesis), testEpoch(1, 0, genesis)
	e2, e3 := testEpoch(2, 0, c2), testEpoch(3, 0, c3)
	initial := testPersistedRegistry([]*types.Epoch{e0, e1})
	active := testPersistedRegistry([]*types.Epoch{e0, e1, e2})
	joined := testPersistedRegistry([]*types.Epoch{e0, e1, e2, e3})
	pruned := testPersistedRegistry([]*types.Epoch{e3})
	r := utils.OrPanic1(NewRegistry(genesis, 0, time.Time{}, utils.None[string]()))
	snapshots := []struct{ got, want *persistedRegistry }{{r.stateRecv.Load(), initial}}
	for _, step := range []struct {
		name string
		run  func() error
		want *persistedRegistry
		fail bool
	}{
		{"mid epoch", func() error { return r.TryPushEpoch(0, weights) }, initial, false},
		{"missing execution boundary", func() error { return r.TryPushEpoch(r.LastRoad(1), weights) }, initial, true},
		{"empty committee", func() error { return r.TryPushEpoch(r.LastRoad(0), nil) }, initial, true},
		{"push", func() error { return r.TryPushEpoch(r.LastRoad(0), weights) }, active, false},
		{"replayed execution", func() error { return r.TryPushEpoch(r.LastRoad(0), map[types.PublicKey]uint64{a: 5}) }, active, false},
		{"rejoin", func() error { return r.TryPushEpoch(r.LastRoad(1), rejoin) }, joined, false},
		{"prune", func() error { return r.PruneBefore(3) }, pruned, false},
		{"replay pruned execution", func() error { return r.TryPushEpoch(r.LastRoad(0), weights) }, pruned, false},
	} {
		before := r.stateRecv.Load()
		err := step.run()
		if step.fail {
			require.Error(t, err, step.name)
		} else {
			require.NoError(t, err, step.name)
		}
		got := r.stateRecv.Load()
		require.Equal(t, step.want, got, step.name)
		if snapshots[len(snapshots)-1].want == step.want {
			require.True(t, before == got, step.name)
		}
		for _, old := range snapshots {
			require.Equal(t, old.want, old.got, step.name)
		}
		if before != got {
			snapshots = append(snapshots, struct{ got, want *persistedRegistry }{got, step.want})
		}
	}
}

func TestRegistryWaitForEpoch(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rng := utils.TestRng()
		committee, _, weights := genCommittee(rng, 3)
		r := utils.OrPanic1(NewRegistry(committee, 7, time.Time{}, utils.None[string]()))
		var got utils.Option[*types.Epoch]
		require.NoError(t, scope.Run(t.Context(), func(ctx context.Context, sc scope.Scope) error {
			sc.Spawn(func() error {
				epoch, err := r.WaitForEpoch(ctx, 2)
				if err == nil {
					got = utils.Some(epoch)
				}
				return err
			})
			synctest.Wait()
			require.Equal(t, utils.None[*types.Epoch](), got)
			require.NoError(t, r.TryPushEpoch(r.LastRoad(0), weights))
			synctest.Wait()
			require.Equal(t, utils.Some(r.MustEpoch(2)), got)

			return nil
		}))
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		_, err := r.WaitForEpoch(ctx, 99)
		require.ErrorIs(t, err, context.Canceled)
	})
}

func TestRegistryPrune(t *testing.T) {
	rng := utils.TestRng()
	r, _ := GenRegistryThrough(rng, 3, 6)
	epochs := make([]*types.Epoch, 7)
	for idx := range epochs {
		epochs[idx] = r.MustEpoch(types.EpochIndex(idx))
	}
	first := types.EpochIndex(0)
	for _, keep := range []types.EpochIndex{0, 3, 1, 6, ^types.EpochIndex(0)} {
		before := r.stateRecv.Load()
		require.NoError(t, r.PruneBefore(keep))
		nextFirst := max(first, min(keep, 6))
		require.Equal(t, testPersistedRegistry(epochs[nextFirst:]), r.stateRecv.Load())
		if nextFirst == first {
			require.True(t, before == r.stateRecv.Load())
		}
		first = nextFirst
	}
	for _, ep := range epochs[:6] {
		_, err := r.Epoch(ep.EpochIndex())
		require.ErrorIs(t, err, types.ErrPruned)
		_, err = r.EpochAt(ep.RoadRange().First)
		require.ErrorIs(t, err, types.ErrPruned)
		_, err = r.WaitForEpoch(t.Context(), ep.EpochIndex())
		require.ErrorIs(t, err, types.ErrPruned)
	}
}

func TestRegistryPersistenceLifecycle(t *testing.T) {
	rng := utils.TestRng()
	genesis, keys, _ := genCommittee(rng, 3)
	a, b := keys[0].Public(), keys[1].Public()
	weights2 := map[types.PublicKey]uint64{b: 2}
	weights3 := map[types.PublicKey]uint64{a: 3, b: 2}
	weights4 := map[types.PublicKey]uint64{a: 4, b: 2}
	c2 := utils.OrPanic1(genesis.DeriveNext(weights2, 2))
	c3 := utils.OrPanic1(c2.DeriveNext(weights3, 3))
	c4 := utils.OrPanic1(c3.DeriveNext(weights4, 4))
	epochs := []*types.Epoch{testEpoch(0, 7, genesis), testEpoch(1, 7, genesis), testEpoch(2, 7, c2), testEpoch(3, 7, c3), testEpoch(4, 7, c4), testEpoch(5, 7, c4)}
	dir, timestamp := utils.Some(t.TempDir()), time.Unix(10, 0)
	r := utils.OrPanic1(NewRegistry(genesis, 7, timestamp, dir))
	require.Equal(t, testPersistedRegistry(epochs[:2]), r.stateRecv.Load())
	for _, step := range []struct {
		name string
		run  func() error
		want *persistedRegistry
	}{
		{"push preserves genesis", func() error { return r.TryPushEpoch(r.LastRoad(0), weights2) }, testPersistedRegistry(epochs[:3])},
		{"prune first genesis", func() error { return r.PruneBefore(1) }, testPersistedRegistry(epochs[1:3])},
		{"push rejoin", func() error { return r.TryPushEpoch(r.LastRoad(1), weights3) }, testPersistedRegistry(epochs[1:4])},
		{"push weights", func() error { return r.TryPushEpoch(r.LastRoad(2), weights4) }, testPersistedRegistry(epochs[1:5])},
		{"prune genesis and derived", func() error { return r.PruneBefore(3) }, testPersistedRegistry(epochs[3:5])},
		{"push after prune", func() error { return r.TryPushEpoch(r.LastRoad(3), weights4) }, testPersistedRegistry(epochs[3:6])},
		{"retain latest", func() error { return r.PruneBefore(99) }, testPersistedRegistry(epochs[5:6])},
	} {
		require.NoError(t, step.run(), step.name)
		require.Equal(t, step.want, r.stateRecv.Load(), step.name)
		r = utils.OrPanic1(NewRegistry(genesis, 7, timestamp, dir))
		require.Equal(t, step.want, r.stateRecv.Load(), step.name)
	}
	require.Equal(t, timestamp, r.GenesisTimestamp())
}

func TestRegistryPersistenceFailure(t *testing.T) {
	rng := utils.TestRng()
	genesis, _, weights := genCommittee(rng, 3)
	r := utils.OrPanic1(NewRegistry(genesis, 0, time.Time{}, utils.None[string]()))
	real := r.persister
	disk := errors.New("disk")
	epochs := []*types.Epoch{testEpoch(0, 0, genesis), testEpoch(1, 0, genesis), testEpoch(2, 0, genesis)}
	initial := testPersistedRegistry(epochs[:2])
	staged := testPersistedRegistry(epochs)
	active := testPersistedRegistry(epochs)
	pruned := testPersistedRegistry(epochs[2:])
	for _, step := range []struct {
		name         string
		run          func() error
		broken, fail bool
		want         *persistedRegistry
	}{
		{"failed stage", func() error { return r.TryPushEpoch(r.LastRoad(0), weights) }, true, true, initial},
		{"push", func() error { return r.TryPushEpoch(r.LastRoad(0), weights) }, false, false, staged},
		{"repeat pushed skips disk", func() error { return r.TryPushEpoch(r.LastRoad(0), weights) }, true, false, staged},
		{"repeat registered skips disk", func() error { return r.TryPushEpoch(r.LastRoad(0), weights) }, true, false, active},
		{"failed prune", func() error { return r.PruneBefore(2) }, true, true, active},
		{"prune", func() error { return r.PruneBefore(2) }, false, false, pruned},
		{"repeat prune skips disk", func() error { return r.PruneBefore(2) }, true, false, pruned},
	} {
		r.persister = real
		if step.broken {
			r.persister = errPersister{err: disk}
		}
		before := r.stateRecv.Load()
		err := step.run()
		if step.fail {
			require.ErrorIs(t, err, disk, step.name)
		} else {
			require.NoError(t, err, step.name)
		}
		require.Equal(t, step.want, r.stateRecv.Load(), step.name)
		if step.broken {
			require.True(t, before == r.stateRecv.Load(), step.name)
		}
	}
}

func TestNewRegistryRestoresPrunedPredecessor(t *testing.T) {
	rng := utils.TestRng()
	genesis, _, _ := genCommittee(rng, 3)
	other, _, _ := genCommittee(rng, 2)
	want := testPersistedRegistry([]*types.Epoch{testEpoch(3, 7, other)})
	r, err := testNewRegistry(t, genesis, 7, persistedRegistryConv.Encode(want))
	require.NoError(t, err)
	require.Equal(t, want, r.stateRecv.Load())
}

func TestNewRegistryRejectsInvalidSnapshots(t *testing.T) {
	rng := utils.TestRng()
	genesis, keys, _ := genCommittee(rng, 3)
	other := utils.OrPanic1(types.NewCommittee(map[types.PublicKey]uint64{keys[0].Public(): 1, types.GenSecretKey(rng).Public(): 1}))
	reject := func(live []*types.Epoch, pending utils.Option[*types.Epoch]) {
		t.Helper()
		if ep, ok := pending.Get(); ok {
			live = append(live, ep)
		}
		_, err := testNewRegistry(t, genesis, 7, persistedRegistryConv.Encode(testPersistedRegistry(live)))
		require.Error(t, err)
	}
	_, err := testNewRegistry(t, genesis, 7, &pb.PersistedEpochRegistry{})
	require.Error(t, err)
	reject([]*types.Epoch{testEpoch(0, 7, other)}, utils.None[*types.Epoch]())
	reject([]*types.Epoch{testEpoch(1, 7, other)}, utils.None[*types.Epoch]())
	live := []*types.Epoch{testEpoch(0, 7, genesis), testEpoch(1, 7, genesis)}
	reject([]*types.Epoch{live[0], live[1], testEpoch(2, 7, other)}, utils.None[*types.Epoch]())
	reject(live, utils.Some(testEpoch(2, 7, other)))
	reject([]*types.Epoch{testEpoch(2, 8, genesis)}, utils.None[*types.Epoch]())
	wrongLength := types.NewEpoch(2, types.RoadRange{First: 7 + 2*epochLength, Next: 8 + 3*epochLength}, genesis)
	reject([]*types.Epoch{wrongLength}, utils.None[*types.Epoch]())
	reject(live, utils.Some(wrongLength))
}
