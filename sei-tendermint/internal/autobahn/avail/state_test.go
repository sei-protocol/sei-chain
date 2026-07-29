package avail

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/memblock"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus/persist"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	pb "github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
	"github.com/stretchr/testify/require"
)

var (
	noBlockCB    = utils.None[func(*types.Signed[*types.LaneProposal])]()
	noCommitQCCB = utils.None[func(*types.CommitQC)]()
)

// registerDuoAtEpoch installs Prev=n-1|Current=n as the state's operating
// window. Seeds those registry epochs only (no restart N+1 lookahead).
func registerDuoAtEpoch(s *State, n types.EpochIndex) {
	r := s.data.Registry()
	if n > 0 {
		r.EnsureEpoch(n - 1)
	}
	r.EnsureEpoch(n)
	duo := utils.OrPanic1(r.DuoAt(epoch.FirstRoad(n)))
	for inner := range s.inner.Lock() {
		inner.epochDuo.Store(duo)
	}
}

// advanceUntilCurrent runs runAdvanceEpoch until Current reaches want, then cancels.
func advanceUntilCurrent(t *testing.T, s *State, want types.EpochIndex) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	errCh := make(chan error, 1)
	go func() { errCh <- s.runAdvanceEpoch(ctx) }()
	_, err := s.epochDuo.Wait(t.Context(), func(duo types.EpochDuo) bool {
		return duo.Current.EpochIndex() >= want
	})
	require.NoError(t, err)
	cancel()
	require.ErrorIs(t, <-errCh, context.Canceled)
}

type byLane[T any] map[types.LaneID][]T

func makeAppVotes(keys []types.SecretKey, proposal *types.AppProposal) []*types.Signed[*types.AppVote] {
	vote := types.NewAppVote(proposal)
	var votes []*types.Signed[*types.AppVote]
	for _, k := range keys {
		votes = append(votes, types.Sign(k, vote))
	}
	return votes
}

func TestSubscribeAppVotesJumpsToDataFloor(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	qc, blocks := data.TestCommitQC(rng, registry.LatestEpoch(), keys, utils.None[*types.CommitQC]())
	gr := qc.QC().GlobalRange()
	require.Greater(t, gr.Len(), uint64(2))

	db := memblock.NewBlockDB()
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, db.WriteQC(qc))
	for i, n := 0, gr.First; n < gr.Next; i, n = i+1, n+1 {
		require.NoError(t, db.WriteBlock(n, blocks[i]))
	}
	require.NoError(t, db.Flush())

	first := gr.First + types.GlobalBlockNumber(gr.Len()/2)
	ds, err := data.NewState(&data.Config{
		Registry:          registry,
		LastExecutedBlock: utils.Some(first),
	}, db)
	require.NoError(t, err)
	appHash := types.GenAppHash(rng)
	require.NoError(t, ds.PushAppHash(t.Context(), first, appHash))

	state, err := NewState(keys[0], ds, utils.None[string]())
	require.NoError(t, err)
	recv := state.SubscribeAppVotes()
	require.Equal(t, types.GlobalBlockNumber(0), recv.next)

	vote, err := recv.Recv(t.Context())
	require.NoError(t, err)
	require.Equal(t, first, vote.Msg().Proposal().GlobalNumber())
}

func makeLaneVotes(keys []types.SecretKey, h *types.BlockHeader) []*types.Signed[*types.LaneVote] {
	var votes []*types.Signed[*types.LaneVote]
	for _, k := range keys {
		votes = append(votes, types.Sign(k, types.NewLaneVote(h)))
	}
	return votes
}

func leaderKey(committee *types.Committee, keys []types.SecretKey, view types.View) types.SecretKey {
	leader := committee.Leader(view)
	for _, k := range keys {
		if k.Public() == leader {
			return k
		}
	}
	panic("leader not in keys")
}

func makeCommitQC(
	ep *types.Epoch,
	keys []types.SecretKey,
	prev utils.Option[*types.CommitQC],
	laneQCs map[types.LaneID]*types.LaneQC,
	appQC utils.Option[*types.AppQC],
) *types.CommitQC {
	return types.BuildCommitQC(ep, keys, prev, laneQCs, appQC)
}

func qcPayloadHashes(qc *types.FullCommitQC) byLane[types.PayloadHash] {
	x := byLane[types.PayloadHash]{}
	for _, h := range qc.Headers() {
		x[h.Lane()] = append(x[h.Lane()], h.PayloadHash())
	}
	return x
}

func TestState(t *testing.T) {
	testState(t, utils.None[string]())
}

// TestStateWithPersistence runs the same flow as TestState but with disk
// persistence enabled. The persist goroutine and prune (triggered by AppQC)
// run concurrently, exercising the cursor-clamp logic that prevents reading
// pruned map entries.
func TestStateWithPersistence(t *testing.T) {
	for range 5 {
		testState(t, utils.Some(t.TempDir()))
	}
}

func testState(t *testing.T, stateDir utils.Option[string]) {
	t.Helper()
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys, _ := epoch.GenRegistry(rng, 3)
	committee := registry.LatestEpoch().Committee()

	if err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		ds := newTestDataState(&data.Config{Registry: registry})
		s.SpawnBgNamed("data.State.Run()", func() error {
			return utils.IgnoreCancel(ds.Run(ctx))
		})
		state, err := NewState(keys[0], ds, stateDir)
		require.NoError(t, err)
		s.SpawnBgNamed("da.State.Run()", func() error {
			return utils.IgnoreCancel(state.Run(ctx))
		})

		for i := range 3 {
			t.Logf("iteration %v", i)
			prev := state.LastCommitQC().Load()

			t.Logf("Push some blocks.")
			want := byLane[types.PayloadHash]{}
			for range 10 {
				key := keys[rng.Intn(len(keys))]
				lane := key.Public()
				p := types.GenPayload(rng)
				want[lane] = append(want[lane], p.Hash())
				b, err := state.produceLocalBlock(state.NextBlock(lane), key, p)
				if err != nil {
					return fmt.Errorf("state.produceLocalBlock(): %w", err)
				}
				if err := utils.TestDiff(b.Msg().Block().Payload(), p); err != nil {
					return fmt.Errorf("snapshot: %w", err)
				}
			}

			t.Logf("Push votes for all the blocks.")
			for lane := range committee.Lanes().All() {
				next := state.NextBlock(lane)
				for i := types.LaneRangeOpt(prev, lane).Next(); i < next; i++ {
					b, err := state.Block(ctx, lane, i)
					if err != nil {
						return fmt.Errorf("state.TryBlock(): %w", err)
					}
					for _, vote := range makeLaneVotes(keys, b.Msg().Block().Header()) {
						if err := state.PushVote(ctx, vote); err != nil {
							return fmt.Errorf("state.PushVote(): %w", err)
						}
					}
				}
			}

			t.Logf("Push a commit QC.")
			laneQCs, _, err := state.WaitForLaneQCs(ctx, prev)
			if err != nil {
				return fmt.Errorf("state.WaitForNewLaneQCs(): %w", err)
			}
			qc := makeCommitQC(registry.EpochAtTip(prev), keys, prev, laneQCs, state.LastAppQC())
			if err := state.PushCommitQC(ctx, qc); err != nil {
				return fmt.Errorf("state.PushCommitQC(): %w", err)
			}

			t.Logf("Push app votes.")
			appProposal := types.NewAppProposal(qc.GlobalRange().Next-1, qc.Proposal().Index(), types.GenAppHash(rng), 0)
			for _, vote := range makeAppVotes(keys, appProposal) {
				if err := state.PushAppVote(ctx, vote); err != nil {
					return fmt.Errorf("state.PushAppVote(): %w", err)
				}
			}

			t.Logf("Previous one should be pruned because of appQC.")
			if _, _, err := state.WaitForAppQC(ctx, appProposal.RoadIndex()); err != nil {
				return fmt.Errorf("state.WaitForAppQC(): %w", err)
			}
			if prev, ok := prev.Get(); ok {
				if _, err := state.CommitQC(ctx, prev.Proposal().Index()); !errors.Is(err, types.ErrPruned) {
					return fmt.Errorf("state.CommitQC(): %w, want %v", err, types.ErrPruned)
				}
			}

			t.Logf("Check that the executed local blocks have been pruned")
			for lane := range committee.Lanes().All() {
				if lr := types.LaneRangeOpt(prev, lane); lr.Next() > 0 {
					if _, err := state.Block(ctx, lane, lr.Next()-1); !errors.Is(err, types.ErrPruned) {
						return fmt.Errorf("state.Block(): %w, want %v", err, types.ErrPruned)
					}
				}
			}

			t.Logf("Check that a CommitQC was successfully reconstructed.")
			got, err := state.fullCommitQC(ctx, qc.Proposal().Index())
			if err != nil {
				return fmt.Errorf("state.fullCommitQC(): %w", err)
			}
			if err := utils.TestDiff(want, qcPayloadHashes(got)); err != nil {
				return fmt.Errorf("snapshot: %w", err)
			}

			t.Logf("Check that the blocks were successfully pushed to data state.")
			gr := got.QC().GlobalRange()
			for i := gr.First; i < gr.Next; i++ {
				b, err := ds.Block(ctx, i)
				if err != nil {
					return fmt.Errorf("ds.Block(): %w", err)
				}
				if err := utils.TestDiff(b.Header(), got.Headers()[i-gr.First]); err != nil {
					return fmt.Errorf("snapshot: %w", err)
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// TestStateRestartFromPersisted runs the state with persistence through 2
// iterations (blocks → votes → commitQC → appQC each), stops, and restarts
// from the same directory. This verifies that what the runtime persist
// goroutine writes can be correctly loaded back by loadPersistedState/newInner.
//
// After iteration 0's AppQC prunes old data, iteration 1 writes new blocks
// and commitQCs at higher indices. If WAL truncation hasn't cleaned up the
// stale entries by shutdown, restart exercises the gap-filtering path in
// loadPersistedState (stale entries below the prune anchor are discarded).
func TestStateRestartFromPersisted(t *testing.T) {
	rng := utils.TestRng()
	registry, keys, _ := epoch.GenRegistry(rng, 3)
	committee := registry.LatestEpoch().Committee()
	dir := t.TempDir()

	// Phase 1: Run state with persistence through 2 iterations.
	var wantAppQCIdx types.RoadIndex
	var wantNextBlocks map[types.LaneID]types.BlockNumber

	require.NoError(t, scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		ds := newTestDataState(&data.Config{Registry: registry})
		s.SpawnBgNamed("data.Run", func() error {
			return utils.IgnoreCancel(ds.Run(ctx))
		})
		state, err := NewState(keys[0], ds, utils.Some(dir))
		if err != nil {
			return err
		}
		s.SpawnBgNamed("avail.Run", func() error {
			return utils.IgnoreCancel(state.Run(ctx))
		})

		for i := range 2 {
			t.Logf("iteration %d", i)
			prev := state.LastCommitQC().Load()

			for range 5 {
				key := keys[rng.Intn(len(keys))]
				if _, err := state.produceLocalBlock(state.NextBlock(key.Public()), key, types.GenPayload(rng)); err != nil {
					return fmt.Errorf("produceLocalBlock: %w", err)
				}
			}

			for lane := range committee.Lanes().All() {
				next := state.NextBlock(lane)
				for n := types.LaneRangeOpt(prev, lane).Next(); n < next; n++ {
					b, err := state.Block(ctx, lane, n)
					if err != nil {
						return fmt.Errorf("Block(%v,%d): %w", lane, n, err)
					}
					for _, vote := range makeLaneVotes(keys, b.Msg().Block().Header()) {
						if err := state.PushVote(ctx, vote); err != nil {
							return fmt.Errorf("PushVote: %w", err)
						}
					}
				}
			}

			laneQCs, _, err := state.WaitForLaneQCs(ctx, prev)
			if err != nil {
				return fmt.Errorf("WaitForLaneQCs: %w", err)
			}
			qc := makeCommitQC(registry.EpochAtTip(prev), keys, prev, laneQCs, state.LastAppQC())
			if err := state.PushCommitQC(ctx, qc); err != nil {
				return fmt.Errorf("PushCommitQC: %w", err)
			}

			appProposal := types.NewAppProposal(qc.GlobalRange().Next-1, qc.Proposal().Index(), types.GenAppHash(rng), 0)
			for _, vote := range makeAppVotes(keys, appProposal) {
				if err := state.PushAppVote(ctx, vote); err != nil {
					return fmt.Errorf("PushAppVote: %w", err)
				}
			}
			if _, _, err := state.WaitForAppQC(ctx, appProposal.RoadIndex()); err != nil {
				return fmt.Errorf("WaitForAppQC: %w", err)
			}
			wantAppQCIdx = appProposal.RoadIndex()
		}

		// Wait for commitQC persistence. markCommitQCsPersisted fires after
		// all commitQCs in the batch are on disk. Block goroutines may still
		// be in flight, but scope.Parallel in runPersist ensures they complete
		// before the next batch, so the data is durable by scope exit.
		if err := state.waitForCommitQC(ctx, wantAppQCIdx); err != nil {
			return fmt.Errorf("waitForCommitQC: %w", err)
		}

		wantNextBlocks = make(map[types.LaneID]types.BlockNumber, committee.Lanes().Len())
		for lane := range committee.Lanes().All() {
			wantNextBlocks[lane] = state.NextBlock(lane)
		}
		return nil
	}))

	// Phase 2: Restart from the same directory.
	ds2 := newTestDataState(&data.Config{Registry: registry})
	state2, err := NewState(keys[0], ds2, utils.Some(dir))
	require.NoError(t, err)

	got, ok := state2.LastAppQC().Get()
	require.True(t, ok, "AppQC should be restored after restart")
	require.Equal(t, wantAppQCIdx, got.Proposal().RoadIndex())

	require.GreaterOrEqual(t, state2.FirstCommitQC(), wantAppQCIdx)

	_, ok = state2.LastCommitQC().Load().Get()
	require.True(t, ok, "LastCommitQC should be set after restart")

	for lane := range committee.Lanes().All() {
		require.Equal(t, wantNextBlocks[lane], state2.NextBlock(lane),
			"NextBlock(%v) should match pre-restart value", lane)
	}
}

func TestStateMismatchedQCs(t *testing.T) {
	rng := utils.TestRng()
	registry, keys, _ := epoch.GenRegistry(rng, 4)
	committee := registry.LatestEpoch().Committee()
	initialBlock := registry.FirstBlock()

	ds := newTestDataState(&data.Config{Registry: registry})
	state, err := NewState(keys[0], ds, utils.Some(t.TempDir()))
	require.NoError(t, err)

	// Helper to create a CommitQC for a specific index
	makeQC := func(prev utils.Option[*types.CommitQC], laneQCs map[types.LaneID]*types.LaneQC) *types.CommitQC {
		vs := types.ViewSpec{CommitQC: prev, Epochs: types.NewEpochDuo(types.NewEpoch(0, types.OpenRoadRange(), committee), utils.None[*types.Epoch]())}
		fullProposal := utils.OrPanic1(types.NewProposal(
			leaderKey(committee, keys, vs.View()),
			vs,
			time.Now(),
			laneQCs,
			utils.None[*types.AppQC](),
		))
		vote := types.NewCommitVote(fullProposal.Proposal().Msg())
		var votes []*types.Signed[*types.CommitVote]
		for _, k := range keys {
			votes = append(votes, types.Sign(k, vote))
		}
		return types.NewCommitQC(votes)
	}

	// 1. Produce a block so we have a non-empty range
	lane := keys[0].Public()
	p := types.GenPayload(rng)
	b, err := state.ProduceLocalBlock(state.NextBlock(lane), p)
	require.NoError(t, err)

	// 2. Form a LaneQC for it
	laneQC := types.NewLaneQC(makeLaneVotes(
		types.TestKeysWithWeight(committee, keys, committee.LaneQuorum()),
		b.Msg().Block().Header(),
	))

	// 3. Create CommitQC for index 0 (finalizes block 0)
	qc0 := makeQC(utils.None[*types.CommitQC](), map[types.LaneID]*types.LaneQC{lane: laneQC})
	require.Equal(t, initialBlock, qc0.GlobalRange().First)
	require.Equal(t, initialBlock+1, qc0.GlobalRange().Next)

	t.Run("PushAppQC mismatch", func(t *testing.T) {
		require := require.New(t)
		// AppQC for index 1, but paired with CommitQC for index 0
		appProposal1 := types.NewAppProposal(initialBlock, 1, types.GenAppHash(rng), 0)
		appQC1 := types.NewAppQC(makeAppVotes(keys, appProposal1))

		err := state.PushAppQC(t.Context(), appQC1, qc0)
		require.Error(err)
	})
}

func TestWaitForAppQC(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistryAt(rng, 4, 0)
	ep0 := utils.OrPanic1(registry.EpochAt(0))
	committee := ep0.Committee()

	ds := newTestDataState(&data.Config{Registry: registry})
	state, err := NewState(keys[0], ds, utils.None[string]())
	require.NoError(t, err)

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	require.ErrorIs(t, state.waitForAppQC(canceled, 0), context.Canceled)

	lane := keys[0].Public()
	b, err := state.ProduceLocalBlock(state.NextBlock(lane), types.GenPayload(rng))
	require.NoError(t, err)
	laneQC := types.NewLaneQC(makeLaneVotes(
		types.TestKeysWithWeight(committee, keys, committee.LaneQuorum()),
		b.Msg().Block().Header(),
	))
	qc0 := makeCommitQC(ep0, keys, utils.None[*types.CommitQC](),
		map[types.LaneID]*types.LaneQC{lane: laneQC}, utils.None[*types.AppQC]())
	require.NoError(t, state.PushCommitQC(ctx, qc0))

	appQC := types.NewAppQC(makeAppVotes(keys, types.NewAppProposal(
		qc0.GlobalRange().Next-1, 0, types.GenAppHash(rng), 0)))

	done := make(chan error, 1)
	go func() { done <- state.waitForAppQC(ctx, 0) }()
	require.NoError(t, state.PushAppQC(ctx, appQC, qc0))
	require.NoError(t, <-done)
	require.NoError(t, state.waitForAppQC(ctx, 0))

	canceled2, cancel2 := context.WithCancel(ctx)
	cancel2()
	require.ErrorIs(t, state.waitForAppQC(canceled2, 1), context.Canceled)
}

// TestPushVote_WaitsForFutureEpochSigner: a voter not yet in Current parks until
// advanceEpoch installs a committee that includes them, then credits.
func TestPushVote_WaitsForFutureEpochSigner(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := t.Context()
		rng := utils.TestRng()
		registry, keys := epoch.GenRegistryAt(rng, 4, 0)
		ds := newTestDataState(&data.Config{Registry: registry})
		state := utils.OrPanic1(NewState(keys[0], ds, utils.None[string]()))

		ep0 := state.epochDuo.Load().Current
		futureKey := types.GenSecretKey(rng)
		lane := futureKey.Public()
		header := types.NewBlock(lane, 0, types.BlockHeaderHash{}, types.GenPayload(rng)).Header()
		vote := types.Sign(futureKey, types.NewLaneVote(header))

		errCh := make(chan error, 1)
		go func() { errCh <- state.PushVote(ctx, vote) }()
		synctest.Wait() // parked: Weight==0 under Current

		weights := map[types.PublicKey]uint64{
			futureKey.Public(): 1000,
			keys[1].Public():   1000,
		}
		ep1 := types.NewEpoch(1, types.RoadRange{First: epoch.FirstRoad(1), Next: epoch.FirstRoad(2)},
			utils.OrPanic1(types.NewCommittee(weights)))
		duo1 := types.NewEpochDuo(ep1, utils.Some(ep0))
		for inner, ctrl := range state.inner.Lock() {
			inner.advanceEpoch(duo1)
			ctrl.Updated()
		}
		synctest.Wait()
		require.NoError(t, <-errCh)

		for inner := range state.inner.Lock() {
			ls, ok := inner.lanes[lane]
			require.True(t, ok)
			require.Contains(t, ls.votes.q[0].byKey, futureKey.Public())
		}
	})
}

// TestPushVote_FutureEpochSignerParks: canceled ctx while signer is not in
// Current returns Canceled (park), not a verify error.
func TestPushVote_FutureEpochSignerParks(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistryAt(rng, 4, 0)
	ds := newTestDataState(&data.Config{Registry: registry})
	state := utils.OrPanic1(NewState(keys[0], ds, utils.None[string]()))

	futureKey := types.GenSecretKey(rng)
	lane := futureKey.Public()
	header := types.NewBlock(lane, 0, types.BlockHeaderHash{}, types.GenPayload(rng)).Header()
	vote := types.Sign(futureKey, types.NewLaneVote(header))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	require.ErrorIs(t, state.PushVote(ctx, vote), context.Canceled)
}

// TestPushVote_DropsSignerAfterEpochAdvance: after verify, capacity WaitUntil
// releases the lock; advanceEpoch installs a Current that excludes the signer —
// the vote must be dropped (Weight==0).
func TestPushVote_DropsSignerAfterEpochAdvance(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := t.Context()
		rng := utils.TestRng()
		registry, keys := epoch.GenRegistryAt(rng, 4, 0)
		ds := newTestDataState(&data.Config{Registry: registry})
		state := utils.OrPanic1(NewState(keys[0], ds, utils.None[string]()))

		ep0 := state.epochDuo.Load().Current
		lane := keys[0].Public()
		n := types.BlockNumber(BlocksPerLane) // WaitUntil: n >= persistedBlockStart+BlocksPerLane
		header := types.NewBlock(lane, n, types.BlockHeaderHash{}, types.GenPayload(rng)).Header()
		vote := types.Sign(keys[0], types.NewLaneVote(header))

		weights := map[types.PublicKey]uint64{}
		for _, k := range keys[1:] {
			weights[k.Public()] = 1000
		}
		ep1 := types.NewEpoch(1, types.RoadRange{First: epoch.FirstRoad(1), Next: epoch.FirstRoad(2)}, utils.OrPanic1(types.NewCommittee(weights)))
		duo1 := types.NewEpochDuo(ep1, utils.Some(ep0))

		errCh := make(chan error, 1)
		go func() { errCh <- state.PushVote(ctx, vote) }()
		synctest.Wait() // blocked in WaitUntil (capacity)

		for inner, ctrl := range state.inner.Lock() {
			inner.advanceEpoch(duo1)
			inner.lanes[lane].persistedBlockStart = 1 // unblock: n < 1+BlocksPerLane
			ctrl.Updated()
		}
		synctest.Wait()
		require.NoError(t, <-errCh)

		for inner := range state.inner.Lock() {
			require.Equal(t, types.EpochIndex(1), inner.epochDuo.Load().Current.EpochIndex())
			require.Equal(t, types.BlockNumber(0), inner.lanes[lane].votes.next,
				"dropped vote must not extend the queue")
		}
	})
}

// TestPushVote_DropsLaneAfterEpochAdvance: after verify, Current advances to a
// committee that retains the signer but not the voted lane.
func TestPushVote_DropsLaneAfterEpochAdvance(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := t.Context()
		rng := utils.TestRng()
		registry, keys := epoch.GenRegistryAt(rng, 4, 0)
		ds := newTestDataState(&data.Config{Registry: registry})
		state := utils.OrPanic1(NewState(keys[0], ds, utils.None[string]()))

		ep0 := state.epochDuo.Load().Current
		lane := keys[1].Public()
		n := types.BlockNumber(BlocksPerLane)
		header := types.NewBlock(lane, n, types.BlockHeaderHash{}, types.GenPayload(rng)).Header()
		vote := types.Sign(keys[0], types.NewLaneVote(header))

		weights := map[types.PublicKey]uint64{
			keys[0].Public(): 1000,
			keys[2].Public(): 1000,
		}
		ep1 := types.NewEpoch(1, types.RoadRange{First: epoch.FirstRoad(1), Next: epoch.FirstRoad(2)},
			utils.OrPanic1(types.NewCommittee(weights)))
		duo1 := types.NewEpochDuo(ep1, utils.Some(ep0))

		errCh := make(chan error, 1)
		go func() { errCh <- state.PushVote(ctx, vote) }()
		synctest.Wait()

		for inner, ctrl := range state.inner.Lock() {
			inner.advanceEpoch(duo1)
			inner.lanes[lane].persistedBlockStart = 1
			ctrl.Updated()
		}
		synctest.Wait()
		require.NoError(t, <-errCh)

		for inner := range state.inner.Lock() {
			require.Equal(t, types.BlockNumber(0), inner.lanes[lane].votes.next,
				"dropped vote must not extend the queue")
		}
	})
}

// TestPushVote_CountsSignerAfterEpochAdvance: same WaitUntil race window, but the
// new Current still includes the signer — count with live committee weights.
func TestPushVote_CountsSignerAfterEpochAdvance(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := t.Context()
		rng := utils.TestRng()
		registry, keys := epoch.GenRegistryAt(rng, 4, 0)
		ds := newTestDataState(&data.Config{Registry: registry})
		state := utils.OrPanic1(NewState(keys[0], ds, utils.None[string]()))

		ep0 := state.epochDuo.Load().Current
		lane := keys[0].Public()
		n := types.BlockNumber(BlocksPerLane)
		header := types.NewBlock(lane, n, types.BlockHeaderHash{}, types.GenPayload(rng)).Header()
		vote := types.Sign(keys[0], types.NewLaneVote(header))

		weights := map[types.PublicKey]uint64{keys[0].Public(): 1000, keys[1].Public(): 1000}
		ep1 := types.NewEpoch(1, types.RoadRange{First: epoch.FirstRoad(1), Next: epoch.FirstRoad(2)}, utils.OrPanic1(types.NewCommittee(weights)))
		duo1 := types.NewEpochDuo(ep1, utils.Some(ep0))

		errCh := make(chan error, 1)
		go func() { errCh <- state.PushVote(ctx, vote) }()
		synctest.Wait()

		for inner, ctrl := range state.inner.Lock() {
			inner.advanceEpoch(duo1)
			inner.lanes[lane].persistedBlockStart = 1
			ctrl.Updated()
		}
		synctest.Wait()
		require.NoError(t, <-errCh)

		for inner := range state.inner.Lock() {
			ls := inner.lanes[lane]
			require.Contains(t, ls.votes.q[n].byKey, keys[0].Public())
			require.Equal(t, uint64(1000), ls.votes.q[n].byHash[header.Hash()].weight)
		}
	})
}

func TestPushBlockRejectsBadParentHash(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys, _ := epoch.GenRegistry(rng, 3)

	ds := newTestDataState(&data.Config{Registry: registry})
	state := utils.OrPanic1(NewState(keys[0], ds, utils.Some(t.TempDir())))

	// Produce a valid first block on our lane.
	_, err := state.ProduceLocalBlock(state.NextBlock(keys[0].Public()), types.GenPayload(rng))
	require.NoError(t, err)

	// Create a second block with a fake parentHash.
	lane := keys[0].Public()
	fakeBlock := types.NewBlock(lane, 1, types.GenBlockHeaderHash(rng), types.GenPayload(rng))
	fakeProp := types.Sign(keys[0], types.NewLaneProposal(fakeBlock))

	// Producer equivocation is logged but not returned as an error.
	require.NoError(t, state.PushBlock(ctx, fakeProp))
	// Queue did not advance — the bad block was dropped.
	require.Equal(t, types.BlockNumber(1), state.NextBlock(lane))
}

func TestPushBlockRejectsWrongSigner(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys, _ := epoch.GenRegistry(rng, 3)

	ds := newTestDataState(&data.Config{Registry: registry})
	state := utils.OrPanic1(NewState(keys[0], ds, utils.Some(t.TempDir())))

	// Create a block on keys[0]'s lane but sign it with keys[1].
	lane := keys[0].Public()
	block := types.NewBlock(lane, 0, types.GenBlockHeaderHash(rng), types.GenPayload(rng))
	prop := types.Sign(keys[1], types.NewLaneProposal(block))

	err := state.PushBlock(ctx, prop)
	require.Error(t, err)
}

func TestNewStateWithPersistence(t *testing.T) {
	rng := utils.TestRng()
	// Road-0 CommitQC chains: pin genesis epoch so LatestEpoch matches roads.
	registry, keys := epoch.GenRegistryAt(rng, 4, 0)
	initialBlock := registry.FirstBlock()

	t.Run("empty dir loads fresh state", func(t *testing.T) {
		dir := t.TempDir()
		ds := newTestDataState(&data.Config{Registry: registry})

		state, err := NewState(keys[0], ds, utils.Some(dir))
		require.NoError(t, err)

		// No persisted AppQC → None.
		require.False(t, state.LastAppQC().IsPresent())
		// Queues start at 0.
		require.Equal(t, types.RoadIndex(0), state.FirstCommitQC())
	})

	t.Run("loads persisted AppQC", func(t *testing.T) {
		dir := t.TempDir()
		ds := newTestDataState(&data.Config{Registry: registry})

		roadIdx := types.RoadIndex(7)
		globalNum := types.GlobalBlockNumber(50)
		appProposal := types.NewAppProposal(globalNum, roadIdx, types.GenAppHash(rng), 0)
		appQC := types.NewAppQC(makeAppVotes(keys, appProposal))

		// Persist commitQCs 0-7 so the matching one at roadIdx exists.
		cp, _, err := persist.NewCommitQCPersister(utils.Some(dir))
		require.NoError(t, err)
		prev := utils.None[*types.CommitQC]()
		var pruneQC *types.CommitQC
		for i := types.RoadIndex(0); i <= roadIdx; i++ {
			qc := makeCommitQC(registry.EpochAtTip(prev), keys, prev, nil, utils.None[*types.AppQC]())
			prev = utils.Some(qc)
			require.NoError(t, cp.MaybePruneAndPersist(utils.None[*types.CommitQC](), []*types.CommitQC{qc}, noCommitQCCB))
			pruneQC = qc
		}

		// Persist prune anchor (AppQC + CommitQC pair).
		prunePers, _, err := persist.NewPersister[*pb.PersistedAvailPruneAnchor](utils.Some(dir), innerFile)
		require.NoError(t, err)
		require.NoError(t, prunePers.Persist(&pb.PersistedAvailPruneAnchor{
			AppQc:    types.AppQCConv.Encode(appQC),
			CommitQc: types.CommitQCConv.Encode(pruneQC),
		}))

		state, err := NewState(keys[0], ds, utils.Some(dir))
		require.NoError(t, err)

		aq := state.LastAppQC()
		got, ok := aq.Get()
		require.True(t, ok)
		require.Equal(t, roadIdx, got.Proposal().RoadIndex())
		require.Equal(t, globalNum, got.Proposal().GlobalNumber())

		require.Equal(t, roadIdx, state.FirstCommitQC())
	})

	t.Run("loads persisted blocks", func(t *testing.T) {
		dir := t.TempDir()
		ds := newTestDataState(&data.Config{Registry: registry})
		lane := keys[0].Public()

		// Persist blocks using BlockPersister.
		bp, _, err := persist.NewBlockPersister(utils.Some(dir))
		require.NoError(t, err)

		var parent types.BlockHeaderHash
		for n := range types.BlockNumber(3) {
			block := types.NewBlock(lane, n, parent, types.GenPayload(rng))
			signed := types.Sign(keys[0], types.NewLaneProposal(block))
			parent = block.Header().Hash()
			require.NoError(t, bp.MaybePruneAndPersistLane(lane, utils.None[*types.CommitQC](), []*types.Signed[*types.LaneProposal]{signed}, noBlockCB))
		}

		// Now construct state — it should load the blocks.
		state, err := NewState(keys[0], ds, utils.Some(dir))
		require.NoError(t, err)

		require.Equal(t, types.BlockNumber(3), state.NextBlock(lane))
	})

	t.Run("loads persisted AppQC and blocks together", func(t *testing.T) {
		dir := t.TempDir()
		ds := newTestDataState(&data.Config{Registry: registry})
		lane := keys[0].Public()

		roadIdx := types.RoadIndex(2)
		globalNum := types.GlobalBlockNumber(5)
		appProposal := types.NewAppProposal(globalNum, roadIdx, types.GenAppHash(rng), 0)
		appQC := types.NewAppQC(makeAppVotes(keys, appProposal))

		// Persist commitQCs 0-2 so the matching one at roadIdx exists.
		cp, _, err := persist.NewCommitQCPersister(utils.Some(dir))
		require.NoError(t, err)
		prev := utils.None[*types.CommitQC]()
		var pruneQC *types.CommitQC
		for range roadIdx + 1 {
			qc := makeCommitQC(registry.EpochAtTip(prev), keys, prev, nil, utils.None[*types.AppQC]())
			prev = utils.Some(qc)
			require.NoError(t, cp.MaybePruneAndPersist(utils.None[*types.CommitQC](), []*types.CommitQC{qc}, noCommitQCCB))
			pruneQC = qc
		}

		// Persist prune anchor (AppQC + CommitQC pair).
		prunePers, _, err := persist.NewPersister[*pb.PersistedAvailPruneAnchor](utils.Some(dir), innerFile)
		require.NoError(t, err)
		require.NoError(t, prunePers.Persist(&pb.PersistedAvailPruneAnchor{
			AppQc:    types.AppQCConv.Encode(appQC),
			CommitQc: types.CommitQCConv.Encode(pruneQC),
		}))

		// Persist blocks starting at 0 (nil laneQCs → lr.First()=0 after prune).
		bp, _, err := persist.NewBlockPersister(utils.Some(dir))
		require.NoError(t, err)

		var parent types.BlockHeaderHash
		for n := range types.BlockNumber(3) {
			block := types.NewBlock(lane, n, parent, types.GenPayload(rng))
			signed := types.Sign(keys[0], types.NewLaneProposal(block))
			parent = block.Header().Hash()
			require.NoError(t, bp.MaybePruneAndPersistLane(lane, utils.None[*types.CommitQC](), []*types.Signed[*types.LaneProposal]{signed}, noBlockCB))
		}

		state, err := NewState(keys[0], ds, utils.Some(dir))
		require.NoError(t, err)

		got, ok := state.LastAppQC().Get()
		require.True(t, ok)
		require.Equal(t, roadIdx, got.Proposal().RoadIndex())

		require.Equal(t, types.BlockNumber(3), state.NextBlock(lane))
		require.Equal(t, roadIdx, state.FirstCommitQC())
	})

	t.Run("loads persisted commitQCs", func(t *testing.T) {
		dir := t.TempDir()
		ds := newTestDataState(&data.Config{Registry: registry})

		// Persist CommitQCs to disk.
		cp, _, err := persist.NewCommitQCPersister(utils.Some(dir))
		require.NoError(t, err)

		qcs := make([]*types.CommitQC, 3)
		prev := utils.None[*types.CommitQC]()
		for i := range qcs {
			qcs[i] = makeCommitQC(registry.EpochAtTip(prev), keys, prev, nil, utils.None[*types.AppQC]())
			prev = utils.Some(qcs[i])
			require.NoError(t, cp.MaybePruneAndPersist(utils.None[*types.CommitQC](), []*types.CommitQC{qcs[i]}, noCommitQCCB))
		}

		state, err := NewState(keys[0], ds, utils.Some(dir))
		require.NoError(t, err)

		// All 3 commitQCs should be loaded (no AppQC to skip past).
		require.Equal(t, types.RoadIndex(0), state.FirstCommitQC())
		// LastCommitQC should be set to the last loaded one.
		require.NoError(t, utils.TestDiff(utils.Some(qcs[2]), state.LastCommitQC().Load()))
	})

	t.Run("loads persisted commitQCs with AppQC", func(t *testing.T) {
		dir := t.TempDir()
		ds := newTestDataState(&data.Config{Registry: registry})

		// Persist AppQC at road index 1.
		roadIdx := types.RoadIndex(1)
		globalNum := types.GlobalBlockNumber(5)
		appProposal := types.NewAppProposal(globalNum, roadIdx, types.GenAppHash(rng), 0)
		appQC := types.NewAppQC(makeAppVotes(keys, appProposal))

		// Persist CommitQCs 0-4.
		cp, _, err := persist.NewCommitQCPersister(utils.Some(dir))
		require.NoError(t, err)

		qcs := make([]*types.CommitQC, 5)
		prev := utils.None[*types.CommitQC]()
		for i := range qcs {
			qcs[i] = makeCommitQC(registry.EpochAtTip(prev), keys, prev, nil, utils.None[*types.AppQC]())
			prev = utils.Some(qcs[i])
			require.NoError(t, cp.MaybePruneAndPersist(utils.None[*types.CommitQC](), []*types.CommitQC{qcs[i]}, noCommitQCCB))
		}

		// Persist prune anchor (AppQC + CommitQC pair at roadIdx).
		prunePers, _, err := persist.NewPersister[*pb.PersistedAvailPruneAnchor](utils.Some(dir), innerFile)
		require.NoError(t, err)
		require.NoError(t, prunePers.Persist(&pb.PersistedAvailPruneAnchor{
			AppQc:    types.AppQCConv.Encode(appQC),
			CommitQc: types.CommitQCConv.Encode(qcs[roadIdx]),
		}))

		state, err := NewState(keys[0], ds, utils.Some(dir))
		require.NoError(t, err)

		// inner.prune(appQC@1, commitQC@1) sets commitQCs.first = 1.
		require.Equal(t, types.RoadIndex(1), state.FirstCommitQC())
		require.NoError(t, utils.TestDiff(utils.Some(qcs[4]), state.LastCommitQC().Load()))
	})

	t.Run("non-contiguous commitQC files return error", func(t *testing.T) {
		dir := t.TempDir()

		// Build 6 sequential CommitQCs (indices 0-5).
		allQCs := make([]*types.CommitQC, 6)
		prev := utils.None[*types.CommitQC]()
		for i := range allQCs {
			allQCs[i] = makeCommitQC(registry.EpochAtTip(prev), keys, prev, nil, utils.None[*types.AppQC]())
			prev = utils.Some(allQCs[i])
		}

		// Persist prune anchor (AppQC + CommitQC pair at road index 0).
		appProposal := types.NewAppProposal(initialBlock, 0, types.GenAppHash(rng), 0)
		appQC := types.NewAppQC(makeAppVotes(keys, appProposal))
		prunePers, _, err := persist.NewPersister[*pb.PersistedAvailPruneAnchor](utils.Some(dir), innerFile)
		require.NoError(t, err)
		require.NoError(t, prunePers.Persist(&pb.PersistedAvailPruneAnchor{
			AppQc:    types.AppQCConv.Encode(appQC),
			CommitQc: types.CommitQCConv.Encode(allQCs[0]),
		}))

		// Persist QCs 0, 1, 2 contiguously, then try to skip to 5.
		// MaybePruneAndPersist enforces strict sequential order, so the gap
		// is caught at write time rather than at load time.
		cp, _, err := persist.NewCommitQCPersister(utils.Some(dir))
		require.NoError(t, err)
		for i := range 3 {
			require.NoError(t, cp.MaybePruneAndPersist(utils.None[*types.CommitQC](), []*types.CommitQC{allQCs[i]}, noCommitQCCB))
		}
		err = cp.MaybePruneAndPersist(utils.None[*types.CommitQC](), []*types.CommitQC{allQCs[5]}, noCommitQCCB)
		require.Error(t, err)
		require.Contains(t, err.Error(), "out of sequence")
		require.NoError(t, cp.Close())
	})

	t.Run("anchor past all persisted commitQCs truncates WAL", func(t *testing.T) {
		dir := t.TempDir()
		ds := newTestDataState(&data.Config{Registry: registry})

		// Build a chain of 10 CommitQCs (indices 0-9).
		qcs := make([]*types.CommitQC, 10)
		prev := utils.None[*types.CommitQC]()
		for i := range qcs {
			qcs[i] = makeCommitQC(registry.EpochAtTip(prev), keys, prev, nil, utils.None[*types.AppQC]())
			prev = utils.Some(qcs[i])
		}

		// Persist only indices 0-4 to the CommitQC WAL.
		cp, _, err := persist.NewCommitQCPersister(utils.Some(dir))
		require.NoError(t, err)
		for i := range 5 {
			require.NoError(t, cp.MaybePruneAndPersist(utils.None[*types.CommitQC](), []*types.CommitQC{qcs[i]}, noCommitQCCB))
		}
		require.NoError(t, cp.Close())

		// Persist a prune anchor at index 9 — well past the persisted range.
		appProposal := types.NewAppProposal(50, 9, types.GenAppHash(rng), 0)
		appQC := types.NewAppQC(makeAppVotes(keys, appProposal))
		prunePers, _, err := persist.NewPersister[*pb.PersistedAvailPruneAnchor](utils.Some(dir), innerFile)
		require.NoError(t, err)
		require.NoError(t, prunePers.Persist(&pb.PersistedAvailPruneAnchor{
			AppQc:    types.AppQCConv.Encode(appQC),
			CommitQc: types.CommitQCConv.Encode(qcs[9]),
		}))

		// NewState should succeed: MaybePruneAndPersist truncates the stale WAL
		// and internally re-persists the anchor's CommitQC for crash recovery.
		state, err := NewState(keys[0], ds, utils.Some(dir))
		require.NoError(t, err)

		require.Equal(t, types.RoadIndex(9), state.FirstCommitQC())
		require.NoError(t, utils.TestDiff(utils.Some(qcs[9]), state.LastCommitQC().Load()))

		got, ok := state.LastAppQC().Get()
		require.True(t, ok)
		require.Equal(t, types.RoadIndex(9), got.Proposal().RoadIndex())
	})

	t.Run("anchor past all persisted blocks truncates lane WAL", func(t *testing.T) {
		dir := t.TempDir()
		ds := newTestDataState(&data.Config{Registry: registry})
		lane := keys[0].Public()

		// Persist commitQCs 0-9 and blocks 0-2 for one lane.
		qcs := make([]*types.CommitQC, 10)
		prev := utils.None[*types.CommitQC]()
		cp, _, err := persist.NewCommitQCPersister(utils.Some(dir))
		require.NoError(t, err)
		for i := range qcs {
			qcs[i] = makeCommitQC(registry.EpochAtTip(prev), keys, prev, nil, utils.None[*types.AppQC]())
			prev = utils.Some(qcs[i])
			require.NoError(t, cp.MaybePruneAndPersist(utils.None[*types.CommitQC](), []*types.CommitQC{qcs[i]}, noCommitQCCB))
		}
		require.NoError(t, cp.Close())

		bp, _, err := persist.NewBlockPersister(utils.Some(dir))
		require.NoError(t, err)
		var parent types.BlockHeaderHash
		for n := range types.BlockNumber(3) {
			block := types.NewBlock(lane, n, parent, types.GenPayload(rng))
			signed := types.Sign(keys[0], types.NewLaneProposal(block))
			parent = block.Header().Hash()
			require.NoError(t, bp.MaybePruneAndPersistLane(lane, utils.None[*types.CommitQC](), []*types.Signed[*types.LaneProposal]{signed}, noBlockCB))
		}

		// Persist a prune anchor at index 9 with a laneRange that starts past
		// all persisted blocks — MaybePruneAndPersistLane will TruncateAll the block WAL.
		appProposal := types.NewAppProposal(50, 9, types.GenAppHash(rng), 0)
		appQC := types.NewAppQC(makeAppVotes(keys, appProposal))
		prunePers, _, err := persist.NewPersister[*pb.PersistedAvailPruneAnchor](utils.Some(dir), innerFile)
		require.NoError(t, err)
		require.NoError(t, prunePers.Persist(&pb.PersistedAvailPruneAnchor{
			AppQc:    types.AppQCConv.Encode(appQC),
			CommitQc: types.CommitQCConv.Encode(qcs[9]),
		}))

		// NewState should succeed: block WAL gets truncated, lane starts clean.
		state, err := NewState(keys[0], ds, utils.Some(dir))
		require.NoError(t, err)

		require.Equal(t, types.RoadIndex(9), state.FirstCommitQC())
		got, ok := state.LastAppQC().Get()
		require.True(t, ok)
		require.Equal(t, types.RoadIndex(9), got.Proposal().RoadIndex())
	})

	t.Run("corrupt AppQC data returns error", func(t *testing.T) {
		dir := t.TempDir()
		ds := newTestDataState(&data.Config{Registry: registry})

		// Create a throwaway persister to discover the A/B filenames,
		// then corrupt them so NewState fails on load.
		_, _, err := persist.NewPersister[*pb.PersistedAvailPruneAnchor](utils.Some(dir), innerFile)
		require.NoError(t, err)
		entries, err := os.ReadDir(dir)
		require.NoError(t, err)
		for _, e := range entries {
			require.NoError(t, os.WriteFile(filepath.Join(dir, e.Name()), []byte("corrupt"), 0600))
		}

		_, err = NewState(keys[0], ds, utils.Some(dir))
		require.Error(t, err)
	})
}

func TestWaitForLaneQCs_OnlyReturnsCommitteeLanes(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()

	committeeKey := types.GenSecretKey(rng)
	committee := utils.OrPanic1(types.NewCommittee(map[types.PublicKey]uint64{
		committeeKey.Public(): 1,
	}))
	registry := utils.OrPanic1(epoch.NewRegistry(committee, 0, time.Time{}))
	ep := registry.LatestEpoch()

	ds := newTestDataState(&data.Config{Registry: registry})
	state, err := NewState(committeeKey, ds, utils.None[string]())
	require.NoError(t, err)

	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBgNamed("data.Run", func() error { return utils.IgnoreCancel(ds.Run(ctx)) })
		s.SpawnBgNamed("avail.Run", func() error { return utils.IgnoreCancel(state.Run(ctx)) })

		// Produce and vote on a block for the committee lane.
		b, err := state.produceLocalBlock(0, committeeKey, types.GenPayload(rng))
		if err != nil {
			return err
		}
		for _, vote := range makeLaneVotes([]types.SecretKey{committeeKey}, b.Msg().Block().Header()) {
			if err := state.PushVote(ctx, vote); err != nil {
				return err
			}
		}

		laneQCs, gotEp, err := state.WaitForLaneQCs(ctx, utils.None[*types.CommitQC]())
		if err != nil {
			return err
		}
		if gotEp.EpochIndex() != ep.EpochIndex() {
			return fmt.Errorf("WaitForLaneQCs returned epoch %d, want %d", gotEp.EpochIndex(), ep.EpochIndex())
		}
		for lane := range laneQCs {
			if !ep.Committee().HasLane(lane) {
				return fmt.Errorf("WaitForLaneQCs returned lane %v outside committee", lane)
			}
		}
		if _, ok := laneQCs[committeeKey.Public()]; !ok {
			return fmt.Errorf("WaitForLaneQCs missing committee lane")
		}
		return nil
	}))
}

func TestPushAppQCOutsideWindowDrops(t *testing.T) {
	rng := utils.TestRng()
	registry, keys, m := epoch.GenRegistryTip(rng, 4)
	registry.EnsureEpoch(m + 1)
	epPrev := utils.OrPanic1(registry.EpochAt(epoch.FirstRoad(m - 1)))

	ds := newTestDataState(&data.Config{Registry: registry})
	state, err := NewState(keys[0], ds, utils.None[string]())
	require.NoError(t, err)

	parentTip := types.NewCommitQC([]*types.Signed[*types.CommitVote]{
		types.Sign(keys[0], types.NewCommitVote(types.ProposalAt(epPrev, types.View{
			EpochIndex: m - 1,
			Index:      epoch.LastRoad(m-1) - 1,
		}))),
	})
	qcPrev := makeCommitQC(epPrev, keys, utils.Some(parentTip), nil, utils.None[*types.AppQC]())
	appQC := types.NewAppQC(makeAppVotes(keys, types.NewAppProposal(
		qcPrev.GlobalRange().First, qcPrev.Index(), types.GenAppHash(rng), epPrev.EpochIndex())))

	// Slide Current to M+1 so M-1 falls behind WindowFirst.
	registerDuoAtEpoch(state, m+1)

	require.NoError(t, state.PushAppQC(t.Context(), appQC, qcPrev))
	require.False(t, state.LastAppQC().IsPresent())
}

func TestPushAppVoteFutureWaitsForCommitQC(t *testing.T) {
	rng := utils.TestRng()
	registry, keys, m := epoch.GenRegistryTip(rng, 4)
	ds := newTestDataState(&data.Config{Registry: registry})
	state, err := NewState(keys[0], ds, utils.None[string]())
	require.NoError(t, err)

	// AppVote for Current's first road while CommitQC tip is still behind:
	// PushAppVote parks in waitForCommitQC (not on the epoch window).
	epM := utils.OrPanic1(registry.EpochAt(epoch.FirstRoad(m)))
	proposal := types.NewAppProposal(
		registry.FirstBlock(), epM.RoadRange().First, types.GenAppHash(rng), epM.EpochIndex())
	vote := types.Sign(keys[0], types.NewAppVote(proposal))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	require.ErrorIs(t, state.PushAppVote(ctx, vote), context.Canceled)
}

// TestPushAppVoteFarFutureParks: an unregistered far-future RoadIndex parks
// (waitForCommitQC) rather than failing EpochAt up front.
func TestPushAppVoteFarFutureParks(t *testing.T) {
	rng := utils.TestRng()
	registry, keys, m := epoch.GenRegistryTip(rng, 4)
	ds := newTestDataState(&data.Config{Registry: registry})
	state, err := NewState(keys[0], ds, utils.None[string]())
	require.NoError(t, err)

	far := epoch.FirstRoad(m + 10)
	proposal := types.NewAppProposal(0, far, types.GenAppHash(rng), m+10)
	vote := types.Sign(keys[0], types.NewAppVote(proposal))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	require.ErrorIs(t, state.PushAppVote(ctx, vote), context.Canceled)
}

// TestWaitCurrentVsDuoRoad: a road in Prev is too late for Current-only
// admission (CommitQC), even though duo admission would still resolve it.
func TestWaitCurrentVsDuoRoad(t *testing.T) {
	rng := utils.TestRng()
	registry, keys, m := epoch.GenRegistryTip(rng, 4)
	ds := newTestDataState(&data.Config{Registry: registry})
	state, err := NewState(keys[0], ds, utils.None[string]())
	require.NoError(t, err)

	registerDuoAtEpoch(state, m) // Prev=M-1|Current=M

	roadInPrev := epoch.FirstRoad(m - 1)
	_, err = state.waitForEpochDuo(t.Context(), roadInPrev)
	require.NoError(t, err, "Prev|Current window still covers Prev roads")

	_, err = state.waitForEpoch(t.Context(), roadInPrev)
	require.ErrorIs(t, err, types.ErrPruned, "Current-only wait must treat Prev roads as too late")
}

func TestPushCommitQCStaleDrops(t *testing.T) {
	rng := utils.TestRng()
	registry, keys, m := epoch.GenRegistryTip(rng, 4)
	registry.EnsureEpoch(m + 1)
	epPrev := utils.OrPanic1(registry.EpochAt(epoch.FirstRoad(m - 1)))

	ds := newTestDataState(&data.Config{Registry: registry})
	state, err := NewState(keys[0], ds, utils.None[string]())
	require.NoError(t, err)

	parentTip := types.NewCommitQC([]*types.Signed[*types.CommitVote]{
		types.Sign(keys[0], types.NewCommitVote(types.ProposalAt(epPrev, types.View{
			EpochIndex: m - 1,
			Index:      epoch.LastRoad(m-1) - 1,
		}))),
	})
	qcStale := makeCommitQC(epPrev, keys, utils.Some(parentTip), nil, utils.None[*types.AppQC]())
	require.Equal(t, epoch.LastRoad(m-1), qcStale.Proposal().Index())

	state.markCommitQCsPersisted(parentTip)
	registerDuoAtEpoch(state, m+1)

	tipBefore := state.LastCommitQC().Load()
	require.NoError(t, state.PushCommitQC(t.Context(), qcStale))
	require.Equal(t, tipBefore, state.LastCommitQC().Load(), "stale CommitQC must not advance tip")
}

// TestFullCommitQCBeforeWindowIsPruned: CommitQC still held but duo has moved
// past its road (ErrRoadBeforeWindow) — ErrPruned so the export loop can jump.
// Live admit should not leave unpruned before-window roads (boundary needs
// AppQC in E); this force-slides the duo to exercise the mapping.
func TestFullCommitQCBeforeWindowIsPruned(t *testing.T) {
	rng := utils.TestRng()
	registry, keys, m := epoch.GenRegistryTip(rng, 4)
	registry.EnsureEpoch(m + 1)
	registry.EnsureEpoch(m + 2)
	epM := utils.OrPanic1(registry.EpochAt(epoch.FirstRoad(m)))
	epPrev := utils.OrPanic1(registry.EpochAt(epoch.FirstRoad(m - 1)))

	ds := newTestDataState(&data.Config{Registry: registry})
	state, err := NewState(keys[0], ds, utils.None[string]())
	require.NoError(t, err)

	prevTip := types.NewCommitQC([]*types.Signed[*types.CommitVote]{
		types.Sign(keys[0], types.NewCommitVote(types.ProposalAt(epPrev, types.View{
			EpochIndex: m - 1,
			Index:      epoch.LastRoad(m - 1),
		}))),
	})
	qc := makeCommitQC(epM, keys, utils.Some(prevTip), nil, utils.None[*types.AppQC]())
	require.Equal(t, epoch.FirstRoad(m), qc.Proposal().Index())

	// Plant an admitted QC, then slide the duo past it (skip live PushCommitQC).
	for inner := range state.inner.Lock() {
		inner.commitQCs.first = epoch.FirstRoad(m)
		inner.commitQCs.next = epoch.FirstRoad(m) + 1
		inner.commitQCs.q[epoch.FirstRoad(m)] = qc
	}
	state.markCommitQCsPersisted(qc)
	registerDuoAtEpoch(state, m+2)

	_, err = state.fullCommitQC(t.Context(), epoch.FirstRoad(m))
	require.ErrorIs(t, err, types.ErrPruned)
}

// TestFullCommitQCAfterWindowHardFails: road ahead of Current is unexpected for
// an admitted CommitQC — ErrRoadAfterWindow, not a wait.
func TestFullCommitQCAfterWindowHardFails(t *testing.T) {
	rng := utils.TestRng()
	registry, keys, m := epoch.GenRegistryTip(rng, 4)
	epM := utils.OrPanic1(registry.EpochAt(epoch.FirstRoad(m)))
	epPrev := utils.OrPanic1(registry.EpochAt(epoch.FirstRoad(m - 1)))

	ds := newTestDataState(&data.Config{Registry: registry})
	state, err := NewState(keys[0], ds, utils.None[string]())
	require.NoError(t, err)

	// Operating window still on Prev; plant a CommitQC for Current's first road.
	registerDuoAtEpoch(state, m-1)

	prevTip := types.NewCommitQC([]*types.Signed[*types.CommitVote]{
		types.Sign(keys[0], types.NewCommitVote(types.ProposalAt(epPrev, types.View{
			EpochIndex: m - 1,
			Index:      epoch.LastRoad(m - 1),
		}))),
	})
	qc1 := makeCommitQC(epM, keys, utils.Some(prevTip), nil, utils.None[*types.AppQC]())
	require.Equal(t, epoch.FirstRoad(m), qc1.Proposal().Index())

	for inner := range state.inner.Lock() {
		inner.commitQCs.first = epoch.FirstRoad(m)
		inner.commitQCs.next = epoch.FirstRoad(m) + 1
		inner.commitQCs.q[epoch.FirstRoad(m)] = qc1
	}
	state.markCommitQCsPersisted(qc1)

	_, err = state.fullCommitQC(t.Context(), epoch.FirstRoad(m))
	require.ErrorIs(t, err, types.ErrRoadAfterWindow)
	require.NotErrorIs(t, err, types.ErrPruned)
}

// TestPushCommitQCMidEpochNoExecLeash: mid-M CommitQC admits without registry
// M+1 (execution may still be in M-1). Only sealing M waits on M+1.
func TestPushCommitQCMidEpochNoExecLeash(t *testing.T) {
	rng := utils.TestRng()
	registry, keys, m := epoch.GenRegistryTip(rng, 4)
	epPrev := utils.OrPanic1(registry.EpochAt(epoch.FirstRoad(m - 1)))
	epM := utils.OrPanic1(registry.EpochAt(epoch.FirstRoad(m)))
	_, err := registry.EpochAt(epoch.FirstRoad(m + 1))
	require.Error(t, err, "test setup: M+1 must be absent")

	ds := newTestDataState(&data.Config{Registry: registry})
	state, err := NewState(keys[0], ds, utils.None[string]())
	require.NoError(t, err)

	registerDuoAtEpoch(state, m)

	prevTip := types.NewCommitQC([]*types.Signed[*types.CommitVote]{
		types.Sign(keys[0], types.NewCommitVote(types.ProposalAt(epPrev, types.View{
			EpochIndex: m - 1,
			Index:      epoch.LastRoad(m - 1),
		}))),
	})
	qc1 := makeCommitQC(epM, keys, utils.Some(prevTip), nil, utils.None[*types.AppQC]())
	require.Equal(t, epoch.FirstRoad(m), qc1.Proposal().Index())

	for inner := range state.inner.Lock() {
		inner.commitQCs.first = epoch.FirstRoad(m)
		inner.commitQCs.next = epoch.FirstRoad(m)
	}
	state.markCommitQCsPersisted(prevTip)

	require.NoError(t, state.PushCommitQC(t.Context(), qc1))
	for inner := range state.inner.Lock() {
		require.Equal(t, epoch.FirstRoad(m)+1, inner.commitQCs.next)
	}
}

// TestPushCommitQCWaitsForEpochUnlock: seal admit waits on registry M+1
// (execution leash) before inserting the last CommitQC of M.
func TestPushCommitQCWaitsForEpochUnlock(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := t.Context()
		rng := utils.TestRng()
		registry, keys, m := epoch.GenRegistryTip(rng, 4)
		epPrev := utils.OrPanic1(registry.EpochAt(epoch.FirstRoad(m - 1)))
		epM := utils.OrPanic1(registry.EpochAt(epoch.FirstRoad(m)))
		_, err := registry.EpochAt(epoch.FirstRoad(m + 1))
		require.Error(t, err, "test setup: M+1 must be absent")

		ds := newTestDataState(&data.Config{Registry: registry})
		state, err := NewState(keys[0], ds, utils.None[string]())
		require.NoError(t, err)

		registerDuoAtEpoch(state, m)

		qcMid := makeCommitQC(epM, keys, utils.Some(types.NewCommitQC([]*types.Signed[*types.CommitVote]{
			types.Sign(keys[0], types.NewCommitVote(types.ProposalAt(epPrev, types.View{
				EpochIndex: m - 1,
				Index:      epoch.LastRoad(m - 1),
			}))),
		})), nil, utils.None[*types.AppQC]())
		appQCM := types.NewAppQC(makeAppVotes(keys, types.NewAppProposal(
			qcMid.GlobalRange().First, qcMid.Index(), types.GenAppHash(rng), epM.EpochIndex())))

		prevOnLast := types.NewCommitQC([]*types.Signed[*types.CommitVote]{
			types.Sign(keys[0], types.NewCommitVote(types.ProposalAt(epM, types.View{
				EpochIndex: m,
				Index:      epoch.LastRoad(m) - 1,
			}))),
		})
		qcLast := makeCommitQC(epM, keys, utils.Some(prevOnLast), nil, utils.None[*types.AppQC]())
		require.Equal(t, epoch.LastRoad(m), qcLast.Proposal().Index())

		for inner := range state.inner.Lock() {
			inner.latestAppQC = utils.Some(appQCM)
			inner.commitQCs.first = epoch.LastRoad(m)
			inner.commitQCs.next = epoch.LastRoad(m)
		}
		state.markCommitQCsPersisted(prevOnLast)

		errCh := make(chan error, 1)
		go func() { errCh <- state.PushCommitQC(ctx, qcLast) }()
		synctest.Wait() // parked on WaitForDuo(M+1)
		for inner := range state.inner.Lock() {
			require.Equal(t, epoch.LastRoad(m), inner.commitQCs.next, "not admitted until exec leash")
		}

		registry.EnsureEpoch(m + 1)
		synctest.Wait()
		require.NoError(t, <-errCh)
		for inner := range state.inner.Lock() {
			require.Equal(t, epoch.LastRoad(m)+1, inner.commitQCs.next)
		}
	})
}

// TestPushAppQCWaitsForEpochUnlock: seal PushAppQC admits after AppQC (prune
// leash satisfied by incoming) but waits on registry M+1 (execution leash).
func TestPushAppQCWaitsForEpochUnlock(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := t.Context()
		rng := utils.TestRng()
		registry, keys, m := epoch.GenRegistryTip(rng, 4)
		epM := utils.OrPanic1(registry.EpochAt(epoch.FirstRoad(m)))
		_, err := registry.EpochAt(epoch.FirstRoad(m + 1))
		require.Error(t, err, "test setup: M+1 must be absent")

		ds := newTestDataState(&data.Config{Registry: registry})
		state, err := NewState(keys[0], ds, utils.None[string]())
		require.NoError(t, err)

		registerDuoAtEpoch(state, m)

		prevOnLast := types.NewCommitQC([]*types.Signed[*types.CommitVote]{
			types.Sign(keys[0], types.NewCommitVote(types.ProposalAt(epM, types.View{
				EpochIndex: m,
				Index:      epoch.LastRoad(m) - 1,
			}))),
		})
		qcLast := makeCommitQC(epM, keys, utils.Some(prevOnLast), nil, utils.None[*types.AppQC]())
		appQCLast := types.NewAppQC(makeAppVotes(keys, types.NewAppProposal(
			qcLast.GlobalRange().First, qcLast.Index(), types.GenAppHash(rng), epM.EpochIndex())))
		require.Equal(t, epoch.LastRoad(m), qcLast.Proposal().Index())

		for inner := range state.inner.Lock() {
			inner.commitQCs.first = epoch.LastRoad(m)
			inner.commitQCs.next = epoch.LastRoad(m)
		}
		state.markCommitQCsPersisted(prevOnLast)

		errCh := make(chan error, 1)
		go func() { errCh <- state.PushAppQC(ctx, appQCLast, qcLast) }()
		synctest.Wait()
		for inner := range state.inner.Lock() {
			require.Equal(t, epoch.LastRoad(m), inner.commitQCs.next, "not admitted until exec leash")
		}

		registry.EnsureEpoch(m + 1)
		synctest.Wait()
		require.NoError(t, <-errCh)
		for inner := range state.inner.Lock() {
			require.Equal(t, epoch.LastRoad(m)+1, inner.commitQCs.next)
		}
	})
}

// TestPushCommitQCBoundaryWaitsForAppQCInEpoch: last CommitQC of epoch M is not
// admitted until AppQC for M exists (prune leash on CommitQC admission).
func TestPushCommitQCBoundaryWaitsForAppQCInEpoch(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := t.Context()
		rng := utils.TestRng()
		registry, keys, m := epoch.GenRegistryTip(rng, 4)
		registry.EnsureEpoch(m + 1) // exec leash for sealing M
		epPrev := utils.OrPanic1(registry.EpochAt(epoch.FirstRoad(m - 1)))
		epM := utils.OrPanic1(registry.EpochAt(epoch.FirstRoad(m)))

		ds := newTestDataState(&data.Config{Registry: registry})
		state, err := NewState(keys[0], ds, utils.None[string]())
		require.NoError(t, err)

		registerDuoAtEpoch(state, m)

		qcPrev := makeCommitQC(epPrev, keys, utils.Some(types.NewCommitQC([]*types.Signed[*types.CommitVote]{
			types.Sign(keys[0], types.NewCommitVote(types.ProposalAt(epPrev, types.View{
				EpochIndex: m - 1,
				Index:      epoch.LastRoad(m-1) - 1,
			}))),
		})), nil, utils.None[*types.AppQC]())
		appQCPrev := types.NewAppQC(makeAppVotes(keys, types.NewAppProposal(
			qcPrev.GlobalRange().First, qcPrev.Index(), types.GenAppHash(rng), epPrev.EpochIndex())))

		prevOnLast := types.NewCommitQC([]*types.Signed[*types.CommitVote]{
			types.Sign(keys[0], types.NewCommitVote(types.ProposalAt(epM, types.View{
				EpochIndex: m,
				Index:      epoch.LastRoad(m) - 1,
			}))),
		})
		qcLast := makeCommitQC(epM, keys, utils.Some(prevOnLast), nil, utils.None[*types.AppQC]())
		require.Equal(t, epoch.LastRoad(m), qcLast.Proposal().Index())

		for inner := range state.inner.Lock() {
			inner.latestAppQC = utils.Some(appQCPrev) // only M-1 — not enough to close M
			inner.commitQCs.first = epoch.LastRoad(m)
			inner.commitQCs.next = epoch.LastRoad(m)
		}
		state.markCommitQCsPersisted(prevOnLast)

		errCh := make(chan error, 1)
		go func() { errCh <- state.PushCommitQC(ctx, qcLast) }()
		synctest.Wait()
		for inner := range state.inner.Lock() {
			require.Equal(t, epoch.LastRoad(m), inner.commitQCs.next, "not admitted without AppQC in M")
		}

		qcM := makeCommitQC(epM, keys, utils.Some(types.NewCommitQC([]*types.Signed[*types.CommitVote]{
			types.Sign(keys[0], types.NewCommitVote(types.ProposalAt(epPrev, types.View{
				EpochIndex: m - 1,
				Index:      epoch.LastRoad(m - 1),
			}))),
		})), nil, utils.None[*types.AppQC]())
		appQCM := types.NewAppQC(makeAppVotes(keys, types.NewAppProposal(
			qcM.GlobalRange().First, qcM.Index(), types.GenAppHash(rng), epM.EpochIndex())))
		for inner, ctrl := range state.inner.Lock() {
			inner.latestAppQC = utils.Some(appQCM)
			ctrl.Updated()
		}
		synctest.Wait()
		require.NoError(t, <-errCh)
		for inner := range state.inner.Lock() {
			require.Equal(t, epoch.LastRoad(m)+1, inner.commitQCs.next)
		}
	})
}

// TestPushCommitQCEpoch0SealWaitsForAppQC: no epoch-0 exemption — admitting
// LastRoad(0) waits for AppQC before insert.
func TestPushCommitQCEpoch0SealWaitsForAppQC(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := t.Context()
		rng := utils.TestRng()
		registry, keys := epoch.GenRegistryAt(rng, 4, 0)
		registry.EnsureEpoch(1) // exec leash for sealing 0
		ep0 := utils.OrPanic1(registry.EpochAt(0))

		ds := newTestDataState(&data.Config{Registry: registry})
		state, err := NewState(keys[0], ds, utils.None[string]())
		require.NoError(t, err)

		prevOnLast := types.NewCommitQC([]*types.Signed[*types.CommitVote]{
			types.Sign(keys[0], types.NewCommitVote(types.ProposalAt(ep0, types.View{
				EpochIndex: 0,
				Index:      epoch.LastRoad(0) - 1,
			}))),
		})
		qcLast := makeCommitQC(ep0, keys, utils.Some(prevOnLast), nil, utils.None[*types.AppQC]())
		require.Equal(t, epoch.LastRoad(0), qcLast.Proposal().Index())

		for inner := range state.inner.Lock() {
			inner.commitQCs.first = epoch.LastRoad(0)
			inner.commitQCs.next = epoch.LastRoad(0)
		}
		state.markCommitQCsPersisted(prevOnLast)

		errCh := make(chan error, 1)
		go func() { errCh <- state.PushCommitQC(ctx, qcLast) }()
		synctest.Wait()
		for inner := range state.inner.Lock() {
			require.Equal(t, epoch.LastRoad(0), inner.commitQCs.next)
		}

		appQC0 := types.NewAppQC(makeAppVotes(keys, types.NewAppProposal(
			0, 0, types.GenAppHash(rng), 0)))
		for inner, ctrl := range state.inner.Lock() {
			inner.latestAppQC = utils.Some(appQC0)
			ctrl.Updated()
		}
		synctest.Wait()
		require.NoError(t, <-errCh)
		for inner := range state.inner.Lock() {
			require.Equal(t, epoch.LastRoad(0)+1, inner.commitQCs.next)
		}
	})
}

// TestPushAppQCBoundaryIncomingAppQC: tipcut closing M may carry the first
// AppQC in M; prune-before-insert makes it visible to runAdvanceEpoch.
func TestPushAppQCBoundaryIncomingAppQC(t *testing.T) {
	rng := utils.TestRng()
	registry, keys, m := epoch.GenRegistryTip(rng, 4)
	registry.EnsureEpoch(m + 1)
	epPrev := utils.OrPanic1(registry.EpochAt(epoch.FirstRoad(m - 1)))
	epM := utils.OrPanic1(registry.EpochAt(epoch.FirstRoad(m)))

	ds := newTestDataState(&data.Config{Registry: registry})
	state, err := NewState(keys[0], ds, utils.None[string]())
	require.NoError(t, err)

	registerDuoAtEpoch(state, m)

	qcPrev := makeCommitQC(epPrev, keys, utils.Some(types.NewCommitQC([]*types.Signed[*types.CommitVote]{
		types.Sign(keys[0], types.NewCommitVote(types.ProposalAt(epPrev, types.View{
			EpochIndex: m - 1,
			Index:      epoch.LastRoad(m-1) - 1,
		}))),
	})), nil, utils.None[*types.AppQC]())
	appQCPrev := types.NewAppQC(makeAppVotes(keys, types.NewAppProposal(
		qcPrev.GlobalRange().First, qcPrev.Index(), types.GenAppHash(rng), epPrev.EpochIndex())))

	prevOnLast := types.NewCommitQC([]*types.Signed[*types.CommitVote]{
		types.Sign(keys[0], types.NewCommitVote(types.ProposalAt(epM, types.View{
			EpochIndex: m,
			Index:      epoch.LastRoad(m) - 1,
		}))),
	})
	qcLast := makeCommitQC(epM, keys, utils.Some(prevOnLast), nil, utils.None[*types.AppQC]())
	appQCLast := types.NewAppQC(makeAppVotes(keys, types.NewAppProposal(
		qcLast.GlobalRange().First, qcLast.Index(), types.GenAppHash(rng), epM.EpochIndex())))

	for inner := range state.inner.Lock() {
		inner.latestAppQC = utils.Some(appQCPrev) // stale; PushAppQC prune installs appQCLast
		inner.commitQCs.first = epoch.LastRoad(m)
		inner.commitQCs.next = epoch.LastRoad(m)
	}
	state.markCommitQCsPersisted(prevOnLast)

	require.NoError(t, state.PushAppQC(t.Context(), appQCLast, qcLast))
	for inner := range state.inner.Lock() {
		require.Equal(t, epoch.LastRoad(m)+1, inner.commitQCs.next)
		require.Equal(t, m, inner.epochDuo.Load().Current.EpochIndex())
	}
	advanceUntilCurrent(t, state, m+1)
}

// TestEpochAdvanceGapHandoff: LastRoad(N) Push leaves Current at N; FirstRoad(N+1)
// parks until runAdvanceEpoch slides the duo.
func TestEpochAdvanceGapHandoff(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rng := utils.TestRng()
		registry, keys, m := epoch.GenRegistryTip(rng, 4)
		registry.EnsureEpoch(m + 1)
		epPrev := utils.OrPanic1(registry.EpochAt(epoch.FirstRoad(m - 1)))
		epM := utils.OrPanic1(registry.EpochAt(epoch.FirstRoad(m)))
		epNext := utils.OrPanic1(registry.EpochAt(epoch.FirstRoad(m + 1)))

		ds := newTestDataState(&data.Config{Registry: registry})
		state, err := NewState(keys[0], ds, utils.None[string]())
		require.NoError(t, err)
		registerDuoAtEpoch(state, m)

		qcMid := makeCommitQC(epM, keys, utils.Some(types.NewCommitQC([]*types.Signed[*types.CommitVote]{
			types.Sign(keys[0], types.NewCommitVote(types.ProposalAt(epPrev, types.View{
				EpochIndex: m - 1,
				Index:      epoch.LastRoad(m - 1),
			}))),
		})), nil, utils.None[*types.AppQC]())
		appQCM := types.NewAppQC(makeAppVotes(keys, types.NewAppProposal(
			qcMid.GlobalRange().First, qcMid.Index(), types.GenAppHash(rng), epM.EpochIndex())))

		prevOnLast := types.NewCommitQC([]*types.Signed[*types.CommitVote]{
			types.Sign(keys[0], types.NewCommitVote(types.ProposalAt(epM, types.View{
				EpochIndex: m,
				Index:      epoch.LastRoad(m) - 1,
			}))),
		})
		qcLast := makeCommitQC(epM, keys, utils.Some(prevOnLast), nil, utils.None[*types.AppQC]())
		qcNext := makeCommitQC(epNext, keys, utils.Some(qcLast), nil, utils.None[*types.AppQC]())
		require.Equal(t, epoch.FirstRoad(m+1), qcNext.Proposal().Index())

		for inner := range state.inner.Lock() {
			inner.latestAppQC = utils.Some(appQCM)
			inner.commitQCs.first = epoch.FirstRoad(m)
			inner.commitQCs.next = epoch.LastRoad(m)
		}
		state.markCommitQCsPersisted(prevOnLast)

		require.NoError(t, state.PushCommitQC(t.Context(), qcLast))
		require.Equal(t, m, state.epochDuo.Load().Current.EpochIndex())
		state.markCommitQCsPersisted(qcLast) // satisfy waitForCommitQC for FirstRoad(m+1)

		advCtx, advCancel := context.WithCancel(t.Context())
		advErr := make(chan error, 1)
		go func() { advErr <- state.runAdvanceEpoch(advCtx) }()

		require.NoError(t, state.PushCommitQC(t.Context(), qcNext))
		require.Equal(t, m+1, state.epochDuo.Load().Current.EpochIndex())
		advCancel()
		require.ErrorIs(t, <-advErr, context.Canceled)
	})
}

func TestPushCommitQCFutureWaitsForCurrent(t *testing.T) {
	rng := utils.TestRng()
	registry, keys, m := epoch.GenRegistryTip(rng, 4)
	epPrev := utils.OrPanic1(registry.EpochAt(epoch.FirstRoad(m - 1)))
	epM := utils.OrPanic1(registry.EpochAt(epoch.FirstRoad(m)))

	ds := newTestDataState(&data.Config{Registry: registry})
	state, err := NewState(keys[0], ds, utils.None[string]())
	require.NoError(t, err)

	registerDuoAtEpoch(state, m-1)

	// Satisfy waitForCommitQC(FirstRoad(m)-1) without pushing EpochLength QCs.
	// Current remains M-1, so FirstRoad(m) is too early for waitForEpoch.
	tipQC := types.NewCommitQC([]*types.Signed[*types.CommitVote]{
		types.Sign(keys[0], types.NewCommitVote(types.ProposalAt(epPrev, types.View{
			EpochIndex: m - 1,
			Index:      epoch.LastRoad(m - 1),
		}))),
	})
	state.markCommitQCsPersisted(tipQC)

	qcM := types.NewCommitQC([]*types.Signed[*types.CommitVote]{
		types.Sign(keys[0], types.NewCommitVote(types.ProposalAt(epM, types.View{
			EpochIndex: m,
			Index:      epoch.FirstRoad(m),
		}))),
	})

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, state.PushCommitQC(ctx, qcM), context.Canceled)
}

// TestPushAppQCPreviousEpoch verifies that a late AppQC whose road falls in
// Prev is accepted after Current has advanced. Its committee is resolved from
// Prev, not Current.
func TestPushAppQCPreviousEpoch(t *testing.T) {
	rng := utils.TestRng()
	registry, keys, m := epoch.GenRegistryTip(rng, 3)
	epPrev := utils.OrPanic1(registry.EpochAt(epoch.FirstRoad(m - 1)))

	prevTip := types.NewCommitQC([]*types.Signed[*types.CommitVote]{
		types.Sign(keys[0], types.NewCommitVote(types.ProposalAt(epPrev, types.View{
			EpochIndex: m - 1,
			Index:      epoch.LastRoad(m-1) - 1,
		}))),
	})
	commitQC := makeCommitQC(epPrev, keys, utils.Some(prevTip), nil, utils.None[*types.AppQC]())
	gr := commitQC.GlobalRange()
	appProposal := types.NewAppProposal(gr.First, commitQC.Index(), types.GenAppHash(rng), epPrev.EpochIndex())
	appQC := types.NewAppQC(makeAppVotes(keys, appProposal))

	ds := newTestDataState(&data.Config{Registry: registry})
	state, err := NewState(keys[0], ds, utils.None[string]())
	require.NoError(t, err)

	registerDuoAtEpoch(state, m)

	require.NoError(t, state.PushAppQC(t.Context(), appQC, commitQC),
		"late AppQC from Prev should be accepted after Current advanced")
	require.True(t, state.LastAppQC().IsPresent())
}

func TestNextCommitQC(t *testing.T) {
	rng := utils.TestRng()
	registry, keys, _ := epoch.GenRegistry(rng, 4)
	require.Equal(t, types.RoadIndex(0), (&loadedAvailState{}).nextCommitQC())

	qcs := make([]*types.CommitQC, 10)
	prev := utils.None[*types.CommitQC]()
	for i := range qcs {
		qcs[i] = makeCommitQC(registry.EpochAtTip(prev), keys, prev, nil, utils.None[*types.AppQC]())
		prev = utils.Some(qcs[i])
	}
	require.Equal(t, types.RoadIndex(3), (&loadedAvailState{
		commitQCs: []persist.LoadedCommitQC{
			{Index: 0, QC: qcs[0]},
			{Index: 1, QC: qcs[1]},
			{Index: 2, QC: qcs[2]},
		},
	}).nextCommitQC())

	// Loaded tip ahead of prune anchor → tip from last QC.
	gr1 := qcs[1].GlobalRange()
	appQC1 := types.NewAppQC(makeAppVotes(keys, types.NewAppProposal(gr1.First, qcs[1].Index(), types.GenAppHash(rng), 0)))
	require.Equal(t, types.RoadIndex(3), (&loadedAvailState{
		pruneAnchor: utils.Some(&PruneAnchor{AppQC: appQC1, CommitQC: qcs[1]}),
		commitQCs: []persist.LoadedCommitQC{
			{Index: 1, QC: qcs[1]},
			{Index: 2, QC: qcs[2]},
		},
	}).nextCommitQC())

	// WAL empty / lagging behind prune anchor → tip from max(., anchor+1).
	gr9 := qcs[9].GlobalRange()
	appQC9 := types.NewAppQC(makeAppVotes(keys, types.NewAppProposal(gr9.First, qcs[9].Index(), types.GenAppHash(rng), 0)))
	require.Equal(t, types.RoadIndex(10), (&loadedAvailState{
		pruneAnchor: utils.Some(&PruneAnchor{AppQC: appQC9, CommitQC: qcs[9]}),
	}).nextCommitQC())
	require.Equal(t, types.RoadIndex(10), (&loadedAvailState{
		pruneAnchor: utils.Some(&PruneAnchor{AppQC: appQC9, CommitQC: qcs[9]}),
		commitQCs: []persist.LoadedCommitQC{
			{Index: 0, QC: qcs[0]},
			{Index: 1, QC: qcs[1]},
		},
	}).nextCommitQC(), "stale WAL below anchor must not win over anchor tipcut")
}
