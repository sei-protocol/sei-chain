package cryptosim

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// A block's contents must not depend on how many goroutines generated it. That is the property the whole
// range split rests on: if it does not hold, two runs at different worker counts are not comparable and
// the benchmark stops measuring the same thing.
func TestBlockContentsIndependentOfWorkerCount(t *testing.T) {
	reference := buildBlocksWithWorkers(t, 1, 3)

	for _, workers := range []int{2, 3, 8, 17} {
		got := buildBlocksWithWorkers(t, workers, 3)
		require.Len(t, got, len(reference))

		for i := range reference {
			context := fmt.Sprintf("block %d at %d workers", i, workers)
			requireSameBlock(t, reference[i], got[i], context)
		}
	}
}

// Every account a block mints must be distinct, and the IDs a block hands out must be exactly the
// contiguous run its arithmetic reserved — no worker may collide with or skip past another's range.
func TestMintedAccountIDsAreContiguousAcrossWorkers(t *testing.T) {
	for _, workers := range []int{1, 4, 8} {
		builder := newMintingTestBuilder(t, workers)
		firstID := builder.dataGenerator.NextAccountID()

		var minted int64
		for i := 0; i < 3; i++ {
			results := builder.buildBlockRanges(builder.nextBlockNumber)
			builder.nextBlockNumber++

			ids := make(map[int64]bool)
			var blockMinted int64
			for _, result := range results {
				blockMinted += result.accountsMinted
			}
			// The reserved run is handed out in order, so the IDs a block uses are exactly
			// [firstID+minted, firstID+minted+blockMinted).
			for id := firstID + minted; id < firstID+minted+blockMinted; id++ {
				require.False(t, ids[id], "id %d minted twice at %d workers", id, workers)
				ids[id] = true
			}
			builder.dataGenerator.AdoptForkResults(blockMinted, 0)
			builder.dataGenerator.ReportEndOfBlock()
			minted += blockMinted
		}

		require.Positive(t, minted, "the cadence must mint something at %d workers", workers)
		require.Equal(t, firstID+minted, builder.dataGenerator.NextAccountID(),
			"the generator must have advanced by exactly what the forks minted, at %d workers", workers)
	}
}

// A cadence of zero must mint nothing, whatever the worker count.
func TestZeroCadenceMintsNoAccounts(t *testing.T) {
	builder := newTestBuilderWithWorkers(t, 64, 4)
	builder.config.TransactionsPerNewAccount = 0
	before := builder.dataGenerator.NextAccountID()

	results := builder.buildBlockRanges(builder.nextBlockNumber)
	for _, result := range results {
		require.Zero(t, result.accountsMinted)
	}
	require.Equal(t, before, builder.dataGenerator.NextAccountID())
}

// newTestBuilderWithWorkers is newTestBuilder with the block split across the given number of workers.
func newTestBuilderWithWorkers(t *testing.T, transactionsPerBlock int, workers int) *blockBuilder {
	t.Helper()
	builder := newTestBuilder(t, transactionsPerBlock)
	builder.config.BlockBuildWorkers = workers
	return builder
}

// newMintingTestBuilder returns a builder whose every account selection mints a new account.
//
// Selection is driven entirely down the minting path — no hot selections, and a cadence of one — because
// the alternative path draws from the cold window, which is empty on a generator that has not been
// through setup. That would make a failed selection, not a minted account, and buildBlock reports a
// failed transaction by printing and carrying on.
func newMintingTestBuilder(t *testing.T, workers int) *blockBuilder {
	t.Helper()
	builder := newTestBuilder(t, 64)
	builder.config.BlockBuildWorkers = workers
	builder.config.HotAccountProbability = 0
	builder.config.TransactionsPerNewAccount = 1
	return builder
}

// buildBlocksWithWorkers builds count blocks from a freshly seeded builder at the given worker count.
func buildBlocksWithWorkers(t *testing.T, workers int, count int) []*block {
	t.Helper()
	builder := newMintingTestBuilder(t, workers)
	blocks := make([]*block, 0, count)
	for i := 0; i < count; i++ {
		blocks = append(blocks, builder.buildBlock())
	}
	return blocks
}

// requireSameBlock asserts two blocks carry the same transactions and the same writes.
func requireSameBlock(t *testing.T, want *block, got *block, context string) {
	t.Helper()

	require.Equal(t, want.BlockNumber(), got.BlockNumber(), "%s: block number", context)

	wantTxns := want.Transactions()
	gotTxns := got.Transactions()
	require.Len(t, gotTxns, len(wantTxns), "%s: transaction count", context)
	for i := range wantTxns {
		require.Equal(t, wantTxns[i].srcAccount, gotTxns[i].srcAccount, "%s: txn %d source", context, i)
		require.Equal(t, wantTxns[i].dstAccount, gotTxns[i].dstAccount, "%s: txn %d dest", context, i)
		require.Equal(t, wantTxns[i].srcAccountSlot, gotTxns[i].srcAccountSlot,
			"%s: txn %d source slot", context, i)
		require.Equal(t, wantTxns[i].dstAccountSlot, gotTxns[i].dstAccountSlot,
			"%s: txn %d dest slot", context, i)
		require.Equal(t, wantTxns[i].newSrcBalance, gotTxns[i].newSrcBalance,
			"%s: txn %d source balance", context, i)
	}

	// Compared as a set: SetWrites flattens a map, so the changeset's order carries no meaning and
	// differs run to run even without any of this.
	wantWrites := writeSet(want)
	gotWrites := writeSet(got)
	require.Len(t, gotWrites, len(wantWrites), "%s: write count", context)
	for key, value := range wantWrites {
		gotValue, ok := gotWrites[key]
		require.True(t, ok, "%s: missing write for %x", context, key)
		require.Equal(t, value, gotValue, "%s: write value for %x", context, key)
	}
}

// writeSet indexes a block's changeset by key, so two blocks' writes can be compared without depending
// on the order the changeset happens to be in.
func writeSet(blk *block) map[string][]byte {
	writes := make(map[string][]byte, len(blk.Changeset()))
	for _, pair := range blk.Changeset() {
		writes[string(pair.Key)] = pair.Value
	}
	return writes
}
