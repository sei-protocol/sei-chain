package littblock

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/blocktest"
)

// mustConfig builds a litt block-db Config rooted at dir with a tiny retention
// so the prune watermark is the sole observable reclamation gate in tests.
func mustConfig(t *testing.T, dir string) *LittBlockConfig {
	config, err := DefaultConfig(dir)
	require.NoError(t, err)
	config.Retention = time.Nanosecond
	return config
}

func TestConformance(t *testing.T) {
	blocktest.RunConformance(t, func(t *testing.T) (block.BlockDB, func() error) {
		// Tiny retention so the prune watermark is the sole observable gate
		// (production uses a 24h failsafe floor).
		config := mustConfig(t, t.TempDir())
		db, err := NewBlockDB(config)
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.Close() })

		settle := func() error {
			if err := db.Flush(); err != nil {
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
	dir := t.TempDir()
	committee, keys := blocktest.BuildCommittee()
	batches := blocktest.GenerateBatches(committee, keys)

	db, err := NewBlockDB(mustConfig(t, dir))
	require.NoError(t, err)
	blocktest.WriteAll(t, db, batches)
	require.NoError(t, db.Flush())
	require.NoError(t, db.Close())

	// Reopen: the segments written above are now sealed and collectable.
	db2, err := NewBlockDB(mustConfig(t, dir))
	require.NoError(t, err)
	defer func() { _ = db2.Close() }()

	beyond := batches[len(batches)-1].Next
	require.NoError(t, db2.PruneBefore(beyond))
	require.NoError(t, db2.(*blockDB).forceGC())

	for _, b := range batches {
		opt, err := db2.ReadBlockByNumber(b.First)
		require.NoError(t, err)
		require.False(t, opt.IsPresent(), "block %d should be reclaimed after restart", b.First)
		qc, err := db2.ReadQCByBlockNumber(b.First)
		require.NoError(t, err)
		require.False(t, qc.IsPresent(), "QC at %d should be reclaimed after restart", b.First)
	}
}
