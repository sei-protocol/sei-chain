package littblock

import (
	"bytes"
	"math"
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

// TestReadBlockSubrange covers the block-store sub-range read primitive: it must return exactly the
// requested byte range of a block's marshalled body, both before and after a flush, and it must yield a
// real transaction's bytes when given that transaction's range within the body.
func TestReadBlockSubrange(t *testing.T) {
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

	// The stored value for the primary block key is exactly encodeBlock's output. Offsets passed to
	// ReadBlockSubrange are relative to the marshalled body, i.e. the stored value minus its fixed prefix.
	stored := encodeBlock(0, blk)
	body := stored[blockValuePrefixLen:]
	bodyLen := uint32(len(body))

	verify := func(stage string) {
		t.Helper()

		// The whole body round-trips, proving the method applies the prefix itself.
		res, err := impl.ReadBlockSubrange(0, 0, bodyLen)
		require.NoError(t, err, stage)
		got, ok := res.Get()
		require.True(t, ok, stage)
		require.Equal(t, body, got, stage)

		// A real transaction's raw bytes appear verbatim as a contiguous run in the body (each tx is a
		// length-delimited `repeated bytes` element), so locating one within the body gives a valid
		// (offset, length) — exactly what a writer computes and records, with no prefix arithmetic.
		// Extracting that range must return the transaction.
		for _, tx := range blk.Payload().Txs() {
			idx := bytes.Index(body, tx)
			require.GreaterOrEqual(t, idx, 0, stage)
			//nolint:gosec // small test offsets/lengths fit u32
			res, err := impl.ReadBlockSubrange(0, uint32(idx), uint32(len(tx)))
			require.NoError(t, err, stage)
			got, ok := res.Get()
			require.True(t, ok, stage)
			require.Equal(t, tx, got, stage)
		}

		// Offset 0 addresses the first body byte, never the version byte: a body-relative read can never
		// reach into the prefix.
		res, err = impl.ReadBlockSubrange(0, 0, 1)
		require.NoError(t, err, stage)
		got, ok = res.Get()
		require.True(t, ok, stage)
		require.Equal(t, body[:1], got, stage)

		// A range past the end of the body is an error, even though those bytes exist in the stored value
		// ahead of the body.
		_, err = impl.ReadBlockSubrange(0, bodyLen-1, 5)
		require.Error(t, err, stage)

		// An offset too large to be prefixed without wrapping is rejected rather than read from the
		// beginning of the block.
		_, err = impl.ReadBlockSubrange(0, math.MaxUint32, 1)
		require.Error(t, err, stage)

		// A block that was never written is simply absent (not an error).
		res, err = impl.ReadBlockSubrange(1, 0, 1)
		require.NoError(t, err, stage)
		require.False(t, res.IsPresent(), stage)
	}

	verify("before flush")
	require.NoError(t, db.Flush())
	verify("after flush")
}

// TestReadBlockSubrangePruned verifies that a block below the retention watermark is reported ErrPruned,
// matching ReadBlockByNumber, rather than served (it may be stranded from its covering QC).
func TestReadBlockSubrangePruned(t *testing.T) {
	dir := t.TempDir()
	rng := utils.TestRngFromSeed(2)

	db, err := NewBlockDB(strandingConfig(t, dir, 8))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	impl := db.(*blockDB)

	writeSyntheticBatches(t, db, rng, 4, 5) // blocks 0..19; QCs [0,5),[5,10),[10,15),[15,20)
	require.NoError(t, db.PruneBefore(5))   // watermark to 5: blocks 0..4 are below it

	res, err := impl.ReadBlockSubrange(2, 0, 1)
	require.ErrorIs(t, err, types.ErrPruned)
	require.False(t, res.IsPresent())

	// Retention wins over argument shape: a below-watermark block asked for with an unusable offset still
	// returns ErrPruned, matching ReadBlockByNumber and the documented contract.
	res, err = impl.ReadBlockSubrange(2, math.MaxUint32, 1)
	require.ErrorIs(t, err, types.ErrPruned)
	require.False(t, res.IsPresent())

	// A block at/above the watermark is still served.
	res, err = impl.ReadBlockSubrange(5, 0, 1)
	require.NoError(t, err)
	require.True(t, res.IsPresent())
}
