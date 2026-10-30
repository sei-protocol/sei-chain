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
	require.Equal(t, fb, state.NextQC())
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
		if err := state.PushPartialBlock(ctx, qc1, TestBlockMap(blocks1)); err != nil {
			return err
		}
		// Verify data is accessible (no panic, no error).
		if _, err := state.Block(ctx, types.LaneBlockPos{Index: qc1.Index()}); err != nil {
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
	dir := t.TempDir()

	qc1, blocks1 := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.None[*types.CommitQC]())
	qc2, blocks2 := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.Some(qc1.QC()))

	// Session 1: write both QCs and all blocks.
	db1 := newTestBlockStore(t, dir)
	writeToBlockStore(t, db1,
		[]*types.FullCommitQC{qc1, qc2},
		[][]*types.Block{blocks1, blocks2})
	require.NoError(t, db1.Close())

	// Session 2: NewState should recover blocks and QCs.
	db2 := newTestBlockStore(t, dir)
	state2 := newTestState(t, &Config{Registry: registry}, db2)

	require.Equal(t, qc2.Index()+1, state2.NextQC())
	for n := qc1.Index(); n < qc2.Index()+1; n++ {
		got, err := state2.TryBlock(types.LaneBlockPos{Index: n})
		require.NoError(t, err)
		require.NotNil(t, got)
	}
	for n := qc1.Index(); n < qc2.Index()+1; n++ {
		got, err := state2.QC(t.Context(), n)
		require.NoError(t, err)
		require.NotNil(t, got)
	}
	require.NoError(t, db2.Close())

	// Session 3: verify session 2 did not corrupt BlockDB.
	db3 := newTestBlockStore(t, dir)
	state3 := newTestState(t, &Config{Registry: registry}, db3)
	require.Equal(t, qc2.Index()+1, state3.NextQC())
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
	_, err := NewState(&Config{Registry: registry}, db)
	require.ErrorIs(t, err, types.ErrQCBlocksMismatch)
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
	require.NoError(t, db.PruneBefore(qc2.Index()))

	state, err := NewState(&Config{Registry: registry}, db)
	require.NoError(t, err)
	_, err = state.SuperBlock(t.Context(), qc1.Index())
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

	state := newTestState(t, &Config{Registry: registry}, newTestBlockStore(t, t.TempDir()))
	require.NoError(t, state.PushPartialBlock(ctx, qc1, TestBlockMap(blocks1)))
	require.NoError(t, state.PushPartialBlock(ctx, qc2, TestBlockMap(blocks2)))
	require.NoError(t, state.PushPartialBlock(ctx, qc3, TestBlockMap(blocks3)))

	// Execute all blocks so they are eligible for pruning.
	require.NoError(t, pushAppHashesRunning(ctx, state, rng, qc1.Index(), qc3.Index()+1))

	// Prune qc1 entirely (keep from qc2 onward).
	require.NoError(t, state.PruneBefore(qc2.Index()))

	got, err := state.TryBlock(types.LaneBlockPos{Index: qc1.Index()})
	require.NoError(t, err)
	require.NotNil(t, got)
	for n := qc2.Index(); n < qc3.Index()+1; n++ {
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

	// Write only qc2 and qc3 — simulating a DB where qc1 was pruned and GC'd.
	db1 := newTestBlockStore(t, dir)
	writeToBlockStore(t, db1,
		[]*types.FullCommitQC{qc2, qc3},
		[][]*types.Block{blocks2, blocks3})
	require.NoError(t, db1.Close())

	// Recovery skipTo(qc2.Index()); qc1 heights are absent from BlockDB → ErrPruned.
	// With no AppQC yet, first stays at the recovery floor (not advanced to 0),
	// so below-floor reads fall through to BlockDB instead of nil maps.
	db2 := newTestBlockStore(t, dir)
	state2 := newTestState(t, &Config{Registry: registry}, db2)

	for inner := range state2.inner.Lock() {
		require.Equal(t, qc2.Index(), inner.first)
	}
	require.Equal(t, qc3.Index()+1, state2.NextQC())
	_, err := state2.TryBlock(types.LaneBlockPos{Index: qc1.Index()})
	require.ErrorIs(t, err, types.ErrPruned)
	_, err = state2.QC(t.Context(), qc1.Index())
	require.ErrorIs(t, err, types.ErrPruned)
	_, err = state2.SuperBlock(t.Context(), qc1.Index())
	require.ErrorIs(t, err, types.ErrPruned)
	for n := qc2.Index(); n < qc3.Index()+1; n++ {
		got, err := state2.TryBlock(types.LaneBlockPos{Index: n})
		require.NoError(t, err)
		require.NotNil(t, got)
	}
}

// TestRecoveryRejectsIncompleteQC verifies that an incomplete road block prevents recovery.
func TestRecoveryRejectsIncompleteQC(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	qc1, blocks1 := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.None[*types.CommitQC]())
	qc2, blocks2 := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.Some(qc1.QC()))
	dir := t.TempDir()
	db := newTestBlockStore(t, dir)
	require.NoError(t, db.WriteBlock(types.NewRoadBlock(qc1, blocks1)))
	require.NoError(t, db.WriteBlock(types.NewRoadBlock(qc2, blocks2[:len(blocks2)-1])))
	require.NoError(t, db.Flush())
	require.NoError(t, db.Close())
	db = newTestBlockStore(t, dir)
	_, err := NewState(&Config{Registry: registry}, db)
	require.ErrorIs(t, err, types.ErrQCBlocksMismatch)
}

func TestRecoveryAfterPruneNoGC(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	dir := t.TempDir()

	qc1, blocks1 := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.None[*types.CommitQC]())
	qc2, blocks2 := TestCommitQC(rng, registry.MustEpoch(0), keys, utils.Some(qc1.QC()))

	// Write both QCs and all their blocks to the store.
	store1 := newTestBlockStore(t, dir)
	writeToBlockStore(t, store1, []*types.FullCommitQC{qc1, qc2}, [][]*types.Block{blocks1, blocks2})

	// Prune qc1's range. GC is NOT called — pruned entries remain on disk.
	require.NoError(t, store1.PruneBefore(qc2.Index()))
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
	for n := qc1.Index(); n < qc2.Index()+1; n++ {
		_, err := state.TryBlock(types.LaneBlockPos{Index: n})
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
	_, err := NewState(&Config{Registry: registry}, db)
	require.ErrorIs(t, err, types.ErrQCBlocksMismatch)
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
	require.Greater(t, qc2.Index(), registry.FirstRoad(), "need skipTo past genesis")

	// First WriteBlock on an empty DB may start past genesis (crash / partial retain).
	db1 := newTestBlockStore(t, dir)
	require.NoError(t, db1.WriteBlock(types.NewRoadBlock(qc2, blocks2)))
	require.NoError(t, db1.Flush())
	require.NoError(t, db1.Close())

	db2 := newTestBlockStore(t, dir)
	tips := db2.Status().OrPanic("non-empty BlockDB status")
	require.Equal(t, qc2.Index(), tips.First)
	require.Equal(t, qc2.Index()+1, tips.NextBlock)
	require.NotZero(t, tips.NextBlock)

	state := newTestState(t, &Config{Registry: registry}, db2)
	require.Equal(t, qc2.Index()+1, state.NextQC())

	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		runCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		s.SpawnBgNamed("state.Run", func() error {
			return utils.IgnoreCancel(state.Run(runCtx))
		})
		if err := state.PushAppHash(ctx, qc2.Index(), types.GenAppHash(rng), nil); err != nil {
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
	_, err := NewState(&Config{Registry: registry}, db)
	require.ErrorIs(t, err, types.ErrQCBlocksMismatch)
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
	return qc.Index()
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

// proposalWriteStore records registry.Pending at each AppProposal write.
type proposalWriteStore struct {
	types.BlockStore
	registry *epoch.Registry
	pending  *utils.Option[types.EpochIndex]
}

func (s proposalWriteStore) WriteAppProposal(p *types.AppProposal) error {
	*s.pending = s.registry.Pending()
	return s.BlockStore.WriteAppProposal(p)
}

// TestPushAppHash_StagesClosingEpochBeforeProposalWrite verifies that C_{E+2}
// is pending when the closing road's AppProposal is written.
func TestPushAppHash_StagesClosingEpochBeforeProposalWrite(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	keeper := keys[0].Public()
	qc, blocks := commitQCAtRoad(registry.MustEpoch(0), keys, registry.LastRoad(0))
	base := newMemoryBlockStore(t)
	writeToBlockStore(t, base, []*types.FullCommitQC{qc}, [][]*types.Block{blocks})
	var pendingAtWrite utils.Option[types.EpochIndex]
	state := newTestState(t, &Config{Registry: registry}, proposalWriteStore{
		BlockStore: base,
		registry:   registry,
		pending:    &pendingAtWrite,
	})

	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBgNamed("state.Run()", func() error { return utils.IgnoreCancel(state.Run(ctx)) })
		if err := state.PushPartialBlock(ctx, qc, TestBlockMap(blocks)); err != nil {
			return err
		}
		return state.PushAppHash(ctx, qc.Index(), types.GenAppHash(rng), map[types.PublicKey]uint64{keeper: 9})
	}))
	require.Equal(t, utils.Some(types.EpochIndex(2)), pendingAtWrite)
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
		if err := state.PushPartialBlock(ctx, qc0, TestBlockMap(blocks0)); err != nil {
			return err
		}
		n0 := qc0.Index()
		if err := state.PushAppHash(ctx, n0, types.GenAppHash(rng), endWeights); err != nil {
			return err
		}
		qc1, blocks1 := commitQCAtRoad(ep1, keys, registry.EpochFirstRoad(1))
		if err := state.PushPartialBlock(ctx, qc1, TestBlockMap(blocks1)); err != nil {
			return err
		}
		n1 = qc1.Index()
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
