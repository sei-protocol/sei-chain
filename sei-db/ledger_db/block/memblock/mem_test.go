package memblock

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/blocktest"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
)

func TestConformance(t *testing.T) {
	blocktest.RunConformance(t, func(t *testing.T) (block.BlockDB, func() error) {
		return NewBlockDB(), func() error { return nil }
	})
}

// TestPruneRemovesBelowWatermark verifies the in-memory store's synchronous,
// exact pruning: everything below the watermark is gone immediately.
func TestPruneRemovesBelowWatermark(t *testing.T) {
	committee, keys := blocktest.BuildCommittee()
	batches := blocktest.GenerateBatches(committee, keys)
	db := NewBlockDB()
	blocktest.WriteAll(t, db, batches)

	watermark := batches[1].First
	require.NoError(t, db.PruneBefore(watermark))

	// First batch (below watermark) is gone.
	for i := range batches[0].Blocks {
		n := batches[0].First + types.GlobalBlockNumber(i) //nolint:gosec // i is a non-negative slice index
		opt, err := db.ReadBlockByNumber(n)
		require.NoError(t, err)
		require.False(t, opt.IsPresent(), "block %d should be pruned", n)
	}
	qc, err := db.ReadQCByBlockNumber(batches[0].First)
	require.NoError(t, err)
	require.False(t, qc.IsPresent(), "QC below watermark should be pruned")

	// Watermark block is retained.
	opt, err := db.ReadBlockByNumber(watermark)
	require.NoError(t, err)
	require.True(t, opt.IsPresent())
}
