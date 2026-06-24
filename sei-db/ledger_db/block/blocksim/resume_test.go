package blocksim

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	tmutils "github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// TestRecoverResumeState exercises the resume glue end-to-end against a real
// litt-backed store: generate a couple of batches with the actual generator,
// persist them, reopen, and assert recoverResumeState recovers the highest
// block number and the last QC so generation can continue contiguously.
func TestRecoverResumeState(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultBlocksimConfig()
	cfg.Backend = "litt"
	cfg.DataDir = dir
	cfg.BlocksPerQc = 5
	cfg.TransactionsPerBlock = 1
	cfg.BytesPerTransaction = 16

	// Mirror NewBlockSim: build the RNG, then the committee (which consumes it),
	// then hand the same RNG to the generator.
	rng := tmutils.TestRngFromSeed(cfg.Seed)
	committee, keys, err := buildCommittee(rng, int(cfg.CommitteeSize)) //nolint:gosec // small config value
	require.NoError(t, err)

	db, err := openBlockDB(cfg)
	require.NoError(t, err)

	// Generate two contiguous batches deterministically, without launching the
	// background goroutine (struct literal instead of NewBlockGenerator).
	gen := &BlockGenerator{
		ctx:       context.Background(),
		config:    cfg,
		rng:       rng,
		committee: committee,
		keys:      keys,
		prev:      tmutils.None[*types.CommitQC](),
	}
	var last *generatedBatch
	for i := 0; i < 2; i++ {
		b := gen.buildBatch()
		require.NoError(t, db.WriteQC(b.first, b.next, b.qc))
		for j, blk := range b.blocks {
			require.NoError(t, db.WriteBlock(b.first+types.GlobalBlockNumber(j), blk)) //nolint:gosec // small index
		}
		last = b
	}
	require.NoError(t, db.Flush())
	require.NoError(t, db.Close())

	// Reopen the same data dir and recover the tail.
	db2, err := openBlockDB(cfg)
	require.NoError(t, err)
	defer func() { _ = db2.Close() }()

	prev, highestOpt, err := recoverResumeState(db2)
	require.NoError(t, err)
	highest, ok := highestOpt.Get()
	require.True(t, ok, "recovered highest must be present after a clean write")
	require.Equal(t, uint64(last.next-1), highest, "recovered highest must be the last persisted block number")

	prevQC, ok := prev.Get()
	require.True(t, ok, "recovered prev QC must be present")
	require.Equal(t, last.first, prevQC.GlobalRange(0).First, "recovered QC must be the last persisted QC")
	require.Equal(t, last.next, prevQC.GlobalRange(0).Next)

	// Empty-store sanity: a fresh dir recovers nothing.
	empty, err := openBlockDB(&BlocksimConfig{Backend: "litt", DataDir: t.TempDir(), LittRetentionSeconds: 1})
	require.NoError(t, err)
	defer func() { _ = empty.Close() }()
	prev0, highest0, err := recoverResumeState(empty)
	require.NoError(t, err)
	require.False(t, prev0.IsPresent(), "empty store must recover no QC")
	require.False(t, highest0.IsPresent(), "empty store must recover no highest block")
}
