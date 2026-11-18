package blockstore

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	littdb "github.com/sei-protocol/sei-chain/sei-db/db_engine/litt"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/disktable/keymap"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/disktable/segment"
	litttypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

// storeOpener opens the same backing directory on each call. The caller must close
// the previous handle before reopening.
type storeOpener func() (*Store, error)

// builder returns an open bound to a fresh, empty backing store, for one subtest.
type builder func(t *testing.T) storeOpener

// storeConfig builds a block store config rooted at dir with a tiny retention so
// the prune watermark is the sole observable reclamation gate in tests.
func storeConfig(t *testing.T, dir string) *Config {
	cfg, err := DefaultConfig(dir)
	require.NoError(t, err)
	cfg.RetentionTime = time.Nanosecond
	return cfg
}

// openStore builds a block store over a LittDB-backed database at cfg's paths,
// closing the database if the store fails to build.
func openStore(cfg *Config) (*Store, error) {
	return New(cfg)
}

// newTestStore builds a block store over a fresh temporary LittDB database.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	return NewTestStore(t)
}

// TestBlockStore exercises the typed storage contract, including durable reopen.
func TestBlockStore(t *testing.T) {
	runContract(t, func(t *testing.T) storeOpener {
		dir := t.TempDir()
		return func() (*Store, error) { return New(storeConfig(t, dir)) }
	})
}

func runContract(t *testing.T, build builder) {
	t.Run("EmptyDB", func(t *testing.T) { testEmptyDB(t, build) })
	t.Run("ReadRoundTrip", func(t *testing.T) { testReadRoundTrip(t, build) })
	t.Run("QCByBlockNumber", func(t *testing.T) { testQCByBlockNumber(t, build) })
	t.Run("AppProposalByBlockNumber", func(t *testing.T) { testAppProposalByBlockNumber(t, build) })
	t.Run("AppQCByBlockNumber", func(t *testing.T) { testAppQCByBlockNumber(t, build) })
	t.Run("ReadSuffix", func(t *testing.T) { testReadSuffix(t, build) })
	t.Run("SuffixSnapshot", func(t *testing.T) { testSuffixSnapshot(t, build) })
	t.Run("RestartPersistsData", func(t *testing.T) { testRestartPersistsData(t, build) })
	t.Run("PruneRetainsAtOrAbove", func(t *testing.T) { testPruneRetainsAtOrAbove(t, build) })
	t.Run("PruneAtRoadRetainsQC", func(t *testing.T) { testPruneAtRoadRetainsQC(t, build) })
	t.Run("PruneRefusesBelowWatermark", func(t *testing.T) { testPruneRefusesBelowWatermark(t, build) })
	t.Run("PrunedDistinctFromNotFound", func(t *testing.T) { testPrunedDistinctFromNotFound(t, build) })
	t.Run("PruneIdempotentMonotonic", func(t *testing.T) { testPruneIdempotentMonotonic(t, build) })
	t.Run("PruneNeverEmpties", func(t *testing.T) { testPruneNeverEmpties(t, build) })
	t.Run("PruneEmptyStoreThenWriteBelow", func(t *testing.T) { testPruneEmptyStoreThenWriteBelow(t, build) })

	t.Run("FirstQCSetsFloor", func(t *testing.T) { testFirstQCSetsFloor(t, build) })
	t.Run("Status", func(t *testing.T) { testStatus(t, build) })
	t.Run("WriteOrderRejected", func(t *testing.T) { testWriteOrderRejected(t, build) })
	t.Run("WriteOrderRejectedAfterRestart", func(t *testing.T) { testWriteOrderRejectedAfterRestart(t, build) })

	t.Run("WriteEmptyQC", func(t *testing.T) { testWriteEmptyQC(t, build) })
	t.Run("WriteAppDataOrderRejected", func(t *testing.T) { testWriteAppDataOrderRejected(t, build) })
	t.Run("PruneWithAppQCNeverEmpties", func(t *testing.T) { testPruneWithAppQCNeverEmpties(t, build) })
	t.Run("PruneAfterRestart", func(t *testing.T) { testPruneAfterRestart(t, build) })

	t.Run("ResumeAfterRestart", func(t *testing.T) { testResumeAfterRestart(t, build) })
}

// openFresh opens a handle to a new, empty backing store and returns it along
// with the open that can reopen the same store (for restart).
func openFresh(t *testing.T, build builder) (*Store, storeOpener) {
	o := build(t)
	db, err := o()
	require.NoError(t, err)
	return db, o
}

// restart flushes and closes db, then reopens a handle to the same backing
// store. The returned handle must be closed by the caller.
func restart(t *testing.T, o storeOpener, db *Store) *Store {
	require.NoError(t, db.Flush())
	require.NoError(t, db.Close())
	reopened, err := o()
	require.NoError(t, err)
	return reopened
}

func status(t *testing.T, db *Store) SuffixRange {
	t.Helper()
	return db.Status().OrPanic("non-empty BlockDB status")
}

func testEmptyDB(t *testing.T, build builder) {
	db, _ := openFresh(t, build)
	defer func() { _ = db.Close() }()

	blk, err := db.ReadBlock(0)
	require.NoError(t, err)
	require.False(t, blk.IsPresent())

	byHash, err := db.ReadBlockByHash(types.RoadBlockHeaderHash{1})
	require.NoError(t, err)
	require.False(t, byHash.IsPresent())

	qc, err := db.ReadQC(0)
	require.NoError(t, err)
	require.False(t, qc.IsPresent())

	appQC, err := db.ReadAppQC(0)
	require.NoError(t, err)
	require.False(t, appQC.IsPresent())

	appProposal, err := db.ReadAppProposal(0)
	require.NoError(t, err)
	require.False(t, appProposal.IsPresent())

	require.Empty(t, drainSuffix(t, db), "empty db should yield no suffix records")

	require.False(t, db.Status().IsPresent(), "empty db has no write tips")
}

// iterEntry is one position observed while draining an iterator.
type iterEntry struct {
	// n is the position's block number.
	n types.RoadIndex

	// qc is the covering QC at the position (never nil).
	qc *types.FullCommitQC

	// blk is the block at the position; nil when no block is persisted there.
	blk *types.SuperBlock

	// appQC is the AppQC at the position; nil when no AppQC is persisted there.
	appQC *types.AppQC

	// appProposal is the AppProposal at the position; nil when no AppProposal is persisted there.
	appProposal *types.AppProposal
}

// drainSuffix reads the recovery-visible suffix batch.
func drainSuffix(t *testing.T, db *Store) []iterEntry {
	t.Helper()
	suffix, err := db.ReadSuffix()
	require.NoError(t, err)
	var entries []iterEntry
	floor := suffix.Status.Or(SuffixRange{
		First:           0,
		NextBlock:       0,
		NextAppQC:       0,
		NextAppProposal: 0,
	}).First
	for _, group := range suffix.Blocks {
		qc := group.QC()
		if qc.Index() >= floor {
			entries = append(entries, iterEntry{n: qc.Index(), qc: qc, blk: group.SuperBlock()})
		}
	}
	for _, appQC := range suffix.AppQCs {
		for i := range entries {
			if entries[i].n == appQC.Proposal().Index() {
				entries[i].appQC = appQC
			}
		}
	}
	for _, appProposal := range suffix.AppProposals {
		for i := range entries {
			if entries[i].n == appProposal.Index() {
				entries[i].appProposal = appProposal
			}
		}
	}
	return entries
}

// qcFirsts returns the distinct covering-QC Firsts across entries, in encounter order.
func qcFirsts(entries []iterEntry) []types.RoadIndex {
	var firsts []types.RoadIndex
	for _, e := range entries {
		first := e.qc.Index()
		if len(firsts) == 0 || firsts[len(firsts)-1] != first {
			firsts = append(firsts, first)
		}
	}
	return firsts
}

// presentBlockNumbers returns the numbers of entries whose block is present, in encounter order.
func presentBlockNumbers(entries []iterEntry) []types.RoadIndex {
	var nums []types.RoadIndex
	for _, e := range entries {
		if e.blk != nil {
			nums = append(nums, e.n)
		}
	}
	return nums
}

// testStatus asserts Status matches the highest block/QC still present
// (via a full iterator scan), including after prune and restart. QC writes
// advance NextBlock atomically with their lane blocks.
func testStatus(t *testing.T, build builder) {
	committee, keys := buildCommittee()
	batches := generateBatches(committee, keys)
	db, o := openFresh(t, build)
	defer func() { _ = db.Close() }()

	require.NoError(t, db.WriteBlock(types.NewRoadBlock(batches[0].qc, batches[0].blocks)))
	tips := status(t, db)
	require.Equal(t, batches[0].first, tips.First)
	require.Equal(t, batches[0].next, tips.NextBlock)
	require.Equal(t, tips.First, tips.NextAppQC, "no AppQC has been written")
	require.Equal(t, tips.First, tips.NextAppProposal, "no AppProposal has been written")
	assertTipsMatchPresent(t, db)

	writeAll(t, db, batches[1:])
	last := batches[len(batches)-1]
	tips = status(t, db)
	require.Equal(t, last.next, tips.NextBlock)
	assertTipsMatchPresent(t, db)

	// Prune away an early cohort; the write tip must still equal the highest
	// present records (newest cohort is never removed).
	require.Greater(t, len(batches), 1)
	require.NoError(t, db.PruneBefore(batches[1].first))
	assertTipsMatchPresent(t, db)
	tips = status(t, db)
	require.Equal(t, last.next, tips.NextBlock, "prune must not move the block write tip")

	db = restart(t, o, db)
	assertTipsMatchPresent(t, db)
	tips = status(t, db)
	require.Equal(t, last.next, tips.NextBlock, "block tip must survive restart")
}

// assertTipsMatchPresent checks Status against point reads for the records the
// public read API still serves.
func assertTipsMatchPresent(t *testing.T, db *Store) {
	t.Helper()
	tips := status(t, db)

	if tips.NextBlock > tips.First {
		blk, err := db.ReadBlock(tips.NextBlock - 1)
		require.NoError(t, err)
		require.True(t, blk.IsPresent(), "NextBlock must point past a readable SuperBlock")
	}

	if tips.NextBlock > tips.First {
		qc, err := db.ReadQC(tips.NextBlock - 1)
		require.NoError(t, err)
		got, ok := qc.Get()
		require.True(t, ok, "NextBlock must point past a readable QC")
		require.Equal(t, tips.NextBlock, got.Index()+1)
	}

	if tips.NextAppQC > tips.First {
		appQC, err := db.ReadAppQC(tips.NextAppQC - 1)
		require.NoError(t, err)
		got, ok := appQC.Get()
		require.True(t, ok, "NextAppQC must point past a readable AppQC")
		require.Equal(t, tips.NextAppQC, got.Proposal().Index()+1)
	}
	if tips.NextAppProposal > tips.First {
		appProposal, err := db.ReadAppProposal(tips.NextAppProposal - 1)
		require.NoError(t, err)
		got, ok := appProposal.Get()
		require.True(t, ok, "NextAppProposal must point past a readable AppProposal")
		require.Equal(t, tips.NextAppProposal, got.Index()+1)
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
	missNum, err := db.ReadBlock(1 << 40)
	require.NoError(t, err)
	require.False(t, missNum.IsPresent())

	missHash, err := db.ReadBlockByHash(types.RoadBlockHeaderHash{1})
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
	miss, err := db.ReadQC(last.next + 1000)
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
		opt, err := db.ReadAppProposal(appProposal.Index())
		require.NoError(t, err)
		got, ok := opt.Get()
		require.True(t, ok, "AppProposal covering %d should exist", appProposal.Index())
		require.Equal(t, appProposal.Index(), got.Index())
		require.Equal(t, appProposal.AppHash(), got.AppHash())
	}
	miss, err := db.ReadAppProposal(batches[2].first)
	require.NoError(t, err)
	require.False(t, miss.IsPresent(), "CommitQCs/blocks past the AppProposal prefix should not imply AppProposal presence")

	entries := drainSuffix(t, db)
	for _, e := range entries {
		switch {
		case e.n < batches[2].first:
			require.NotNil(t, e.appProposal, "iterator should expose AppProposal at %d", e.n)
		default:
			require.Nil(t, e.appProposal, "iterator should not expose AppProposal past the persisted AppProposal prefix at %d", e.n)
		}
	}

	tips := status(t, db)
	require.Equal(t, batches[1].next, tips.NextAppProposal)
	db = restart(t, o, db)
	tips = status(t, db)
	require.Equal(t, batches[1].next, tips.NextAppProposal, "AppProposal tip must survive restart")
	assertTipsMatchPresent(t, db)

	for _, appProposal := range appProposals {
		opt, err := db.ReadAppProposal(appProposal.Index())
		require.NoError(t, err)
		got, ok := opt.Get()
		require.True(t, ok, "AppProposal %d must survive restart", appProposal.Index())
		require.Equal(t, appProposal.Index(), got.Index())
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
		opt, err := db.ReadAppQC(appQC.Proposal().Index())
		require.NoError(t, err)
		got, ok := opt.Get()
		require.True(t, ok, "AppQC covering %d should exist", appQC.Proposal().Index())
		require.Equal(t, appQC.Proposal().Index(), got.Proposal().Index())
		require.Equal(t, appQC.Proposal().AppHash(), got.Proposal().AppHash())
	}
	miss, err := db.ReadAppQC(batches[2].first)
	require.NoError(t, err)
	require.False(t, miss.IsPresent(), "CommitQCs/blocks past the AppQC prefix should not imply AppQC presence")

	entries := drainSuffix(t, db)
	for _, e := range entries {
		switch {
		case e.n < batches[2].first:
			require.NotNil(t, e.appQC, "iterator should expose AppQC at %d", e.n)
		default:
			require.Nil(t, e.appQC, "iterator should not expose AppQC past the persisted AppQC prefix at %d", e.n)
		}
	}

	tips := status(t, db)
	require.Equal(t, batches[1].next, tips.NextAppQC)
	db = restart(t, o, db)
	tips = status(t, db)
	require.Equal(t, batches[1].next, tips.NextAppQC, "AppQC tip must survive restart")
	assertTipsMatchPresent(t, db)

	for _, appQC := range appQCs {
		opt, err := db.ReadAppQC(appQC.Proposal().Index())
		require.NoError(t, err)
		got, ok := opt.Get()
		require.True(t, ok, "AppQC %d must survive restart", appQC.Proposal().Index())
		require.Equal(t, appQC.Proposal().Index(), got.Proposal().Index())
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

// testFirstQCSetsFloor asserts that a store whose first QC starts above zero reports
// everything below that QC as pruned, both before and after a restart.
func testFirstQCSetsFloor(t *testing.T, build builder) {
	committee, keys := buildCommittee()
	batches := generateBatches(committee, keys)
	require.GreaterOrEqual(t, len(batches), 2)
	start := batches[1]
	db, o := openFresh(t, build)
	defer func() { _ = db.Close() }()

	require.NoError(t, db.WriteBlock(types.NewRoadBlock(start.qc, start.blocks)))
	check := func(db *Store) {
		t.Helper()
		require.Equal(t, start.first, db.First())
		_, err := db.ReadQC(start.first - 1)
		require.ErrorIs(t, err, types.ErrPruned)
		_, err = db.ReadBlock(start.first - 1)
		require.ErrorIs(t, err, types.ErrPruned)
		got, err := db.ReadQC(start.first)
		require.NoError(t, err)
		require.True(t, got.IsPresent())
	}
	check(db)
	db = restart(t, o, db)
	check(db)
}

func testReadSuffix(t *testing.T, build builder) {
	committee, keys := buildCommittee()
	batches := generateBatches(committee, keys)
	db, _ := openFresh(t, build)
	defer func() { _ = db.Close() }()

	writeAll(t, db, batches[:2])
	appQC := appQCForBatch(utils.TestRngFromSeed(testSeed+400), keys, batches[0])
	require.NoError(t, db.WriteAppProposal(appQC.Proposal()))
	require.NoError(t, db.WriteAppQC(appQC))
	require.NoError(t, db.WriteBlock(types.NewRoadBlock(batches[2].qc, batches[2].blocks)))

	suffix, err := db.ReadSuffix()
	require.NoError(t, err)
	require.Equal(t, status(t, db), suffix.Status.OrPanic("suffix status"))
	require.NotEmpty(t, suffix.AppQCs)
	gotAppQC := suffix.AppQCs[0]
	require.Equal(t, appQC.Proposal().Index(), gotAppQC.Proposal().Index())
	entries := drainSuffix(t, db)
	recoveryFloor := appQC.Proposal().Index()
	require.Equal(t, []types.RoadIndex{batches[0].first, batches[1].first, batches[2].first}, qcFirsts(entries))
	require.Equal(t, batches[2].next-recoveryFloor, types.RoadIndex(len(entries)))
	require.Equal(t, recoveryFloor, entries[0].n)
	require.Len(t, presentBlockNumbers(entries), int(batches[2].next-recoveryFloor))
}

func testWriteOrderRejected(t *testing.T, build builder) {
	committee, keys := buildCommittee()
	batches := generateBatches(committee, keys)
	db, _ := openFresh(t, build)
	defer func() { _ = db.Close() }()
	b0 := batches[0]
	require.NoError(t, db.WriteBlock(types.NewRoadBlock(b0.qc, b0.blocks)))
	require.ErrorIs(t, db.WriteBlock(types.NewRoadBlock(b0.qc, b0.blocks)), types.ErrUnexpectedIndex)
	require.ErrorIs(t, db.WriteBlock(types.NewRoadBlock(batches[2].qc, batches[2].blocks)), types.ErrUnexpectedIndex)
	assertBlocksReadable(t, db, batches[:1])
	require.NoError(t, db.WriteBlock(types.NewRoadBlock(batches[1].qc, batches[1].blocks)))
	assertBlocksReadable(t, db, batches[:2])
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
	require.ErrorIs(t, err, types.ErrMissingQC, "AppProposal before CommitQC must fail")

	require.NoError(t, db.WriteBlock(types.NewRoadBlock(b0.qc, b0.blocks)))
	require.NoError(t, db.WriteBlock(types.NewRoadBlock(b1.qc, b1.blocks)))
	err = db.WriteAppProposal(appProposalForBatch(rng, b1))
	require.ErrorIs(t, err, types.ErrUnexpectedIndex, "first AppProposal must start at retained QC floor")

	appProposal0 := appProposalForBatch(rng, b0)
	require.NoError(t, db.WriteAppProposal(appProposal0))

	err = db.WriteAppProposal(appProposal0)
	require.ErrorIs(t, err, types.ErrUnexpectedIndex, "duplicate AppProposal write must fail")

	require.NoError(t, db.WriteBlock(types.NewRoadBlock(b2.qc, b2.blocks)))
	err = db.WriteAppProposal(appProposalForBatch(rng, b2))
	require.ErrorIs(t, err, types.ErrUnexpectedIndex, "AppProposal gap must fail")

	require.NoError(t, db.WriteAppProposal(appProposalForBatch(rng, b1)))
	tips := status(t, db)
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
	require.ErrorIs(t, err, types.ErrMissingAppProposal, "AppQC before CommitQC must fail")

	require.NoError(t, db.WriteBlock(types.NewRoadBlock(b0.qc, b0.blocks)))
	require.NoError(t, db.WriteBlock(types.NewRoadBlock(b1.qc, b1.blocks)))
	err = db.WriteAppQC(appQCForBatch(rng, keys, b1))
	require.ErrorIs(t, err, types.ErrMissingAppProposal, "AppQC requires its AppProposal")

	require.NoError(t, db.WriteAppProposal(appProposalForBatch(rng, b0)))
	appQC0 := appQCForBatch(rng, keys, b0)
	require.NoError(t, db.WriteAppQC(appQC0))

	err = db.WriteAppQC(appQC0)
	require.ErrorIs(t, err, types.ErrUnexpectedIndex, "duplicate AppQC write must fail")

	require.NoError(t, db.WriteBlock(types.NewRoadBlock(b2.qc, b2.blocks)))
	require.NoError(t, db.WriteAppProposal(appProposalForBatch(rng, b1)))
	require.NoError(t, db.WriteAppProposal(appProposalForBatch(rng, b2)))
	err = db.WriteAppQC(appQCForBatch(rng, keys, b2))
	require.ErrorIs(t, err, types.ErrUnexpectedIndex, "AppQC gap must fail")

	require.NoError(t, db.WriteAppQC(appQCForBatch(rng, keys, b1)))
	tips := status(t, db)
	require.Equal(t, b1.next, tips.NextAppQC)
}

// testWriteOrderRejectedAfterRestart asserts the write-order cursors are
// reloaded from persisted state on reopen. After a restart a freshly opened DB
// must still reject a non-contiguous QC and its blocks, and must
// accept the contiguous continuation. A DB that forgot its cursors on restart
// would treat itself as empty and silently accept writes that overwrite or gap
// existing data.
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

	// A reopened store rejects an already-persisted QC and its blocks.
	err := db.WriteBlock(types.NewRoadBlock(last.qc, last.blocks))
	require.ErrorIs(t, err, types.ErrUnexpectedIndex)

	// The contiguous continuation is accepted — this succeeds only if the cursors
	// were recovered to their exact pre-restart values.
	require.NoError(t, db.WriteBlock(types.NewRoadBlock(tail.qc, tail.blocks)))

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
	require.Equal(t, last.first, prevQC.Index(), "recovered QC must be the last persisted QC")
	require.Equal(t, last.next, prevQC.Index()+1)

	tips := status(t, db)
	require.Equal(t, highest+1, tips.NextBlock, "Status block tip must match the iterator scan")
	require.Equal(t, prevQC.Index()+1, tips.NextBlock, "Status QC tip must match the iterator scan")
	covering, err := db.ReadQC(tips.NextBlock - 1)
	require.NoError(t, err)
	got, ok := covering.Get()
	require.True(t, ok, "the newest QC must be resolvable by point read (blocksim's resume path)")
	require.Equal(t, last.first, got.Index())

	// The recovered QC's upper bound is exactly where the continuation begins;
	// writing the next contiguous batch must be accepted.
	require.NoError(t, db.WriteBlock(types.NewRoadBlock(tail.qc, tail.blocks)))

	assertBlocksReadable(t, db, batches)
	assertQCsReadable(t, db, committee, batches)
}

// recoverHighestBlock returns the highest persisted block number via a full
// iterator scan (false if the store has no blocks). Test-side independent
// verification; production resume uses Status (see blocksim.recoverResumeState).
func recoverHighestBlock(t *testing.T, db *Store) (types.RoadIndex, bool) {
	t.Helper()
	present := presentBlockNumbers(drainSuffix(t, db))
	if len(present) == 0 {
		return 0, false
	}
	return present[len(present)-1], true
}

// recoverLastQC returns the most recently persisted QC's *CommitQC via a full
// iterator scan (false if the store has no QCs). Test-side independent
// verification; production resume uses Status (see blocksim.recoverResumeState).
func recoverLastQC(t *testing.T, db *Store) (*types.CommitQC, bool) {
	t.Helper()
	entries := drainSuffix(t, db)
	if len(entries) == 0 {
		return nil, false
	}
	return entries[len(entries)-1].qc.QC(), true
}

// recoverLastAppQC returns the most recently persisted AppQC via a full
// iterator scan (false if the store has no AppQCs).
func recoverLastAppQC(t *testing.T, db *Store) (*types.AppQC, bool) {
	t.Helper()
	entries := drainSuffix(t, db)
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].appQC != nil {
			return entries[i].appQC, true
		}
	}
	return nil, false
}

// testWriteEmptyQC verifies that an empty QC persists a SuperBlock and advances the road.
func testWriteEmptyQC(t *testing.T, build builder) {
	rng := utils.TestRng()
	db, reopen := openFresh(t, build)
	defer func() { _ = db.Close() }()
	qc := types.GenFullCommitQCAt(rng, 0, 0)
	require.NoError(t, db.WriteBlock(types.NewRoadBlock(qc, nil)))
	require.Equal(t, types.RoadIndex(1), status(t, db).NextBlock)
	db = restart(t, reopen, db)
	block, err := db.ReadBlock(qc.Index())
	require.NoError(t, err)
	got, ok := block.Get()
	require.True(t, ok)
	require.Empty(t, got.LaneBlocks())
	byHash, err := db.ReadBlockByHash(got.Header().Hash())
	require.NoError(t, err)
	require.True(t, byHash.IsPresent())
	committee, keys := buildCommittee()
	b := generateBatches(committee, keys)[1]
	require.NoError(t, db.WriteBlock(types.NewRoadBlock(b.qc, b.blocks)))
	require.Equal(t, b.next, status(t, db).NextBlock)
}

// --- shared assertions ---

func assertSuperBlocksEqual(t *testing.T, want, got *types.SuperBlock) {
	t.Helper()
	require.Equal(t, want.Hash, got.Hash)
	require.Equal(t, want.Height, got.Height)
	require.True(t, want.Timestamp.Equal(got.Timestamp))
	require.Equal(t, want.GasWanted, got.GasWanted)
	require.Equal(t, want.Txs, got.Txs)
}

func assertBlocksReadable(t *testing.T, db *Store, batches []batch) {
	for _, b := range batches {
		for offset, laneBlock := range b.blocks {
			opt, err := db.ReadLaneBlock(types.LaneBlockPos{Index: b.first, Offset: uint64(offset)})
			require.NoError(t, err)
			got, ok := opt.Get()
			require.True(t, ok)
			require.Equal(t, laneBlock.Header().Hash(), got.Header().Hash())
			require.Equal(t, laneBlock.Payload().Txs(), got.Payload().Txs())
			alias, err := db.ReadBlockByHash(types.RoadBlockHeaderHash(laneBlock.Header().Hash()))
			require.NoError(t, err)
			require.False(t, alias.IsPresent(), "lane block hashes must not index SuperBlocks")
		}
		missing, err := db.ReadLaneBlock(types.LaneBlockPos{Index: b.first, Offset: uint64(len(b.blocks))})
		require.NoError(t, err)
		require.False(t, missing.IsPresent())
		for i, blk := range b.superBlocks() {
			n := b.first + gbn(i)

			byNum, err := db.ReadBlock(n)
			require.NoError(t, err)
			got, ok := byNum.Get()
			require.True(t, ok, "block %d should exist", n)
			assertSuperBlocksEqual(t, blk, got.SuperBlock())
			require.Equal(t, types.FullCommitQCConv.Encode(b.qc), types.FullCommitQCConv.Encode(got.QC()))
			require.Len(t, got.LaneBlocks(), len(b.blocks))
			for offset, laneBlock := range b.blocks {
				require.Equal(t, types.BlockConv.Encode(laneBlock), types.BlockConv.Encode(got.LaneBlocks()[offset]))
			}

			byHash, err := db.ReadBlockByHash(b.qc.Header().Hash())
			require.NoError(t, err)
			bwn, ok := byHash.Get()
			require.True(t, ok, "block by hash should exist")
			assertSuperBlocksEqual(t, blk, bwn.SuperBlock())
			require.Equal(t, n, bwn.QC().Index(), "block %d hash lookup should return its number", n)
		}
	}
}

func assertQCsReadable(t *testing.T, db *Store, committee *types.Committee, batches []batch) {
	for _, b := range batches {
		opt, err := db.ReadQC(b.qc.Index())
		require.NoError(t, err)
		got, ok := opt.Get()
		require.True(t, ok, "QC covering %d should exist", b.qc.Index())
		require.Equal(t, b.qc.Index(), got.Index())
		require.Len(t, got.Headers(), len(b.qc.Headers()), "QC must round-trip its full header set")
		for j := range b.qc.Headers() {
			require.Equal(t, b.qc.Headers()[j].Hash(), got.Headers()[j].Hash())
		}
	}
}

func assertIterators(t *testing.T, db *Store, committee *types.Committee, batches []batch) {
	totalBlocks := 0
	for range batches {
		totalBlocks += 1
	}

	entries := drainSuffix(t, db)
	require.Len(t, entries, totalBlocks, "one position per covered number in a fully-written store")
	require.Equal(t, batches[0].first, entries[0].n, "the scan must begin at the first covered number")
	for i, e := range entries {
		if i > 0 {
			require.Equal(t, entries[i-1].n+1, e.n, "positions must be densely ascending")
		}
		require.NotNil(t, e.blk, "every covered number has a block in a fully-written store")
	}

	wantFirsts := make([]types.RoadIndex, 0, len(batches))
	for _, b := range batches {
		wantFirsts = append(wantFirsts, b.first)
	}
	require.Equal(t, wantFirsts, qcFirsts(entries), "every QC must be observed once, ascending by First")
}

// --- block/QC generation (mirrors data.TestCommitQC, which is not importable
// from sei-db because it lives in an internal package) ---

// testSeed is the base seed every fixture derives from. A backend offsets it to
// draw data that is independent of another test's yet still deterministic.
const testSeed = 20260615

const (
	committeeSize = 4
	blocksPerQC   = 5
	numBatches    = 4
)

var genesisTime = time.Unix(1_700_000_000, 0)

// batch is a SuperBlock at First with its CommitQC and ordered lane blocks.
// Next is First+1.
type batch struct {
	first  types.RoadIndex
	next   types.RoadIndex
	blocks []*types.Block
	qc     *types.FullCommitQC
}

// GBN converts a non-negative slice index to a RoadIndex offset.
func gbn(i int) types.RoadIndex {
	return types.RoadIndex(i) //nolint:gosec // i is a non-negative slice index
}

// writeAll persists every batch's QC atomically with its blocks.
func writeAll(t *testing.T, db *Store, batches []batch) {
	for _, b := range batches {
		require.NoError(t, db.WriteBlock(types.NewRoadBlock(b.qc, b.blocks)))
	}
}

// WriteAppData writes an AppProposal and its AppQC for every batch.
func writeAppData(t *testing.T, db *Store, rng utils.Rng, keys []types.SecretKey, batches []batch) {
	t.Helper()
	for _, b := range batches {
		appQC := appQCForBatch(rng, keys, b)
		require.NoError(t, db.WriteAppProposal(appQC.Proposal()))
		require.NoError(t, db.WriteAppQC(appQC))
	}
}

// BuildCommittee returns a deterministic committee (via GenCommittee with a
// fixed seed) and the secret keys that sign its QCs.
func buildCommittee() (*types.Committee, []types.SecretKey) {
	return types.GenCommittee(utils.TestRngFromSeed(testSeed), committeeSize)
}

// GenerateBatches builds a deterministic sequence of contiguous finalized
// batches for the given committee/keys.
func generateBatches(committee *types.Committee, keys []types.SecretKey) []batch {
	rng := utils.TestRngFromSeed(testSeed + 1)
	prev := utils.None[*types.CommitQC]()
	batches := make([]batch, 0, numBatches)
	for range numBatches {
		fqc, blocks := buildFullCommitQC(rng, committee, keys, prev)
		batches = append(batches, batch{first: fqc.Index(), next: fqc.Index() + 1, blocks: blocks, qc: fqc})
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
	ep := types.NewEpoch(0, types.OpenRoadRange(), committee)
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
	return types.NewAppProposal(b.qc.QC().View(), types.GenAppHash(rng))
}

func testAppQC(keys []types.SecretKey, proposal *types.AppProposal) *types.AppQC {
	vote := types.NewAppVote(proposal)
	votes := make([]*types.Signed[*types.AppVote], 0, len(keys))
	for _, k := range keys {
		votes = append(votes, types.Sign(k, vote))
	}
	return types.NewAppQC(votes)
}

// superBlocks returns the execution unit represented by this batch.
func (b batch) superBlocks() []*types.SuperBlock {
	return []*types.SuperBlock{types.NewRoadBlock(b.qc, b.blocks).SuperBlock()}
}

func testSuffixSnapshot(t *testing.T, build builder) {
	db, _ := openFresh(t, build)
	defer func() { require.NoError(t, db.Close()) }()
	committee, keys := buildCommittee()
	batches := generateBatches(committee, keys)[:2]
	writeAll(t, db, batches[:1])
	before, err := db.ReadSuffix()
	require.NoError(t, err)
	require.Len(t, before.Blocks, 1)
	firstHash := before.Blocks[0].Header().Hash()
	writeAll(t, db, batches[1:])
	writeAppData(t, db, utils.TestRngFromSeed(testSeed+505), keys, batches)
	require.NoError(t, db.PruneBefore(batches[1].first))
	require.Len(t, before.Blocks, 1)
	require.Equal(t, firstHash, before.Blocks[0].Header().Hash())
	require.Empty(t, before.AppProposals)
	require.Empty(t, before.AppQCs)
	after, err := db.ReadSuffix()
	require.NoError(t, err)
	require.Len(t, after.Blocks, 1)
	require.Equal(t, batches[1].first, after.Blocks[0].QC().Index())
}

func TestFailedFirstWriteKeepsStoreEmpty(t *testing.T) {
	rng := utils.TestRngFromSeed(42)
	failure := errors.New("write failed")
	cfg, err := DefaultConfig(t.TempDir())
	require.NoError(t, err)
	store, err := New(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	store.table = failingWriteTable{Table: store.table, err: failure}
	err = store.WriteBlock(types.NewRoadBlock(types.GenFullCommitQCAt(rng, 42, 0), nil))
	require.ErrorIs(t, err, failure)
	require.False(t, store.Status().IsPresent())
	require.Zero(t, store.First())
	suffix, err := store.ReadSuffix()
	require.NoError(t, err)
	require.False(t, suffix.Status.IsPresent())
}

type failingWriteTable struct {
	littdb.Table
	err error
}

func (t failingWriteTable) Put([]byte, []byte, ...*litttypes.SecondaryKey) error { return t.err }

func persistenceConfig(t *testing.T, dir string) *Config {
	t.Helper()
	cfg, err := DefaultConfig(dir)
	require.NoError(t, err)
	cfg.RetentionTime = time.Nanosecond
	return cfg
}

func openPersistedStore(t *testing.T, cfg *Config) *Store {
	t.Helper()
	store, err := New(cfg)
	require.NoError(t, err)
	return store
}

// TestTableDirectoryName pins the on-disk directory the store keeps its data in.
func TestTableDirectoryName(t *testing.T) {
	dir := t.TempDir()
	store := openPersistedStore(t, persistenceConfig(t, dir))
	require.NoError(t, store.Close())
	info, err := os.Stat(filepath.Join(dir, "blocks"))
	require.NoError(t, err)
	require.True(t, info.IsDir())
}

func storageBlocks(rng utils.Rng, count, lanes int) []*types.RoadBlock {
	blocks := make([]*types.RoadBlock, count)
	for n := range blocks {
		laneBlocks := make([]*types.Block, lanes)
		headers := make([]*types.BlockHeader, lanes)
		for i := range laneBlocks {
			laneBlocks[i] = types.GenBlock(rng)
			headers[i] = laneBlocks[i].Header()
		}
		qc := types.GenFullCommitQCAt(rng, types.RoadIndex(n), lanes)
		qc = types.NewFullCommitQC(qc.QC(), headers)
		blocks[n] = types.NewRoadBlock(qc, laneBlocks)
	}
	return blocks
}

func writeStorageBlocks(t *testing.T, store *Store, blocks []*types.RoadBlock) {
	t.Helper()
	for _, block := range blocks {
		require.NoError(t, store.WriteBlock(block))
	}
}

func writeStorageAppData(t *testing.T, store *Store, rng utils.Rng, blocks []*types.RoadBlock) {
	t.Helper()
	key := types.GenSecretKey(rng)
	for _, block := range blocks {
		proposal := types.NewAppProposal(block.QC().QC().View(), types.GenAppHash(rng))
		require.NoError(t, store.WriteAppProposal(proposal))
		qc := types.NewAppQC([]*types.Signed[*types.AppVote]{types.Sign(key, types.NewAppVote(proposal))})
		require.NoError(t, store.WriteAppQC(qc))
	}
}

func requireStorageBlock(t *testing.T, want, got *types.RoadBlock) {
	t.Helper()
	require.Equal(t, want.Header().Hash(), got.Header().Hash())
	require.Equal(t, types.FullCommitQCConv.Marshal(want.QC()), types.FullCommitQCConv.Marshal(got.QC()))
	require.Len(t, got.LaneBlocks(), len(want.LaneBlocks()))
	for i, lane := range want.LaneBlocks() {
		require.Equal(t, types.BlockConv.Marshal(lane), types.BlockConv.Marshal(got.LaneBlocks()[i]))
	}
}

// TestRefusesToOpenWithoutBlock verifies that app records cannot supply a missing recovery anchor.
func TestRefusesToOpenWithoutBlock(t *testing.T) {
	rng := utils.TestRng()
	cfg := strandingConfig(t, t.TempDir(), 8)
	store := openPersistedStore(t, cfg)
	block := storageBlocks(rng, 1, 1)[0]
	proposal := types.NewAppProposal(block.QC().QC().View(), types.GenAppHash(rng))
	// Deliberately create an invalid persisted store: an application record with no block.
	require.NoError(t, store.table.Put((appProposalKey{Index: proposal.Index()}).encode().bytes(), types.AppProposalConv.Marshal(proposal)))
	require.NoError(t, store.Flush())
	require.NoError(t, store.Close())
	_, err := New(cfg)
	require.Error(t, err)
}

const (
	// crashCohorts and crashPerCohort size the fixture both crash tests write.
	crashCohorts    = 4
	crashPerCohort  = 5
	crashTotalBlock = crashCohorts * crashPerCohort
)

// TestNoBlockWithoutQCAfterTornTail verifies that a torn value discards the
// entire QC group and preserves the earlier groups.
func TestNoBlockWithoutQCAfterTornTail(t *testing.T) {
	rng := utils.TestRng()
	blocks := storageBlocks(rng, crashCohorts, crashPerCohort)
	dir := t.TempDir()

	db := openPersistedStore(t, persistenceConfig(t, dir))
	writeStorageBlocks(t, db, blocks)
	require.NoError(t, db.Flush())
	require.NoError(t, db.Close())

	// Corrupt the segment holding the most value bytes (where the most recent
	// writes landed): drop the tail of its value file, then flip its metadata back
	// to unsealed so LoadSegment re-runs recovery on reopen.
	//
	// Dropping tornTailBytes models a partial write of the last value. That stays
	// a single torn tail, rather than spilling across value boundaries, only while
	// every stored value is larger than it.
	const tornTailBytes = 16
	require.Greater(t, len(types.BlockConv.Marshal(blocks[len(blocks)-1].LaneBlocks()[crashPerCohort-1])), tornTailBytes,
		"fixture values must exceed the torn tail, or the truncation model breaks")

	valPath, metaPath := largestValueSegmentFiles(t, dir)
	truncateFileBy(t, valPath, tornTailBytes)
	markSegmentUnsealedOnDisk(t, metaPath)

	// Reopen: recovery discards the torn tail, keeping a contiguous prefix.
	db2 := openPersistedStore(t, persistenceConfig(t, dir))
	defer func() { _ = db2.Close() }()

	survived := requireEveryBlockHasItsQC(t, db2, blocks)

	// The truncation must have actually dropped a block, otherwise the recovery
	// path was never exercised and the invariant proves nothing.
	require.Less(t, survived, crashTotalBlock, "expected the torn tail to drop at least one block")
}

// TestFlushSurvivesHardKill is the counterpart to TestNoBlockWithoutQCAfterTornTail:
// where the torn-tail test proves an unflushed partial write degrades to a
// contiguous prefix, with some loss expected, this proves a clean Flush loses
// NOTHING across a real, uncatchable process kill.
//
// It re-execs this test binary as a child (gated by crashChildEnv) that writes
// every cohort, Flushes, then SIGKILLs itself. SIGKILL cannot be caught, so no
// deferred Close and no graceful shutdown run — it is the strongest possible
// "kill process". Because the kill happens only after Flush returns, the parent
// can reopen the DB the dead child left behind and require every flushed record
// to be present. We must not SIGKILL the process running the tests, so the crash
// is isolated to the child subprocess.
func TestFlushSurvivesHardKill(t *testing.T) {
	if os.Getenv(crashChildEnv) == "1" {
		dir := os.Getenv(crashDirEnv)
		blocks := make([]*types.RoadBlock, crashCohorts)
		for i := range blocks {
			value, err := os.ReadFile(filepath.Join(dir, "input-"+strconv.Itoa(i)+".block"))
			require.NoError(t, err)
			blocks[i], err = decodeBlock(value)
			require.NoError(t, err)
		}
		runCrashChild(t, blocks)
		return
	}
	rng := utils.TestRng()
	blocks := storageBlocks(rng, crashCohorts, crashPerCohort)

	if runtime.GOOS == "windows" {
		t.Skip("hard-kill crash test relies on Unix SIGKILL / WaitStatus")
	}

	// t.TempDir is removed when this test ends, cleaning up the child's data too.
	dir := t.TempDir()

	// Pass the exact typed inputs across the process boundary.
	for i, block := range blocks {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "input-"+strconv.Itoa(i)+".block"), encodeBlock(block).Value, 0600))
	}

	// Re-exec only this test as the crash child, pointed at dir. We pass just
	// -test.run and -test.v (not the parent's flags) so the child writes no
	// coverprofile it can never finish.
	cmd := exec.Command(os.Args[0], "-test.run", "^"+t.Name()+"$", "-test.v") //nolint:gosec // os.Args[0] is the test binary
	cmd.Env = append(os.Environ(), crashChildEnv+"=1", crashDirEnv+"="+dir)
	out, err := cmd.CombinedOutput()

	// The child MUST have died from SIGKILL; otherwise no real crash happened (it
	// exited cleanly or failed a require) and the test proves nothing.
	var exitErr *exec.ExitError
	require.True(t, errors.As(err, &exitErr), "child should have been signal-killed; output:\n%s", out)
	ws, ok := exitErr.Sys().(syscall.WaitStatus)
	require.True(t, ok, "unexpected wait status type %T; output:\n%s", exitErr.Sys(), out)
	require.True(t, ws.Signaled(), "child should exit via signal; output:\n%s", out)
	require.Equal(t, syscall.SIGKILL, ws.Signal(), "child should be SIGKILLed; output:\n%s", out)

	// Reopen the DB the crashed child left behind and assert nothing flushed was lost.
	db := openPersistedStore(t, persistenceConfig(t, dir))
	defer func() { _ = db.Close() }()

	survived := requireEveryBlockHasItsQC(t, db, blocks)

	// Unlike the torn-tail test, which expects loss, a clean Flush before the kill
	// must lose nothing.
	require.Equal(t, crashTotalBlock, survived, "flushed blocks must all survive a hard kill")
}

// runCrashChild is the subprocess body of TestFlushSurvivesHardKill: it opens the
// DB at crashDirEnv, writes every cohort, Flushes, then hard-kills its own
// process. It never returns.
func runCrashChild(t *testing.T, blocks []*types.RoadBlock) {
	dir := os.Getenv(crashDirEnv)
	require.NotEmpty(t, dir)

	db := openPersistedStore(t, persistenceConfig(t, dir))
	writeStorageBlocks(t, db, blocks)
	require.NoError(t, db.Flush())

	// Hard crash: uncatchable, runs no defers and no graceful Close.
	require.NoError(t, syscall.Kill(syscall.Getpid(), syscall.SIGKILL))
	select {} // unreachable — block so nothing else runs before the kernel reaps us
}

// requireEveryBlockHasItsQC fails unless every surviving block record is still
// covered by a surviving QC, and returns how many blocks survived.
//
// With no app data, the recovery suffix contains every surviving group.
func requireEveryBlockHasItsQC(t *testing.T, store *Store, blocks []*types.RoadBlock) int {
	t.Helper()
	suffix, err := store.ReadSuffix()
	require.NoError(t, err)
	count := 0
	for i, block := range suffix.Blocks {
		require.Equal(t, types.RoadIndex(i), block.QC().Index(), "recovery must preserve a contiguous prefix")
		want := blocks[i]
		requireStorageBlock(t, want, block)
		byHash, err := store.ReadBlockByHash(want.Header().Hash())
		require.NoError(t, err)
		require.True(t, byHash.IsPresent())
		requireStorageBlock(t, want, byHash.OrPanic("surviving hash"))
		qc, err := store.ReadQC(want.QC().Index())
		require.NoError(t, err)
		require.True(t, qc.IsPresent())
		require.Equal(t, types.FullCommitQCConv.Marshal(want.QC()), types.FullCommitQCConv.Marshal(qc.OrPanic("surviving QC")))
		for offset, lane := range want.LaneBlocks() {
			got, err := store.ReadLaneBlock(types.LaneBlockPos{Index: want.QC().Index(), Offset: uint64(offset)})
			require.NoError(t, err)
			require.True(t, got.IsPresent())
			require.Equal(t, types.BlockConv.Marshal(lane), types.BlockConv.Marshal(got.OrPanic("surviving lane block")))
			count++
		}
	}
	return count
}

// largestValueSegmentFiles walks the litt data directory under dir and returns
// the value-file and sibling metadata-file paths of the segment with the most
// value bytes (the one most recently written into; robust to empty rollover
// segments that may exist after a clean Close).
func largestValueSegmentFiles(t *testing.T, dir string) (valPath string, metaPath string) {
	t.Helper()
	var bestSize int64 = -1
	var bestIndex string
	require.NoError(t, filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), segment.ValuesFileExtension) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Size() > bestSize {
			// File name is "<index>-<shard>.values"; the index is everything
			// before the dash.
			base := strings.TrimSuffix(d.Name(), segment.ValuesFileExtension)
			index := base
			if i := strings.IndexByte(base, '-'); i >= 0 {
				index = base[:i]
			}
			bestSize = info.Size()
			bestIndex = index
			valPath = p
		}
		return nil
	}))
	require.NotEmpty(t, valPath, "no value file found under %s", dir)
	_, err := strconv.ParseUint(bestIndex, 10, 32)
	require.NoError(t, err, "unexpected segment index %q", bestIndex)
	metaPath = filepath.Join(filepath.Dir(valPath), bestIndex+segment.MetadataFileExtension)
	return valPath, metaPath
}

// truncateFileBy drops the last n bytes of the file at p.
func truncateFileBy(t *testing.T, p string, n int) {
	t.Helper()
	data, err := os.ReadFile(p)
	require.NoError(t, err)
	require.Greater(t, len(data), n, "file %s too small to truncate by %d", p, n)
	require.NoError(t, os.WriteFile(p, data[:len(data)-n], 0600))
}

// markSegmentUnsealedOnDisk flips the sealed byte in a segment's metadata file
// from 1 back to 0, simulating a segment that crashed before sealing so that
// LoadSegment runs the recovery path on reopen.
func markSegmentUnsealedOnDisk(t *testing.T, metaPath string) {
	t.Helper()
	data, err := os.ReadFile(metaPath)
	require.NoError(t, err)
	require.Equal(t, segment.V4MetadataSize, len(data), "unexpected metadata size for %s", metaPath)
	data[segment.MetadataSealedByteOffset] = 0
	require.NoError(t, os.WriteFile(metaPath, data, 0600))
}

const (
	// crashChildEnv gates the child branch of TestFlushSurvivesHardKill. When set,
	// the test re-runs as the crash subprocess instead of the parent orchestrator.
	crashChildEnv = "LITTBLOCK_CRASH_CHILD"
	// crashDirEnv carries the data directory the parent created down to the child,
	// so both processes operate on the same on-disk DB.
	crashDirEnv = "LITTBLOCK_CRASH_DIR"
)

// TestPackedQCRecoversAtomically verifies that a torn packed value loses every
// lookup key in that group and preserves earlier groups.
func TestPackedQCRecoversAtomically(t *testing.T) {
	for _, cohorts := range []uint64{1, 2} {
		t.Run(strconv.FormatUint(cohorts, 10), func(t *testing.T) {
			rng := utils.TestRng()
			blocks := storageBlocks(rng, int(cohorts), crashPerCohort)
			dir := t.TempDir()
			cfg := persistenceConfig(t, dir)
			// A volatile keymap models a crash before its derived index is durable.
			cfg.Litt.KeymapType = keymap.MemKeymapType
			db := openPersistedStore(t, cfg)
			writeStorageBlocks(t, db, blocks)
			require.NoError(t, db.Flush())
			require.NoError(t, db.Close())
			valPath, metaPath := largestValueSegmentFiles(t, dir)
			truncateFileBy(t, valPath, 16)
			markSegmentUnsealedOnDisk(t, metaPath)

			for range 2 {
				db = openPersistedStore(t, cfg)
				for _, wantBlock := range blocks {
					n := wantBlock.QC().Index()
					want := n < types.RoadIndex(cohorts-1)
					got, err := db.ReadBlock(n)
					require.NoError(t, err)
					require.Equal(t, want, got.IsPresent())
					if want {
						requireStorageBlock(t, wantBlock, got.OrPanic("surviving block"))
					}
					qc, err := db.ReadQC(n)
					require.NoError(t, err)
					require.Equal(t, want, qc.IsPresent())
					byHash, err := db.ReadBlockByHash(wantBlock.Header().Hash())
					require.NoError(t, err)
					require.Equal(t, want, byHash.IsPresent())
					for offset := range wantBlock.LaneBlocks() {
						lane, err := db.ReadLaneBlock(types.LaneBlockPos{Index: n, Offset: uint64(offset)})
						require.NoError(t, err)
						require.Equal(t, want, lane.IsPresent())
					}
				}
				require.Equal(t, int((cohorts-1)*crashPerCohort), requireEveryBlockHasItsQC(t, db, blocks))
				require.NoError(t, db.Close())
			}
		})
	}
}
