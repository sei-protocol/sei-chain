package blocksim

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	crand "github.com/sei-protocol/sei-chain/sei-db/common/rand"
	blocktypes "github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/types"
)

// writeBatches persists n generated batches through the same calls the benchmark's
// write path uses, and returns the last one.
func writeBatches(t *testing.T, db blocktypes.BlockDB, gen *BlockGenerator, n int) *generatedBatch {
	t.Helper()
	var last *generatedBatch
	for range n {
		batch := gen.buildBatch()
		require.NoError(t, db.PutRecord(blocktypes.KindQC, batch.first, batch.next, batch.qc))
		for i, value := range batch.blocks {
			require.NoError(t, db.PutBlock(batch.first+uint64(i), blockHash(batch.first+uint64(i)), value))
		}
		last = batch
	}
	return last
}

// newResumeConfig returns a litt-backed config small enough to write a few batches
// quickly.
func newResumeConfig(t *testing.T) *BlocksimConfig {
	t.Helper()
	cfg := DefaultBlocksimConfig()
	cfg.Backend = "litt"
	cfg.DataDir = t.TempDir()
	cfg.BlocksPerQc = 5
	cfg.TransactionsPerBlock = 1
	cfg.BytesPerTransaction = 16
	return cfg
}

// newGenerator builds a generator without launching its background goroutine, so a
// test can pull batches deterministically.
func newGenerator(cfg *BlocksimConfig, first uint64) *BlockGenerator {
	return &BlockGenerator{
		ctx:    context.Background(),
		config: cfg,
		rand:   crand.NewCannedRandom(int(cfg.RandomDataBufferSizeBytes), cfg.Seed), //nolint:gosec // bounded by config
		next:   first,
	}
}

// TestRecoverResumeState exercises the resume glue end-to-end against a real
// litt-backed store: write a couple of batches, reopen, and assert the newest QC's
// range and the newest block are recovered so generation continues contiguously.
func TestRecoverResumeState(t *testing.T) {
	cfg := newResumeConfig(t)

	db, err := openBlockDB(cfg)
	require.NoError(t, err)
	last := writeBatches(t, db, newGenerator(cfg, 0), 2)
	require.NoError(t, db.Flush())
	require.NoError(t, db.Close())

	// Reopen the same data dir and recover the tail.
	db2, err := openBlockDB(cfg)
	require.NoError(t, err)
	defer func() { _ = db2.Close() }()

	resume, found, err := recoverResumeState(db2)
	require.NoError(t, err)
	require.True(t, found, "a store holding batches must recover a resume point")
	require.Equal(t, last.first, resume.qcFirst, "recovered QC must be the last persisted QC")
	require.Equal(t, last.next, resume.qcNext)
	require.True(t, resume.hasBlocks, "blocks were written, so the tail must report them")
	require.Equal(t, last.next-1, resume.highestBlock, "recovered highest must be the last persisted block")
}

// TestRecoverResumeStateEmptyStore pins that a fresh store resumes from genesis
// rather than reporting a bogus tail.
func TestRecoverResumeStateEmptyStore(t *testing.T) {
	db, err := openBlockDB(newResumeConfig(t))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	_, found, err := recoverResumeState(db)
	require.NoError(t, err)
	require.False(t, found, "an empty store must recover no resume point")
}

// TestRecoverResumeStateQCWithoutBlocks covers the crash window: a batch writes its
// QC before its blocks, so a torn tail can leave a QC covering blocks that were
// never written. Resume must report the QC's full range and no blocks, which is what
// tells the caller to backfill it.
func TestRecoverResumeStateQCWithoutBlocks(t *testing.T) {
	cfg := newResumeConfig(t)

	db, err := openBlockDB(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	gen := newGenerator(cfg, 0)
	writeBatches(t, db, gen, 1)

	// The QC of the next batch lands, but none of its blocks do.
	torn := gen.buildBatch()
	require.NoError(t, db.PutRecord(blocktypes.KindQC, torn.first, torn.next, torn.qc))

	resume, found, err := recoverResumeState(db)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, torn.first, resume.qcFirst, "the torn QC is the newest one")
	require.Equal(t, torn.next, resume.qcNext)
	require.False(t, resume.hasBlocks, "none of the torn QC's blocks were written")
}
