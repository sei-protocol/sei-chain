package littblock

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/management/gc"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// gcConfig builds a config for the collector-facing tests: reclamation is gated solely by the
// prune watermark (tiny TTL) and only ForceGC reclaims, so a watermark assertion is never
// confused by a background pass. Segment sizing is left at its defaults — these tests are about
// the decisions the collector drives, not about which segment a record lands in.
func gcConfig(t *testing.T, dir string) *BlockDBConfig {
	cfg, err := DefaultConfig(dir)
	require.NoError(t, err)
	cfg.RetentionTime = time.Nanosecond
	cfg.Litt.GCPeriod = time.Hour
	cfg.Litt.Fsync = false
	return cfg
}

// openForGC opens a store at dir and returns it as the collector sees it, closing it on cleanup.
// Opening a store is the expensive part of these tests, so each one opens as few as it can.
func openForGC(t *testing.T, dir string) (types.BlockDB, gc.PrunableStore) {
	t.Helper()
	db, err := NewBlockDB(gcConfig(t, dir))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	store, ok := db.(gc.PrunableStore)
	require.True(t, ok, "a littblock store must satisfy gc.PrunableStore")
	return db, store
}

// The head the collector reads is the newest crash-recoverable block — not the newest written
// block and not the newest QC's coverage. Counting an unflushed suffix would let the collector
// prune durable history against records a process crash can lose, leaving less than the configured
// rollback window. The reopen at the end covers recovery.
func TestGCLatestBlock(t *testing.T) {
	dir := t.TempDir()
	rng := utils.TestRngFromSeed(1)
	db, store := openForGC(t, dir)

	latest, err := store.GetLatestBlock()
	require.NoError(t, err)
	require.Equal(t, uint64(0), latest, "a store that has ingested nothing has no head")

	writeSyntheticBatches(t, db, rng, 4, 5) // blocks 0..19, QCs [0,5)..[15,20)
	latest, err = store.GetLatestBlock()
	require.NoError(t, err)
	require.Equal(t, uint64(0), latest, "written but unflushed blocks must not advance the pruning head")

	require.NoError(t, db.Flush())
	latest, err = store.GetLatestBlock()
	require.NoError(t, err)
	require.Equal(t, uint64(19), latest)

	// A QC covering 20..24 is written but none of its blocks are, so the head must not move.
	require.NoError(t, db.WriteQC(types.GenFullCommitQCRange(rng, 20, 25)))
	latest, err = store.GetLatestBlock()
	require.NoError(t, err)
	require.Equal(t, uint64(19), latest, "a QC ahead of its blocks must not advance the head")

	require.NoError(t, db.WriteBlock(20, types.GenBlock(rng)))
	latest, err = store.GetLatestBlock()
	require.NoError(t, err)
	require.Equal(t, uint64(19), latest, "an unflushed block must not advance the pruning head")

	require.NoError(t, db.Flush())
	latest, err = store.GetLatestBlock()
	require.NoError(t, err)
	require.Equal(t, uint64(20), latest)
	require.NoError(t, db.Close())

	_, reopened := openForGC(t, dir)
	latest, err = reopened.GetLatestBlock()
	require.NoError(t, err)
	require.Equal(t, uint64(20), latest, "the head must be re-derived on open")
}

// blockDB keeps no snapshots, so its half of the split contract is a no-op. It is still called
// every cycle, and answering with an error would fail a cycle that has nothing to do with it.
func TestGCPruneSnapshotsIsANoOp(t *testing.T) {
	db, store := openForGC(t, t.TempDir())
	rng := utils.TestRngFromSeed(4)
	writeSyntheticBatches(t, db, rng, 4, 5) // blocks 0..19

	require.NoError(t, store.PruneSnapshots(10))
	require.Equal(t, uint64(0), db.(*blockDB).watermark.Load(),
		"pruning snapshots must not move the history watermark")

	blk, err := db.ReadBlockByNumber(0)
	require.NoError(t, err)
	require.True(t, blk.IsPresent(), "no block may be dropped by a snapshot prune")
}

// A contiguous store resolves the rollback window against its own head: head - rollbackWindow, or 0
// where that head has not cleared the window. The states below are the ones where a snapshot store
// would answer differently.
//
// The prune assertions ride along on the same store because both methods are answers about the
// same three states, and opening a second store to re-reach them is the slow part of this file.
func TestGCRollbackFloorAndPruneHistory(t *testing.T) {
	db, store := openForGC(t, t.TempDir())
	rng := utils.TestRngFromSeed(2)
	impl := db.(*blockDB)

	// Empty, so the head is 0 and every window reports 0 — "keep everything", which holds the
	// fleet's history where it is until this store fills. The prune that follows is a no-op rather
	// than an error.
	require.Equal(t, uint64(0), store.GetRollbackFloor(0))
	require.Equal(t, uint64(0), store.GetRollbackFloor(42))
	require.NoError(t, store.PruneHistory(1_000))
	require.Equal(t, uint64(0), impl.watermark.Load())

	writeSyntheticBatches(t, db, rng, 4, 5) // blocks 0..19, QCs [0,5),[5,10),[10,15),[15,20)
	require.NoError(t, db.Flush())
	require.Equal(t, uint64(19), store.GetRollbackFloor(0), "the whole store is inside a window of 0")
	require.Equal(t, uint64(7), store.GetRollbackFloor(12))
	// A window deeper than the store's own head is a rollback promise reaching past genesis, so
	// nothing here is eligible for pruning yet.
	require.Equal(t, uint64(0), store.GetRollbackFloor(1_000))

	require.NoError(t, store.PruneHistory(7))
	require.Equal(t, uint64(5), impl.watermark.Load(), "a prune inside QC[5,10) rounds down to its start")
	require.NoError(t, store.PruneHistory(3))
	require.Equal(t, uint64(5), impl.watermark.Load(), "the watermark must not move backwards")

	// Already pruned above the answer: the floor is a function of the head, not of the retention
	// floor, so a store pruned higher by an earlier cycle does not hold the others back to where it
	// started.
	require.Equal(t, uint64(15), store.GetRollbackFloor(4))
}

// The collector prunes every store to a shared minimum, so a store lagging the head is asked to
// prune past everything it holds. The never-empty cap turns that into a prune to the newest
// cohort rather than a store that can serve nothing.
func TestGCPruneHistoryAboveHeadIsCapped(t *testing.T) {
	db, store := openForGC(t, t.TempDir())
	rng := utils.TestRngFromSeed(3)

	writeSyntheticBatches(t, db, rng, 4, 5) // blocks 0..19, newest cohort QC[15,20)
	require.NoError(t, db.Flush())
	require.NoError(t, store.PruneHistory(1_000))
	require.Equal(t, uint64(15), db.(*blockDB).watermark.Load(), "a prune past the head is capped to the newest cohort")

	for n := types.GlobalBlockNumber(15); n < 20; n++ {
		blk, err := db.ReadBlockByNumber(n)
		require.NoError(t, err)
		require.True(t, blk.IsPresent(), "block %d in the newest cohort must survive", n)
	}
}

// The never-empty cap is measured from the durable tip, not the newest write. Otherwise a concurrent
// unflushed suffix could let pruning reclaim every crash-recoverable block; a process crash would
// then lose the suffix and reopen without the history the cap promised to preserve.
func TestGCPruneHistoryAboveHeadIsCappedToDurableCohort(t *testing.T) {
	db, store := openForGC(t, t.TempDir())
	rng := utils.TestRngFromSeed(5)

	writeSyntheticBatches(t, db, rng, 2, 5) // durable blocks 0..9, newest durable cohort QC[5,10)
	require.NoError(t, db.Flush())
	for i := 2; i < 4; i++ {
		first := types.GlobalBlockNumber(i * 5)
		next := first + 5
		require.NoError(t, db.WriteQC(types.GenFullCommitQCRange(rng, first, next)))
		for n := first; n < next; n++ {
			require.NoError(t, db.WriteBlock(n, types.GenBlock(rng)))
		}
	}

	require.NoError(t, store.PruneHistory(1_000))
	require.Equal(t, uint64(5), db.(*blockDB).watermark.Load(),
		"the store must retain the newest durable cohort, not rely on an unflushed suffix")
}

func TestConfigValidateRetentionTime(t *testing.T) {
	cfg, err := DefaultConfig(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, cfg.Validate())

	// RetentionTime gates reclamation underneath the watermark, so a non-positive value would
	// release records the moment the watermark passed them, removing the failsafe entirely.
	for _, retentionTime := range []time.Duration{0, -time.Second} {
		cfg.RetentionTime = retentionTime
		require.ErrorContains(t, cfg.Validate(), "RetentionTime")
	}
}
