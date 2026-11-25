package data

import (
	"context"
	"fmt"
	"slices"
	"testing"

	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

// TestRecoveryEmpty verifies that NewState is a no-op on a fresh BlockStore.
func TestRecoveryEmpty(t *testing.T) {
	rng := utils.TestRng()
	registry, _ := epoch.GenRegistry(rng, 3)
	dir := t.TempDir()
	fb := registry.MustEpoch(0).RoadRange().First

	db := newTestBlockStore(t, dir)
	state := newTestState(t, &Config{App: abevm.TestEpochApp(registry)}, db)
	require.Equal(t, fb, state.First())
	require.Equal(t, fb, state.NextQC())
	for inner := range state.inner.Lock() {
		require.Equal(t, fb, inner.nextPartialBlock)
		require.Equal(t, fb, inner.nextAppProposal)
	}
}

// TestNewStateRun verifies that NewState followed by Run serves QCs and blocks.
func TestNewStateRun(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	qc1, blocks1 := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.None[*types.CommitQC]())

	store := newTestStore(t)
	state := utils.OrPanic1(NewState(&Config{App: abevm.TestEpochApp(registry)}, store))

	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBgNamed("state", func() error { return utils.IgnoreCancel(state.Run(ctx)) })
		if err := state.PushPartialBlock(ctx, qc1, TestBlockMap(blocks1)); err != nil {
			return err
		}
		// Verify data is accessible (no panic, no error).
		if _, err := state.Block(ctx, types.LaneBlockPos{Height: qc1.Height()}); err != nil {
			return err
		}
		return nil
	}))
}

// TestRecoveryNormal verifies that NewState fully restores QCs and blocks
// from BlockDB on restart.
func TestRecoveryNormal(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	ep0 := registry.MustEpoch(0)
	dir := t.TempDir()

	qc1, blocks1 := TestCommitQC(rng, ep0, keys, utils.None[*types.CommitQC]())
	qc2, blocks2 := TestCommitQC(rng, ep0, keys, utils.Some(qc1.QC()))

	// Session 1: write both QCs and all blocks.
	db1 := newTestBlockStore(t, dir)
	writeToBlockStore(t, db1,
		[]*types.FullCommitQC{qc1, qc2},
		[][]*types.LaneBlock{blocks1, blocks2})
	require.NoError(t, db1.Close())

	// Session 2: NewState should recover blocks and QCs.
	db2 := newTestBlockStore(t, dir)
	state2 := newTestState(t, &Config{App: abevm.TestEpochApp(registry)}, db2)

	require.Equal(t, qc2.Height()+1, state2.NextQC())
	for n := qc1.Height(); n < qc2.Height()+1; n++ {
		got, err := state2.TryBlock(types.LaneBlockPos{Height: n})
		require.NoError(t, err)
		require.NotNil(t, got)
	}
	for n := qc1.Height(); n < qc2.Height()+1; n++ {
		got, err := state2.QC(t.Context(), n)
		require.NoError(t, err)
		require.NotNil(t, got)
	}
	require.NoError(t, db2.Close())

	// Session 3: verify session 2 did not corrupt BlockDB.
	db3 := newTestBlockStore(t, dir)
	state3 := newTestState(t, &Config{App: abevm.TestEpochApp(registry)}, db3)
	require.Equal(t, qc2.Height()+1, state3.NextQC())
}

func TestRecoveryRejectsMissingFirstCommittedBlock(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	qc, blocks := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.None[*types.CommitQC]())
	dir := t.TempDir()
	db := newTestBlockStore(t, dir)
	require.NoError(t, db.WriteBlock(types.NewRoadBlock(qc, blocks[1:])))
	require.NoError(t, db.Flush())
	require.NoError(t, db.Close())
	db = newTestBlockStore(t, dir)
	_, err := NewState(&Config{App: abevm.TestEpochApp(registry)}, db)
	require.ErrorIs(t, err, types.ErrQCBlocksMismatch)
}

func TestRecoveryLeavesAppTipBelowPruneFloorUnreadable(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	ep0 := registry.MustEpoch(0)
	qc1, blocks1 := TestCommitQC(rng, ep0, keys, utils.None[*types.CommitQC]())
	qc2, blocks2 := TestCommitQC(rng, ep0, keys, utils.Some(qc1.QC()))

	db := newTestBlockStore(t, t.TempDir())
	writeToBlockStore(t, db,
		[]*types.FullCommitQC{qc1, qc2},
		[][]*types.LaneBlock{blocks1, blocks2})
	writeAppDataToBlockStore(t, rng, db, keys, qc1, qc2)
	require.NoError(t, db.PruneBefore(qc2.Height()))

	state, err := NewState(&Config{App: abevm.TestEpochApp(registry)}, db)
	require.NoError(t, err)
	_, err = state.SuperBlock(t.Context(), qc1.Height())
	require.ErrorIs(t, err, types.ErrPruned)
}

// TestPruningDiscards verifies that PruneBefore advances BlockDB's watermark but
// does not discard RAM-retained blocks. Memory is cleared only by the AppQC
// eviction floor.
func TestPruningDiscards(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	ep0 := registry.MustEpoch(0)

	qc1, blocks1 := TestCommitQC(rng, ep0, keys, utils.None[*types.CommitQC]())
	qc2, blocks2 := TestCommitQC(rng, ep0, keys, utils.Some(qc1.QC()))
	qc3, blocks3 := TestCommitQC(rng, ep0, keys, utils.Some(qc2.QC()))

	state := newTestState(t, &Config{App: abevm.TestEpochApp(registry)}, newTestBlockStore(t, t.TempDir()))
	require.NoError(t, state.PushPartialBlock(ctx, qc1, TestBlockMap(blocks1)))
	require.NoError(t, state.PushPartialBlock(ctx, qc2, TestBlockMap(blocks2)))
	require.NoError(t, state.PushPartialBlock(ctx, qc3, TestBlockMap(blocks3)))

	// Execute all blocks so they are eligible for pruning.
	require.NoError(t, pushAppHashesRunning(ctx, state, rng, qc1.Height(), qc3.Height()+1))

	// Prune qc1 entirely (keep from qc2 onward).
	require.NoError(t, state.PruneBefore(qc2.Height()))

	got, err := state.TryBlock(types.LaneBlockPos{Height: qc1.Height()})
	require.NoError(t, err)
	require.NotNil(t, got)
	for n := qc2.Height(); n < qc3.Height()+1; n++ {
		got, err := state.TryBlock(types.LaneBlockPos{Height: n})
		require.NoError(t, err)
		require.NotNil(t, got)
	}
}

// TestRecoveryAfterPruning verifies that NewState recovers correctly when
// BlockDB only contains data from a later QC range (as left by pruning + GC).
func TestRecoveryAfterPruning(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	ep0 := registry.MustEpoch(0)
	dir := t.TempDir()

	qc1, _ := TestCommitQC(rng, ep0, keys, utils.None[*types.CommitQC]())
	qc2, blocks2 := TestCommitQC(rng, ep0, keys, utils.Some(qc1.QC()))
	qc3, blocks3 := TestCommitQC(rng, ep0, keys, utils.Some(qc2.QC()))

	// Write only qc2 and qc3 — simulating a DB where qc1 was pruned and GC'd.
	db1 := newTestBlockStore(t, dir)
	writeToBlockStore(t, db1,
		[]*types.FullCommitQC{qc2, qc3},
		[][]*types.LaneBlock{blocks2, blocks3})
	require.NoError(t, db1.Close())

	// Recovery skipTo(qc2.Height()); qc1 heights are absent from BlockDB → ErrPruned.
	// With no AppQC yet, first stays at the recovery floor (not advanced to 0),
	// so below-floor reads fall through to BlockDB instead of nil maps.
	db2 := newTestBlockStore(t, dir)
	state2 := newTestState(t, &Config{App: abevm.TestEpochApp(registry)}, db2)

	for inner := range state2.inner.Lock() {
		require.Equal(t, qc2.Height(), inner.first)
	}
	require.Equal(t, qc3.Height()+1, state2.NextQC())
	_, err := state2.TryBlock(types.LaneBlockPos{Height: qc1.Height()})
	require.ErrorIs(t, err, types.ErrPruned)
	_, err = state2.QC(t.Context(), qc1.Height())
	require.ErrorIs(t, err, types.ErrPruned)
	_, err = state2.SuperBlock(t.Context(), qc1.Height())
	require.ErrorIs(t, err, types.ErrPruned)
	for n := qc2.Height(); n < qc3.Height()+1; n++ {
		got, err := state2.TryBlock(types.LaneBlockPos{Height: n})
		require.NoError(t, err)
		require.NotNil(t, got)
	}
}

// TestRecoveryRejectsIncompleteQC verifies that an incomplete road block prevents recovery.
func TestRecoveryRejectsIncompleteQC(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	ep0 := registry.MustEpoch(0)
	qc1, blocks1 := TestCommitQC(rng, ep0, keys, utils.None[*types.CommitQC]())
	qc2, blocks2 := TestCommitQC(rng, ep0, keys, utils.Some(qc1.QC()))
	dir := t.TempDir()
	db := newTestBlockStore(t, dir)
	require.NoError(t, db.WriteBlock(types.NewRoadBlock(qc1, blocks1)))
	require.NoError(t, db.WriteBlock(types.NewRoadBlock(qc2, blocks2[:len(blocks2)-1])))
	require.NoError(t, db.Flush())
	require.NoError(t, db.Close())
	db = newTestBlockStore(t, dir)
	_, err := NewState(&Config{App: abevm.TestEpochApp(registry)}, db)
	require.ErrorIs(t, err, types.ErrQCBlocksMismatch)
}

// TestRecoveryRejectsIncompleteFirstBlockAfterPruning verifies that recovery still verifies the
// first stored road block when its parent is pruned.
func TestRecoveryRejectsIncompleteFirstBlockAfterPruning(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	ep0 := registry.MustEpoch(0)
	qc1, _ := TestCommitQC(rng, ep0, keys, utils.None[*types.CommitQC]())
	qc2, blocks2 := TestCommitQC(rng, ep0, keys, utils.Some(qc1.QC()))
	store := newTestStore(t)
	writeToBlockStore(t, store, []*types.FullCommitQC{qc2}, [][]*types.LaneBlock{blocks2[:len(blocks2)-1]})
	_, err := NewState(&Config{App: abevm.TestEpochApp(registry)}, store)
	require.ErrorIs(t, err, types.ErrQCBlocksMismatch)
}

func TestRecoveryAfterPruneNoGC(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	ep0 := registry.MustEpoch(0)
	dir := t.TempDir()

	qc1, blocks1 := TestCommitQC(rng, ep0, keys, utils.None[*types.CommitQC]())
	qc2, blocks2 := TestCommitQC(rng, ep0, keys, utils.Some(qc1.QC()))

	// Write both QCs and all their blocks to the store.
	store1 := newTestBlockStore(t, dir)
	writeToBlockStore(t, store1, []*types.FullCommitQC{qc1, qc2}, [][]*types.LaneBlock{blocks1, blocks2})

	// Prune qc1's range. GC is NOT called — pruned entries remain on disk.
	require.NoError(t, store1.PruneBefore(qc2.Height()))
	require.NoError(t, store1.Close())

	// Reopen the same dir without ForceGC — pruned entries may still be present.
	store2 := newTestBlockStore(t, dir)

	// NewState must succeed — below-watermark blocks never outlive their QCs
	// because blocks and QCs share the same GC filter in blockstore. Without GC,
	// all entries are still present and recovery treats the store as unpruned.
	// This is the PruneBefore-without-GC path: the watermark advanced but
	// physical reclamation has not happened yet, so the store looks like a
	// fresh store containing all data from qc1 and qc2.
	state := newTestState(t, &Config{App: abevm.TestEpochApp(registry)}, store2)

	// Without GC all data is still present; qc1 and qc2 blocks are accessible.
	for n := qc1.Height(); n < qc2.Height()+1; n++ {
		_, err := state.TryBlock(types.LaneBlockPos{Height: n})
		require.NoError(t, err)
	}
}

// TestRecoveryRejectsQCWithoutBlocks verifies that recovery rejects a QC missing its lane blocks.
func TestRecoveryRejectsQCWithoutBlocks(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	qc, _ := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.None[*types.CommitQC]())
	dir := t.TempDir()
	db := newTestBlockStore(t, dir)
	require.NoError(t, db.WriteBlock(types.NewRoadBlock(qc, nil)))
	require.NoError(t, db.Flush())
	require.NoError(t, db.Close())
	db = newTestBlockStore(t, dir)
	_, err := NewState(&Config{App: abevm.TestEpochApp(registry)}, db)
	require.ErrorIs(t, err, types.ErrQCBlocksMismatch)
}

// TestRunPersistSeedsFromRecoveryFloor verifies that runPersist does not walk
// [genesis, recoveryFloor) when the first persisted QC starts past FirstBlock.
func TestRunPersistSeedsFromRecoveryFloor(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	ep0 := registry.MustEpoch(0)
	dir := t.TempDir()

	qc1, _ := TestCommitQC(rng, ep0, keys, utils.None[*types.CommitQC]())
	qc2, blocks2 := TestCommitQC(rng, ep0, keys, utils.Some(qc1.QC()))
	require.Greater(t, qc2.Height(), ep0.RoadRange().First, "need skipTo past genesis")

	// First WriteBlock on an empty DB may start past genesis (crash / partial retain).
	db1 := newTestBlockStore(t, dir)
	require.NoError(t, db1.WriteBlock(types.NewRoadBlock(qc2, blocks2)))
	require.NoError(t, db1.Flush())
	require.NoError(t, db1.Close())

	db2 := newTestBlockStore(t, dir)
	tips := db2.Status().OrPanic("non-empty BlockDB status")
	require.Equal(t, qc2.Height(), tips.First)
	require.Equal(t, qc2.Height()+1, tips.NextBlock)
	require.NotZero(t, tips.NextBlock)

	state := newTestState(t, &Config{App: abevm.TestEpochApp(registry)}, db2)
	require.Equal(t, qc2.Height()+1, state.NextQC())

	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		runCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		s.SpawnBgNamed("state.Run", func() error {
			return utils.IgnoreCancel(state.Run(runCtx))
		})
		if err := state.PushAppHash(ctx, qc2.Height(), types.GenAppHash(rng)); err != nil {
			return err
		}
		return nil
	}))
}

// TestRecoveryBlockGap verifies that recovery rejects a missing lane block.
func TestRecoveryBlockGap(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	qc, blocks := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.None[*types.CommitQC]())
	db := newTestBlockStore(t, t.TempDir())
	mid := len(blocks) / 2
	gapped := append(slices.Clone(blocks[:mid]), blocks[mid+1:]...)
	require.NoError(t, db.WriteBlock(types.NewRoadBlock(qc, gapped)))
	_, err := NewState(&Config{App: abevm.TestEpochApp(registry)}, db)
	require.ErrorIs(t, err, types.ErrQCBlocksMismatch)
}

// TestPushAppHashDoesNotDeriveTheClosingEpoch pins that recording an app hash
// leaves the registry unchanged. Putting TryPushEpoch back into PushAppHash
// makes this push return an error: a closing road has no weights here.
func TestPushAppHashDoesNotDeriveTheClosingEpoch(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	qc, blocks := commitQCAtRoad(registry.MustEpoch(0), keys, registry.LastRoad(0))
	store := newTestStore(t)
	writeToBlockStore(t, store, []*types.FullCommitQC{qc}, [][]*types.LaneBlock{blocks})
	state := newTestState(t, &Config{App: abevm.TestEpochApp(registry)}, store)
	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBgNamed("state.Run()", func() error { return utils.IgnoreCancel(state.Run(ctx)) })
		return state.PushAppHash(ctx, qc.Height(), types.GenAppHash(rng))
	}))
	_, err := registry.Epoch(2)
	require.ErrorIs(t, err, types.ErrNotFound)
}

func TestNewState_PreservesExecutionDerivedEpoch(t *testing.T) {
	for _, certified := range []bool{false, true} {
		t.Run(fmt.Sprint(certified), func(t *testing.T) {
			rng := utils.TestRng()
			registry, keys := epoch.GenRegistry(rng, 3)
			ep := registry.MustEpoch(0)
			store := newTestStore(t)
			qc, blocks := commitQCAtRoad(ep, keys, registry.LastRoad(0))
			writeToBlockStore(t, store, []*types.FullCommitQC{qc}, [][]*types.LaneBlock{blocks})
			require.NoError(t, registry.TestTryPushEpoch(qc.Height(), map[types.PublicKey]uint64{keys[0].Public(): 9}))
			if certified {
				writeAppDataToBlockStore(t, rng, store, keys, qc)
			}
			state := newTestState(t, &Config{App: abevm.TestEpochApp(registry)}, store)
			require.Equal(t, uint64(9), registry.MustEpoch(2).Committee().Weight(keys[0].Public()))
			require.Equal(t, [2]*types.Epoch{ep, registry.MustEpoch(1)}, state.Anchor().Load().Epochs)
			require.Equal(t, certified, state.Anchor().Load().First.IsPresent())
		})
	}
}

func TestRecoveryAfterGenesisEpochsPruned(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistryThrough(rng, 3, 3)
	ep := registry.MustEpoch(2)
	qc, blocks := commitQCAtRoad(ep, keys, ep.RoadRange().First)
	dir := t.TempDir()
	store := newTestBlockStore(t, dir)
	writeToBlockStore(t, store, []*types.FullCommitQC{qc}, [][]*types.LaneBlock{blocks})
	require.NoError(t, store.Close())
	// Deriving through epoch 3 keeps epochs 1, 2, and 3. Epoch 0 is already gone.
	_, err := registry.Epoch(0)
	require.ErrorIs(t, err, types.ErrPruned)
	store = newTestBlockStore(t, dir)
	state, err := NewState(&Config{App: abevm.TestEpochApp(registry)}, store)
	require.NoError(t, err)
	require.Equal(t, qc.Height(), state.First())
	require.Equal(t, qc.Height()+1, state.NextQC())
	require.Equal(t, types.EpochIndex(2), state.Anchor().Load().NextHeightEpoch().EpochIndex())
}
