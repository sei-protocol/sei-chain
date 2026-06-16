package litt

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/blocktest"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
)

func TestConformance(t *testing.T) {
	blocktest.RunConformance(t, func(t *testing.T, committee *types.Committee) (block.BlockDB, func() error) {
		ctx := context.Background()
		// Tiny retention so the prune watermark is the sole observable gate
		// (production uses a 24h failsafe floor).
		db, err := NewBlockDB(t.TempDir(), committee, time.Nanosecond)
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.Close(ctx) })

		settle := func() error {
			if err := db.Flush(ctx); err != nil {
				return err
			}
			return db.(*blockDB).forceGC()
		}
		return db, settle
	})
}

// TestPruneReclaimsAcrossRestart verifies the durable reclamation path: data
// written, then pruned past after a restart (which seals the segments it landed
// in), is collected by GC. This is the realistic shape — the active segment of
// a running DB only holds the newest data, which is never below the watermark.
func TestPruneReclaimsAcrossRestart(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	committee, keys := blocktest.BuildCommittee()
	batches := blocktest.GenerateBatches(committee, keys)

	db, err := NewBlockDB(dir, committee, time.Nanosecond)
	require.NoError(t, err)
	blocktest.WriteAll(t, db, batches)
	require.NoError(t, db.Flush(ctx))
	require.NoError(t, db.Close(ctx))

	// Reopen: the segments written above are now sealed and collectable.
	db2, err := NewBlockDB(dir, committee, time.Nanosecond)
	require.NoError(t, err)
	defer func() { _ = db2.Close(ctx) }()

	last := batches[len(batches)-1]
	beyond := last.QC.QC().GlobalRange(committee).Next
	require.NoError(t, db2.PruneBefore(ctx, beyond))
	require.NoError(t, db2.(*blockDB).forceGC())

	for _, b := range batches {
		opt, err := db2.ReadBlockByNumber(ctx, b.First)
		require.NoError(t, err)
		require.False(t, opt.IsPresent(), "block %d should be reclaimed after restart", b.First)
		qc, err := db2.ReadQCByBlockNumber(ctx, b.First)
		require.NoError(t, err)
		require.False(t, qc.IsPresent(), "QC at %d should be reclaimed after restart", b.First)
	}
}
