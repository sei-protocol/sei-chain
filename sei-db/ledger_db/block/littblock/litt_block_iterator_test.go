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
	require.NoError(t, db.WriteQC(0, 5, types.GenFullCommitQCRange(rng, 0, 5)))
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
		ok, err := it.Next()
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, want, it.Number())
	}
	_, err = it.Next()
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
	require.NoError(t, db.WriteQC(0, 2, types.GenFullCommitQCRange(rng, 0, 2)))
	require.NoError(t, db.WriteBlock(0, types.GenBlock(rng)))
	require.NoError(t, db.WriteBlock(1, types.GenBlock(rng)))

	// Corrupt the store: a block at 2, past every QC's range, injected past WriteBlock.
	blk := types.GenBlock(rng)
	require.NoError(t, impl.table.Put(blockKey(2), encodeBlock(2, blk)))

	it, err := db.Iterator(0)
	require.NoError(t, err)
	defer func() { _ = it.Close() }()

	for _, want := range []types.GlobalBlockNumber{0, 1} {
		ok, err := it.Next()
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, want, it.Number())
	}
	_, err = it.Next()
	require.Error(t, err, "a block with no QC coverage must surface as corruption")
	require.Contains(t, err.Error(), "no QC coverage")
}
