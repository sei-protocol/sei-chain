package block

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/littblock"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/memblock"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// open opens a handle to a types.BlockDB. Calling it more than once reopens a
// handle to the SAME backing store, simulating a process restart (in-memory
// impls return the same instance; durable impls reopen their files). The caller
// must Close the previous handle before reopening.
type open func() (types.BlockDB, error)

// builder returns an open bound to a fresh, empty backing store, for one subtest.
type builder func(t *testing.T) open

// TestBlockDB exercises the types.BlockDB contract against every implementation,
// building each via its public constructor. Reclamation-below-watermark is
// impl-specific (see TestLittblockReclaimsAcrossRestart and
// TestMemblockPruneRemovesBelowWatermark); these tests only assert the portable
// safety guarantee (nothing at/above the watermark is removed).
func TestBlockDB(t *testing.T) {
	impls := []struct {
		name  string
		build builder
	}{
		{"memblock", func(t *testing.T) open {
			// One shared instance: reopening returns it, so an in-memory
			// "restart" preserves data exactly as a durable reopen would.
			db := memblock.NewBlockDB()
			return func() (types.BlockDB, error) { return db, nil }
		}},
		{"littblock", func(t *testing.T) open {
			// One backing directory: each open reopens a fresh DB over the same
			// files, so a "restart" actually reloads persisted state from disk.
			dir := t.TempDir()
			return func() (types.BlockDB, error) {
				return littblock.NewBlockDB(littConfig(t, dir))
			}
		}},
	}

	for _, impl := range impls {
		t.Run(impl.name, func(t *testing.T) {
			t.Run("EmptyDB", func(t *testing.T) { testEmptyDB(t, impl.build) })
			t.Run("ReadRoundTrip", func(t *testing.T) { testReadRoundTrip(t, impl.build) })
			t.Run("QCByBlockNumber", func(t *testing.T) { testQCByBlockNumber(t, impl.build) })
			t.Run("Iterators", func(t *testing.T) { testIterators(t, impl.build) })
			t.Run("IteratorSnapshot", func(t *testing.T) { testIteratorSnapshot(t, impl.build) })
			t.Run("RestartPersistsData", func(t *testing.T) { testRestartPersistsData(t, impl.build) })
			t.Run("PruneRetainsAtOrAbove", func(t *testing.T) { testPruneRetainsAtOrAbove(t, impl.build) })
			t.Run("PruneStraddleRetainsQC", func(t *testing.T) { testPruneStraddleRetainsQC(t, impl.build) })
			t.Run("PruneRefusesBelowWatermark", func(t *testing.T) { testPruneRefusesBelowWatermark(t, impl.build) })
			t.Run("PruneIdempotentMonotonic", func(t *testing.T) { testPruneIdempotentMonotonic(t, impl.build) })
			t.Run("PruneNeverEmpties", func(t *testing.T) { testPruneNeverEmpties(t, impl.build) })
			t.Run("PruneEmptyStoreThenWriteBelow", func(t *testing.T) {
				testPruneEmptyStoreThenWriteBelow(t, impl.build)
			})
			t.Run("PruneQCAheadOfBlocks", func(t *testing.T) { testPruneQCAheadOfBlocks(t, impl.build) })
			t.Run("PruneQCOnlyThenWriteBlock", func(t *testing.T) {
				testPruneQCOnlyThenWriteBlock(t, impl.build)
			})
			t.Run("WriteOrderRejected", func(t *testing.T) { testWriteOrderRejected(t, impl.build) })
			t.Run("WriteOrderRejectedAfterRestart", func(t *testing.T) {
				testWriteOrderRejectedAfterRestart(t, impl.build)
			})
			t.Run("WriteBlockGaps", func(t *testing.T) { testWriteBlockGaps(t, impl.build) })
			t.Run("WriteBlockRequiresQC", func(t *testing.T) { testWriteBlockRequiresQC(t, impl.build) })
			t.Run("ReverseIteratorEmpty", func(t *testing.T) { testReverseIteratorEmpty(t, impl.build) })
			t.Run("ReverseIteratorOrdering", func(t *testing.T) { testReverseIteratorOrdering(t, impl.build) })
			t.Run("ResumeAfterRestart", func(t *testing.T) { testResumeAfterRestart(t, impl.build) })
		})
	}
}

// openFresh opens a handle to a new, empty backing store and returns it along
// with the open that can reopen the same store (for restart).
func openFresh(t *testing.T, build builder) (types.BlockDB, open) {
	o := build(t)
	db, err := o()
	require.NoError(t, err)
	return db, o
}

// restart flushes and closes db, then reopens a handle to the same backing
// store. The returned handle must be closed by the caller.
func restart(t *testing.T, o open, db types.BlockDB) types.BlockDB {
	require.NoError(t, db.Flush())
	require.NoError(t, db.Close())
	reopened, err := o()
	require.NoError(t, err)
	return reopened
}

func testEmptyDB(t *testing.T, build builder) {
	db, _ := openFresh(t, build)
	defer func() { _ = db.Close() }()

	blk, err := db.ReadBlockByNumber(0)
	require.NoError(t, err)
	require.False(t, blk.IsPresent())

	byHash, err := db.ReadBlockByHash(types.GenBlockHeaderHash(utils.TestRngFromSeed(1)))
	require.NoError(t, err)
	require.False(t, byHash.IsPresent())

	qc, err := db.ReadQCByBlockNumber(0)
	require.NoError(t, err)
	require.False(t, qc.IsPresent())

	blockIt, err := db.Blocks(false)
	require.NoError(t, err)
	ok, err := blockIt.Next()
	require.NoError(t, err)
	require.False(t, ok, "empty db should yield no blocks")
	require.NoError(t, blockIt.Close())

	qcIt, err := db.QCs(false)
	require.NoError(t, err)
	ok, err = qcIt.Next()
	require.NoError(t, err)
	require.False(t, ok, "empty db should yield no QCs")
	require.NoError(t, qcIt.Close())
}

func testReadRoundTrip(t *testing.T, build builder) {
	committee, keys := buildCommittee()
	batches := generateBatches(committee, keys)
	db, _ := openFresh(t, build)
	defer func() { _ = db.Close() }()
	writeAll(t, db, batches)

	assertBlocksReadable(t, db, batches)

	// Misses.
	missNum, err := db.ReadBlockByNumber(1 << 40)
	require.NoError(t, err)
	require.False(t, missNum.IsPresent())

	missHash, err := db.ReadBlockByHash(types.GenBlockHeaderHash(utils.TestRngFromSeed(1)))
	require.NoError(t, err)
	require.False(t, missHash.IsPresent())
}

func testQCByBlockNumber(t *testing.T, build builder) {
	committee, keys := buildCommittee()
	batches := generateBatches(committee, keys)
	db, _ := openFresh(t, build)
	defer func() { _ = db.Close() }()
	writeAll(t, db, batches)

	assertQCsReadable(t, db, committee, batches)

	last := batches[len(batches)-1]
	miss, err := db.ReadQCByBlockNumber(last.next + 1000)
	require.NoError(t, err)
	require.False(t, miss.IsPresent())
}

func testIterators(t *testing.T, build builder) {
	committee, keys := buildCommittee()
	batches := generateBatches(committee, keys)
	db, _ := openFresh(t, build)
	defer func() { _ = db.Close() }()
	writeAll(t, db, batches)

	assertIterators(t, db, committee, batches)
}

// testRestartPersistsData writes a dataset, restarts (close + reopen the same
// backing store), and asserts every read path and iterator still returns the
// full dataset.
func testRestartPersistsData(t *testing.T, build builder) {
	committee, keys := buildCommittee()
	batches := generateBatches(committee, keys)
	db, o := openFresh(t, build)
	defer func() { _ = db.Close() }()
	writeAll(t, db, batches)

	db = restart(t, o, db)

	assertBlocksReadable(t, db, batches)
	assertQCsReadable(t, db, committee, batches)
	assertIterators(t, db, committee, batches)
}

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
		for i, blk := range b.blocks {
			n := b.first + gbn(i)
			if n < watermark {
				continue
			}
			opt, err := db.ReadBlockByNumber(n)
			require.NoError(t, err)
			got, ok := opt.Get()
			require.True(t, ok, "block %d (>= watermark %d) must be retained", n, watermark)
			require.Equal(t, blk.Header().Hash(), got.Header().Hash())
		}
		if b.next > watermark {
			lookup := b.first
			if lookup < watermark {
				lookup = watermark
			}
			opt, err := db.ReadQCByBlockNumber(lookup)
			require.NoError(t, err)
			require.True(t, opt.IsPresent(), "QC [%d,%d) (Next > watermark) must be retained", b.first, b.next)
		}
	}
}

// testPruneStraddleRetainsQC asserts the one nontrivial prune case: a watermark
// that falls strictly *inside* a QC's range. The straddling QC (First < n < Next)
// and every block at or above the watermark must be retained.
func testPruneStraddleRetainsQC(t *testing.T, build builder) {
	committee, keys := buildCommittee()
	batches := generateBatches(committee, keys)
	db, _ := openFresh(t, build)
	defer func() { _ = db.Close() }()
	writeAll(t, db, batches)

	straddled := batches[1]
	watermark := straddled.first + 2
	require.Greater(t, straddled.next, watermark, "watermark must fall strictly inside the batch range")
	require.NoError(t, db.PruneBefore(watermark))

	// Blocks at or above the watermark within the straddled batch survive.
	for i, blk := range straddled.blocks {
		n := straddled.first + gbn(i)
		if n < watermark {
			continue
		}
		opt, err := db.ReadBlockByNumber(n)
		require.NoError(t, err)
		got, ok := opt.Get()
		require.True(t, ok, "block %d (>= watermark %d) must be retained", n, watermark)
		require.Equal(t, blk.Header().Hash(), got.Header().Hash())
	}

	// The straddling QC stays (its Next > watermark); a lookup at or above the
	// watermark inside its range still resolves to it.
	opt, err := db.ReadQCByBlockNumber(watermark)
	require.NoError(t, err)
	got, ok := opt.Get()
	require.True(t, ok, "straddling QC must be retained")
	require.Equal(t, straddled.first, got.QC().GlobalRange().First)
}

// testPruneRefusesBelowWatermark asserts the refuse direction of PruneBefore:
// once the watermark advances past a block, that block is no longer served by
// ReadBlockByNumber, ReadBlockByHash, or the Blocks iterator — so a caller can
// never observe a block whose covering QC may have been pruned out from under it.
func testPruneRefusesBelowWatermark(t *testing.T, build builder) {
	committee, keys := buildCommittee()
	batches := generateBatches(committee, keys)
	db, _ := openFresh(t, build)
	defer func() { _ = db.Close() }()
	writeAll(t, db, batches)

	// Prune at the start of the second batch: all of the first batch is below it.
	watermark := batches[1].first
	require.NoError(t, db.PruneBefore(watermark))

	below := batches[0]
	for i, blk := range below.blocks {
		n := below.first + gbn(i)
		require.Less(t, n, watermark)

		byNum, err := db.ReadBlockByNumber(n)
		require.NoError(t, err)
		require.False(t, byNum.IsPresent(), "block %d below watermark %d must not be served", n, watermark)

		byHash, err := db.ReadBlockByHash(blk.Header().Hash())
		require.NoError(t, err)
		require.False(t, byHash.IsPresent(), "block %d below watermark %d must not be served by hash", n, watermark)
	}

	blockIt, err := db.Blocks(false)
	require.NoError(t, err)
	defer func() { _ = blockIt.Close() }()
	for {
		ok, err := blockIt.Next()
		require.NoError(t, err)
		if !ok {
			break
		}
		require.GreaterOrEqual(t, blockIt.Number(), watermark,
			"iterator must not yield block %d below watermark %d", blockIt.Number(), watermark)
	}
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
		for i, blk := range b.blocks {
			n := b.first + gbn(i)
			if n < watermark {
				continue
			}
			opt, err := db.ReadBlockByNumber(n)
			require.NoError(t, err)
			got, ok := opt.Get()
			require.True(t, ok, "block %d (>= watermark %d) must survive redundant prunes", n, watermark)
			require.Equal(t, blk.Header().Hash(), got.Header().Hash())
		}
	}
}

// testPruneEmptyStoreThenWriteBelow asserts a prune on an empty store neither
// refuses nor reclaims data written afterward, even below the requested point.
// Regression for the empty-store watermark bug, and a memblock/littblock parity
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
	for _, prune := range []types.GlobalBlockNumber{last.first + 1, newest, last.next + 1000} {
		t.Run(fmt.Sprintf("prune=%d", prune), func(t *testing.T) {
			db, _ := openFresh(t, build)
			defer func() { _ = db.Close() }()
			writeAll(t, db, batches)

			require.NoError(t, db.PruneBefore(prune))

			// Every block in the newest cohort is still served on every read path.
			for i, blk := range last.blocks {
				n := last.first + gbn(i)

				byNum, err := db.ReadBlockByNumber(n)
				require.NoError(t, err)
				got, ok := byNum.Get()
				require.True(t, ok, "block %d in the newest cohort must survive PruneBefore(%d)", n, prune)
				require.Equal(t, blk.Header().Hash(), got.Header().Hash())

				byHash, err := db.ReadBlockByHash(blk.Header().Hash())
				require.NoError(t, err)
				bwn, ok := byHash.Get()
				require.True(t, ok, "block %d must survive lookup by hash", n)
				require.Equal(t, n, bwn.Number)

				qc, err := db.ReadQCByBlockNumber(n)
				require.NoError(t, err)
				require.True(t, qc.IsPresent(), "the QC covering the newest cohort must survive")
			}

			// A block below the newest cohort is gone (clamped watermark refuses/removes it).
			belowBatch := batches[len(batches)-2]
			require.Less(t, belowBatch.first, last.first)
			below, err := db.ReadBlockByNumber(belowBatch.first)
			require.NoError(t, err)
			require.False(t, below.IsPresent(), "blocks below the newest cohort must not be served")

			// The block iterator yields exactly the newest cohort, and the QC
			// iterator exactly its covering QC.
			var expected []types.GlobalBlockNumber
			for i := range last.blocks {
				expected = append(expected, last.first+gbn(i))
			}
			blockIt, err := db.Blocks(false)
			require.NoError(t, err)
			defer func() { _ = blockIt.Close() }()
			var blockNums []types.GlobalBlockNumber
			for {
				ok, err := blockIt.Next()
				require.NoError(t, err)
				if !ok {
					break
				}
				blockNums = append(blockNums, blockIt.Number())
			}
			require.Equal(t, expected, blockNums,
				"exactly the newest cohort must remain after PruneBefore(%d)", prune)

			qcIt, err := db.QCs(false)
			require.NoError(t, err)
			defer func() { _ = qcIt.Close() }()
			qcCount := 0
			for {
				ok, err := qcIt.Next()
				require.NoError(t, err)
				if !ok {
					break
				}
				fqc, err := qcIt.QC()
				require.NoError(t, err)
				require.Equal(t, last.first, fqc.QC().GlobalRange().First,
					"only the QC covering the newest cohort must remain")
				qcCount++
			}
			require.Equal(t, 1, qcCount, "exactly one QC (covering the newest cohort) must remain")
		})
	}
}

// testPruneQCAheadOfBlocks pins the min() guard in the prune clamp. QCs are
// written before the blocks they cover, so between writing a QC and its first
// block — and after a crash that persisted a QC but not its blocks — the newest
// QC starts above the newest block (latestQCStartBlock > lastBlockNumber). A
// prune-to-empty request must clamp to the newest actual block, not the newest
// QC's first: clamping to the latter would push the watermark past every written
// block and empty the store. This holds across both implementations.
func testPruneQCAheadOfBlocks(t *testing.T, build builder) {
	committee, keys := buildCommittee()
	batches := generateBatches(committee, keys)
	require.GreaterOrEqual(t, len(batches), 2, "need a filled cohort plus an unfilled newest QC")
	db, _ := openFresh(t, build)
	defer func() { _ = db.Close() }()

	// Fill the first cohort, then write only the QC of the second — no blocks in
	// its range. Now latestQCStartBlock (b1.first) exceeds lastBlockNumber (the
	// last block of b0), since QCs are contiguous (b1.first == b0.next).
	b0 := batches[0]
	require.NoError(t, db.WriteQC(b0.first, b0.next, b0.qc))
	for i, blk := range b0.blocks {
		require.NoError(t, db.WriteBlock(b0.first+gbn(i), blk))
	}
	b1 := batches[1]
	require.NoError(t, db.WriteQC(b1.first, b1.next, b1.qc))
	require.Equal(t, b0.next, b1.first, "QCs must be contiguous for this setup")

	newest := b0.next - 1 // newest actual block; b1.first == b0.next > newest

	require.NoError(t, db.PruneBefore(b1.next+1000))

	// The newest actual block and its covering QC are still served: the clamp
	// used min(latestQCStartBlock, lastBlockNumber), not latestQCStartBlock —
	// otherwise the watermark would sit above every written block.
	blk, err := db.ReadBlockByNumber(newest)
	require.NoError(t, err)
	require.True(t, blk.IsPresent(), "newest block %d must survive; the clamp must not pass it", newest)
	qc, err := db.ReadQCByBlockNumber(newest)
	require.NoError(t, err)
	require.True(t, qc.IsPresent(), "covering QC of the newest block must survive")
}

// testPruneQCOnlyThenWriteBlock asserts that pruning while QCs exist but no
// blocks have been written yet does not delete the covering QC. A subsequent
// WriteBlock still passes its coverage check, so deleting the QC here would
// strand a readable block with no readable covering QC. Regression for the
// memblock PruneBefore fall-through (the clamp was guarded by hasBlocks but the
// deletion loops ran regardless); littblock returns early on !hasBlocks.
func testPruneQCOnlyThenWriteBlock(t *testing.T, build builder) {
	committee, keys := buildCommittee()
	batches := generateBatches(committee, keys)
	db, _ := openFresh(t, build)
	defer func() { _ = db.Close() }()

	// Write only the QC of the first cohort — no blocks yet (hasQC, !hasBlocks).
	b0 := batches[0]
	require.NoError(t, db.WriteQC(b0.first, b0.next, b0.qc))

	// Prune far past the QC. With no blocks, this must be a no-op; the QC cannot
	// be deleted or a later covered WriteBlock would be orphaned.
	require.NoError(t, db.PruneBefore(b0.next+1000))

	// The block is still within [b0.first, b0.next), so its coverage check passes.
	require.NoError(t, db.WriteBlock(b0.first, b0.blocks[0]))

	blk, err := db.ReadBlockByNumber(b0.first)
	require.NoError(t, err)
	require.True(t, blk.IsPresent(), "block %d must be readable after write", b0.first)
	qc, err := db.ReadQCByBlockNumber(b0.first)
	require.NoError(t, err)
	require.True(t, qc.IsPresent(), "covering QC of block %d must survive the earlier prune", b0.first)
}

// testIteratorSnapshot asserts that an iterator observes only the records present
// when it was created — writes made afterward are invisible to it.
func testIteratorSnapshot(t *testing.T, build builder) {
	committee, keys := buildCommittee()
	batches := generateBatches(committee, keys)
	db, _ := openFresh(t, build)
	defer func() { _ = db.Close() }()

	// Write only the first batch, then snapshot iterators over it.
	first := batches[0]
	require.NoError(t, db.WriteQC(first.first, first.next, first.qc))
	for i, blk := range first.blocks {
		require.NoError(t, db.WriteBlock(first.first+gbn(i), blk))
	}

	blockIt, err := db.Blocks(false)
	require.NoError(t, err)
	defer func() { _ = blockIt.Close() }()
	qcIt, err := db.QCs(false)
	require.NoError(t, err)
	defer func() { _ = qcIt.Close() }()

	// Write the remaining batches AFTER both iterators were created.
	writeAll(t, db, batches[1:])

	blockCount := 0
	for {
		ok, err := blockIt.Next()
		require.NoError(t, err)
		if !ok {
			break
		}
		blockCount++
	}
	require.Equal(t, len(first.blocks), blockCount, "block iterator must not observe writes after creation")

	qcCount := 0
	for {
		ok, err := qcIt.Next()
		require.NoError(t, err)
		if !ok {
			break
		}
		qcCount++
	}
	require.Equal(t, 1, qcCount, "QC iterator must not observe writes after creation")
}

func testWriteOrderRejected(t *testing.T, build builder) {
	committee, keys := buildCommittee()
	batches := generateBatches(committee, keys)
	db, _ := openFresh(t, build)
	defer func() { _ = db.Close() }()

	// Write the first batch normally (QC before its blocks).
	b0 := batches[0]
	require.NoError(t, db.WriteQC(b0.first, b0.next, b0.qc))
	for i, blk := range b0.blocks {
		require.NoError(t, db.WriteBlock(b0.first+gbn(i), blk))
	}

	// Re-writing an already-written block number is rejected (not idempotent).
	err := db.WriteBlock(b0.first, b0.blocks[0])
	require.ErrorIs(t, err, types.ErrBlockOutOfOrder)

	// Re-writing the same QC (non-contiguous lowerBound) is rejected.
	err = db.WriteQC(b0.first, b0.next, b0.qc)
	require.ErrorIs(t, err, types.ErrQCNonContiguous)

	// The original records are intact after the rejected writes.
	opt, err := db.ReadBlockByNumber(b0.first)
	require.NoError(t, err)
	require.True(t, opt.IsPresent())
}

// testWriteOrderRejectedAfterRestart asserts the write-order cursors are
// reloaded from persisted state on reopen. After a restart a freshly opened DB
// must still reject an out-of-order block and a non-contiguous QC, and must
// accept the contiguous continuation. A DB that forgot its cursors on restart
// would treat itself as empty and silently accept writes that overwrite or gap
// existing data. (For memblock a "restart" returns the same in-memory instance,
// so its cursors are inherently preserved; this pins the durable reload path.)
func testWriteOrderRejectedAfterRestart(t *testing.T, build builder) {
	committee, keys := buildCommittee()
	batches := generateBatches(committee, keys)
	require.GreaterOrEqual(t, len(batches), 2, "need pre-restart data plus a continuation batch")

	db, o := openFresh(t, build)
	defer func() { _ = db.Close() }()

	// Persist everything except the final batch, then restart.
	head := batches[:len(batches)-1]
	tail := batches[len(batches)-1]
	writeAll(t, db, head)
	db = restart(t, o, db)

	last := head[len(head)-1]

	// Re-writing the last persisted block number is still an ordering violation:
	// only true if lastBlockNumber/hasBlocks were recovered from disk.
	err := db.WriteBlock(last.next-1, last.blocks[len(last.blocks)-1])
	require.ErrorIs(t, err, types.ErrBlockOutOfOrder,
		"reopened DB must reject a non-ascending block (lastBlockNumber not recovered)")

	// Re-writing an already-persisted QC is still a contiguity violation: only
	// true if lastQCNext/hasQC were recovered from disk.
	err = db.WriteQC(last.first, last.next, last.qc)
	require.ErrorIs(t, err, types.ErrQCNonContiguous,
		"reopened DB must reject a non-contiguous QC (lastQCNext not recovered)")

	// The contiguous continuation is accepted — this succeeds only if the cursors
	// were recovered to their exact pre-restart values.
	require.NoError(t, db.WriteQC(tail.first, tail.next, tail.qc))
	for i, blk := range tail.blocks {
		require.NoError(t, db.WriteBlock(tail.first+gbn(i), blk))
	}

	// All data, written on both sides of the restart, reads back.
	assertBlocksReadable(t, db, batches)
	assertQCsReadable(t, db, committee, batches)
}

// testReverseIteratorEmpty asserts a reverse iterator over an empty store
// yields nothing — the resume path relies on this to detect a fresh start.
func testReverseIteratorEmpty(t *testing.T, build builder) {
	db, _ := openFresh(t, build)
	defer func() { _ = db.Close() }()

	blockIt, err := db.Blocks(true)
	require.NoError(t, err)
	ok, err := blockIt.Next()
	require.NoError(t, err)
	require.False(t, ok, "reverse block iterator over empty store must yield nothing")
	require.NoError(t, blockIt.Close())

	qcIt, err := db.QCs(true)
	require.NoError(t, err)
	ok, err = qcIt.Next()
	require.NoError(t, err)
	require.False(t, ok, "reverse QC iterator over empty store must yield nothing")
	require.NoError(t, qcIt.Close())
}

// testReverseIteratorOrdering asserts reverse iteration yields blocks and QCs
// newest-first: the highest block and last QC come first, ordering is strictly
// descending, and secondary keys are skipped (one entry per block / per QC).
func testReverseIteratorOrdering(t *testing.T, build builder) {
	committee, keys := buildCommittee()
	batches := generateBatches(committee, keys)
	db, _ := openFresh(t, build)
	defer func() { _ = db.Close() }()
	writeAll(t, db, batches)

	totalBlocks := 0
	for _, b := range batches {
		totalBlocks += len(b.blocks)
	}
	highest := batches[len(batches)-1].next - 1

	blockIt, err := db.Blocks(true)
	require.NoError(t, err)
	count := 0
	var prev types.GlobalBlockNumber
	havePrev := false
	for {
		ok, err := blockIt.Next()
		require.NoError(t, err)
		if !ok {
			break
		}
		n := blockIt.Number()
		if count == 0 {
			require.Equal(t, highest, n, "reverse blocks must surface the highest block first")
		}
		if havePrev {
			require.Less(t, n, prev, "reverse blocks must iterate descending")
		}
		prev, havePrev = n, true
		count++
	}
	require.NoError(t, blockIt.Close())
	require.Equal(t, totalBlocks, count, "reverse iterator must yield one entry per block (secondaries skipped)")

	lastFirst := batches[len(batches)-1].first
	qcIt, err := db.QCs(true)
	require.NoError(t, err)
	qcCount := 0
	var prevFirst types.GlobalBlockNumber
	haveQC := false
	for {
		ok, err := qcIt.Next()
		require.NoError(t, err)
		if !ok {
			break
		}
		qc, err := qcIt.QC()
		require.NoError(t, err)
		first := qc.QC().GlobalRange().First
		if qcCount == 0 {
			require.Equal(t, lastFirst, first, "reverse QCs must surface the last QC first")
		}
		if haveQC {
			require.Less(t, first, prevFirst, "reverse QCs must iterate descending by First")
		}
		prevFirst, haveQC = first, true
		qcCount++
	}
	require.NoError(t, qcIt.Close())
	require.Equal(t, len(batches), qcCount, "reverse iterator must yield one entry per QC (secondaries skipped)")
}

// testResumeAfterRestart asserts the resume recovery path: after a restart, a
// reverse-iterator scan recovers the highest block number and the last QC, and
// the contiguous continuation is accepted. This is the mechanism blocksim uses
// to append to an existing store instead of restarting at global block 0.
func testResumeAfterRestart(t *testing.T, build builder) {
	committee, keys := buildCommittee()
	batches := generateBatches(committee, keys)
	require.GreaterOrEqual(t, len(batches), 2, "need pre-restart data plus a continuation batch")

	db, o := openFresh(t, build)
	defer func() { _ = db.Close() }()

	head := batches[:len(batches)-1]
	tail := batches[len(batches)-1]
	writeAll(t, db, head)
	db = restart(t, o, db)

	last := head[len(head)-1]

	// Recover the tail via reverse iteration (mirrors blocksim.recoverResumeState).
	highest, ok := recoverHighestBlock(t, db)
	require.True(t, ok)
	require.Equal(t, last.next-1, highest, "recovered highest block must be the last persisted number")

	prevQC, ok := recoverLastQC(t, db)
	require.True(t, ok)
	require.Equal(t, last.first, prevQC.GlobalRange().First, "recovered QC must be the last persisted QC")
	require.Equal(t, last.next, prevQC.GlobalRange().Next)

	// The recovered QC's upper bound is exactly where the continuation begins;
	// writing the next contiguous batch must be accepted.
	require.NoError(t, db.WriteQC(tail.first, tail.next, tail.qc))
	for i, blk := range tail.blocks {
		require.NoError(t, db.WriteBlock(tail.first+gbn(i), blk))
	}

	assertBlocksReadable(t, db, batches)
	assertQCsReadable(t, db, committee, batches)
}

// recoverHighestBlock returns the highest persisted block number via a single
// reverse-iterator step (false if the store is empty).
func recoverHighestBlock(t *testing.T, db types.BlockDB) (types.GlobalBlockNumber, bool) {
	it, err := db.Blocks(true)
	require.NoError(t, err)
	defer func() { _ = it.Close() }()
	ok, err := it.Next()
	require.NoError(t, err)
	if !ok {
		return 0, false
	}
	return it.Number(), true
}

// recoverLastQC returns the most recently persisted QC's *CommitQC via a single
// reverse-iterator step (false if the store has no QCs).
func recoverLastQC(t *testing.T, db types.BlockDB) (*types.CommitQC, bool) {
	it, err := db.QCs(true)
	require.NoError(t, err)
	defer func() { _ = it.Close() }()
	ok, err := it.Next()
	require.NoError(t, err)
	if !ok {
		return nil, false
	}
	fqc, err := it.QC()
	require.NoError(t, err)
	return fqc.QC(), true
}

// testWriteBlockRequiresQC asserts the QC-before-block contract: a block may
// only be written once a QC covering its number has been written, otherwise
// WriteBlock returns ErrBlockMissingQC. This also pins the genesis rule — the
// first write to an empty store must be a QC, never a block.
func testWriteBlockRequiresQC(t *testing.T, build builder) {
	committee, keys := buildCommittee()
	batches := generateBatches(committee, keys)
	db, _ := openFresh(t, build)
	defer func() { _ = db.Close() }()

	b := batches[0]

	// No QC has been written yet: any block is rejected (genesis must be a QC).
	err := db.WriteBlock(b.first, b.blocks[0])
	require.ErrorIs(t, err, types.ErrBlockMissingQC, "block before any QC must be rejected")

	// After the covering QC, every block in its range is accepted.
	require.NoError(t, db.WriteQC(b.first, b.next, b.qc))
	for i, blk := range b.blocks {
		require.NoError(t, db.WriteBlock(b.first+gbn(i), blk))
	}

	// A block at next (just past the covered range) has no covering QC yet.
	err = db.WriteBlock(b.next, batches[1].blocks[0])
	require.ErrorIs(t, err, types.ErrBlockMissingQC, "block past the covered range must be rejected")
}

// testWriteBlockGaps asserts that block numbers need only be strictly
// ascending, not contiguous: within a covering QC's range, gaps are permitted,
// reads resolve only the written numbers, and the iterator surfaces exactly
// those numbers in ascending order. (Every written block still needs a covering
// QC, so the gap numbers are ones the QC covers but no block was written for —
// the same shape a hard crash leaves behind.)
func testWriteBlockGaps(t *testing.T, build builder) {
	committee, keys := buildCommittee()
	batches := generateBatches(committee, keys)
	db, _ := openFresh(t, build)
	defer func() { _ = db.Close() }()

	// A single QC covers the contiguous range [first, next); write blocks at a
	// gapped subset of that range.
	b := batches[0]
	require.NoError(t, db.WriteQC(b.first, b.next, b.qc))
	require.GreaterOrEqual(t, len(b.blocks), 5, "need at least 5 covered numbers to leave gaps")

	written := []types.GlobalBlockNumber{b.first, b.first + 2, b.first + 4}
	gaps := []types.GlobalBlockNumber{b.first + 1, b.first + 3}
	blocks := make(map[types.GlobalBlockNumber]*types.Block, len(written))
	for _, n := range written {
		blk := types.GenBlock(utils.TestRngFromSeed(testSeed + 2 + int64(n)))
		blocks[n] = blk
		require.NoError(t, db.WriteBlock(n, blk))
	}

	for _, n := range written {
		byNum, err := db.ReadBlockByNumber(n)
		require.NoError(t, err)
		got, ok := byNum.Get()
		require.True(t, ok, "block %d should exist", n)
		require.Equal(t, blocks[n].Header().Hash(), got.Header().Hash())

		byHash, err := db.ReadBlockByHash(blocks[n].Header().Hash())
		require.NoError(t, err)
		bwn, ok := byHash.Get()
		require.True(t, ok, "block %d should be found by hash", n)
		require.Equal(t, blocks[n].Header().Hash(), bwn.Block.Header().Hash())
		require.Equal(t, n, bwn.Number, "block %d hash lookup should return its number", n)
	}

	// Numbers in the gaps were never written and must miss.
	for _, gap := range gaps {
		opt, err := db.ReadBlockByNumber(gap)
		require.NoError(t, err)
		require.False(t, opt.IsPresent(), "gap number %d must not be present", gap)
	}

	// The iterator yields exactly the written numbers, ascending.
	blockIt, err := db.Blocks(false)
	require.NoError(t, err)
	defer func() { _ = blockIt.Close() }()
	var got []types.GlobalBlockNumber
	for {
		ok, err := blockIt.Next()
		require.NoError(t, err)
		if !ok {
			break
		}
		got = append(got, blockIt.Number())
	}
	require.Equal(t, written, got, "iterator must surface exactly the gapped numbers in ascending order")
}

// TestMemblockPruneRemovesBelowWatermark verifies the in-memory store's
// synchronous, exact pruning: everything below the watermark is gone
// immediately. Impl-specific (durable stores prune asynchronously) but uses only
// the public API.
func TestMemblockPruneRemovesBelowWatermark(t *testing.T) {
	committee, keys := buildCommittee()
	batches := generateBatches(committee, keys)
	db := memblock.NewBlockDB()
	writeAll(t, db, batches)

	watermark := batches[1].first
	require.NoError(t, db.PruneBefore(watermark))

	// First batch (below watermark) is gone.
	for i := range batches[0].blocks {
		n := batches[0].first + gbn(i)
		opt, err := db.ReadBlockByNumber(n)
		require.NoError(t, err)
		require.False(t, opt.IsPresent(), "block %d should be pruned", n)
	}
	qc, err := db.ReadQCByBlockNumber(batches[0].first)
	require.NoError(t, err)
	require.False(t, qc.IsPresent(), "QC below watermark should be pruned")

	// Watermark block is retained.
	opt, err := db.ReadBlockByNumber(watermark)
	require.NoError(t, err)
	require.True(t, opt.IsPresent())

	// Iterators must skip the pruned records entirely.
	blockIt, err := db.Blocks(false)
	require.NoError(t, err)
	for {
		ok, err := blockIt.Next()
		require.NoError(t, err)
		if !ok {
			break
		}
		require.GreaterOrEqual(t, blockIt.Number(), watermark, "block iterator must not surface pruned blocks")
	}
	require.NoError(t, blockIt.Close())

	qcIt, err := db.QCs(false)
	require.NoError(t, err)
	for {
		ok, err := qcIt.Next()
		require.NoError(t, err)
		if !ok {
			break
		}
		fqc, err := qcIt.QC()
		require.NoError(t, err)
		require.GreaterOrEqual(t, fqc.QC().GlobalRange().First, watermark,
			"QC iterator must not surface pruned QCs")
	}
	require.NoError(t, qcIt.Close())
}

// TestMemblockPruneStraddlingQC verifies the exact in-memory behavior when the
// watermark falls inside a QC's range: blocks below it are removed, blocks at or
// above it stay, and the straddling QC survives and resolves for every in-range
// lookup. memblock keeps no watermark, so even sub-watermark lookups hit the
// retained QC — which the contract permits (below-watermark lookups MAY miss,
// but are not required to).
func TestMemblockPruneStraddlingQC(t *testing.T) {
	committee, keys := buildCommittee()
	batches := generateBatches(committee, keys)
	db := memblock.NewBlockDB()
	writeAll(t, db, batches)

	straddled := batches[1]
	watermark := straddled.first + 2
	require.Greater(t, straddled.next, watermark, "watermark must fall strictly inside the batch range")
	require.NoError(t, db.PruneBefore(watermark))

	// Blocks below the watermark within the straddled batch are gone...
	for i := 0; gbn(i) < watermark-straddled.first; i++ {
		opt, err := db.ReadBlockByNumber(straddled.first + gbn(i))
		require.NoError(t, err)
		require.False(t, opt.IsPresent(), "block %d below watermark must be pruned", straddled.first+gbn(i))
	}
	// ...while those at or above it remain.
	for i := int(watermark - straddled.first); i < len(straddled.blocks); i++ {
		opt, err := db.ReadBlockByNumber(straddled.first + gbn(i))
		require.NoError(t, err)
		require.True(t, opt.IsPresent(), "block %d at/above watermark must be retained", straddled.first+gbn(i))
	}

	// The straddling QC stays (its Next > watermark). memblock tracks no
	// watermark, so it resolves the retained QC for every n in its range,
	// including sub-watermark lookups — which the contract permits.
	above, err := db.ReadQCByBlockNumber(watermark)
	require.NoError(t, err)
	require.True(t, above.IsPresent(), "straddling QC must be retained for lookups at/above watermark")

	below, err := db.ReadQCByBlockNumber(straddled.first)
	require.NoError(t, err)
	require.True(t, below.IsPresent(), "memblock retains the straddling QC for sub-watermark in-range lookups")
}

// The durable reclamation path (data pruned past after a restart is physically
// collected by GC) is covered by TestLittblockReclaimsAcrossRestart in package
// littblock, which inspects the raw table directly — public reads can no longer
// distinguish "reclaimed" from "refused by the read watermark".

// littConfig builds a littblock config rooted at dir with a tiny retention so
// the prune watermark is the sole observable reclamation gate in tests.
func littConfig(t *testing.T, dir string) *littblock.LittBlockConfig {
	cfg, err := littblock.DefaultConfig(dir)
	require.NoError(t, err)
	cfg.Retention = time.Nanosecond
	return cfg
}

// --- shared assertions ---

func assertBlocksReadable(t *testing.T, db types.BlockDB, batches []batch) {
	for _, b := range batches {
		for i, blk := range b.blocks {
			n := b.first + gbn(i)

			byNum, err := db.ReadBlockByNumber(n)
			require.NoError(t, err)
			got, ok := byNum.Get()
			require.True(t, ok, "block %d should exist", n)
			require.Equal(t, blk.Header().Hash(), got.Header().Hash())

			byHash, err := db.ReadBlockByHash(blk.Header().Hash())
			require.NoError(t, err)
			bwn, ok := byHash.Get()
			require.True(t, ok, "block by hash should exist")
			require.Equal(t, blk.Header().Hash(), bwn.Block.Header().Hash())
			require.Equal(t, n, bwn.Number, "block %d hash lookup should return its number", n)
		}
	}
}

func assertQCsReadable(t *testing.T, db types.BlockDB, committee *types.Committee, batches []batch) {
	for _, b := range batches {
		r := b.qc.QC().GlobalRange()
		for n := r.First; n < r.Next; n++ {
			opt, err := db.ReadQCByBlockNumber(n)
			require.NoError(t, err)
			got, ok := opt.Get()
			require.True(t, ok, "QC covering %d should exist", n)
			gr := got.QC().GlobalRange()
			require.Equal(t, r.First, gr.First)
			require.Equal(t, r.Next, gr.Next)
			require.Len(t, got.Headers(), len(b.qc.Headers()), "QC must round-trip its full header set")
			for j := range b.qc.Headers() {
				require.Equal(t, b.qc.Headers()[j].Hash(), got.Headers()[j].Hash())
			}
		}
	}
}

func assertIterators(t *testing.T, db types.BlockDB, committee *types.Committee, batches []batch) {
	totalBlocks := 0
	for _, b := range batches {
		totalBlocks += len(b.blocks)
	}

	blockIt, err := db.Blocks(false)
	require.NoError(t, err)
	count := 0
	var prev types.GlobalBlockNumber
	havePrev := false
	for {
		ok, err := blockIt.Next()
		require.NoError(t, err)
		if !ok {
			break
		}
		n := blockIt.Number()
		if havePrev {
			require.Greater(t, n, prev, "blocks must iterate ascending")
		}
		prev, havePrev = n, true
		blk, err := blockIt.Block()
		require.NoError(t, err)
		require.NotNil(t, blk)
		count++
	}
	require.NoError(t, blockIt.Close())
	require.Equal(t, totalBlocks, count)

	qcIt, err := db.QCs(false)
	require.NoError(t, err)
	qcCount := 0
	var prevFirst types.GlobalBlockNumber
	haveQC := false
	for {
		ok, err := qcIt.Next()
		require.NoError(t, err)
		if !ok {
			break
		}
		qc, err := qcIt.QC()
		require.NoError(t, err)
		first := qc.QC().GlobalRange().First
		if haveQC {
			require.Greater(t, first, prevFirst, "QCs must iterate ascending by First")
		}
		prevFirst, haveQC = first, true
		qcCount++
	}
	require.NoError(t, qcIt.Close())
	require.Equal(t, len(batches), qcCount)
}

// --- block/QC generation (mirrors data.TestCommitQC, which is not importable
// from sei-db because it lives in an internal package) ---

const (
	committeeSize = 4
	blocksPerQC   = 5
	numBatches    = 4
	testSeed      = 20260615
)

var genesisTime = time.Unix(1_700_000_000, 0)

// batch is a contiguous run of blocks at global numbers [first, next) together
// with the QC that finalizes them. next == first+len(blocks).
type batch struct {
	first  types.GlobalBlockNumber
	next   types.GlobalBlockNumber
	blocks []*types.Block
	qc     *types.FullCommitQC
}

// gbn converts a non-negative slice index to a GlobalBlockNumber offset.
func gbn(i int) types.GlobalBlockNumber {
	return types.GlobalBlockNumber(i) //nolint:gosec // i is a non-negative slice index
}

// writeAll writes every batch's QC followed by its blocks (at first+i). The QC
// is written first because WriteBlock rejects a block with no covering QC.
func writeAll(t *testing.T, db types.BlockDB, batches []batch) {
	for _, b := range batches {
		require.NoError(t, db.WriteQC(b.first, b.next, b.qc))
		for i, blk := range b.blocks {
			require.NoError(t, db.WriteBlock(b.first+gbn(i), blk))
		}
	}
}

// buildCommittee returns a deterministic round-robin committee (global numbering
// from 0) and the secret keys that sign its QCs.
func buildCommittee() (*types.Committee, []types.SecretKey) {
	rng := utils.TestRngFromSeed(testSeed)
	keys := make([]types.SecretKey, committeeSize)
	replicas := make([]types.PublicKey, committeeSize)
	for i := range keys {
		keys[i] = types.GenSecretKey(rng)
		replicas[i] = keys[i].Public()
	}
	committee := utils.OrPanic1(types.NewRoundRobinElection(replicas))
	return committee, keys
}

// generateBatches builds a deterministic sequence of contiguous finalized
// batches for the given committee/keys.
func generateBatches(committee *types.Committee, keys []types.SecretKey) []batch {
	rng := utils.TestRngFromSeed(testSeed + 1)
	prev := utils.None[*types.CommitQC]()
	batches := make([]batch, 0, numBatches)
	for range numBatches {
		fqc, blocks := buildFullCommitQC(rng, committee, keys, prev)
		r := fqc.QC().GlobalRange()
		batches = append(batches, batch{first: r.First, next: r.Next, blocks: blocks, qc: fqc})
		prev = utils.Some(fqc.QC())
	}
	return batches
}

func buildFullCommitQC(
	rng utils.Rng,
	committee *types.Committee,
	keys []types.SecretKey,
	prev utils.Option[*types.CommitQC],
) (*types.FullCommitQC, []*types.Block) {
	blocks := map[types.LaneID][]*types.Block{}
	makeBlock := func(producer types.LaneID) *types.Block {
		if bs := blocks[producer]; len(bs) > 0 {
			parent := bs[len(bs)-1]
			return types.NewBlock(producer, parent.Header().Next(), parent.Header().Hash(), types.GenPayload(rng))
		}
		return types.NewBlock(producer, types.LaneRangeOpt(prev, producer).Next(), types.GenBlockHeaderHash(rng), types.GenPayload(rng))
	}
	for range blocksPerQC {
		producer := committee.Lanes().At(rng.Intn(committee.Lanes().Len()))
		blocks[producer] = append(blocks[producer], makeBlock(producer))
	}
	laneQCs := map[types.LaneID]*types.LaneQC{}
	var headers []*types.BlockHeader
	var blockList []*types.Block
	for lane := range committee.Lanes().All() {
		if bs := blocks[lane]; len(bs) > 0 {
			laneQCs[lane] = testLaneQC(keys, bs[len(bs)-1].Header())
			for _, b := range bs {
				headers = append(headers, b.Header())
				blockList = append(blockList, b)
			}
		}
	}
	var appQC utils.Option[*types.AppQC]
	if cqc, ok := prev.Get(); ok {
		p := types.NewAppProposal(cqc.GlobalRange().Next-1, types.NextIndexOpt(prev), types.GenAppHash(rng), cqc.Proposal().EpochIndex())
		appQC = utils.Some(testAppQC(keys, p))
	} else {
		appQC = utils.None[*types.AppQC]()
	}
	ep := types.NewEpoch(0, types.OpenRoadRange(), genesisTime, committee, 0)
	cqc := types.BuildCommitQC(ep, keys, prev, laneQCs, appQC)
	return types.NewFullCommitQC(cqc, headers), blockList
}

func testLaneQC(keys []types.SecretKey, header *types.BlockHeader) *types.LaneQC {
	vote := types.NewLaneVote(header)
	votes := make([]*types.Signed[*types.LaneVote], 0, len(keys))
	for _, k := range keys {
		votes = append(votes, types.Sign(k, vote))
	}
	return types.NewLaneQC(votes)
}

func testAppQC(keys []types.SecretKey, proposal *types.AppProposal) *types.AppQC {
	vote := types.NewAppVote(proposal)
	votes := make([]*types.Signed[*types.AppVote], 0, len(keys))
	for _, k := range keys {
		votes = append(votes, types.Sign(k, vote))
	}
	return types.NewAppQC(votes)
}
