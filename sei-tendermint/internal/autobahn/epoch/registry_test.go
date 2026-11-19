package epoch

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

const testFirstRoad types.RoadHeight = 1

func testSchedule(length types.RoadHeight) GenesisSchedule {
	return GenesisSchedule{FirstRoad: testFirstRoad, Length: length, Timestamp: time.Unix(1, 0)}
}

// testWeights returns weights for n validators with distinct keys, all weight 1000.
func testWeights(rng utils.Rng, n int) map[types.PublicKey]uint64 {
	weights := map[types.PublicKey]uint64{}
	for _, sk := range utils.GenSliceN(rng, n, types.GenSecretKey) {
		weights[sk.Public()] = 1000
	}
	return weights
}

func testRegistry(t *testing.T, weights map[types.PublicKey]uint64, length types.RoadHeight) *Registry {
	t.Helper()
	row, err := GenesisRow(weights, testSchedule(length))
	require.NoError(t, err)
	epochs, err := Decode(row)
	require.NoError(t, err)
	r, err := NewRegistry(epochs)
	require.NoError(t, err)
	return r
}

func TestGenesisRowHoldsEpochsZeroAndOne(t *testing.T) {
	rng := utils.TestRng()
	weights := testWeights(rng, 3)
	epochs, err := Decode(utils.OrPanic1(GenesisRow(weights, testSchedule(10))))
	require.NoError(t, err)
	require.Len(t, epochs, 2)
	require.Equal(t, types.RoadRange{First: 1, Next: 11}, epochs[0].RoadRange())
	require.Equal(t, types.RoadRange{First: 11, Next: 21}, epochs[1].RoadRange())
	require.Equal(t, types.EpochIndex(0), epochs[0].EpochIndex())
	require.Equal(t, types.EpochIndex(1), epochs[1].EpochIndex())
}

func TestGenesisRowRejects(t *testing.T) {
	_, err := GenesisRow(testWeights(utils.TestRng(), 1), testSchedule(0))
	require.Error(t, err)
	_, err = GenesisRow(map[types.PublicKey]uint64{}, testSchedule(10))
	require.Error(t, err)
}

func TestAdvanceOnlyDerivesAtTheRoadBeforeTheBoundary(t *testing.T) {
	rng := utils.TestRng()
	weights := testWeights(rng, 3)
	epochs := utils.OrPanic1(Decode(utils.OrPanic1(GenesisRow(weights, testSchedule(10)))))
	// Epoch 2 is derived at LastRoad(0) = 10, the road before epoch 1's boundary.
	for road := types.RoadHeight(1); road < 10; road++ {
		next, err := Advance(epochs, road, weights)
		require.NoError(t, err)
		require.False(t, next.IsPresent(), "road %d", road)
	}
	next, err := Advance(epochs, 10, weights)
	require.NoError(t, err)
	require.True(t, next.IsPresent())
	got := next.OrPanic("window")
	require.Len(t, got, 3)
	require.Equal(t, types.EpochIndex(2), got[2].EpochIndex())
	require.Equal(t, types.RoadRange{First: 21, Next: 31}, got[2].RoadRange())
	round, err := Decode(Encode(got))
	require.NoError(t, err)
	require.Equal(t, got[2].EpochIndex(), round[2].EpochIndex())
	require.Equal(t, got[2].RoadRange(), round[2].RoadRange())
}

func TestAdvanceKeepsTheThreeNewestEpochs(t *testing.T) {
	rng := utils.TestRng()
	weights := testWeights(rng, 3)
	r := testRegistry(t, weights, 10)
	for target := types.EpochIndex(2); target <= 5; target++ {
		require.NoError(t, r.TestPushEpoch(target-2, weights))
	}
	epochs := r.Epochs()
	require.Len(t, epochs, 3)
	require.Equal(t, types.EpochIndex(3), epochs[0].EpochIndex())
	require.Equal(t, types.EpochIndex(5), epochs[2].EpochIndex())
}

func TestAdvanceRejects(t *testing.T) {
	rng := utils.TestRng()
	weights := testWeights(rng, 3)
	epochs := utils.OrPanic1(Decode(utils.OrPanic1(GenesisRow(weights, testSchedule(10)))))
	_, err := Advance(epochs, 11, weights)
	require.Error(t, err)
	_, err = Advance(nil, 10, weights)
	require.Error(t, err)
	_, err = Advance(epochs, 10, map[types.PublicKey]uint64{})
	require.Error(t, err)
}

func TestDecodeRejectsInvalidRows(t *testing.T) {
	rng := utils.TestRng()
	committee := utils.OrPanic1(types.NewCommittee(testWeights(rng, 3)))
	record := func(idx types.EpochIndex, first, next types.RoadHeight) *pb.Epoch {
		return types.EpochConv.Encode(types.NewEpoch(idx, types.RoadRange{First: first, Next: next}, committee))
	}
	reject := func(name string, live ...*pb.Epoch) {
		t.Helper()
		_, err := Decode(protoutils.Marshal(&pb.PersistedEpochRegistry{LiveV2: live}))
		require.Error(t, err, name)
	}
	reject("empty")
	reject("gap in index", record(2, 20, 30), record(4, 30, 40))
	reject("noncontiguous roads", record(2, 20, 30), record(3, 31, 40))
	reject("first index is not the next epoch", record(2, 20, 30), record(2, 30, 40))
	malformed := record(2, 20, 30)
	malformed.Index = nil
	reject("missing index field", malformed)
	_, err := Decode([]byte{0xff})
	require.Error(t, err)
	extra := utils.OrPanic1(types.NewCommittee(testWeights(rng, 1)))
	reject("committee does not follow", record(2, 20, 30), types.EpochConv.Encode(types.NewEpoch(3, types.RoadRange{First: 30, Next: 40}, extra)))
}

func TestStoreReplacesWindowWithTheSameNewestEpoch(t *testing.T) {
	rng := utils.TestRng()
	weights := testWeights(rng, 3)
	r := testRegistry(t, weights, 10)
	require.NoError(t, r.TestPushEpoch(0, weights))
	window := r.Epochs()
	r.Store(window[1:])
	require.Equal(t, window[1:], r.Epochs())
}

func TestStorePanicsWhenNewestEpochMovesBack(t *testing.T) {
	rng := utils.TestRng()
	weights := testWeights(rng, 3)
	_, err := NewRegistry(nil)
	require.Error(t, err)
	r := testRegistry(t, weights, 10)
	require.NoError(t, r.TestPushEpoch(0, weights))
	older := r.Epochs()[:2]
	require.Panics(t, func() { r.Store(nil) })
	require.Panics(t, func() { r.Store(older) })
}

func TestWaitReturnsWindowThatIsReady(t *testing.T) {
	rng := utils.TestRng()
	weights := testWeights(rng, 3)
	r := testRegistry(t, weights, 10)
	ctx := t.Context()
	window, err := r.Wait(ctx, func(w []*types.Epoch) (bool, error) { return len(w) == 2, nil })
	require.NoError(t, err)
	require.Len(t, window, 2)
}

func TestWaitKeepsWaitingUntilTheWindowIsReady(t *testing.T) {
	rng := utils.TestRng()
	weights := testWeights(rng, 3)
	r := testRegistry(t, weights, 10)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan []*types.Epoch, 1)
	go func() {
		window, err := r.Wait(ctx, func(w []*types.Epoch) (bool, error) { return w[len(w)-1].EpochIndex() >= 2, nil })
		if err == nil {
			done <- window
		}
		close(done)
	}()
	// Epoch 2 is not stored yet, so the waiter must still be blocked.
	select {
	case <-done:
		t.Fatal("Wait returned before the window was ready")
	default:
	}
	require.NoError(t, r.TestPushEpoch(0, weights))
	window := <-done
	require.Equal(t, types.EpochIndex(2), window[len(window)-1].EpochIndex())
}

func TestWaitFailsWhenReadyReportsAnError(t *testing.T) {
	rng := utils.TestRng()
	weights := testWeights(rng, 3)
	r := testRegistry(t, weights, 10)
	errPruned := errors.New("pruned")
	_, err := r.Wait(t.Context(), func([]*types.Epoch) (bool, error) { return false, errPruned })
	require.ErrorIs(t, err, errPruned)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = r.Wait(ctx, func([]*types.Epoch) (bool, error) { return false, nil })
	require.ErrorIs(t, err, context.Canceled)
}
