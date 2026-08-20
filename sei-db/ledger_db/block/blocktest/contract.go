// Package blocktest holds the blocktypes.BlockDB contract suite and the record
// fixtures its backends test against.
//
// The suite is shared rather than per-backend: every backend owes the same
// observable behavior, and a copy per backend is a copy that drifts. It knows
// nothing of the consensus layer above — a BlockDB stores opaque bytes, so the
// fixtures store opaque bytes, and the ordering and encoding rules that give
// those bytes meaning are tested where they live.
package blocktest

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	blocktypes "github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/types"
)

// Open opens a handle to the BlockDB under test. Calling it more than once
// reopens a handle to the SAME backing store, simulating a process restart:
// in-memory backends hand back the same instance, durable ones reopen their
// files. The caller must Close the previous handle before reopening.
type Open func() (blocktypes.BlockDB, error)

// Builder returns an Open bound to a fresh, empty backing store, for one subtest.
type Builder func(t *testing.T) Open

// RunContract exercises the blocktypes.BlockDB contract against the backing
// store build produces.
//
// Where the watermark is allowed to go is absent, because a BlockDB does not
// decide that: it moves the floor where it is told. So is physical reclamation —
// the contract makes eligibility observable through PruneWatermark but leaves the
// timing and granularity of the reclaim to the backend, so what a backend
// actually drops, and when, is covered in that backend's own package.
func RunContract(t *testing.T, build Builder) {
	t.Run("EmptyStore", func(t *testing.T) { testEmptyStore(t, build) })
	t.Run("RecordRoundTrip", func(t *testing.T) { testRecordRoundTrip(t, build) })
	t.Run("BlockHashAlias", func(t *testing.T) { testBlockHashAlias(t, build) })
	t.Run("ScanFollowsWriteOrder", func(t *testing.T) { testScanFollowsWriteOrder(t, build) })
	t.Run("ScanIsASnapshot", func(t *testing.T) { testScanIsASnapshot(t, build) })
	t.Run("RestartPersistsRecords", func(t *testing.T) { testRestartPersistsRecords(t, build) })
	t.Run("WatermarkIsMonotonic", func(t *testing.T) { testWatermarkIsMonotonic(t, build) })
}

// openFresh opens a handle to a new, empty backing store and returns it along
// with the Open that can reopen the same store (for restart).
func openFresh(t *testing.T, build Builder) (blocktypes.BlockDB, Open) {
	t.Helper()
	o := build(t)
	db, err := o()
	require.NoError(t, err)
	return db, o
}

// restart flushes and closes db, then reopens a handle to the same backing
// store. The returned handle must be closed by the caller.
func restart(t *testing.T, o Open, db blocktypes.BlockDB) blocktypes.BlockDB {
	t.Helper()
	require.NoError(t, db.Flush())
	require.NoError(t, db.Close())
	reopened, err := o()
	require.NoError(t, err)
	return reopened
}

// allKinds is every RecordKind, for assertions that must hold across the whole
// partitioned number space.
var allKinds = []blocktypes.RecordKind{
	blocktypes.KindBlock,
	blocktypes.KindQC,
	blocktypes.KindAppProposal,
	blocktypes.KindAppQC,
}

func testEmptyStore(t *testing.T, build Builder) {
	db, _ := openFresh(t, build)
	defer func() { _ = db.Close() }()

	for _, kind := range allKinds {
		_, exists, err := db.GetRecord(kind, 0)
		require.NoError(t, err)
		require.False(t, exists, "%s must be absent from an empty store", kind)
	}
	_, exists, err := db.GetBlockByHash(BlockHash(0))
	require.NoError(t, err)
	require.False(t, exists, "no hash resolves in an empty store")

	require.Empty(t, drain(t, db, false), "an empty store scans to nothing")

	require.Equal(t, uint64(0), db.PruneWatermark(), "nothing is eligible for reclamation yet")
}

func testRecordRoundTrip(t *testing.T, build Builder) {
	db, _ := openFresh(t, build)
	defer func() { _ = db.Close() }()

	PutQC(t, db, 3, 8)

	want := RecordValue(blocktypes.KindQC, 3)
	for n := uint64(3); n < 8; n++ {
		value, exists, err := db.GetRecord(blocktypes.KindQC, n)
		require.NoError(t, err)
		require.True(t, exists, "the record must be addressable at covered number %d", n)
		require.Equal(t, want, value, "every covered number resolves to the one stored value")
	}
	for _, n := range []uint64{2, 8} {
		_, exists, err := db.GetRecord(blocktypes.KindQC, n)
		require.NoError(t, err)
		require.False(t, exists, "%d is outside the stored range", n)
	}

	// Kinds partition the number space, so nothing else was filed at 3.
	for _, kind := range allKinds {
		if kind == blocktypes.KindQC {
			continue
		}
		_, exists, err := db.GetRecord(kind, 3)
		require.NoError(t, err)
		require.False(t, exists, "a %s and a qc at the same number are distinct records", kind)
	}
}

func testBlockHashAlias(t *testing.T, build Builder) {
	db, _ := openFresh(t, build)
	defer func() { _ = db.Close() }()

	PutBlocks(t, db, 0, 3)

	for n := uint64(0); n < 3; n++ {
		want := RecordValue(blocktypes.KindBlock, n)

		byNumber, exists, err := db.GetRecord(blocktypes.KindBlock, n)
		require.NoError(t, err)
		require.True(t, exists, "block %d must resolve by number", n)
		require.Equal(t, want, byNumber)

		byHash, exists, err := db.GetBlockByHash(BlockHash(n))
		require.NoError(t, err)
		require.True(t, exists, "block %d must resolve by hash", n)
		require.Equal(t, want, byHash, "both addresses reach the same value")
	}

	_, exists, err := db.GetBlockByHash([]byte("no-block-was-stored-under-this"))
	require.NoError(t, err)
	require.False(t, exists)
}

func testScanFollowsWriteOrder(t *testing.T, build Builder) {
	db, _ := openFresh(t, build)
	defer func() { _ = db.Close() }()

	PutQC(t, db, 0, 3)
	PutBlocks(t, db, 0, 3)
	PutAppData(t, db, 0, 3)

	// One entry per record, at the number it was filed under — never at a covered
	// alias, and never at a block's hash.
	want := []position{
		{blocktypes.KindQC, 0},
		{blocktypes.KindBlock, 0},
		{blocktypes.KindBlock, 1},
		{blocktypes.KindBlock, 2},
		{blocktypes.KindAppProposal, 0},
		{blocktypes.KindAppQC, 0},
	}
	require.Equal(t, want, positions(drain(t, db, false)), "oldest-first must reproduce the write order")

	newestFirst := positions(drain(t, db, true))
	require.Equal(t, reversed(want), newestFirst, "newest-first must be the exact reverse")
}

func testScanIsASnapshot(t *testing.T, build Builder) {
	db, _ := openFresh(t, build)
	defer func() { _ = db.Close() }()

	PutQC(t, db, 0, 2)
	PutBlocks(t, db, 0, 2)

	it, err := db.Scan(false)
	require.NoError(t, err)
	defer func() { require.NoError(t, it.Close()) }()

	// Written after the scan opened, so the walk below must not see it.
	PutQC(t, db, 2, 4)
	PutBlocks(t, db, 2, 4)

	seen := 0
	for {
		ok, err := it.Next()
		require.NoError(t, err)
		if !ok {
			break
		}
		require.Less(t, it.Number(), uint64(2), "the scan must not surface a record written after it opened")
		seen++
	}
	require.Equal(t, 3, seen, "the snapshot holds exactly the QC and two blocks present at Scan")
}

func testRestartPersistsRecords(t *testing.T, build Builder) {
	db, o := openFresh(t, build)
	WriteCohorts(t, db, 2, 3)
	before := drain(t, db, false)

	db = restart(t, o, db)
	defer func() { _ = db.Close() }()

	require.Equal(t, before, drain(t, db, false), "a restart must not disturb the record log")
	for n := uint64(0); n < 6; n++ {
		_, exists, err := db.GetRecord(blocktypes.KindBlock, n)
		require.NoError(t, err)
		require.True(t, exists, "block %d must survive the restart", n)

		_, exists, err = db.GetBlockByHash(BlockHash(n))
		require.NoError(t, err)
		require.True(t, exists, "block %d's hash alias must survive the restart", n)
	}
}

func testWatermarkIsMonotonic(t *testing.T, build Builder) {
	db, _ := openFresh(t, build)
	defer func() { _ = db.Close() }()

	WriteCohorts(t, db, 4, 5)

	db.SetPruneWatermark(10)
	require.Equal(t, uint64(10), db.PruneWatermark(), "the floor moves where it is told")

	db.SetPruneWatermark(3)
	require.Equal(t, uint64(10), db.PruneWatermark(), "the floor must never move backwards")

	db.SetPruneWatermark(10)
	require.Equal(t, uint64(10), db.PruneWatermark(), "repeating a request changes nothing")

	// A floor above everything held is legal: the layer above is the one that caps
	// the request, and refusing it here would hide that it stopped doing so.
	db.SetPruneWatermark(1_000)
	require.Equal(t, uint64(1_000), db.PruneWatermark())
}

// --- scanning ---

// position is a record's kind and the number it was filed under.
type position struct {
	Kind   blocktypes.RecordKind
	Number uint64
}

// entry is one record observed while draining a scan.
type entry struct {
	position
	Value []byte
}

// drain walks a whole scan and returns what it visited.
func drain(t *testing.T, db blocktypes.BlockDB, newestFirst bool) []entry {
	t.Helper()
	it, err := db.Scan(newestFirst)
	require.NoError(t, err)
	defer func() { require.NoError(t, it.Close()) }()

	var out []entry
	for {
		ok, err := it.Next()
		require.NoError(t, err)
		if !ok {
			return out
		}
		value, err := it.Value()
		require.NoError(t, err)
		out = append(out, entry{
			position: position{Kind: it.Kind(), Number: it.Number()},
			Value:    bytes.Clone(value),
		})
	}
}

// positions drops the values, for assertions about what was visited and in what
// order.
func positions(entries []entry) []position {
	out := make([]position, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.position)
	}
	return out
}

// reversed returns ps back to front.
func reversed(ps []position) []position {
	out := make([]position, 0, len(ps))
	for i := len(ps) - 1; i >= 0; i-- {
		out = append(out, ps[i])
	}
	return out
}

// --- record fixtures ---

// valueSize is the width every fixture value is padded to, so a test that
// truncates bytes off a file can reason about how many records it damaged.
const valueSize = 64

// BlockHash returns the hash the fixtures file the block at n under. A hash must
// be unique per block, so it is derived from the number.
func BlockHash(n uint64) []byte {
	return []byte(fmt.Sprintf("block-hash-%020d", n))
}

// RecordValue returns the bytes the fixtures store for kind at first. Values are
// distinct per record, so a read can prove it found the one it asked for.
func RecordValue(kind blocktypes.RecordKind, first uint64) []byte {
	value := []byte(fmt.Sprintf("%s@%d", kind, first))
	if len(value) >= valueSize {
		return value
	}
	return append(value, bytes.Repeat([]byte("."), valueSize-len(value))...)
}

// PutQC stores a QC record covering [first, next).
func PutQC(t *testing.T, db blocktypes.BlockDB, first, next uint64) {
	t.Helper()
	require.NoError(t, db.PutRecord(blocktypes.KindQC, first, next, RecordValue(blocktypes.KindQC, first)))
}

// PutBlocks stores one block record at every number in [first, next).
func PutBlocks(t *testing.T, db blocktypes.BlockDB, first, next uint64) {
	t.Helper()
	for n := first; n < next; n++ {
		require.NoError(t, db.PutBlock(n, BlockHash(n), RecordValue(blocktypes.KindBlock, n)))
	}
}

// PutAppData stores an AppProposal and an AppQC, each covering [first, next).
// The pair is what moves the ceiling a prune is capped at.
func PutAppData(t *testing.T, db blocktypes.BlockDB, first, next uint64) {
	t.Helper()
	require.NoError(t, db.PutRecord(
		blocktypes.KindAppProposal, first, next, RecordValue(blocktypes.KindAppProposal, first)))
	require.NoError(t, db.PutRecord(
		blocktypes.KindAppQC, first, next, RecordValue(blocktypes.KindAppQC, first)))
}

// WriteCohorts stores cohorts consecutive cohorts of perCohort blocks starting at
// 0. Every cohort is stored QC-first, which is the order the layer above writes
// in and the order the crash guarantee is stated against: a surviving block
// always has a surviving QC, never the reverse.
//
// It writes no app data, so a store built this way has committed nothing and
// cannot be pruned. A test that needs pruning to move calls WriteAppData too.
func WriteCohorts(t *testing.T, db blocktypes.BlockDB, cohorts, perCohort uint64) {
	t.Helper()
	for i := range cohorts {
		first := i * perCohort
		PutQC(t, db, first, first+perCohort)
		PutBlocks(t, db, first, first+perCohort)
	}
}

// WriteAppData stores app data covering each of the cohorts WriteCohorts wrote,
// which is what lifts the ceiling a prune is capped at. It trails the whole run,
// as it does in production, where the application commits behind consensus.
func WriteAppData(t *testing.T, db blocktypes.BlockDB, cohorts, perCohort uint64) {
	t.Helper()
	for i := range cohorts {
		first := i * perCohort
		PutAppData(t, db, first, first+perCohort)
	}
}
