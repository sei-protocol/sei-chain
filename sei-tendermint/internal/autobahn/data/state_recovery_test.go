package data

import (
	"context"
	"slices"
	"testing"

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
	fb := registry.FirstRoad()

	db := newTestBlockStore(t, dir)
	state := newTestState(t, &Config{Registry: registry}, db)
	require.Equal(t, fb, state.NextBlock())
	for inner := range state.inner.Lock() {
		require.Equal(t, fb, inner.nextPartialBlock)
		require.Equal(t, fb, inner.nextAppProposal)
	}
}

// TestNewStateInMemoryMode verifies that NewState with an in-memory store followed by Run
// works end-to-end: QCs and blocks are accessible without a durable store dir.
func TestNewStateInMemoryMode(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	qc1, blocks1 := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.None[*types.CommitQC]())

	store := newMemoryBlockStore(t)
	state := utils.OrPanic1(NewState(&Config{Registry: registry}, store))

	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBgNamed("state", func() error { return utils.IgnoreCancel(state.Run(ctx)) })
		if err := state.PushQC(ctx, qc1, blocks1); err != nil {
			return err
		}
		// Verify data is accessible (no panic, no error).
		gr := types.RoadRange{First: qc1.QC().Index(), Next: qc1.QC().Next()}
		for n := gr.First; n < gr.Next; n++ {
			if _, err := state.Block(ctx, types.LaneBlockPos{Index: n}); err != nil {
				return err
			}
		}
		return nil
	}))
}

// TestRecoveryNormal verifies that NewState fully restores QCs and blocks
// from BlockDB on restart.
func TestRecoveryNormal(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	dir := t.TempDir()

	qc1, blocks1 := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.None[*types.CommitQC]())
	qc2, blocks2 := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.Some(qc1.QC()))
	gr1 := types.RoadRange{First: qc1.QC().Index(), Next: qc1.QC().Next()}
	gr2 := types.RoadRange{First: qc2.QC().Index(), Next: qc2.QC().Next()}

	// Session 1: write both QCs and all blocks.
	db1 := newTestBlockStore(t, dir)
	writeToBlockStore(t, db1,
		[]*types.FullCommitQC{qc1, qc2},
		[][]*types.Block{blocks1, blocks2})
	require.NoError(t, db1.Close())

	// Session 2: NewState should recover blocks and QCs.
	db2 := newTestBlockStore(t, dir)
	state2 := newTestState(t, &Config{Registry: registry}, db2)

	require.Equal(t, gr2.Next, state2.NextBlock())
	for n := gr1.First; n < gr2.Next; n++ {
		got, err := state2.TryBlock(types.LaneBlockPos{Index: n})
		require.NoError(t, err)
		require.NotNil(t, got)
	}
	for n := gr1.First; n < gr2.Next; n++ {
		got, err := state2.QC(t.Context(), n)
		require.NoError(t, err)
		require.NotNil(t, got)
	}
	require.NoError(t, db2.Close())

	// Session 3: verify session 2 did not corrupt BlockDB.
	db3 := newTestBlockStore(t, dir)
	state3 := newTestState(t, &Config{Registry: registry}, db3)
	require.Equal(t, gr2.Next, state3.NextBlock())
}

func TestRecoveryRejectsMissingFirstCommittedBlock(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	qc, blocks := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.None[*types.CommitQC]())
	dir := t.TempDir()
	db := newTestBlockStore(t, dir)
	require.ErrorIs(t, db.WriteQC(qc, blocks[1:]), types.ErrQCBlocksMismatch)
	require.NoError(t, db.Flush())
	require.NoError(t, db.Close())
	db = newTestBlockStore(t, dir)
	state := newTestState(t, &Config{Registry: registry}, db)
	require.Equal(t, registry.FirstRoad(), state.NextBlock())
}

func TestRecoveryLeavesAppTipBelowPruneFloorUnreadable(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	qc1, blocks1 := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.None[*types.CommitQC]())
	qc2, blocks2 := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.Some(qc1.QC()))

	db := newTestBlockStore(t, t.TempDir())
	writeToBlockStore(t, db,
		[]*types.FullCommitQC{qc1, qc2},
		[][]*types.Block{blocks1, blocks2})
	writeAppDataToBlockStore(t, rng, db, keys, qc1, qc2)
	require.NoError(t, db.PruneBefore(qc2.QC().Index()))

	state, err := NewState(&Config{Registry: registry}, db)
	require.NoError(t, err)
	_, err = state.SuperBlock(t.Context(), qc1.QC().Index())
	require.ErrorIs(t, err, types.ErrPruned)
}

// TestPruningDiscards verifies that PruneBefore advances BlockDB's watermark but
// does not discard RAM-retained blocks. Memory is cleared only by the AppQC
// eviction floor.
func TestPruningDiscards(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)

	qc1, blocks1 := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.None[*types.CommitQC]())
	qc2, blocks2 := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.Some(qc1.QC()))
	qc3, blocks3 := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.Some(qc2.QC()))
	gr1 := types.RoadRange{First: qc1.QC().Index(), Next: qc1.QC().Next()}
	gr2 := types.RoadRange{First: qc2.QC().Index(), Next: qc2.QC().Next()}
	gr3 := types.RoadRange{First: qc3.QC().Index(), Next: qc3.QC().Next()}

	state := newTestState(t, &Config{Registry: registry}, newTestBlockStore(t, t.TempDir()))
	require.NoError(t, state.PushQC(ctx, qc1, blocks1))
	require.NoError(t, state.PushQC(ctx, qc2, blocks2))
	require.NoError(t, state.PushQC(ctx, qc3, blocks3))

	// Execute all blocks so they are eligible for pruning.
	require.NoError(t, pushAppHashesRunning(ctx, state, rng, gr1.First, gr3.Next))

	// Prune qc1 entirely (keep from qc2 onward).
	require.NoError(t, state.PruneBefore(gr2.First))

	for n := gr1.First; n < gr2.First; n++ {
		got, err := state.TryBlock(types.LaneBlockPos{Index: n})
		require.NoError(t, err)
		require.NotNil(t, got)
	}
	for n := gr2.First; n < gr3.Next; n++ {
		got, err := state.TryBlock(types.LaneBlockPos{Index: n})
		require.NoError(t, err)
		require.NotNil(t, got)
	}
}

// TestRecoveryAfterPruning verifies that NewState recovers correctly when
// BlockDB only contains data from a later QC range (as left by pruning + GC).
func TestRecoveryAfterPruning(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	dir := t.TempDir()

	qc1, _ := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.None[*types.CommitQC]())
	qc2, blocks2 := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.Some(qc1.QC()))
	qc3, blocks3 := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.Some(qc2.QC()))
	gr2 := types.RoadRange{First: qc2.QC().Index(), Next: qc2.QC().Next()}
	gr3 := types.RoadRange{First: qc3.QC().Index(), Next: qc3.QC().Next()}

	// Write only qc2 and qc3 — simulating a DB where qc1 was pruned and GC'd.
	db1 := newTestBlockStore(t, dir)
	writeToBlockStore(t, db1,
		[]*types.FullCommitQC{qc2, qc3},
		[][]*types.Block{blocks2, blocks3})
	require.NoError(t, db1.Close())

	// Recovery skipTo(gr2.First); qc1 heights are absent from BlockDB → ErrPruned.
	// With no AppQC yet, first stays at the recovery floor (not advanced to 0),
	// so below-floor reads fall through to BlockDB instead of nil maps.
	db2 := newTestBlockStore(t, dir)
	state2 := newTestState(t, &Config{Registry: registry}, db2)

	for inner := range state2.inner.Lock() {
		require.Equal(t, gr2.First, inner.first)
	}
	require.Equal(t, gr3.Next, state2.NextBlock())
	for n := qc1.QC().Index(); n < gr2.First; n++ {
		_, err := state2.TryBlock(types.LaneBlockPos{Index: n})
		require.ErrorIs(t, err, types.ErrPruned)
		_, err = state2.QC(t.Context(), n)
		require.ErrorIs(t, err, types.ErrPruned)
		_, err = state2.SuperBlock(t.Context(), n)
		require.ErrorIs(t, err, types.ErrPruned)
	}
	for n := gr2.First; n < gr3.Next; n++ {
		got, err := state2.TryBlock(types.LaneBlockPos{Index: n})
		require.NoError(t, err)
		require.NotNil(t, got)
	}
}

// TestRecoveryRejectsIncompleteQC verifies that an incomplete QC write leaves
// the previous complete QC as the recovery head.
func TestRecoveryRejectsIncompleteQC(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	qc1, blocks1 := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.None[*types.CommitQC]())
	qc2, blocks2 := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.Some(qc1.QC()))
	dir := t.TempDir()
	db := newTestBlockStore(t, dir)
	require.NoError(t, db.WriteQC(qc1, blocks1))
	require.ErrorIs(t, db.WriteQC(qc2, blocks2[:len(blocks2)-1]), types.ErrQCBlocksMismatch)
	require.NoError(t, db.Flush())
	require.NoError(t, db.Close())
	db = newTestBlockStore(t, dir)
	state := newTestState(t, &Config{Registry: registry}, db)
	require.Equal(t, qc1.QC().Next(), state.NextBlock())
	for n := qc1.QC().Index(); n < qc1.QC().Next(); n++ {
		block, err := state.TryBlock(types.LaneBlockPos{Index: n})
		require.NoError(t, err)
		require.NotNil(t, block)
	}
	require.NoError(t, state.PushQC(t.Context(), qc2, blocks2))
	require.Equal(t, qc2.QC().Next(), state.NextBlock())
}

func TestRecoveryAfterPruneNoGC(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	dir := t.TempDir()

	qc1, blocks1 := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.None[*types.CommitQC]())
	qc2, blocks2 := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.Some(qc1.QC()))
	gr1 := types.RoadRange{First: qc1.QC().Index(), Next: qc1.QC().Next()}
	gr2 := types.RoadRange{First: qc2.QC().Index(), Next: qc2.QC().Next()}

	// Write both QCs and all their blocks to the store.
	store1 := newTestBlockStore(t, dir)
	writeToBlockStore(t, store1, []*types.FullCommitQC{qc1, qc2}, [][]*types.Block{blocks1, blocks2})

	// Prune qc1's range. GC is NOT called — pruned entries remain on disk.
	require.NoError(t, store1.PruneBefore(gr2.First))
	require.NoError(t, store1.Close())

	// Reopen the same dir without ForceGC — pruned entries may still be present.
	store2 := newTestBlockStore(t, dir)

	// NewState must succeed — below-watermark blocks never outlive their QCs
	// because blocks and QCs share the same GC filter in littblock. Without GC,
	// all entries are still present and recovery treats the store as unpruned.
	// This is the PruneBefore-without-GC path: the watermark advanced but
	// physical reclamation has not happened yet, so the store looks like a
	// fresh store containing all data from qc1 and qc2.
	state := newTestState(t, &Config{Registry: registry}, store2)

	// Without GC all data is still present; qc1 and qc2 blocks are accessible.
	for n := gr1.First; n < gr2.Next; n++ {
		_, err := state.TryBlock(types.LaneBlockPos{Index: n})
		require.NoError(t, err)
	}
}

// TestRecoveryRejectsQCWithoutBlocks verifies that a QC cannot survive without its blocks.
func TestRecoveryRejectsQCWithoutBlocks(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	qc, _ := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.None[*types.CommitQC]())
	dir := t.TempDir()
	db := newTestBlockStore(t, dir)
	require.ErrorIs(t, db.WriteQC(qc, nil), types.ErrQCBlocksMismatch)
	require.NoError(t, db.Flush())
	require.NoError(t, db.Close())
	db = newTestBlockStore(t, dir)
	require.False(t, db.Status().IsPresent())
	state := newTestState(t, &Config{Registry: registry}, db)
	require.Equal(t, registry.FirstRoad(), state.NextBlock())
}

// TestRunPersistSeedsFromRecoveryFloor verifies that runPersist does not walk
// [genesis, recoveryFloor) when the first persisted QC starts past FirstBlock.
func TestRunPersistSeedsFromRecoveryFloor(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	dir := t.TempDir()

	qc1, _ := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.None[*types.CommitQC]())
	qc2, blocks2 := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.Some(qc1.QC()))
	gr2 := types.RoadRange{First: qc2.QC().Index(), Next: qc2.QC().Next()}
	require.Greater(t, gr2.First, registry.FirstRoad(), "need skipTo past genesis")

	// First WriteQC on an empty DB may start past genesis (crash / partial retain).
	db1 := newTestBlockStore(t, dir)
	require.NoError(t, db1.WriteQC(qc2, blocks2))
	require.NoError(t, db1.Flush())
	require.NoError(t, db1.Close())

	db2 := newTestBlockStore(t, dir)
	tips := db2.Status().OrPanic("non-empty BlockDB status")
	require.Equal(t, gr2.First, tips.First)
	require.Equal(t, gr2.Next, tips.NextQC)
	require.NotZero(t, tips.NextQC)

	state := newTestState(t, &Config{Registry: registry}, db2)
	require.Equal(t, gr2.Next, state.NextBlock())

	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		runCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		s.SpawnBgNamed("state.Run", func() error {
			return utils.IgnoreCancel(state.Run(runCtx))
		})
		for n := gr2.First; n < gr2.Next; n++ {
			if err := state.PushAppHash(ctx, n, types.GenAppHash(rng), nil); err != nil {
				return err
			}
		}
		return nil
	}))
}

// TestRecoveryBlockGap verifies that omitting a block rejects the whole QC write.
func TestRecoveryBlockGap(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	qc, blocks := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.None[*types.CommitQC]())
	db := newTestBlockStore(t, t.TempDir())
	mid := len(blocks) / 2
	gapped := append(slices.Clone(blocks[:mid]), blocks[mid+1:]...)
	require.ErrorIs(t, db.WriteQC(qc, gapped), types.ErrQCBlocksMismatch)
	require.NoError(t, db.WriteQC(qc, blocks))
	state := newTestState(t, &Config{Registry: registry}, db)
	require.Equal(t, qc.QC().Next(), state.NextBlock())
}

func TestNewState_NextCommitEpochAtBoundaryTip(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistryThrough(rng, 3, 2)
	ep2 := registry.MustEpoch(2)
	ep1 := registry.MustEpoch(1)

	qc, blocks := commitQCAtRoad(ep1, keys, registry.LastRoad(1))
	db := newTestBlockStore(t, t.TempDir())
	writeToBlockStore(t, db, []*types.FullCommitQC{qc}, [][]*types.Block{blocks})
	writeAppDataToBlockStore(t, rng, db, keys, qc)

	state := newTestState(t, &Config{Registry: registry}, db)
	require.Equal(t, ep2, state.NextCommitEpoch().Load())
}

// writeExecutedTip writes a one-block CommitQC and its AppProposal at road.
func writeExecutedTip(
	t *testing.T,
	store types.BlockStore,
	ep *types.Epoch,
	keys []types.SecretKey,
	road types.RoadIndex,
	appHash types.AppHash,
) types.RoadIndex {
	t.Helper()
	qc, blocks := commitQCAtRoad(ep, keys, road)
	writeToBlockStore(t, store, []*types.FullCommitQC{qc}, [][]*types.Block{blocks})
	utils.OrPanic(store.WriteAppProposal(types.NewAppProposal(qc.QC().Proposal(), appHash)))
	utils.OrPanic(store.Flush())
	return qc.QC().Next() - 1
}

func TestPushAppHash_ReplayClosingRoadStagesEpoch(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	keeper := keys[0].Public()
	appHash := types.GenAppHash(rng)
	store := newMemoryBlockStore(t)
	n := writeExecutedTip(t, store, registry.MustEpoch(0), keys, registry.LastRoad(0), appHash)
	state := newTestState(t, &Config{Registry: registry}, store)
	_, err := registry.EpochAt(registry.EpochFirstRoad(2))
	require.Error(t, err)

	staged := utils.Some(types.EpochIndex(2))
	require.NoError(t, state.PushAppHash(ctx, n, appHash, map[types.PublicKey]uint64{keeper: 9}))
	require.Equal(t, staged, registry.Pending())
	_, err = registry.EpochAt(registry.EpochFirstRoad(2))
	require.Error(t, err)

	require.NoError(t, state.PushAppHash(ctx, n, appHash, map[types.PublicKey]uint64{keeper: 9}))
	require.Equal(t, staged, registry.Pending())
	require.Error(t, state.PushAppHash(ctx, n, appHash, map[types.PublicKey]uint64{keys[1].Public(): 1}))
	require.Equal(t, staged, registry.Pending())
}

func TestPushAppHash_RestartAtEpoch1KeepsLastRoadStake(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	base, keys := epoch.GenRegistry(rng, 3)
	keeper := keys[0].Public()
	later := keys[1].Public()
	endWeights := map[types.PublicKey]uint64{keeper: 9}
	laterWeights := map[types.PublicKey]uint64{later: 4}

	regDir := utils.Some(t.TempDir())
	registry := utils.OrPanic1(epoch.NewRegistry(
		base.MustEpoch(0).Committee(),
		base.FirstRoad(),
		base.GenesisTimestamp(),
		regDir))
	storeDir := t.TempDir()
	store := newTestBlockStore(t, storeDir)
	ep0 := registry.MustEpoch(0)
	ep1 := registry.MustEpoch(1)

	qc0, blocks0 := commitQCAtRoad(ep0, keys, registry.LastRoad(0))
	writeToBlockStore(t, store, []*types.FullCommitQC{qc0}, [][]*types.Block{blocks0})
	state := newTestState(t, &Config{Registry: registry}, store)
	var n1 types.RoadIndex
	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBgNamed("state.Run()", func() error { return utils.IgnoreCancel(state.Run(ctx)) })
		if err := state.PushQC(ctx, qc0, blocks0); err != nil {
			return err
		}
		n0 := qc0.QC().Next() - 1
		if err := state.PushAppHash(ctx, n0, types.GenAppHash(rng), endWeights); err != nil {
			return err
		}
		qc1, blocks1 := commitQCAtRoad(ep1, keys, registry.EpochFirstRoad(1))
		if err := state.PushQC(ctx, qc1, blocks1); err != nil {
			return err
		}
		n1 = qc1.QC().Next() - 1
		return state.PushAppHash(ctx, n1, types.GenAppHash(rng), laterWeights)
	}))
	require.Equal(t, utils.Some(types.EpochIndex(2)), registry.Pending())
	require.NoError(t, store.Close())

	registry2 := utils.OrPanic1(epoch.NewRegistry(
		base.MustEpoch(0).Committee(),
		base.FirstRoad(),
		base.GenesisTimestamp(),
		regDir))
	store2 := newTestBlockStore(t, storeDir)
	state2 := newTestState(t, &Config{Registry: registry2}, store2)
	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBgNamed("state.Run()", func() error { return utils.IgnoreCancel(state2.Run(ctx)) })
		return state2.PushAppHash(ctx, n1, types.GenAppHash(rng), laterWeights)
	}))
	require.Equal(t, utils.Some(types.EpochIndex(2)), registry2.Pending())
	require.NoError(t, registry2.ActivateEpoch(2))
	got := registry2.MustEpoch(2).Committee()
	require.Equal(t, uint64(9), got.Weight(keeper))
	require.False(t, got.HasReplica(later))
}

func TestPushAppHash_ReplayClosingSuperBlockStagesEpoch(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	keeper := keys[0].Public()
	appHash := types.GenAppHash(rng)
	store := newMemoryBlockStore(t)
	ep := registry.MustEpoch(0)
	qc, blocks := commitQCAtRoadBlocks(ep, keys, registry.LastRoad(0), 2)
	writeToBlockStore(t, store, []*types.FullCommitQC{qc}, [][]*types.Block{blocks})
	utils.OrPanic(store.WriteAppProposal(types.NewAppProposal(qc.QC().Proposal(), appHash)))
	utils.OrPanic(store.Flush())
	state := newTestState(t, &Config{Registry: registry}, store)

	require.Equal(t, uint64(2), qc.QC().Proposal().NumBlocks())
	require.NoError(t, state.PushAppHash(ctx, qc.Index(), appHash, map[types.PublicKey]uint64{keeper: 9}))
	require.Equal(t, utils.Some(types.EpochIndex(2)), registry.Pending())
}

func TestPushAppHash_ReplayMidEpochDoesNotStage(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	appHash := types.GenAppHash(rng)
	store := newMemoryBlockStore(t)
	n := writeExecutedTip(t, store, registry.MustEpoch(0), keys, registry.EpochFirstRoad(0), appHash)
	state := newTestState(t, &Config{Registry: registry}, store)

	require.NoError(t, state.PushAppHash(ctx, n, appHash, map[types.PublicKey]uint64{keys[0].Public(): 9}))
	require.Equal(t, utils.None[types.EpochIndex](), registry.Pending())
	_, err := registry.EpochAt(registry.EpochFirstRoad(2))
	require.Error(t, err)
}

func TestNewState_ActivatesStagedEpochFromPersistedAppQC(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	keeper := keys[0].Public()
	ep := registry.MustEpoch(0)
	store := newMemoryBlockStore(t)
	qc, blocks := commitQCAtRoad(ep, keys, registry.LastRoad(0))
	writeToBlockStore(t, store, []*types.FullCommitQC{qc}, [][]*types.Block{blocks})
	writeAppDataToBlockStore(t, rng, store, keys, qc)

	require.NoError(t, registry.StageEpoch(0, map[types.PublicKey]uint64{keeper: 9}))
	_, err := registry.EpochByIndex(2)
	require.Error(t, err)

	_ = newTestState(t, &Config{Registry: registry}, store)
	require.Equal(t, uint64(9), registry.MustEpoch(2).Committee().Weight(keeper))
	require.Equal(t, utils.None[types.EpochIndex](), registry.Pending())
}

func TestNewState_LeavesStagedEpochWithoutAppQC(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	keeper := keys[0].Public()
	appHash := types.GenAppHash(rng)
	store := newMemoryBlockStore(t)
	_ = writeExecutedTip(t, store, registry.MustEpoch(0), keys, registry.LastRoad(0), appHash)
	require.NoError(t, registry.StageEpoch(0, map[types.PublicKey]uint64{keeper: 9}))

	_ = newTestState(t, &Config{Registry: registry}, store)
	require.Equal(t, utils.Some(types.EpochIndex(2)), registry.Pending())
	_, err := registry.EpochByIndex(2)
	require.Error(t, err)
}
