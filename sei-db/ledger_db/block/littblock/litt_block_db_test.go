package littblock

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// genBlockWithTxs returns a random block guaranteed to carry at least one transaction, so the
// tx-extraction assertions below are meaningful.
func genBlockWithTxs(rng utils.Rng) *types.Block {
	for {
		blk := types.GenBlock(rng)
		if len(blk.Payload().Txs()) > 0 {
			return blk
		}
	}
}

// TestGetTxByOffset covers the block-store sub-range read primitive: it must return exactly the requested
// byte range of a block's stored value (which is what encodeBlock produced), both before and after a flush,
// and it must extract a real transaction's bytes when given that transaction's byte range within the value.
func TestGetTxByOffset(t *testing.T) {
	dir := t.TempDir()
	rng := utils.TestRngFromSeed(1)

	cfg, err := DefaultConfig(dir)
	require.NoError(t, err)
	db, err := NewBlockDB(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	impl := db.(*blockDB)

	// Write a QC covering block 0, then the block itself.
	blk := genBlockWithTxs(rng)
	require.NoError(t, db.WriteQC(0, 1, types.GenFullCommitQCRange(rng, 0, 1)))
	require.NoError(t, db.WriteBlock(0, blk))

	// The stored value for the primary block key is exactly encodeBlock's output; GetTxByOffset returns a
	// byte range of that value.
	stored := encodeBlock(0, blk)
	storedLen := uint32(len(stored))

	verify := func(stage string) {
		t.Helper()

		// The full range round-trips the whole stored value.
		res, err := impl.GetTxByOffset(0, 0, storedLen)
		require.NoError(t, err, stage)
		got, ok := res.Get()
		require.True(t, ok, stage)
		require.Equal(t, stored, got, stage)

		// A real transaction's raw bytes appear verbatim as a contiguous run in the value (each tx is a
		// length-delimited `repeated bytes` element), so locating one gives a valid (offset, length) —
		// exactly what a writer would record. Extracting that range must return the transaction.
		for _, tx := range blk.Payload().Txs() {
			idx := bytes.Index(stored, tx)
			require.GreaterOrEqual(t, idx, 0, stage)
			//nolint:gosec // small test offsets/lengths fit u32
			res, err := impl.GetTxByOffset(0, uint32(idx), uint32(len(tx)))
			require.NoError(t, err, stage)
			got, ok := res.Get()
			require.True(t, ok, stage)
			require.Equal(t, tx, got, stage)
		}

		// A range past the end of the value is an error.
		_, err = impl.GetTxByOffset(0, storedLen-1, 5)
		require.Error(t, err, stage)

		// A block that was never written is simply absent (not an error).
		res, err = impl.GetTxByOffset(1, 0, 1)
		require.NoError(t, err, stage)
		require.False(t, res.IsPresent(), stage)
	}

	verify("before flush")
	require.NoError(t, db.Flush())
	verify("after flush")
}

// TestGetTxByOffsetPruned verifies that a block below the retention watermark is reported ErrPruned,
// matching ReadBlockByNumber, rather than served (it may be stranded from its covering QC).
func TestGetTxByOffsetPruned(t *testing.T) {
	dir := t.TempDir()
	rng := utils.TestRngFromSeed(2)

	db, err := NewBlockDB(strandingConfig(t, dir, 8))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	impl := db.(*blockDB)

	writeSyntheticBatches(t, db, rng, 4, 5) // blocks 0..19; QCs [0,5),[5,10),[10,15),[15,20)
	require.NoError(t, db.PruneBefore(5))   // watermark to 5: blocks 0..4 are below it

	res, err := impl.GetTxByOffset(2, 0, 1)
	require.ErrorIs(t, err, types.ErrPruned)
	require.False(t, res.IsPresent())

	// A block at/above the watermark is still served.
	res, err = impl.GetTxByOffset(5, 0, 1)
	require.NoError(t, err)
	require.True(t, res.IsPresent())
}
