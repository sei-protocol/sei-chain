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
			t.Run("AppProposalByBlockNumber", func(t *testing.T) { testAppProposalByBlockNumber(t, impl.build) })
			t.Run("AppQCByBlockNumber", func(t *testing.T) { testAppQCByBlockNumber(t, impl.build) })
			t.Run("ReadRecent", func(t *testing.T) { testReadRecent(t, impl.build) })
			t.Run("RestartPersistsData", func(t *testing.T) { testRestartPersistsData(t, impl.build) })
			t.Run("PruneRetainsAtOrAbove", func(t *testing.T) { testPruneRetainsAtOrAbove(t, impl.build) })
			t.Run("PruneStraddleRetainsQC", func(t *testing.T) { testPruneStraddleRetainsQC(t, impl.build) })
			t.Run("PruneRefusesBelowWatermark", func(t *testing.T) { testPruneRefusesBelowWatermark(t, impl.build) })
			t.Run("PrunedDistinctFromNotFound", func(t *testing.T) { testPrunedDistinctFromNotFound(t, impl.build) })
			t.Run("PruneIdempotentMonotonic", func(t *testing.T) { testPruneIdempotentMonotonic(t, impl.build) })
			t.Run("PruneNeverEmpties", func(t *testing.T) { testPruneNeverEmpties(t, impl.build) })
			t.Run("PruneEmptyStoreThenWriteBelow", func(t *testing.T) {
				testPruneEmptyStoreThenWriteBelow(t, impl.build)
			})
			t.Run("PruneQCAheadOfBlocks", func(t *testing.T) { testPruneQCAheadOfBlocks(t, impl.build) })
			t.Run("PruneQCOnlyThenWriteBlock", func(t *testing.T) {
				testPruneQCOnlyThenWriteBlock(t, impl.build)
			})
			t.Run("Status", func(t *testing.T) { testStatus(t, impl.build) })
			t.Run("WriteOrderRejected", func(t *testing.T) { testWriteOrderRejected(t, impl.build) })
			t.Run("WriteOrderRejectedAfterRestart", func(t *testing.T) {
				testWriteOrderRejectedAfterRestart(t, impl.build)
			})
			t.Run("WriteBlockGapRejected", func(t *testing.T) { testWriteBlockGapRejected(t, impl.build) })
			t.Run("WriteQCCoversNoBlocksRejected", func(t *testing.T) {
				testWriteQCCoversNoBlocksRejected(t, impl.build)
			})
			t.Run("WriteAppDataOrderRejected", func(t *testing.T) {
				testWriteAppDataOrderRejected(t, impl.build)
			})
			t.Run("PruneWithAppQCNeverEmpties", func(t *testing.T) {
				testPruneWithAppQCNeverEmpties(t, impl.build)
			})
			t.Run("WriteBlockRequiresQC", func(t *testing.T) { testWriteBlockRequiresQC(t, impl.build) })
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

	appQC, err := db.ReadAppQCByBlockNumber(0)
	require.NoError(t, err)
	require.False(t, appQC.IsPresent())

	appProposal, err := db.ReadAppProposalByBlockNumber(0)
	require.NoError(t, err)
	require.False(t, appProposal.IsPresent())

	require.Empty(t, drainRecent(t, db), "empty db should yield no recent records")

	tips := db.Status()
	require.Zero(t, tips.NextBlock, "empty db has no block write tip")
	require.Zero(t, tips.NextQC, "empty db has no QC write tip")
	require.Zero(t, tips.NextAppQC, "empty db has no AppQC write tip")
	require.Zero(t, tips.NextAppProposal, "empty db has no AppProposal write tip")
}

// iterEntry is one position observed while draining an iterator.
type iterEntry struct {
	// n is the position's block number.
	n types.GlobalBlockNumber

	// qc is the covering QC at the position (never nil).
	qc *types.FullCommitQC

	// blk is the block at the position; nil when no block is persisted there.
	blk *types.Block

	// appQC is the AppQC at the position; nil when no AppQC is persisted there.
	appQC *types.AppQC

	// appProposal is the AppProposal at the position; nil when no AppProposal is persisted there.
	appProposal *types.AppProposal
}

// drainRecent reads the recovery-visible recent batch.
func drainRecent(t *testing.T, db types.BlockDB) []iterEntry {
	t.Helper()
	recent, err := db.ReadRecent()
	require.NoError(t, err)
	var entries []iterEntry
	floor := recent.Status.Floor()
	for _, qc := range recent.CommitQCs {
		first := qc.QC().GlobalRange().First
		next := first + gbn(len(qc.Headers()))
		for n := max(first, floor); n < next; n++ {
			entries = append(entries, iterEntry{n: n, qc: qc})
		}
	}
	for _, b := range recent.Blocks {
		found := false
		for i := range entries {
			if entries[i].n == b.Number {
				entries[i].blk = b.Block
				found = true
				break
			}
		}
		require.True(t, found, "block %d must be covered by a recent QC", b.Number)
	}
	if appQC, ok := recent.AppQC.Get(); ok {
		gr := appQC.Proposal().GlobalRange()
		for i := range entries {
			if gr.Has(entries[i].n) {
				entries[i].appQC = appQC
			}
		}
	}
	for _, appProposal := range recent.AppProposals {
		gr := appProposal.GlobalRange()
		for i := range entries {
			if gr.Has(entries[i].n) {
				entries[i].appProposal = appProposal
			}
		}
	}
	return entries
}

// qcFirsts returns the distinct covering-QC Firsts across entries, in encounter order.
func qcFirsts(entries []iterEntry) []types.GlobalBlockNumber {
	var firsts []types.GlobalBlockNumber
	for _, e := range entries {
		first := e.qc.QC().GlobalRange().First
		if len(firsts) == 0 || firsts[len(firsts)-1] != first {
			firsts = append(firsts, first)
		}
	}
	return firsts
}

// presentBlockNumbers returns the numbers of entries whose block is present, in encounter order.
func presentBlockNumbers(entries []iterEntry) []types.GlobalBlockNumber {
	var nums []types.GlobalBlockNumber
	for _, e := range entries {
		if e.blk != nil {
			nums = append(nums, e.n)
		}
	}
	return nums
}

// testStatus asserts Status matches the highest block/QC still present
// (via a full iterator scan), including after prune and restart, and that a QC
// written ahead of its blocks advances only NextQC.
func testStatus(t *testing.T, build builder) {
	committee, keys := buildCommittee()
	batches := generateBatches(committee, keys)
	db, o := openFresh(t, build)
	defer func() { _ = db.Close() }()

	require.NoError(t, db.WriteQC(batches[0].qc))
	tips := db.Status()
	require.Equal(t, batches[0].next, tips.NextQC)
	require.Zero(t, tips.NextBlock, "QC-only store has no block tip")
	assertTipsMatchPresent(t, db)

	for i, blk := range batches[0].blocks {
		require.NoError(t, db.WriteBlock(batches[0].first+gbn(i), blk))
	}
	writeAll(t, db, batches[1:])
	last := batches[len(batches)-1]
	tips = db.Status()
	require.Equal(t, last.next, tips.NextBlock)
	require.Equal(t, last.next, tips.NextQC)
	assertTipsMatchPresent(t, db)

	// Prune away an early cohort; the write tip must still equal the highest
	// present records (newest cohort is never removed).
	require.Greater(t, len(batches), 1)
	require.NoError(t, db.PruneBefore(batches[1].first))
	assertTipsMatchPresent(t, db)
	tips = db.Status()
	require.Equal(t, last.next, tips.NextBlock, "prune must not move the block write tip")
	require.Equal(t, last.next, tips.NextQC, "prune must not move the QC write tip")

	db = restart(t, o, db)
	assertTipsMatchPresent(t, db)
	tips = db.Status()
	require.Equal(t, last.next, tips.NextBlock, "block tip must survive restart")
	require.Equal(t, last.next, tips.NextQC, "QC tip must survive restart")
}

// assertTipsMatchPresent checks Status against point reads for the records the
// public read API still serves.
func assertTipsMatchPresent(t *testing.T, db types.BlockDB) {
	t.Helper()
	tips := db.Status()

	if tips.NextBlock != 0 {
		blk, err := db.ReadBlockByNumber(tips.NextBlock - 1)
		require.NoError(t, err)
		require.True(t, blk.IsPresent(), "NextBlock must point past a readable block")
	}

	if tips.NextQC != 0 {
		qc, err := db.ReadQCByBlockNumber(tips.NextQC - 1)
		require.NoError(t, err)
		got, ok := qc.Get()
		require.True(t, ok, "NextQC must point past a readable QC")
		require.Equal(t, tips.NextQC, got.QC().GlobalRange().Next)
	}

	if tips.NextAppQC != 0 {
		appQC, err := db.ReadAppQCByBlockNumber(tips.NextAppQC - 1)
		require.NoError(t, err)
		got, ok := appQC.Get()
		require.True(t, ok, "NextAppQC must point past a readable AppQC")
		require.Equal(t, tips.NextAppQC, got.Proposal().GlobalRange().Next)
	}
	if tips.NextAppProposal != 0 {
		appProposal, err := db.ReadAppProposalByBlockNumber(tips.NextAppProposal - 1)
		require.NoError(t, err)
		got, ok := appProposal.Get()
		require.True(t, ok, "NextAppProposal must point past a readable AppProposal")
		require.Equal(t, tips.NextAppProposal, got.GlobalRange().Next)
	}
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

func testAppProposalByBlockNumber(t *testing.T, build builder) {
	committee, keys := buildCommittee()
	batches := generateBatches(committee, keys)
	db, o := openFresh(t, build)
	defer func() { _ = db.Close() }()
	writeAll(t, db, batches)

	rng := utils.TestRngFromSeed(testSeed + 90)
	appProposals := []*types.AppProposal{
		appProposalForBatch(rng, batches[0]),
		appProposalForBatch(rng, batches[1]),
	}
	for _, appProposal := range appProposals {
		require.NoError(t, db.WriteAppProposal(appProposal))
	}

	for _, appProposal := range appProposals {
		gr := appProposal.GlobalRange()
		for n := gr.First; n < gr.Next; n++ {
			opt, err := db.ReadAppProposalByBlockNumber(n)
			require.NoError(t, err)
			got, ok := opt.Get()
			require.True(t, ok, "AppProposal covering %d should exist", n)
			require.Equal(t, gr, got.GlobalRange())
			require.Equal(t, appProposal.AppHash(), got.AppHash())
		}
	}
	miss, err := db.ReadAppProposalByBlockNumber(batches[2].first)
	require.NoError(t, err)
	require.False(t, miss.IsPresent(), "CommitQCs/blocks past the AppProposal prefix should not imply AppProposal presence")

	entries := drainRecent(t, db)
	for _, e := range entries {
		switch {
		case e.n < batches[2].first:
			require.NotNil(t, e.appProposal, "iterator should expose AppProposal at %d", e.n)
		default:
			require.Nil(t, e.appProposal, "iterator should not expose AppProposal past the persisted AppProposal prefix at %d", e.n)
		}
	}

	tips := db.Status()
	require.Equal(t, batches[1].next, tips.NextAppProposal)
	db = restart(t, o, db)
	tips = db.Status()
	require.Equal(t, batches[1].next, tips.NextAppProposal, "AppProposal tip must survive restart")
	assertTipsMatchPresent(t, db)

	for _, appProposal := range appProposals {
		gr := appProposal.GlobalRange()
		opt, err := db.ReadAppProposalByBlockNumber(gr.First)
		require.NoError(t, err)
		got, ok := opt.Get()
		require.True(t, ok, "AppProposal [%d,%d) must survive restart", gr.First, gr.Next)
		require.Equal(t, gr, got.GlobalRange())
	}
}

func testAppQCByBlockNumber(t *testing.T, build builder) {
	committee, keys := buildCommittee()
	batches := generateBatches(committee, keys)
	db, o := openFresh(t, build)
	defer func() { _ = db.Close() }()
	writeAll(t, db, batches)

	rng := utils.TestRngFromSeed(testSeed + 100)
	appQCs := []*types.AppQC{
		appQCForBatch(rng, keys, batches[0]),
		appQCForBatch(rng, keys, batches[1]),
	}
	for _, appQC := range appQCs {
		require.NoError(t, db.WriteAppProposal(appQC.Proposal()))
		require.NoError(t, db.WriteAppQC(appQC))
	}

	for _, appQC := range appQCs {
		gr := appQC.Proposal().GlobalRange()
		for n := gr.First; n < gr.Next; n++ {
			opt, err := db.ReadAppQCByBlockNumber(n)
			require.NoError(t, err)
			got, ok := opt.Get()
			require.True(t, ok, "AppQC covering %d should exist", n)
			require.Equal(t, gr, got.Proposal().GlobalRange())
			require.Equal(t, appQC.Proposal().AppHash(), got.Proposal().AppHash())
		}
	}
	miss, err := db.ReadAppQCByBlockNumber(batches[2].first)
	require.NoError(t, err)
	require.False(t, miss.IsPresent(), "CommitQCs/blocks past the AppQC prefix should not imply AppQC presence")

	entries := drainRecent(t, db)
	for _, e := range entries {
		switch {
		case e.n < batches[2].first:
			require.NotNil(t, e.appQC, "iterator should expose AppQC at %d", e.n)
		default:
			require.Nil(t, e.appQC, "iterator should not expose AppQC past the persisted AppQC prefix at %d", e.n)
		}
	}

	tips := db.Status()
	require.Equal(t, batches[1].next, tips.NextAppQC)
	db = restart(t, o, db)
	tips = db.Status()
	require.Equal(t, batches[1].next, tips.NextAppQC, "AppQC tip must survive restart")
	assertTipsMatchPresent(t, db)

	for _, appQC := range appQCs {
		gr := appQC.Proposal().GlobalRange()
		opt, err := db.ReadAppQCByBlockNumber(gr.First)
		require.NoError(t, err)
		got, ok := opt.Get()
		require.True(t, ok, "AppQC [%d,%d) must survive restart", gr.First, gr.Next)
		require.Equal(t, gr, got.Proposal().GlobalRange())
	}
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
	writeAppData(t, db, utils.TestRngFromSeed(testSeed+500), keys, batches)

	// Prune at the start of the second batch: all of the first batch is below it.
	watermark := batches[1].first
	require.NoError(t, db.PruneBefore(watermark))

	below := batches[0]
	for i, blk := range below.blocks {
		n := below.first + gbn(i)
		require.Less(t, n, watermark)

		byNum, err := db.ReadBlockByNumber(n)
		require.ErrorIs(t, err, types.ErrPruned, "block %d below watermark %d must be reported pruned", n, watermark)
		require.False(t, byNum.IsPresent(), "block %d below watermark %d must not be served", n, watermark)

		byHash, err := db.ReadBlockByHash(blk.Header().Hash())
		require.NoError(t, err)
		require.False(t, byHash.IsPresent(), "block %d below watermark %d must not be served by hash", n, watermark)

		appProposal, err := db.ReadAppProposalByBlockNumber(n)
		require.ErrorIs(t, err, types.ErrPruned, "AppProposal at block %d below watermark %d must be reported pruned", n, watermark)
		require.False(t, appProposal.IsPresent(), "AppProposal at block %d below watermark %d must not be served", n, watermark)

		appQC, err := db.ReadAppQCByBlockNumber(n)
		require.ErrorIs(t, err, types.ErrPruned, "AppQC at block %d below watermark %d must be reported pruned", n, watermark)
		require.False(t, appQC.IsPresent(), "AppQC at block %d below watermark %d must not be served", n, watermark)
	}

	for _, e := range drainRecent(t, db) {
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
// The prune point is placed strictly *inside* a QC's range. A QC's cohort of
// blocks must change readability atomically — the watermark may never split a
// cohort — so the effective watermark rounds down to the cohort boundary: the
// whole straddled cohort stays readable, and only the fully-below first cohort
// becomes pruned.
func testPrunedDistinctFromNotFound(t *testing.T, build builder) {
	committee, keys := buildCommittee()
	batches := generateBatches(committee, keys)
	db, _ := openFresh(t, build)
	defer func() { _ = db.Close() }()
	writeAll(t, db, batches)
	writeAppData(t, db, utils.TestRngFromSeed(testSeed+501), keys, batches)

	straddled := batches[1]
	pruneAt := straddled.first + 2
	require.Greater(t, straddled.next, pruneAt, "prune point must fall strictly inside the cohort")
	require.LessOrEqual(t, batches[0].next, straddled.first, "first cohort must sit below the straddled one")
	require.NoError(t, db.PruneBefore(pruneAt))

	// Cohort atomicity: every block in the straddled cohort is still served,
	// including those numerically below the prune point — the cohort does not
	// split.
	for i := range straddled.blocks {
		n := straddled.first + gbn(i)
		opt, err := db.ReadBlockByNumber(n)
		require.NoError(t, err, "block %d in the straddled cohort must not report ErrPruned", n)
		require.True(t, opt.IsPresent(), "block %d in the straddled cohort must remain served", n)
		appProposal, err := db.ReadAppProposalByBlockNumber(n)
		require.NoError(t, err, "AppProposal at block %d in the straddled cohort must not report ErrPruned", n)
		require.True(t, appProposal.IsPresent(), "AppProposal at block %d in the straddled cohort must remain served", n)
		appQC, err := db.ReadAppQCByBlockNumber(n)
		require.NoError(t, err, "AppQC at block %d in the straddled cohort must not report ErrPruned", n)
		require.True(t, appQC.IsPresent(), "AppQC at block %d in the straddled cohort must remain served", n)
	}
	qcOpt, err := db.ReadQCByBlockNumber(straddled.first)
	require.NoError(t, err, "straddled cohort's QC must not report ErrPruned")
	got, ok := qcOpt.Get()
	require.True(t, ok, "straddled cohort's QC must remain served")
	require.Equal(t, straddled.first, got.QC().GlobalRange().First)

	// The fully-below first cohort is pruned: ErrPruned for both the block and
	// its covering QC.
	belowNum := batches[0].first
	blk, err := db.ReadBlockByNumber(belowNum)
	require.ErrorIs(t, err, types.ErrPruned, "below-watermark block must report ErrPruned")
	require.False(t, blk.IsPresent())
	qc, err := db.ReadQCByBlockNumber(belowNum)
	require.ErrorIs(t, err, types.ErrPruned, "below-watermark QC must report ErrPruned")
	require.False(t, qc.IsPresent())
	appProposal, err := db.ReadAppProposalByBlockNumber(belowNum)
	require.ErrorIs(t, err, types.ErrPruned, "below-watermark AppProposal must report ErrPruned")
	require.False(t, appProposal.IsPresent())
	appQC, err := db.ReadAppQCByBlockNumber(belowNum)
	require.ErrorIs(t, err, types.ErrPruned, "below-watermark AppQC must report ErrPruned")
	require.False(t, appQC.IsPresent())

	// Above the watermark but never written: not pruned, just absent.
	unwritten := batches[len(batches)-1].next + 1000
	missBlk, err := db.ReadBlockByNumber(unwritten)
	require.NoError(t, err, "never-written height must not report ErrPruned")
	require.False(t, missBlk.IsPresent())
	missQC, err := db.ReadQCByBlockNumber(unwritten)
	require.NoError(t, err, "never-written height must not report ErrPruned")
	require.False(t, missQC.IsPresent())
	missAppProposal, err := db.ReadAppProposalByBlockNumber(unwritten)
	require.NoError(t, err, "never-written AppProposal height must not report ErrPruned")
	require.False(t, missAppProposal.IsPresent())
	missAppQC, err := db.ReadAppQCByBlockNumber(unwritten)
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
			writeAppData(t, db, utils.TestRngFromSeed(testSeed+502), keys, batches)

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

				appProposal, err := db.ReadAppProposalByBlockNumber(n)
				require.NoError(t, err)
				require.True(t, appProposal.IsPresent(), "the AppProposal covering the newest cohort must survive")

				appQC, err := db.ReadAppQCByBlockNumber(n)
				require.NoError(t, err)
				require.True(t, appQC.IsPresent(), "the AppQC covering the newest cohort must survive")
			}

			// A block below the newest cohort is gone (clamped watermark refuses/removes it).
			belowBatch := batches[len(batches)-2]
			require.Less(t, belowBatch.first, last.first)
			below, err := db.ReadBlockByNumber(belowBatch.first)
			require.ErrorIs(t, err, types.ErrPruned, "blocks below the newest cohort must be reported pruned")
			require.False(t, below.IsPresent(), "blocks below the newest cohort must not be served")
			belowAppProposal, err := db.ReadAppProposalByBlockNumber(belowBatch.first)
			require.ErrorIs(t, err, types.ErrPruned, "AppProposals below the newest cohort must be reported pruned")
			require.False(t, belowAppProposal.IsPresent(), "AppProposals below the newest cohort must not be served")
			belowAppQC, err := db.ReadAppQCByBlockNumber(belowBatch.first)
			require.ErrorIs(t, err, types.ErrPruned, "AppQCs below the newest cohort must be reported pruned")
			require.False(t, belowAppQC.IsPresent(), "AppQCs below the newest cohort must not be served")

			// ReadRecent starts at recentFloor(), while the prune watermark still
			// rounds down to keep the whole newest QC cohort readable.
			entries := drainRecent(t, db)
			require.Equal(t, []types.GlobalBlockNumber{newest}, presentBlockNumbers(entries),
				"ReadRecent must start at recentFloor() after PruneBefore(%d)", prune)
			require.Equal(t, []types.GlobalBlockNumber{last.first}, qcFirsts(entries),
				"ReadRecent must include the QC covering the recent floor")
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
	blk, err := db.ReadBlockByNumber(below.first)
	require.ErrorIs(t, err, types.ErrPruned)
	require.False(t, blk.IsPresent())
	appQC, err := db.ReadAppQCByBlockNumber(below.first)
	require.ErrorIs(t, err, types.ErrPruned)
	require.False(t, appQC.IsPresent())
	appProposal, err := db.ReadAppProposalByBlockNumber(below.first)
	require.ErrorIs(t, err, types.ErrPruned)
	require.False(t, appProposal.IsPresent())

	blk, err = db.ReadBlockByNumber(latestApp.first)
	require.NoError(t, err)
	require.True(t, blk.IsPresent(), "newest AppQC cohort must retain a block")
	qc, err := db.ReadQCByBlockNumber(latestApp.first)
	require.NoError(t, err)
	require.True(t, qc.IsPresent(), "newest AppQC cohort must retain its CommitQC")
	appQC, err = db.ReadAppQCByBlockNumber(latestApp.first)
	require.NoError(t, err)
	got, ok := appQC.Get()
	require.True(t, ok, "newest AppQC cohort must retain its AppQC")
	require.Equal(t, latestApp.qc.QC().GlobalRange(), got.Proposal().GlobalRange())
	appProposal, err = db.ReadAppProposalByBlockNumber(latestApp.first)
	require.NoError(t, err)
	gotProposal, ok := appProposal.Get()
	require.True(t, ok, "newest AppQC cohort must retain its AppProposal")
	require.Equal(t, latestApp.qc.QC().GlobalRange(), gotProposal.GlobalRange())
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
	require.NoError(t, db.WriteQC(b0.qc))
	for i, blk := range b0.blocks {
		require.NoError(t, db.WriteBlock(b0.first+gbn(i), blk))
	}
	b1 := batches[1]
	require.NoError(t, db.WriteQC(b1.qc))
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
	require.NoError(t, db.WriteQC(b0.qc))

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

func testReadRecent(t *testing.T, build builder) {
	committee, keys := buildCommittee()
	batches := generateBatches(committee, keys)
	db, _ := openFresh(t, build)
	defer func() { _ = db.Close() }()

	writeAll(t, db, batches[:2])
	appQC := appQCForBatch(utils.TestRngFromSeed(testSeed+400), keys, batches[0])
	require.NoError(t, db.WriteAppProposal(appQC.Proposal()))
	require.NoError(t, db.WriteAppQC(appQC))
	require.NoError(t, db.WriteQC(batches[2].qc))
	for i, blk := range batches[2].blocks {
		require.NoError(t, db.WriteBlock(batches[2].first+gbn(i), blk))
	}

	recent, err := db.ReadRecent()
	require.NoError(t, err)
	require.Equal(t, db.Status(), recent.Status)
	gotAppQC, ok := recent.AppQC.Get()
	require.True(t, ok)
	require.Equal(t, appQC.Proposal().RoadIndex(), gotAppQC.Proposal().RoadIndex())
	entries := drainRecent(t, db)
	recoveryFloor := appQC.Proposal().GlobalRange().Next - 1
	require.Equal(t, []types.GlobalBlockNumber{batches[0].first, batches[1].first, batches[2].first}, qcFirsts(entries))
	require.Equal(t, batches[2].next-recoveryFloor, types.GlobalBlockNumber(len(entries)))
	require.Equal(t, recoveryFloor, entries[0].n)
	require.Len(t, presentBlockNumbers(entries), int(batches[2].next-recoveryFloor))
}

func testWriteOrderRejected(t *testing.T, build builder) {
	committee, keys := buildCommittee()
	batches := generateBatches(committee, keys)
	db, _ := openFresh(t, build)
	defer func() { _ = db.Close() }()

	// Write the first batch normally (QC before its blocks).
	b0 := batches[0]
	require.NoError(t, db.WriteQC(b0.qc))
	for i, blk := range b0.blocks {
		require.NoError(t, db.WriteBlock(b0.first+gbn(i), blk))
	}

	// Re-writing an already-written block number is rejected (not idempotent).
	err := db.WriteBlock(b0.first, b0.blocks[0])
	require.ErrorIs(t, err, types.ErrBlockOutOfOrder)

	// Re-writing the same QC (its range no longer starts at NextQC) is rejected.
	err = db.WriteQC(b0.qc)
	require.ErrorIs(t, err, types.ErrQCNonContiguous)

	// The original records are intact after the rejected writes.
	opt, err := db.ReadBlockByNumber(b0.first)
	require.NoError(t, err)
	require.True(t, opt.IsPresent())
}

func testWriteAppDataOrderRejected(t *testing.T, build builder) {
	t.Run("AppProposal", func(t *testing.T) {
		testWriteAppProposalOrderRejected(t, build)
	})
	t.Run("AppQC", func(t *testing.T) {
		testWriteAppQCOrderRejected(t, build)
	})
}

func testWriteAppProposalOrderRejected(t *testing.T, build builder) {
	committee, keys := buildCommittee()
	batches := generateBatches(committee, keys)
	db, _ := openFresh(t, build)
	defer func() { _ = db.Close() }()
	rng := utils.TestRngFromSeed(testSeed + 190)

	b0 := batches[0]
	b1 := batches[1]
	b2 := batches[2]

	err := db.WriteAppProposal(appProposalForBatch(rng, b0))
	require.ErrorIs(t, err, types.ErrAppProposalMissingQC, "AppProposal before CommitQC must fail")

	require.NoError(t, db.WriteQC(b0.qc))
	require.NoError(t, db.WriteQC(b1.qc))
	err = db.WriteAppProposal(appProposalForBatch(rng, b1))
	require.ErrorIs(t, err, types.ErrAppProposalNonContiguous, "first AppProposal must start at retained QC floor")

	appProposal0 := appProposalForBatch(rng, b0)
	require.NoError(t, db.WriteAppProposal(appProposal0))

	err = db.WriteAppProposal(appProposal0)
	require.ErrorIs(t, err, types.ErrAppProposalNonContiguous, "duplicate AppProposal write must fail")

	require.NoError(t, db.WriteQC(b2.qc))
	err = db.WriteAppProposal(appProposalForBatch(rng, b2))
	require.ErrorIs(t, err, types.ErrAppProposalNonContiguous, "AppProposal gap must fail")

	require.NoError(t, db.WriteAppProposal(appProposalForBatch(rng, b1)))
	tips := db.Status()
	require.Equal(t, b1.next, tips.NextAppProposal)
}

func testWriteAppQCOrderRejected(t *testing.T, build builder) {
	committee, keys := buildCommittee()
	batches := generateBatches(committee, keys)
	db, _ := openFresh(t, build)
	defer func() { _ = db.Close() }()
	rng := utils.TestRngFromSeed(testSeed + 200)

	b0 := batches[0]
	b1 := batches[1]
	b2 := batches[2]

	err := db.WriteAppQC(appQCForBatch(rng, keys, b0))
	require.ErrorIs(t, err, types.ErrAppQCMissingQC, "AppQC before CommitQC must fail")

	require.NoError(t, db.WriteQC(b0.qc))
	require.NoError(t, db.WriteQC(b1.qc))
	err = db.WriteAppQC(appQCForBatch(rng, keys, b1))
	require.ErrorIs(t, err, types.ErrAppQCNonContiguous, "first AppQC must start at retained QC floor")

	appQC0 := appQCForBatch(rng, keys, b0)
	require.NoError(t, db.WriteAppQC(appQC0))

	err = db.WriteAppQC(appQC0)
	require.ErrorIs(t, err, types.ErrAppQCNonContiguous, "duplicate AppQC write must fail")

	require.NoError(t, db.WriteQC(b2.qc))
	err = db.WriteAppQC(appQCForBatch(rng, keys, b2))
	require.ErrorIs(t, err, types.ErrAppQCNonContiguous, "AppQC gap must fail")

	require.NoError(t, db.WriteAppQC(appQCForBatch(rng, keys, b1)))
	tips := db.Status()
	require.Equal(t, b1.next, tips.NextAppQC)
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
	err = db.WriteQC(last.qc)
	require.ErrorIs(t, err, types.ErrQCNonContiguous,
		"reopened DB must reject a non-contiguous QC (lastQCNext not recovered)")

	// The contiguous continuation is accepted — this succeeds only if the cursors
	// were recovered to their exact pre-restart values.
	require.NoError(t, db.WriteQC(tail.qc))
	for i, blk := range tail.blocks {
		require.NoError(t, db.WriteBlock(tail.first+gbn(i), blk))
	}

	// All data, written on both sides of the restart, reads back.
	assertBlocksReadable(t, db, batches)
	assertQCsReadable(t, db, committee, batches)
}

// testResumeAfterRestart asserts the resume recovery path: after a restart, the
// highest block number and the last QC are recoverable (verified here by a full
// iterator scan; production resume reads Status), and the contiguous continuation
// is accepted. This is the mechanism blocksim uses to append to an existing
// store instead of restarting at global block 0.
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

	// Recover the tail via an iterator scan, and cross-check the Status tips that
	// blocksim.recoverResumeState actually resumes from.
	highest, ok := recoverHighestBlock(t, db)
	require.True(t, ok)
	require.Equal(t, last.next-1, highest, "recovered highest block must be the last persisted number")

	prevQC, ok := recoverLastQC(t, db)
	require.True(t, ok)
	require.Equal(t, last.first, prevQC.GlobalRange().First, "recovered QC must be the last persisted QC")
	require.Equal(t, last.next, prevQC.GlobalRange().Next)

	tips := db.Status()
	require.Equal(t, highest+1, tips.NextBlock, "Status block tip must match the iterator scan")
	require.Equal(t, prevQC.GlobalRange().Next, tips.NextQC, "Status QC tip must match the iterator scan")
	covering, err := db.ReadQCByBlockNumber(tips.NextQC - 1)
	require.NoError(t, err)
	got, ok := covering.Get()
	require.True(t, ok, "the newest QC must be resolvable by point read (blocksim's resume path)")
	require.Equal(t, last.first, got.QC().GlobalRange().First)

	// The recovered QC's upper bound is exactly where the continuation begins;
	// writing the next contiguous batch must be accepted.
	require.NoError(t, db.WriteQC(tail.qc))
	for i, blk := range tail.blocks {
		require.NoError(t, db.WriteBlock(tail.first+gbn(i), blk))
	}

	assertBlocksReadable(t, db, batches)
	assertQCsReadable(t, db, committee, batches)
}

// recoverHighestBlock returns the highest persisted block number via a full
// iterator scan (false if the store has no blocks). Test-side independent
// verification; production resume uses Status (see blocksim.recoverResumeState).
func recoverHighestBlock(t *testing.T, db types.BlockDB) (types.GlobalBlockNumber, bool) {
	t.Helper()
	present := presentBlockNumbers(drainRecent(t, db))
	if len(present) == 0 {
		return 0, false
	}
	return present[len(present)-1], true
}

// recoverLastQC returns the most recently persisted QC's *CommitQC via a full
// iterator scan (false if the store has no QCs). Test-side independent
// verification; production resume uses Status (see blocksim.recoverResumeState).
func recoverLastQC(t *testing.T, db types.BlockDB) (*types.CommitQC, bool) {
	t.Helper()
	entries := drainRecent(t, db)
	if len(entries) == 0 {
		return nil, false
	}
	return entries[len(entries)-1].qc.QC(), true
}

// recoverLastAppQC returns the most recently persisted AppQC via a full
// iterator scan (false if the store has no AppQCs).
func recoverLastAppQC(t *testing.T, db types.BlockDB) (*types.AppQC, bool) {
	t.Helper()
	entries := drainRecent(t, db)
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].appQC != nil {
			return entries[i].appQC, true
		}
	}
	return nil, false
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
	require.NoError(t, db.WriteQC(b.qc))
	for i, blk := range b.blocks {
		require.NoError(t, db.WriteBlock(b.first+gbn(i), blk))
	}

	// A block at next (just past the covered range) has no covering QC yet.
	err = db.WriteBlock(b.next, batches[1].blocks[0])
	require.ErrorIs(t, err, types.ErrBlockMissingQC, "block past the covered range must be rejected")
}

// testWriteQCCoversNoBlocksRejected asserts that a QC covering an empty range is
// rejected identically by every backend. WriteQC derives the covered range from
// the QC alone, so a zero-header QC is the only way to ask a backend to store a
// QC that serves no block number — and storing one would put a record in the
// table that no iterator position can ever reach.
func testWriteQCCoversNoBlocksRejected(t *testing.T, build builder) {
	rng := utils.TestRngFromSeed(testSeed)
	db, _ := openFresh(t, build)
	defer func() { _ = db.Close() }()

	err := db.WriteQC(types.GenFullCommitQCRange(rng, 0, 0))
	require.ErrorIs(t, err, types.ErrQCNonContiguous, "QC covering no blocks must be rejected")

	// The rejection persisted nothing: the store is still empty, so a QC that
	// does cover blocks is still accepted at 0.
	require.Zero(t, db.Status().NextQC)
	require.NoError(t, db.WriteQC(types.GenFullCommitQCRange(rng, 0, 3)))
	require.Equal(t, gbn(3), db.Status().NextQC)
}

// testWriteBlockGapRejected asserts that blocks must be written densely: a
// number that skips past lastBlockNumber+1 is rejected with ErrBlockOutOfOrder
// and persists nothing, even when the covering QC allows it.
func testWriteBlockGapRejected(t *testing.T, build builder) {
	committee, keys := buildCommittee()
	batches := generateBatches(committee, keys)
	db, _ := openFresh(t, build)
	defer func() { _ = db.Close() }()

	b := batches[0]
	require.NoError(t, db.WriteQC(b.qc))
	require.GreaterOrEqual(t, len(b.blocks), 3, "need at least 3 covered numbers to attempt a gap")
	require.NoError(t, db.WriteBlock(b.first, b.blocks[0]))

	// Skipping a covered number is rejected...
	err := db.WriteBlock(b.first+2, b.blocks[2])
	require.ErrorIs(t, err, types.ErrBlockOutOfOrder, "a gapped block write must be rejected")

	// ...and nothing was persisted at the skipped-to number.
	opt, err := db.ReadBlockByNumber(b.first + 2)
	require.NoError(t, err)
	require.False(t, opt.IsPresent(), "the rejected write must not persist")

	// The contiguous continuation is still accepted.
	require.NoError(t, db.WriteBlock(b.first+1, b.blocks[1]))
	require.NoError(t, db.WriteBlock(b.first+2, b.blocks[2]))
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
	writeAppData(t, db, utils.TestRngFromSeed(testSeed+503), keys, batches)

	watermark := batches[1].first
	require.NoError(t, db.PruneBefore(watermark))

	// First batch (below watermark) is gone.
	for i := range batches[0].blocks {
		n := batches[0].first + gbn(i)
		opt, err := db.ReadBlockByNumber(n)
		require.ErrorIs(t, err, types.ErrPruned, "block %d should be pruned", n)
		require.False(t, opt.IsPresent(), "block %d should be pruned", n)
	}
	qc, err := db.ReadQCByBlockNumber(batches[0].first)
	require.ErrorIs(t, err, types.ErrPruned, "QC below watermark should be pruned")
	require.False(t, qc.IsPresent(), "QC below watermark should be pruned")

	// Watermark block is retained.
	opt, err := db.ReadBlockByNumber(watermark)
	require.NoError(t, err)
	require.True(t, opt.IsPresent())

	// The iterator must skip the pruned records entirely.
	for _, e := range drainRecent(t, db) {
		require.GreaterOrEqual(t, e.n, watermark, "iterator must not surface pruned positions")
		require.GreaterOrEqual(t, e.qc.QC().GlobalRange().First, watermark,
			"iterator must not surface pruned QCs")
	}
}

// TestMemblockPruneIntoCohortRoundsDown verifies memblock's in-memory behavior
// when a prune point lands strictly inside a QC's range: the watermark rounds
// down to that cohort's start, so the cohort's blocks change readability
// atomically — the whole straddled cohort stays served (never split), the
// straddling QC is retained, and the fully-below cohort is pruned. Matches
// littblock.
func TestMemblockPruneIntoCohortRoundsDown(t *testing.T) {
	committee, keys := buildCommittee()
	batches := generateBatches(committee, keys)
	db := memblock.NewBlockDB()
	writeAll(t, db, batches)
	writeAppData(t, db, utils.TestRngFromSeed(testSeed+504), keys, batches)

	straddled := batches[1]
	pruneAt := straddled.first + 2
	require.Greater(t, straddled.next, pruneAt, "prune point must fall strictly inside the cohort")
	require.NoError(t, db.PruneBefore(pruneAt))

	// The whole straddled cohort is still served — the watermark rounded down to
	// its start, so none of its blocks are split off below the gate.
	for i := range straddled.blocks {
		n := straddled.first + gbn(i)
		opt, err := db.ReadBlockByNumber(n)
		require.NoError(t, err, "block %d in the straddled cohort must be served", n)
		require.True(t, opt.IsPresent(), "block %d in the straddled cohort must be served", n)
	}

	// The straddling QC is retained and resolves across its whole range.
	for i := range straddled.blocks {
		n := straddled.first + gbn(i)
		qc, err := db.ReadQCByBlockNumber(n)
		require.NoError(t, err, "cohort QC must resolve at %d", n)
		require.True(t, qc.IsPresent(), "cohort QC must resolve at %d", n)
	}

	// The fully-below first cohort is pruned.
	for i := range batches[0].blocks {
		n := batches[0].first + gbn(i)
		opt, err := db.ReadBlockByNumber(n)
		require.ErrorIs(t, err, types.ErrPruned, "block %d in the fully-below cohort must be pruned", n)
		require.False(t, opt.IsPresent(), "block %d in the fully-below cohort must not be served", n)
	}
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

	entries := drainRecent(t, db)
	require.Len(t, entries, totalBlocks, "one position per covered number in a fully-written store")
	require.Equal(t, batches[0].first, entries[0].n, "the scan must begin at the first covered number")
	for i, e := range entries {
		if i > 0 {
			require.Equal(t, entries[i-1].n+1, e.n, "positions must be densely ascending")
		}
		require.NotNil(t, e.blk, "every covered number has a block in a fully-written store")
	}

	wantFirsts := make([]types.GlobalBlockNumber, 0, len(batches))
	for _, b := range batches {
		wantFirsts = append(wantFirsts, b.first)
	}
	require.Equal(t, wantFirsts, qcFirsts(entries), "every QC must be observed once, ascending by First")
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
		require.NoError(t, db.WriteQC(b.qc))
		for i, blk := range b.blocks {
			require.NoError(t, db.WriteBlock(b.first+gbn(i), blk))
		}
	}
}

func writeAppData(t *testing.T, db types.BlockDB, rng utils.Rng, keys []types.SecretKey, batches []batch) {
	t.Helper()
	for _, b := range batches {
		appQC := appQCForBatch(rng, keys, b)
		require.NoError(t, db.WriteAppProposal(appQC.Proposal()))
		require.NoError(t, db.WriteAppQC(appQC))
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
	ep := types.NewEpoch(0, types.OpenRoadRange(), genesisTime, committee, 0)
	cqc := types.BuildCommitQC(ep, keys, prev, laneQCs)
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

func appQCForBatch(rng utils.Rng, keys []types.SecretKey, b batch) *types.AppQC {
	return testAppQC(keys, appProposalForBatch(rng, b))
}

func appProposalForBatch(rng utils.Rng, b batch) *types.AppProposal {
	return types.NewAppProposal(b.qc.QC().Proposal(), types.GenAppHash(rng))
}

func testAppQC(keys []types.SecretKey, proposal *types.AppProposal) *types.AppQC {
	vote := types.NewAppVote(proposal)
	votes := make([]*types.Signed[*types.AppVote], 0, len(keys))
	for _, k := range keys {
		votes = append(votes, types.Sign(k, vote))
	}
	return types.NewAppQC(votes)
}
