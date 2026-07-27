package littblock

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// TestLittblockIteratorGapIsCorruption pins the iterator's gap detection: blocks are written
// densely (WriteBlock enforces contiguity), so an interior missing block on disk can only be
// corruption and must surface as an error rather than a silent None. The gap is injected by
// writing a block record directly to the raw table, bypassing WriteBlock's contiguity cursor.
func TestLittblockIteratorGapIsCorruption(t *testing.T) {
	dir := t.TempDir()
	rng := utils.TestRngFromSeed(7)

	db, err := NewBlockDB(strandingConfig(t, dir, 1024))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	impl := db.(*blockDB)

	// One QC covering [0,5) with blocks 0 and 1 written normally.
	require.NoError(t, db.WriteQC(types.GenFullCommitQCRange(rng, 0, 5)))
	require.NoError(t, db.WriteBlock(0, types.GenBlock(rng)))
	require.NoError(t, db.WriteBlock(1, types.GenBlock(rng)))

	// Corrupt the store: a block at 3 with no block at 2, injected past WriteBlock.
	blk := types.GenBlock(rng)
	require.NoError(t, impl.table.Put(blockKey(3), encodeBlock(3, blk)))

	it, err := db.Iterator(0)
	require.NoError(t, err)
	defer func() { _ = it.Close() }()

	// Positions 0 and 1 are intact; advancing to 2 must surface the gap as corruption.
	for _, want := range []types.GlobalBlockNumber{0, 1} {
		pos, ok, err := it.Next()
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, want, pos.Number)
	}
	_, _, err = it.Next()
	require.ErrorIs(t, err, types.ErrBlockGap)
}

// TestLittblockIteratorUncoveredBlockIsCorruption pins the iterator's coverage check: a QC
// covering every block is always written first, so a block record beyond every QC's range can
// only be corruption and must surface as an error. The uncovered block is injected by writing a
// block record directly to the raw table, bypassing WriteBlock's coverage check.
func TestLittblockIteratorUncoveredBlockIsCorruption(t *testing.T) {
	dir := t.TempDir()
	rng := utils.TestRngFromSeed(8)

	db, err := NewBlockDB(strandingConfig(t, dir, 1024))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	impl := db.(*blockDB)

	// One QC covering [0,2), fully blocked.
	require.NoError(t, db.WriteQC(types.GenFullCommitQCRange(rng, 0, 2)))
	require.NoError(t, db.WriteBlock(0, types.GenBlock(rng)))
	require.NoError(t, db.WriteBlock(1, types.GenBlock(rng)))

	// Corrupt the store: a block at 2, past every QC's range, injected past WriteBlock.
	blk := types.GenBlock(rng)
	require.NoError(t, impl.table.Put(blockKey(2), encodeBlock(2, blk)))

	it, err := db.Iterator(0)
	require.NoError(t, err)
	defer func() { _ = it.Close() }()

	for _, want := range []types.GlobalBlockNumber{0, 1} {
		pos, ok, err := it.Next()
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, want, pos.Number)
	}
	_, _, err = it.Next()
	require.Error(t, err, "a block with no QC coverage must surface as corruption")
	require.Contains(t, err.Error(), "no QC coverage")
}

// TestLittblockIteratorMidChainStartNeedsNoScan pins that a store whose coverage begins above 0 is
// positioned directly, without the full-scan fallback that used to serve this case. It must behave
// identically before and after a restart: in the writing session the clamp comes from
// oldestQCStart, after a reopen recoverReadWatermark derives the same floor into the watermark.
func TestLittblockIteratorMidChainStartNeedsNoScan(t *testing.T) {
	dir := t.TempDir()
	rng := utils.TestRngFromSeed(23)
	cfg := strandingConfig(t, dir, 1<<20)

	db, err := NewBlockDB(cfg)
	require.NoError(t, err)

	// Coverage begins at 100 and nothing below it was ever written.
	require.NoError(t, db.WriteQC(types.GenFullCommitQCRange(rng, 100, 103)))
	for n := types.GlobalBlockNumber(100); n < 103; n++ {
		require.NoError(t, db.WriteBlock(n, types.GenBlock(rng)))
	}
	require.NoError(t, db.Flush())

	drain := func(t *testing.T, d types.BlockDB, n types.GlobalBlockNumber) []types.GlobalBlockNumber {
		t.Helper()
		it, err := d.Iterator(n)
		require.NoError(t, err)
		defer func() { _ = it.Close() }()
		var got []types.GlobalBlockNumber
		for {
			pos, ok, err := it.Next()
			require.NoError(t, err)
			if !ok {
				return got
			}
			got = append(got, pos.Number)
		}
	}

	want := []types.GlobalBlockNumber{100, 101, 102}
	for _, start := range []types.GlobalBlockNumber{0, 50, 100} {
		require.Equal(t, want, drain(t, db, start), "same session, Iterator(%d)", start)
	}
	require.Equal(t, []types.GlobalBlockNumber{101, 102}, drain(t, db, 101), "same session, mid-cohort start")

	require.NoError(t, db.Close())
	db2, err := NewBlockDB(cfg)
	require.NoError(t, err)
	defer func() { _ = db2.Close() }()

	for _, start := range []types.GlobalBlockNumber{0, 50, 100} {
		require.Equal(t, want, drain(t, db2, start), "after restart, Iterator(%d)", start)
	}
}

// TestLittblockIteratorMissingStartQCIsCorruption pins that a positioned lookup which misses inside
// known coverage is an error, not a silent full-scan fallback. The clamp guarantees some QC record
// is stored under qcKey(start), so a miss means a record that must exist does not.
func TestLittblockIteratorMissingStartQCIsCorruption(t *testing.T) {
	rng := utils.TestRngFromSeed(24)
	db, err := NewBlockDB(strandingConfig(t, t.TempDir(), 1<<20))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	impl := db.(*blockDB)

	require.NoError(t, db.WriteQC(types.GenFullCommitQCRange(rng, 0, 3)))
	for n := types.GlobalBlockNumber(0); n < 3; n++ {
		require.NoError(t, db.WriteBlock(n, types.GenBlock(rng)))
	}
	require.NoError(t, db.Flush())

	// Claim coverage runs to 10 while only [0,3) is stored, so qcKey(5) has no record. This is the
	// shape a truncated key file or a segment missing from the snapshot presents as.
	impl.mu.Lock()
	impl.lastQCNext = 10
	impl.mu.Unlock()

	_, err = db.Iterator(5)
	require.Error(t, err, "a missing start QC inside claimed coverage must not fall back to a full scan")
	require.Contains(t, err.Error(), "corrupt store")
}

// TestLittblockIteratorDoesNotServeBlockBelowCoverage pins the start-clamp boundary for a block
// that no retained QC covers: it is not served, and not reported as corruption either.
//
// This test previously asserted the opposite. It was written when Iterator fell back to a plain
// full scan whenever the positioned lookup missed, which meant Iterator(0) walked over the
// uncovered block and flagged it. Iterator now clamps its start up to oldestQCStart and refuses to
// scan below it, so the block is never visited. That is the intended semantics on both counts:
//
//   - Iterator(n) is contractually clamped up to the lowest number a retained QC covers, and this
//     block is below every retained QC's range, so it is not a position the iterator may yield.
//   - Detecting corruption strictly below the requested start is exactly what a positioned
//     iterator exists to avoid (see the audit's finding 6), and the detection here was incidental
//     to the fallback rather than designed.
//
// The detection is also not recoverable in principle: this same on-disk state — a block below the
// oldest retained QC — is the *legitimate* stranded state after a prune whose GC pass reclaimed
// the covering QC, and littblock does not persist the watermark, so after a restart the two are
// indistinguishable. The check only ever fired in the window before the watermark advanced.
//
// What still holds: an uncovered block at or above the start is corruption and errors — see
// TestLittblockIteratorUncoveredBlockIsCorruption. And finding 2's actual hazard does not return,
// because this iterator is not empty: loadFromBlockDB replays 5..7 rather than seeing an empty
// scan and silently restarting from committee genesis.
func TestLittblockIteratorDoesNotServeBlockBelowCoverage(t *testing.T) {
	dir := t.TempDir()
	rng := utils.TestRngFromSeed(9)

	db, err := NewBlockDB(strandingConfig(t, dir, 1024))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	impl := db.(*blockDB)

	// A block at 0 injected past WriteBlock's coverage check while no QC exists at all, so the
	// record precedes every QC in insertion order.
	require.NoError(t, impl.table.Put(blockKey(0), encodeBlock(0, types.GenBlock(rng))))

	// A well-formed cohort above it, written normally. This sets oldestQCStart to 5.
	require.NoError(t, db.WriteQC(types.GenFullCommitQCRange(rng, 5, 8)))
	for n := types.GlobalBlockNumber(5); n < 8; n++ {
		require.NoError(t, db.WriteBlock(n, types.GenBlock(rng)))
	}

	it, err := db.Iterator(0)
	require.NoError(t, err)
	defer func() { _ = it.Close() }()

	var got []types.GlobalBlockNumber
	for {
		pos, ok, err := it.Next()
		require.NoError(t, err, "the uncovered block below the clamped start must not be visited")
		if !ok {
			break
		}
		got = append(got, pos.Number)
	}
	require.Equal(t, []types.GlobalBlockNumber{5, 6, 7}, got,
		"Iterator(0) must clamp up to the first retained QC and yield only covered numbers")
}

// TestLittblockIteratorEmptyStoreIsCleanExhaustion pins the negative of the coverage check: with no
// block record held, no retained QC means there is genuinely nothing to serve, which must stay a
// clean (false, nil) rather than becoming a corruption error.
func TestLittblockIteratorEmptyStoreIsCleanExhaustion(t *testing.T) {
	db, err := NewBlockDB(strandingConfig(t, t.TempDir(), 1024))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	it, err := db.Iterator(0)
	require.NoError(t, err)
	defer func() { _ = it.Close() }()

	_, ok, err := it.Next()
	require.NoError(t, err)
	require.False(t, ok)
}
