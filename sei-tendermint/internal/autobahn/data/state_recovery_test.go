package data

import (
	"context"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/littblock"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/memblock"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

// TestRecoveryEmpty verifies that NewState is a no-op on a fresh BlockDB.
func TestRecoveryEmpty(t *testing.T) {
	rng := utils.TestRng()
	registry, _ := epoch.GenRegistry(rng, 3)
	dir := t.TempDir()
	fb := registry.FirstBlock()

	db := newTestBlockDB(t, dir)
	state := newTestState(t, &Config{Registry: registry}, db)
	for inner := range state.inner.Lock() {
		require.Equal(t, fb, inner.first)
		require.Equal(t, fb, inner.nextQC)
		require.Equal(t, fb, inner.nextBlock)
	}
}

// TestNewStateInMemoryMode verifies that NewState with memblock followed by Run
// works end-to-end: QCs and blocks are accessible without a durable BlockDB dir.
func TestNewStateInMemoryMode(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	qc1, blocks1 := TestCommitQC(rng, registry.LatestEpoch(), keys, utils.None[*types.CommitQC]())

	state, err := NewState(&Config{Registry: registry}, memblock.NewBlockDB())
	require.NoError(t, err)

	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBgNamed("state", func() error { return utils.IgnoreCancel(state.Run(ctx)) })
		if err := state.PushQC(ctx, qc1, blocks1); err != nil {
			return err
		}
		// Verify data is accessible (no panic, no error).
		gr := qc1.QC().GlobalRange()
		for n := gr.First; n < gr.Next; n++ {
			if _, err := state.Block(ctx, n); err != nil {
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

	qc1, blocks1 := TestCommitQC(rng, registry.LatestEpoch(), keys, utils.None[*types.CommitQC]())
	qc2, blocks2 := TestCommitQC(rng, registry.LatestEpoch(), keys, utils.Some(qc1.QC()))
	gr1 := qc1.QC().GlobalRange()
	gr2 := qc2.QC().GlobalRange()

	// Session 1: write both QCs and all blocks.
	db1 := newTestBlockDB(t, dir)
	writeToBlockDB(t, db1,
		[]*types.FullCommitQC{qc1, qc2},
		[][]*types.Block{blocks1, blocks2})
	require.NoError(t, db1.Close())

	// Session 2: NewState should recover blocks and QCs.
	db2 := newTestBlockDB(t, dir)
	state2 := newTestState(t, &Config{Registry: registry}, db2)

	require.Equal(t, gr2.Next, state2.NextBlock())
	for n := gr1.First; n < gr2.Next; n++ {
		got, err := state2.TryBlock(n)
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
	db3 := newTestBlockDB(t, dir)
	state3 := newTestState(t, &Config{Registry: registry}, db3)
	require.Equal(t, gr2.Next, state3.NextBlock())
}

// TestPruningDiscards verifies that PruneBefore advances inner.first and causes
// TryBlock to return ErrPruned for the discarded range, while keeping at least
// one entry and keeping later blocks accessible.
func TestPruningDiscards(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)

	qc1, blocks1 := TestCommitQC(rng, registry.LatestEpoch(), keys, utils.None[*types.CommitQC]())
	qc2, blocks2 := TestCommitQC(rng, registry.LatestEpoch(), keys, utils.Some(qc1.QC()))
	qc3, blocks3 := TestCommitQC(rng, registry.LatestEpoch(), keys, utils.Some(qc2.QC()))
	gr1 := qc1.QC().GlobalRange()
	gr2 := qc2.QC().GlobalRange()
	gr3 := qc3.QC().GlobalRange()

	state := newTestState(t, &Config{Registry: registry}, newTestBlockDB(t, t.TempDir()))
	require.NoError(t, state.PushQC(ctx, qc1, blocks1))
	require.NoError(t, state.PushQC(ctx, qc2, blocks2))
	require.NoError(t, state.PushQC(ctx, qc3, blocks3))

	// Execute all blocks so they are eligible for pruning.
	require.NoError(t, pushAppHashesRunning(ctx, state, rng, gr1.First, gr3.Next))

	// Prune qc1 entirely (keep from qc2 onward).
	require.NoError(t, state.PruneBefore(gr2.First))

	// PruneBefore keeps one sentinel block at firstToKeep-1; capture it.
	var survivingBlock types.GlobalBlockNumber
	for inner := range state.inner.Lock() {
		survivingBlock = inner.first
	}
	for n := gr1.First; n < survivingBlock; n++ {
		_, err := state.TryBlock(n)
		require.ErrorIs(t, err, ErrPruned)
	}
	// survivingBlock itself and everything from gr2.First onward must be readable.
	for n := survivingBlock; n < gr3.Next; n++ {
		got, err := state.TryBlock(n)
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

	qc1, _ := TestCommitQC(rng, registry.LatestEpoch(), keys, utils.None[*types.CommitQC]())
	qc2, blocks2 := TestCommitQC(rng, registry.LatestEpoch(), keys, utils.Some(qc1.QC()))
	qc3, blocks3 := TestCommitQC(rng, registry.LatestEpoch(), keys, utils.Some(qc2.QC()))
	gr2 := qc2.QC().GlobalRange()
	gr3 := qc3.QC().GlobalRange()

	// Write only qc2 and qc3 — simulating a DB where qc1 was pruned and GC'd.
	db1 := newTestBlockDB(t, dir)
	writeToBlockDB(t, db1,
		[]*types.FullCommitQC{qc2, qc3},
		[][]*types.Block{blocks2, blocks3})
	require.NoError(t, db1.Close())

	// Recovery: first = gr2.First; qc1's range is before first, so ErrPruned.
	db2 := newTestBlockDB(t, dir)
	state2 := newTestState(t, &Config{Registry: registry}, db2)

	require.Equal(t, gr3.Next, state2.NextBlock())
	for n := qc1.QC().GlobalRange().First; n < gr2.First; n++ {
		_, err := state2.TryBlock(n)
		require.ErrorIs(t, err, ErrPruned)
	}
	for n := gr2.First; n < gr3.Next; n++ {
		got, err := state2.TryBlock(n)
		require.NoError(t, err)
		require.NotNil(t, got)
	}
}

// TestRecoveryBlocksBehind verifies recovery when QCs cover more range than
// blocks (e.g. a crash during block writes). Blocks up to the crash point are
// available; the rest are re-fetched via PushBlock.
func TestRecoveryBlocksBehind(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	dir := t.TempDir()

	qc1, blocks1 := TestCommitQC(rng, registry.LatestEpoch(), keys, utils.None[*types.CommitQC]())
	qc2, blocks2 := TestCommitQC(rng, registry.LatestEpoch(), keys, utils.Some(qc1.QC()))
	gr1 := qc1.QC().GlobalRange()
	gr2 := qc2.QC().GlobalRange()

	// Write both QCs but only qc1's blocks (simulate crash before qc2 blocks).
	db1 := newTestBlockDB(t, dir)
	require.NoError(t, db1.WriteQC(gr1.First, gr1.Next, qc1))
	require.NoError(t, db1.WriteQC(gr2.First, gr2.Next, qc2))
	for i, n := 0, gr1.First; n < gr1.Next; n++ {
		require.NoError(t, db1.WriteBlock(n, blocks1[i]))
		i++
	}
	require.NoError(t, db1.Flush())
	require.NoError(t, db1.Close())

	// Recovery: both QCs loaded, but only qc1's blocks.
	db2 := newTestBlockDB(t, dir)
	state2 := newTestState(t, &Config{Registry: registry}, db2)

	for n := gr1.First; n < gr1.Next; n++ {
		got, err := state2.TryBlock(n)
		require.NoError(t, err)
		require.NotNil(t, got)
	}
	for n := gr2.First; n < gr2.Next; n++ {
		_, err := state2.TryBlock(n)
		require.ErrorIs(t, err, ErrNotFound)
	}

	// Re-push qc2's blocks to fill the gap.
	for i, n := 0, gr2.First; n < gr2.Next; n++ {
		require.NoError(t, state2.PushBlock(ctx, n, blocks2[i]))
		i++
	}
	require.Equal(t, gr2.Next, state2.NextBlock())
}

// TestRecoveryPartialQCPrefix verifies that when the QC spans a wider range
// than the available blocks (the block prefix was pruned), recovery sets first
// from the blocks iterator (not the QC iterator).
func TestRecoveryPartialQCPrefix(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	dir := t.TempDir()

	qc1, blocks1 := TestCommitQC(rng, registry.LatestEpoch(), keys, utils.None[*types.CommitQC]())
	gr1 := qc1.QC().GlobalRange()
	if gr1.Next-gr1.First < 3 {
		t.Skip("need at least 3 blocks in QC range to test split")
	}

	// Write the QC for the full range, but write blocks only from mid onwards.
	// This is a valid write sequence: QC first, then blocks in ascending order
	// starting from mid. It simulates a DB where the block prefix [gr1.First, mid)
	// was pruned and physically removed.
	mid := gr1.First + (gr1.Next-gr1.First)/2
	db1 := newTestBlockDB(t, dir)
	require.NoError(t, db1.WriteQC(gr1.First, gr1.Next, qc1))
	for i, n := 0, gr1.First; n < gr1.Next; n++ {
		if n >= mid {
			require.NoError(t, db1.WriteBlock(n, blocks1[i]))
		}
		i++
	}
	require.NoError(t, db1.Flush())
	require.NoError(t, db1.Close())

	// Recovery should use blocks as golden: first == mid, not gr1.First.
	db2 := newTestBlockDB(t, dir)
	state2 := newTestState(t, &Config{Registry: registry}, db2)

	for inner := range state2.inner.Lock() {
		require.Equal(t, mid, inner.first,
			"first should be where blocks start, not where QC starts")
		require.Equal(t, gr1.Next, inner.nextQC,
			"QC should still cover the full range")
		// QC entries below first must be absent — they are unreachable via
		// pruneFirst and should have been trimmed during recovery.
		for n := gr1.First; n < mid; n++ {
			_, ok := inner.qcs[n]
			require.False(t, ok, "orphaned QC entry at %d should have been trimmed", n)
		}
	}
	for n := gr1.First; n < mid; n++ {
		_, err := state2.TryBlock(n)
		require.ErrorIs(t, err, ErrPruned)
	}
	for n := mid; n < gr1.Next; n++ {
		got, err := state2.TryBlock(n)
		require.NoError(t, err)
		require.NotNil(t, got)
	}
}

// TestRecoveryAfterPruneNoGC verifies that restarting before async GC reclaims
// pruned entries does not cause NewState to fail. Blocks and QCs share the same
// GC filter in littblock, so below-watermark blocks never survive past their
// corresponding QCs — the first block iterator entry is always >= inner.first
// set by the QC pass, so the "block predates first QC start" guard never fires.
func TestRecoveryAfterPruneNoGC(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	dir := t.TempDir()

	qc1, blocks1 := TestCommitQC(rng, registry.LatestEpoch(), keys, utils.None[*types.CommitQC]())
	qc2, blocks2 := TestCommitQC(rng, registry.LatestEpoch(), keys, utils.Some(qc1.QC()))
	gr1 := qc1.QC().GlobalRange()
	gr2 := qc2.QC().GlobalRange()

	// Write both QCs and all their blocks to the DB.
	cfg1, err := littblock.DefaultConfig(dir)
	require.NoError(t, err)
	cfg1.Retention = time.Nanosecond
	db1, err := littblock.NewBlockDB(cfg1)
	require.NoError(t, err)
	writeToBlockDB(t, db1, []*types.FullCommitQC{qc1, qc2}, [][]*types.Block{blocks1, blocks2})

	// Prune qc1's range. GC is NOT called — pruned entries remain on disk.
	require.NoError(t, db1.PruneBefore(gr2.First))
	require.NoError(t, db1.Close())

	// Reopen the same dir without ForceGC — pruned entries may still be present.
	cfg2, err := littblock.DefaultConfig(dir)
	require.NoError(t, err)
	cfg2.Retention = time.Nanosecond
	db2, err := littblock.NewBlockDB(cfg2)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db2.Close() })

	// NewState must succeed — below-watermark blocks never outlive their QCs
	// because blocks and QCs share the same GC filter in littblock. Without GC,
	// all entries are still present and recovery treats the DB as unpruned.
	// This is the PruneBefore-without-GC path: the watermark advanced but
	// physical reclamation has not happened yet, so the DB looks like a
	// fresh DB containing all data from qc1 and qc2.
	state := newTestState(t, &Config{Registry: registry}, db2)

	// Without GC all data is still present; qc1 and qc2 blocks are accessible.
	for n := gr1.First; n < gr2.Next; n++ {
		_, err := state.TryBlock(n)
		require.NoError(t, err)
	}
}

// TestRecoveryQCsNoBlocks verifies that NewState succeeds when the DB contains
// QCs but no blocks (crash between QC flush and block writes). The state
// cursor sits at the QC start with nextBlock == first and no block data.
func TestRecoveryQCsNoBlocks(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	dir := t.TempDir()

	qc1, _ := TestCommitQC(rng, registry.LatestEpoch(), keys, utils.None[*types.CommitQC]())
	gr1 := qc1.QC().GlobalRange()

	db1 := newTestBlockDB(t, dir)
	require.NoError(t, db1.WriteQC(gr1.First, gr1.Next, qc1))
	require.NoError(t, db1.Flush())
	require.NoError(t, db1.Close())

	db2 := newTestBlockDB(t, dir)
	state2 := newTestState(t, &Config{Registry: registry}, db2)

	for inner := range state2.inner.Lock() {
		require.Equal(t, gr1.First, inner.first)
		require.Equal(t, gr1.Next, inner.nextQC)
		require.Equal(t, gr1.First, inner.nextBlock)
	}
	for n := gr1.First; n < gr1.Next; n++ {
		_, err := state2.TryBlock(n)
		require.ErrorIs(t, err, ErrNotFound)
	}
}

// TestRecoveryBlockGap verifies that NewState returns an error when blocks in
// BlockDB are not contiguous. WriteBlock only enforces strictly-ascending and
// QC coverage, not continuity, so a gap can arise from corruption.
func TestRecoveryBlockGap(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	dir := t.TempDir()

	qc1, blocks1 := TestCommitQC(rng, registry.LatestEpoch(), keys, utils.None[*types.CommitQC]())
	gr1 := qc1.QC().GlobalRange()

	// TestCommitQC generates 10 global blocks, so the range is always wide
	// enough to skip one block in the middle.
	mid := gr1.First + (gr1.Next-gr1.First)/2

	db1 := newTestBlockDB(t, dir)
	require.NoError(t, db1.WriteQC(gr1.First, gr1.Next, qc1))
	for i, n := 0, gr1.First; n < gr1.Next; n++ {
		if n != mid {
			require.NoError(t, db1.WriteBlock(n, blocks1[i]))
		}
		i++
	}
	require.NoError(t, db1.Flush())
	require.NoError(t, db1.Close())

	db2 := newTestBlockDB(t, dir)
	_, err := NewState(&Config{Registry: registry}, db2)
	require.Error(t, err)
	require.Contains(t, err.Error(), "block gap in BlockDB")
}
