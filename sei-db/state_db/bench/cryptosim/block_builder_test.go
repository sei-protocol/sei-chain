package cryptosim

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	crand "github.com/sei-protocol/sei-chain/sei-db/common/rand"
)

// newTestBuilder returns a builder over a database that records writes but serves no reads, with a
// small transaction count so a block is cheap to build.
//
// Selection is pinned to the hot account and hot contract sets, whose bounds come from config rather
// than from the generator's counters. A generator that has not been through setup has a cold window
// running from a negative ID and an empty contract range, so cold selection cannot succeed — and
// buildBlock reports a failed transaction by printing and moving on, which would leave an empty block
// rather than a failing test.
func newTestBuilder(t *testing.T, transactionsPerBlock int) *blockBuilder {
	t.Helper()

	cfg := DefaultCryptoSimConfig()
	cfg.TransactionsPerBlock = transactionsPerBlock
	cfg.GenerateReceipts = false
	cfg.NumberOfHotAccounts = 16
	cfg.HotAccountProbability = 1.0
	cfg.NewAccountProbability = 0
	cfg.HotErc20ContractProbability = 1.0
	cfg.HotErc20ContractSetSize = 4

	db := NewDatabase(cfg, &readTrackingWrapper{}, nil, 0)
	random := crand.NewCannedRandom(cfg.CannedRandomSize, cfg.Seed)
	generator, err := NewDataGenerator(cfg, db, random, nil)
	require.NoError(t, err)

	// The hot contract range is bounded by how many contracts exist, so it is empty until some do.
	for i := 0; i < cfg.HotErc20ContractSetSize; i++ {
		_, _, err := generator.CreateNewErc20Contract(cfg.Erc20ContractSize, false)
		require.NoError(t, err)
	}
	generator.ReportEndOfBlock()
	db.HarvestWrites()

	builder := NewBlockBuilder(context.Background(), cfg, nil, generator, db)
	return builder
}

// requireFullBlock guards against buildBlock silently producing a short block: it reports a failed
// transaction by printing and continuing, so an invalid fixture reads as a passing test over no data.
func requireFullBlock(t *testing.T, b *blockBuilder, blk *block) {
	t.Helper()
	require.Len(t, blk.transactions, b.config.TransactionsPerBlock,
		"block is short, so some transactions failed to build")
}

// The fee collection account is written once per block, not once per transaction. Every transaction
// still draws a fee balance — the draw is part of the random sequence the block is defined by — but
// they all name one key, so only the last draw is written.
func TestBuildBlockWritesFeeCollectionAccountOnce(t *testing.T) {
	const transactions = 8
	b := newTestBuilder(t, transactions)
	feeKey := string(b.dataGenerator.FeeCollectionAddress())

	blk := b.buildBlock()
	requireFullBlock(t, b, blk)

	feeWrites := 0
	var feeValue []byte
	for _, pair := range blk.Changeset() {
		if string(pair.Key) == feeKey {
			feeWrites++
			feeValue = pair.Value
		}
	}
	require.Equal(t, 1, feeWrites, "the fee collection account must be written exactly once per block")

	// The surviving value is the last transaction's, which is what the per-transaction version left in
	// the map after every earlier write was overwritten.
	last := blk.transactions[len(blk.transactions)-1]
	require.Equal(t, last.newFeeBalance, feeValue)
}

// Transaction values are windows onto the canned random buffer rather than copies of it. That is only
// sound if the buffer is never rewritten, so a block's values must still read the same after later
// blocks have been built.
func TestBuildBlockValuesSurviveLaterBlocks(t *testing.T) {
	b := newTestBuilder(t, 8)

	first := b.buildBlock()
	requireFullBlock(t, b, first)
	before := make([][]byte, 0, len(first.Changeset()))
	for _, pair := range first.Changeset() {
		before = append(before, bytes.Clone(pair.Value))
	}

	for i := 0; i < 4; i++ {
		b.buildBlock()
	}

	for i, pair := range first.Changeset() {
		require.True(t, bytes.Equal(before[i], pair.Value),
			"value %d changed after later blocks were built", i)
	}
}
