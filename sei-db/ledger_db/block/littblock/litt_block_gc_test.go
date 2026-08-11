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

// The head the collector reads is the newest block written — not the newest QC's coverage, since
// a QC is written before the blocks it covers — and 0 while nothing has been ingested, so an
// empty store drops out of the head minimum instead of dragging the lookback floor to 0. The reopen
// at the end covers recovery: a store that reported 0 after a restart would let the other stores
// prune past a height this one still holds.
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
	require.Equal(t, uint64(19), latest)

	// A QC covering 20..24 is written but none of its blocks are, so the head must not move.
	require.NoError(t, db.WriteQC(types.GenFullCommitQCRange(rng, 20, 25)))
	latest, err = store.GetLatestBlock()
	require.NoError(t, err)
	require.Equal(t, uint64(19), latest, "a QC ahead of its blocks must not advance the head")

	require.NoError(t, db.WriteBlock(20, types.GenBlock(rng)))
	require.NoError(t, db.Flush())
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
	require.NoError(t, store.PruneHistory(1_000))
	require.Equal(t, uint64(15), db.(*blockDB).watermark.Load(), "a prune past the head is capped to the newest cohort")

	for n := types.GlobalBlockNumber(15); n < 20; n++ {
		blk, err := db.ReadBlockByNumber(n)
		require.NoError(t, err)
		require.True(t, blk.IsPresent(), "block %d in the newest cohort must survive", n)
	}
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
