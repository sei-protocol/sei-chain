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
// empty store drops out of the head minimum instead of dragging every cut line to 0. The reopen
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

// The window the collector applies is the configured one, sentinel included: -1 has to survive
// the trip verbatim or an operator asking for "never prune" gets a 1-block window instead.
func TestGCRetentionWindowComesFromConfig(t *testing.T) {
	for _, retentionWindow := range []int64{gc.InfiniteRetentionWindow, 100_000} {
		cfg := gcConfig(t, t.TempDir())
		cfg.RetentionWindow = retentionWindow

		db, err := NewBlockDB(cfg)
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, db.Close()) })

		require.Equal(t, retentionWindow, db.(gc.PrunableStore).GetRetentionWindow())
	}
}

// A contiguous store answers the cut line it was given whatever it holds. The states below are
// the ones where a snapshot store would answer differently, and answering under cutLine in any
// of them would hold every other store back to this store's floor for no benefit.
//
// The prune assertions ride along on the same store because both methods are answers about the
// same three states, and opening a second store to re-reach them is the slow part of this file.
func TestGCPruningBoundaryAndPruneBelow(t *testing.T) {
	db, store := openForGC(t, t.TempDir())
	rng := utils.TestRngFromSeed(2)
	impl := db.(*blockDB)

	// Empty. The prune that follows is a no-op rather than an error — a store still filling is
	// pruned like any other — and the boundary is still cutLine, since CannotServeRollback here
	// would stall every other store.
	require.Equal(t, uint64(42), store.GetPruningBoundary(42))
	require.NoError(t, store.PruneBelow(1_000))
	require.Equal(t, uint64(0), impl.watermark.Load())

	writeSyntheticBatches(t, db, rng, 4, 5) // blocks 0..19, QCs [0,5),[5,10),[10,15),[15,20)
	require.Equal(t, uint64(7), store.GetPruningBoundary(7))
	// Above the head, which happens whenever another store's data puts the head above this one's.
	require.Equal(t, uint64(1_000), store.GetPruningBoundary(1_000))

	require.NoError(t, store.PruneBelow(7))
	require.Equal(t, uint64(5), impl.watermark.Load(), "a prune inside QC[5,10) rounds down to its start")
	require.NoError(t, store.PruneBelow(3))
	require.Equal(t, uint64(5), impl.watermark.Load(), "the watermark must not move backwards")

	// Pruned above the cut line: nothing below it survives to protect, so cutLine still stands.
	require.Equal(t, uint64(4), store.GetPruningBoundary(4))
}

// The collector prunes every store to a shared minimum, so a store lagging the head is asked to
// prune past everything it holds. The never-empty cap turns that into a prune to the newest
// cohort rather than a store that can serve nothing.
func TestGCPruneBelowAboveHeadIsCapped(t *testing.T) {
	db, store := openForGC(t, t.TempDir())
	rng := utils.TestRngFromSeed(3)

	writeSyntheticBatches(t, db, rng, 4, 5) // blocks 0..19, newest cohort QC[15,20)
	require.NoError(t, store.PruneBelow(1_000))
	require.Equal(t, uint64(15), db.(*blockDB).watermark.Load(), "a prune past the head is capped to the newest cohort")

	for n := types.GlobalBlockNumber(15); n < 20; n++ {
		blk, err := db.ReadBlockByNumber(n)
		require.NoError(t, err)
		require.True(t, blk.IsPresent(), "block %d in the newest cohort must survive", n)
	}
}

func TestConfigValidateRetentionWindow(t *testing.T) {
	cfg, err := DefaultConfig(t.TempDir())
	require.NoError(t, err)

	for _, retentionWindow := range []int64{gc.InfiniteRetentionWindow, 0, 1} {
		cfg.RetentionWindow = retentionWindow
		require.NoError(t, cfg.Validate())
	}

	// Below InfiniteRetentionWindow there is no meaning left to assign, and the collector reads
	// any negative value as infinite retention — so a typo'd -2 would silently disable pruning.
	cfg.RetentionWindow = -2
	require.ErrorContains(t, cfg.Validate(), "RetentionWindow")
}
