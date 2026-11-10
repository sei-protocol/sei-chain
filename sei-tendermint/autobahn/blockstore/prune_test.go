package blockstore

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

// testPruneRetainsAtOrAbove asserts the safety direction of PruneBefore: nothing
// at or above the watermark is removed.
func testPruneRetainsAtOrAbove(t *testing.T, build builder) {
	committee, keys := buildCommittee()
	batches := generateBatches(committee, keys)
	db, _ := openFresh(t, build)
	defer func() { _ = db.Close() }()
	writeAll(t, db, batches)

	// Prune at the start of the second batch.
	watermark := batches[1].first
	require.NoError(t, db.PruneBefore(watermark))

	for _, b := range batches {
		for i, blk := range b.superBlocks() {
			n := b.first + gbn(i)
			if n < watermark {
				continue
			}
			opt, err := db.ReadBlock(n)
			require.NoError(t, err)
			got, ok := opt.Get()
			require.True(t, ok, "block %d (>= watermark %d) must be retained", n, watermark)
			assertSuperBlocksEqual(t, blk, got.SuperBlock())
		}
		if b.next > watermark {
			lookup := b.first
			if lookup < watermark {
				lookup = watermark
			}
			opt, err := db.ReadQC(lookup)
			require.NoError(t, err)
			require.True(t, opt.IsPresent(), "QC [%d,%d) (Next > watermark) must be retained", b.first, b.next)
		}
	}
}

// testPruneAtRoadRetainsQC checks that pruning at a road preserves its complete QC group.
func testPruneAtRoadRetainsQC(t *testing.T, build builder) {
	committee, keys := buildCommittee()
	batches := generateBatches(committee, keys)
	db, _ := openFresh(t, build)
	defer func() { _ = db.Close() }()
	writeAll(t, db, batches)

	retained := batches[1]
	watermark := retained.first
	require.Greater(t, retained.next, watermark, "watermark must precede the next road")
	require.NoError(t, db.PruneBefore(watermark))

	// Blocks at or above the watermark within the retained batch survive.
	for i, blk := range retained.superBlocks() {
		n := retained.first + gbn(i)
		if n < watermark {
			continue
		}
		opt, err := db.ReadBlock(n)
		require.NoError(t, err)
		got, ok := opt.Get()
		require.True(t, ok, "block %d (>= watermark %d) must be retained", n, watermark)
		assertSuperBlocksEqual(t, blk, got.SuperBlock())
	}

	// The retained QC stays (its Next > watermark); a lookup at or above the
	// watermark inside its range still resolves to it.
	opt, err := db.ReadQC(watermark)
	require.NoError(t, err)
	got, ok := opt.Get()
	require.True(t, ok, "retained QC must be retained")
	require.Equal(t, retained.first, got.Index())
}

// testPruneRefusesBelowWatermark asserts the refuse direction of PruneBefore:
// once the watermark advances past a block, that block is no longer served by
// ReadBlock, ReadBlockByHash, or the Blocks iterator — so a caller can
// never observe a block whose covering QC may have been pruned out from under it.
func testPruneRefusesBelowWatermark(t *testing.T, build builder) {
	committee, keys := buildCommittee()
	batches := generateBatches(committee, keys)
	db, _ := openFresh(t, build)
	defer func() { _ = db.Close() }()
	writeAll(t, db, batches)
	writeAppData(t, db, utils.TestRngFromSeed(testSeed+500), keys, batches)

	// Prune at the start of the second batch: all of the first batch is below it.
	watermark := batches[1].first
	require.NoError(t, db.PruneBefore(watermark))

	below := batches[0]
	for i := range below.superBlocks() {
		n := below.first + gbn(i)
		require.Less(t, n, watermark)

		byNum, err := db.ReadBlock(n)
		require.ErrorIs(t, err, types.ErrPruned, "block %d below watermark %d must be reported pruned", n, watermark)
		require.False(t, byNum.IsPresent(), "block %d below watermark %d must not be served", n, watermark)

		byHash, err := db.ReadBlockByHash(below.qc.Header().Hash())
		require.NoError(t, err)
		require.False(t, byHash.IsPresent(), "block %d below watermark %d must not be served by hash", n, watermark)

		appProposal, err := db.ReadAppProposal(n)
		require.ErrorIs(t, err, types.ErrPruned, "AppProposal at block %d below watermark %d must be reported pruned", n, watermark)
		require.False(t, appProposal.IsPresent(), "AppProposal at block %d below watermark %d must not be served", n, watermark)

		appQC, err := db.ReadAppQC(n)
		require.ErrorIs(t, err, types.ErrPruned, "AppQC at block %d below watermark %d must be reported pruned", n, watermark)
		require.False(t, appQC.IsPresent(), "AppQC at block %d below watermark %d must not be served", n, watermark)
	}

	for _, e := range drainSuffix(t, db) {
		require.GreaterOrEqual(t, e.n, watermark,
			"iterator must not yield position %d below watermark %d", e.n, watermark)
	}
}

// testPrunedDistinctFromNotFound is the crux of the ErrPruned contract: after a
// prune, a below-watermark by-number read reports ErrPruned (not served while
// below the watermark), while a never-written height at or above the watermark
// reports a plain utils.None with a nil error (absent, but a future write may
// fill it). Both implementations must agree.
//
// The prune point is a road boundary: the retained SuperBlock stays readable,
// and the previous SuperBlock becomes pruned.
func testPrunedDistinctFromNotFound(t *testing.T, build builder) {
	committee, keys := buildCommittee()
	batches := generateBatches(committee, keys)
	db, _ := openFresh(t, build)
	defer func() { _ = db.Close() }()
	writeAll(t, db, batches)
	writeAppData(t, db, utils.TestRngFromSeed(testSeed+501), keys, batches)

	retained := batches[1]
	pruneAt := retained.first
	require.Greater(t, retained.next, pruneAt, "prune point must precede the next road")
	require.LessOrEqual(t, batches[0].next, retained.first, "first cohort must sit below the retained one")
	require.NoError(t, db.PruneBefore(pruneAt))

	// Cohort atomicity: every block in the retained cohort is still served,
	// including those numerically below the prune point — the cohort does not
	// split.
	for i := range retained.superBlocks() {
		n := retained.first + gbn(i)
		opt, err := db.ReadBlock(n)
		require.NoError(t, err, "block %d in the retained cohort must not report ErrPruned", n)
		require.True(t, opt.IsPresent(), "block %d in the retained cohort must remain served", n)
		appProposal, err := db.ReadAppProposal(n)
		require.NoError(t, err, "AppProposal at block %d in the retained cohort must not report ErrPruned", n)
		require.True(t, appProposal.IsPresent(), "AppProposal at block %d in the retained cohort must remain served", n)
		appQC, err := db.ReadAppQC(n)
		require.NoError(t, err, "AppQC at block %d in the retained cohort must not report ErrPruned", n)
		require.True(t, appQC.IsPresent(), "AppQC at block %d in the retained cohort must remain served", n)
	}
	qcOpt, err := db.ReadQC(retained.first)
	require.NoError(t, err, "retained cohort's QC must not report ErrPruned")
	got, ok := qcOpt.Get()
	require.True(t, ok, "retained cohort's QC must remain served")
	require.Equal(t, retained.first, got.Index())

	// The fully-below first cohort is pruned: ErrPruned for both the block and
	// its covering QC.
	belowNum := batches[0].first
	blk, err := db.ReadBlock(belowNum)
	require.ErrorIs(t, err, types.ErrPruned, "below-watermark block must report ErrPruned")
	require.False(t, blk.IsPresent())
	qc, err := db.ReadQC(belowNum)
	require.ErrorIs(t, err, types.ErrPruned, "below-watermark QC must report ErrPruned")
	require.False(t, qc.IsPresent())
	appProposal, err := db.ReadAppProposal(belowNum)
	require.ErrorIs(t, err, types.ErrPruned, "below-watermark AppProposal must report ErrPruned")
	require.False(t, appProposal.IsPresent())
	appQC, err := db.ReadAppQC(belowNum)
	require.ErrorIs(t, err, types.ErrPruned, "below-watermark AppQC must report ErrPruned")
	require.False(t, appQC.IsPresent())

	// Above the watermark but never written: not pruned, just absent.
	unwritten := batches[len(batches)-1].next + 1000
	missBlk, err := db.ReadBlock(unwritten)
	require.NoError(t, err, "never-written height must not report ErrPruned")
	require.False(t, missBlk.IsPresent())
	missQC, err := db.ReadQC(unwritten)
	require.NoError(t, err, "never-written height must not report ErrPruned")
	require.False(t, missQC.IsPresent())
	missAppProposal, err := db.ReadAppProposal(unwritten)
	require.NoError(t, err, "never-written AppProposal height must not report ErrPruned")
	require.False(t, missAppProposal.IsPresent())
	missAppQC, err := db.ReadAppQC(unwritten)
	require.NoError(t, err, "never-written AppQC height must not report ErrPruned")
	require.False(t, missAppQC.IsPresent())
}

// testPruneIdempotentMonotonic asserts PruneBefore is idempotent and the
// watermark only advances: re-pruning at the same point, or at a lower point,
// is a no-op that neither errors nor disturbs retained data.
func testPruneIdempotentMonotonic(t *testing.T, build builder) {
	committee, keys := buildCommittee()
	batches := generateBatches(committee, keys)
	db, _ := openFresh(t, build)
	defer func() { _ = db.Close() }()
	writeAll(t, db, batches)

	watermark := batches[1].first
	require.NoError(t, db.PruneBefore(watermark))
	require.NoError(t, db.PruneBefore(watermark), "re-pruning at the same watermark must be a no-op")
	require.NoError(t, db.PruneBefore(watermark-1), "pruning below the current watermark must be a no-op")
	require.NoError(t, db.PruneBefore(0), "pruning at zero must be a no-op")

	// Everything at or above the highest watermark is still intact and correct.
	for _, b := range batches {
		for i, blk := range b.superBlocks() {
			n := b.first + gbn(i)
			if n < watermark {
				continue
			}
			opt, err := db.ReadBlock(n)
			require.NoError(t, err)
			got, ok := opt.Get()
			require.True(t, ok, "block %d (>= watermark %d) must survive redundant prunes", n, watermark)
			assertSuperBlocksEqual(t, blk, got.SuperBlock())
		}
	}
}

// testPruneEmptyStoreThenWriteBelow asserts a prune on an empty store neither
// refuses nor reclaims data written afterward, even below the requested point.
// Regression for the empty-store watermark bug, and a storage consistency
// check: an empty-store prune must not advance a read/GC watermark past data that
// does not exist yet.
func testPruneEmptyStoreThenWriteBelow(t *testing.T, build builder) {
	committee, keys := buildCommittee()
	batches := generateBatches(committee, keys)
	db, _ := openFresh(t, build)
	defer func() { _ = db.Close() }()

	// Prune above where we are about to write, while the store is still empty.
	require.NoError(t, db.PruneBefore(batches[1].first))

	// Blocks start at 0, below the prune point; all must remain readable.
	writeAll(t, db, batches)
	assertBlocksReadable(t, db, batches)
	assertQCsReadable(t, db, committee, batches)
}

// testPruneNeverEmpties asserts the store is never emptied by pruning and that
// pruning is monotonic around the newest cohort. Any request whose watermark
// would enter the newest block's cohort — from just past the cohort's first,
// through the newest block, to well beyond every block — is clamped to the
// cohort's first, so the whole newest cohort (and its shared QC) stays readable
// while everything below is gone. The clamp lands on the cohort's first, not
// merely the newest block: the covering QC is retained regardless and covers the
// entire cohort, so a larger n must never retain more. Holds across both
// implementations.
func testPruneNeverEmpties(t *testing.T, build builder) {
	committee, keys := buildCommittee()
	batches := generateBatches(committee, keys)
	require.GreaterOrEqual(t, len(batches), 2, "need a below-cohort batch plus the newest cohort")
	last := batches[len(batches)-1] // the newest block's cohort
	newest := last.next - 1
	require.Greater(t, len(last.blocks), 1, "need a multi-block cohort to exercise a within-cohort prune")

	// Every request lands the watermark inside (or past) the newest cohort:
	// within the cohort, exactly at the newest block, and well past every block.
	// All must clamp identically to the cohort's first.
	for _, prune := range []types.RoadIndex{last.first + 1, newest, last.next + 1000} {
		t.Run(fmt.Sprintf("prune=%d", prune), func(t *testing.T) {
			db, _ := openFresh(t, build)
			defer func() { _ = db.Close() }()
			writeAll(t, db, batches)
			writeAppData(t, db, utils.TestRngFromSeed(testSeed+502), keys, batches)

			require.NoError(t, db.PruneBefore(prune))

			// Every block in the newest cohort is still served on every read path.
			for i, blk := range last.superBlocks() {
				n := last.first + gbn(i)

				byNum, err := db.ReadBlock(n)
				require.NoError(t, err)
				got, ok := byNum.Get()
				require.True(t, ok, "block %d in the newest cohort must survive PruneBefore(%d)", n, prune)
				assertSuperBlocksEqual(t, blk, got.SuperBlock())

				byHash, err := db.ReadBlockByHash(last.qc.Header().Hash())
				require.NoError(t, err)
				bwn, ok := byHash.Get()
				require.True(t, ok, "block %d must survive lookup by hash", n)
				require.Equal(t, n, bwn.QC().Index())

				qc, err := db.ReadQC(n)
				require.NoError(t, err)
				require.True(t, qc.IsPresent(), "the QC covering the newest cohort must survive")

				appProposal, err := db.ReadAppProposal(n)
				require.NoError(t, err)
				require.True(t, appProposal.IsPresent(), "the AppProposal covering the newest cohort must survive")

				appQC, err := db.ReadAppQC(n)
				require.NoError(t, err)
				require.True(t, appQC.IsPresent(), "the AppQC covering the newest cohort must survive")
			}

			// A block below the newest cohort is gone (clamped watermark refuses/removes it).
			belowBatch := batches[len(batches)-2]
			require.Less(t, belowBatch.first, last.first)
			below, err := db.ReadBlock(belowBatch.first)
			require.ErrorIs(t, err, types.ErrPruned, "blocks below the newest cohort must be reported pruned")
			require.False(t, below.IsPresent(), "blocks below the newest cohort must not be served")
			belowAppProposal, err := db.ReadAppProposal(belowBatch.first)
			require.ErrorIs(t, err, types.ErrPruned, "AppProposals below the newest cohort must be reported pruned")
			require.False(t, belowAppProposal.IsPresent(), "AppProposals below the newest cohort must not be served")
			belowAppQC, err := db.ReadAppQC(belowBatch.first)
			require.ErrorIs(t, err, types.ErrPruned, "AppQCs below the newest cohort must be reported pruned")
			require.False(t, belowAppQC.IsPresent(), "AppQCs below the newest cohort must not be served")

			// ReadSuffix and pruning both retain the newest complete QC group.
			entries := drainSuffix(t, db)
			require.Equal(t, []types.RoadIndex{newest}, presentBlockNumbers(entries),
				"ReadSuffix must start at recentFloor() after PruneBefore(%d)", prune)
			require.Equal(t, []types.RoadIndex{last.first}, qcFirsts(entries),
				"ReadSuffix must include the QC covering the suffix floor")
		})
	}
}

func testPruneWithAppQCNeverEmpties(t *testing.T, build builder) {
	committee, keys := buildCommittee()
	batches := generateBatches(committee, keys)
	require.GreaterOrEqual(t, len(batches), 4, "need AppQC prefix to lag CommitQC tip")
	db, _ := openFresh(t, build)
	defer func() { _ = db.Close() }()
	writeAll(t, db, batches)

	rng := utils.TestRngFromSeed(testSeed + 300)
	for _, b := range batches[:3] {
		appQC := appQCForBatch(rng, keys, b)
		require.NoError(t, db.WriteAppProposal(appQC.Proposal()))
		require.NoError(t, db.WriteAppQC(appQC))
	}

	latestApp := batches[2]
	require.NoError(t, db.PruneBefore(batches[len(batches)-1].next+1000))

	below := batches[1]
	blk, err := db.ReadBlock(below.first)
	require.ErrorIs(t, err, types.ErrPruned)
	require.False(t, blk.IsPresent())
	appQC, err := db.ReadAppQC(below.first)
	require.ErrorIs(t, err, types.ErrPruned)
	require.False(t, appQC.IsPresent())
	appProposal, err := db.ReadAppProposal(below.first)
	require.ErrorIs(t, err, types.ErrPruned)
	require.False(t, appProposal.IsPresent())

	blk, err = db.ReadBlock(latestApp.first)
	require.NoError(t, err)
	require.True(t, blk.IsPresent(), "newest AppQC cohort must retain a block")
	qc, err := db.ReadQC(latestApp.first)
	require.NoError(t, err)
	require.True(t, qc.IsPresent(), "newest AppQC cohort must retain its CommitQC")
	appQC, err = db.ReadAppQC(latestApp.first)
	require.NoError(t, err)
	got, ok := appQC.Get()
	require.True(t, ok, "newest AppQC cohort must retain its AppQC")
	require.Equal(t, latestApp.qc.Index(), got.Proposal().Index())
	appProposal, err = db.ReadAppProposal(latestApp.first)
	require.NoError(t, err)
	gotProposal, ok := appProposal.Get()
	require.True(t, ok, "newest AppQC cohort must retain its AppProposal")
	require.Equal(t, latestApp.qc.Index(), gotProposal.Index())
}

// testPruneAfterRestart checks the recovered pruning ceiling: how far the
// application has committed, restored with the rest of the write cursors. A recovery that
// lost it would not fail loudly: the ceiling would fall back to the oldest cohort
// and pruning would silently stop reclaiming.
//
// App data covers only the first three cohorts, so the ceiling is well below the
// CommitQC tip and the two cannot be confused. The floor a prune-to-empty settles
// on must be the newest committed cohort's first, exactly as it is for a store
// that never restarted.
func testPruneAfterRestart(t *testing.T, build builder) {
	committee, keys := buildCommittee()
	batches := generateBatches(committee, keys)
	require.GreaterOrEqual(t, len(batches), 4, "need AppQC prefix to lag CommitQC tip")
	db, o := openFresh(t, build)
	writeAll(t, db, batches)

	rng := utils.TestRngFromSeed(testSeed + 400)
	for _, b := range batches[:3] {
		appQC := appQCForBatch(rng, keys, b)
		require.NoError(t, db.WriteAppProposal(appQC.Proposal()))
		require.NoError(t, db.WriteAppQC(appQC))
	}

	db = restart(t, o, db)
	defer func() { _ = db.Close() }()

	latestApp := batches[2]
	require.NoError(t, db.PruneBefore(batches[len(batches)-1].next+1000))
	require.Equal(t, latestApp.first, db.First(),
		"the recovered ceiling must cap the prune at the newest committed cohort")

	// The floor is where it claims to be, in both directions.
	blk, err := db.ReadBlock(batches[1].first)
	require.ErrorIs(t, err, types.ErrPruned)
	require.False(t, blk.IsPresent())
	blk, err = db.ReadBlock(latestApp.first)
	require.NoError(t, err)
	require.True(t, blk.IsPresent(), "newest committed cohort must survive the capped prune")
}

// TestPruneHidesEverythingBelowWatermark verifies the retention floor on typed reads.
func TestPruneHidesEverythingBelowWatermark(t *testing.T) {
	committee, keys := buildCommittee()
	batches := generateBatches(committee, keys)
	db := newTestStore(t)
	writeAll(t, db, batches)
	writeAppData(t, db, utils.TestRngFromSeed(testSeed+503), keys, batches)

	watermark := batches[1].first
	require.NoError(t, db.PruneBefore(watermark))

	// The first batch is below the watermark and must read as pruned.
	for i := range batches[0].superBlocks() {
		n := batches[0].first + gbn(i)
		opt, err := db.ReadBlock(n)
		require.ErrorIs(t, err, types.ErrPruned, "block %d should be pruned", n)
		require.False(t, opt.IsPresent(), "block %d should be pruned", n)
	}
	qc, err := db.ReadQC(batches[0].first)
	require.ErrorIs(t, err, types.ErrPruned, "QC below watermark should be pruned")
	require.False(t, qc.IsPresent(), "QC below watermark should be pruned")

	// Watermark block is retained.
	opt, err := db.ReadBlock(watermark)
	require.NoError(t, err)
	require.True(t, opt.IsPresent())

	assertSuffixAbove(t, db, watermark)
}

// TestPruneRetainsWholeSuperBlock checks retention at a SuperBlock boundary.
func TestPruneRetainsWholeSuperBlock(t *testing.T) {
	committee, keys := buildCommittee()
	batches := generateBatches(committee, keys)
	db := newTestStore(t)
	writeAll(t, db, batches)
	writeAppData(t, db, utils.TestRngFromSeed(testSeed+504), keys, batches)

	retained := batches[1]
	pruneAt := retained.first
	require.Greater(t, retained.next, pruneAt, "prune point must precede the next road")
	require.NoError(t, db.PruneBefore(pruneAt))

	// The retained SuperBlock and all its lane blocks remain readable.
	for i := range retained.superBlocks() {
		n := retained.first + gbn(i)
		opt, err := db.ReadBlock(n)
		require.NoError(t, err, "block %d in the retained cohort must be served", n)
		require.True(t, opt.IsPresent(), "block %d in the retained cohort must be served", n)
	}

	// The retained QC is retained and resolves across its whole range.
	for i := range retained.superBlocks() {
		n := retained.first + gbn(i)
		qc, err := db.ReadQC(n)
		require.NoError(t, err, "cohort QC must resolve at %d", n)
		require.True(t, qc.IsPresent(), "cohort QC must resolve at %d", n)
	}

	// The fully-below first cohort is pruned.
	for i := range batches[0].superBlocks() {
		n := batches[0].first + gbn(i)
		opt, err := db.ReadBlock(n)
		require.ErrorIs(t, err, types.ErrPruned, "block %d in the fully-below cohort must be pruned", n)
		require.False(t, opt.IsPresent(), "block %d in the fully-below cohort must not be served", n)
	}
}

// assertSuffixAbove fails when the recovery suffix surfaces a position, or a
// covering QC, below floor.
func assertSuffixAbove(t *testing.T, db *Store, floor types.RoadIndex) {
	t.Helper()
	for _, e := range drainSuffix(t, db) {
		require.GreaterOrEqual(t, e.n, floor, "iterator must not surface pruned positions")
		require.GreaterOrEqual(t, e.qc.Index(), floor,
			"iterator must not surface pruned QCs")
	}
}

// collectorBuilder opens a store for the collector contract tests.
type collectorBuilder func(t *testing.T) *Store

// TestCollectorSurface exercises the storage garbage collector contract.
func TestCollectorSurface(t *testing.T) {
	build := collectorBuilder(newTestStore)
	t.Run("EmptyStore", func(t *testing.T) { testCollectorEmptyStore(t, build) })
	t.Run("LatestBlock", func(t *testing.T) { testCollectorLatestBlock(t, build) })
	t.Run("RollbackFloor", func(t *testing.T) { testCollectorRollbackFloor(t, build) })
	t.Run("PruneHistory", func(t *testing.T) { testCollectorPruneHistory(t, build) })
	t.Run("PruneSnapshotsIsANoOp", func(t *testing.T) { testCollectorPruneSnapshots(t, build) })
}

func testCollectorEmptyStore(t *testing.T, build collectorBuilder) {
	db := build(t)
	defer func() { _ = db.Close() }()

	require.NotEmpty(t, db.Name(), "the collector reports the store by name")
	require.True(t, db.ExternalPruning(), "this store reclaims only what the collector released")

	latest, err := db.GetLatestBlock()
	require.NoError(t, err)
	require.Equal(t, uint64(0), latest, "a store that has ingested nothing has no head")
	require.Equal(t, uint64(0), db.GetRollbackFloor(0))
	require.Equal(t, uint64(0), db.GetRollbackFloor(42))

	// The collector prunes every store to a shared minimum, so a store holding
	// nothing is still asked. Answering with an error would fail a cycle that has
	// nothing to do with this store.
	require.NoError(t, db.PruneHistory(1_000))
	require.NoError(t, db.PruneSnapshots(1_000))
	require.Equal(t, uint64(0), uint64(db.First()), "a prune before the first write must not move the floor")
	require.Equal(t, int64(0), gatherPruneWatermark(t), "New on an empty store seeds prune_watermark at 0")
}

func testCollectorLatestBlock(t *testing.T, build collectorBuilder) {
	committee, keys := buildCommittee()
	batches := generateBatches(committee, keys)
	db := build(t)
	defer func() { _ = db.Close() }()

	writeAll(t, db, batches)

	head := batches[len(batches)-1].next - 1
	latest, err := db.GetLatestBlock()
	require.NoError(t, err)
	require.Equal(t, uint64(head), latest, "the head is the newest block written")
}

func testCollectorRollbackFloor(t *testing.T, build collectorBuilder) {
	committee, keys := buildCommittee()
	batches := generateBatches(committee, keys)
	db := build(t)
	defer func() { _ = db.Close() }()

	writeAll(t, db, batches)
	head := uint64(batches[len(batches)-1].next - 1)

	require.Equal(t, head, db.GetRollbackFloor(0), "the whole store is inside a window of 0")
	require.Equal(t, head-1, db.GetRollbackFloor(1))
	// A window deeper than this store's own head is a rollback promise reaching
	// past genesis, so nothing here is eligible for pruning.
	require.Equal(t, uint64(0), db.GetRollbackFloor(head+1))
}

func testCollectorPruneHistory(t *testing.T, build collectorBuilder) {
	committee, keys := buildCommittee()
	batches := generateBatches(committee, keys)
	db := build(t)
	defer func() { _ = db.Close() }()

	writeAll(t, db, batches)
	writeAppData(t, db, utils.TestRng(), keys, batches)

	// PruneHistory is the collector's spelling of PruneBefore, so the floor it
	// settles on is bound by the same rules: rounded down to a cohort start and
	// capped so the store never empties.
	require.NoError(t, db.PruneHistory(uint64(batches[1].first)))
	require.Equal(t, batches[1].first, db.First())
	require.Equal(t, int64(db.First()), gatherPruneWatermark(t), "prune_watermark tracks the clamped BlockDB floor")
}

func testCollectorPruneSnapshots(t *testing.T, build collectorBuilder) {
	committee, keys := buildCommittee()
	batches := generateBatches(committee, keys)
	db := build(t)
	defer func() { _ = db.Close() }()

	writeAll(t, db, batches)
	writeAppData(t, db, utils.TestRng(), keys, batches)

	// This store keeps no snapshots, so its half of the collector's split contract
	// does nothing. It is still called every cycle.
	require.NoError(t, db.PruneSnapshots(uint64(batches[1].first)))
	require.Equal(t, uint64(0), uint64(db.First()), "a snapshot prune must not move the history floor")
}

func gatherPruneWatermark(t *testing.T) int64 {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	const name = "tendermint_autobahn_blockstore_prune_watermark"
	for _, fam := range families {
		if fam.GetName() != name {
			continue
		}
		metrics := fam.GetMetric()
		require.Len(t, metrics, 1, name)
		return int64(metrics[0].GetGauge().GetValue())
	}
	t.Fatalf("%s not registered", name)
	return 0
}

// strandingConfig places a segment boundary at maxSegmentKeyCount keys and disables periodic GC.
func strandingConfig(t *testing.T, dir string, maxSegmentKeyCount uint32) *Config {
	t.Helper()
	cfg := persistenceConfig(t, dir)
	cfg.Litt.TargetSegmentFileSize = math.MaxUint32
	cfg.Litt.MaxSegmentKeyCount = maxSegmentKeyCount
	cfg.Litt.GCPeriod = time.Hour
	cfg.Litt.Fsync = false
	return cfg
}

// TestRecoveredWatermarkTracksOldestBlock verifies the retained floor after GC and restart.
func TestRecoveredWatermarkTracksOldestBlock(t *testing.T) {
	rng := utils.TestRng()
	blocks := storageBlocks(rng, 4, 5)
	dir := t.TempDir()
	cfg := strandingConfig(t, dir, 8)
	store := openPersistedStore(t, cfg)
	writeStorageBlocks(t, store, blocks)
	writeStorageAppData(t, store, rng, blocks)
	require.NoError(t, store.Flush())
	require.NoError(t, store.Close())
	store = openPersistedStore(t, cfg)
	require.NoError(t, store.PruneBefore(1))
	require.NoError(t, store.ForceGC())
	require.NoError(t, store.Close())
	store = openPersistedStore(t, cfg)
	defer func() { require.NoError(t, store.Close()) }()
	require.Equal(t, types.RoadIndex(1), store.First())
	requireRetainedBlocks(t, store, blocks, 1)
}

// TestReclaimsAcrossRestart verifies reclamation of sealed segments below the floor.
func TestReclaimsAcrossRestart(t *testing.T) {
	rng := utils.TestRng()
	blocks := storageBlocks(rng, 4, 5)
	dir := t.TempDir()
	cfg := strandingConfig(t, dir, 8)
	store := openPersistedStore(t, cfg)
	writeStorageBlocks(t, store, blocks)
	writeStorageAppData(t, store, rng, blocks)
	require.NoError(t, store.Flush())
	require.NoError(t, store.Close())
	store = openPersistedStore(t, cfg)
	defer func() { require.NoError(t, store.Close()) }()
	require.NoError(t, store.PruneBefore(3))
	require.NoError(t, store.ForceGC())
	requireRetainedBlocks(t, store, blocks, 3)
}

func requireRetainedBlocks(t *testing.T, store *Store, blocks []*types.RoadBlock, first types.RoadIndex) {
	t.Helper()
	for _, wantBlock := range blocks {
		n := wantBlock.QC().Index()
		retained := n >= first
		block, err := store.ReadBlock(n)
		if retained {
			require.NoError(t, err)
			require.True(t, block.IsPresent())
			requireStorageBlock(t, wantBlock, block.OrPanic("retained block"))
		} else {
			require.ErrorIs(t, err, types.ErrPruned)
			require.False(t, block.IsPresent())
		}
		byHash, err := store.ReadBlockByHash(wantBlock.Header().Hash())
		require.NoError(t, err)
		require.Equal(t, retained, byHash.IsPresent())
		// Check LittDB itself: the typed read floor must not conceal stale primary or alias keys.
		keys := []ledgerKey{(blockKey{Index: n}).encode(), (qcKey{Index: n}).encode(), (blockHashKey{Hash: wantBlock.Header().Hash()}).encode()}
		for offset := range wantBlock.LaneBlocks() {
			keys = append(keys, (laneBlockKey{Index: n, Offset: uint64(offset)}).encode())
		}
		for _, key := range keys {
			exists, err := store.table.Exists(key.bytes())
			require.NoError(t, err)
			require.Equal(t, retained, exists)
		}
	}
}

// TestPackedQCPruneRetainsWholeGroup verifies that a packed cohort spanning the
// segment key-count threshold is retained until its entire range is eligible.
func TestPackedQCPruneRetainsWholeGroup(t *testing.T) {
	rng := utils.TestRng()
	blocks := storageBlocks(rng, 3, crashPerCohort)
	cfg := strandingConfig(t, t.TempDir(), 8)
	store := openPersistedStore(t, cfg)
	writeStorageBlocks(t, store, blocks)
	writeStorageAppData(t, store, rng, blocks)
	require.NoError(t, store.Flush())
	require.NoError(t, store.Close())
	store = openPersistedStore(t, cfg)
	defer func() { require.NoError(t, store.Close()) }()
	require.NoError(t, store.PruneBefore(1))
	require.NoError(t, store.ForceGC())
	requireRetainedBlocks(t, store, blocks, 1)
}
