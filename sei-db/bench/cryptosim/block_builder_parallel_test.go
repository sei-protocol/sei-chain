package cryptosim

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
)

// A block's contents must not depend on how many goroutines generated it. That is the property the
// whole range split rests on: if it does not hold, two runs at different worker counts are not
// comparable and the benchmark stops measuring the same thing.
func TestBlockContentsIndependentOfWorkerCount(t *testing.T) {
	t.Parallel()

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

// A cadence of zero must mint nothing, whatever the worker count.
func TestZeroCadenceMintsNoAccounts(t *testing.T) {
	t.Parallel()

	builder := newTestBuilderWithWorkers(t, 64, 4)
	builder.config.SelectionsPerNewAccount = 0
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

// newMintingTestBuilder returns a builder whose every account selection creates a new account, which
// is what a cadence of one means now that creating takes precedence over a hot selection.
func newMintingTestBuilder(t *testing.T, workers int) *blockBuilder {
	t.Helper()
	builder := newTestBuilder(t, 64)
	builder.config.BlockBuildWorkers = workers
	builder.config.SelectionsPerNewAccount = 1
	return builder
}

// newMixedSelectionTestBuilder returns a builder whose selections are a mix of all three kinds: a
// cadence that creates often enough for several accounts per block, and hot selections left on.
func newMixedSelectionTestBuilder(t *testing.T, workers int) *blockBuilder {
	t.Helper()
	builder := newTestBuilder(t, 64)
	builder.config.BlockBuildWorkers = workers
	// A cadence of nine so that some create slots land on hot selections: multiples of an even cadence
	// never do, which would leave the rule that settles the tie untested.
	builder.config.SelectionsPerNewAccount = 9
	builder.config.HotAccountProbability = 0.1
	return builder
}

// TestABlockCreatesExactlyTheReservedAccountIDs pins what makes a worker's reserved range of account
// IDs correct: the IDs a block hands out are the run reserved for it, all of it and nothing else.
//
// A worker's range is reserved by counting the selections in its slice of the block that create an
// account, before any worker runs. Two ways for that to be wrong are a worker leaving the tail of its
// range unused — which happened when a hot selection could win the tie and talk a selection out of
// creating — and two workers being handed overlapping starts, which no count of accounts created can
// see. So the addresses the block actually created are compared against the addresses the reserved run
// implies, which catches a gap and a collision alike.
func TestABlockCreatesExactlyTheReservedAccountIDs(t *testing.T) {
	t.Parallel()

	const blocks = 3

	for _, workers := range []int{1, 4, 8} {
		builder := newMixedSelectionTestBuilder(t, workers)
		selections := int64(builder.config.TransactionsPerBlock) * selectionsPerTransaction
		var createdInRun int64

		for block := 0; block < blocks; block++ {
			// Taken per block from where the block starts in the run: the cadence does not restart at a
			// block boundary, so how many accounts a block reserves depends on which block it is.
			reserved := builder.dataGenerator.AccountsMintedPerSelections(
				builder.firstSelectionOf(builder.nextBlockNumber, 0), selections)
			require.Positive(t, reserved,
				"the fixture must reserve something to be a test at %d workers", workers)

			firstID := builder.dataGenerator.NextAccountID()

			created := createdAccountAddresses(builder.buildBlock())

			// Counted before being compared as a set, so that one address created twice is a failure
			// rather than a set that happens to match.
			require.Len(t, created, int(reserved),
				"block %d at %d workers created %d accounts against %d reserved",
				block, workers, len(created), reserved)
			require.Equal(t,
				reservedAccountAddresses(builder.dataGenerator, firstID, reserved),
				asAddressSet(created),
				"block %d at %d workers did not create the run reserved for it", block, workers)

			require.Equal(t, firstID+reserved, builder.dataGenerator.NextAccountID(),
				"the counter must cover every ID the block used, at %d workers", workers)

			createdInRun += int64(len(created))
		}

		// The cadence runs across the whole sequence, so a run of blocks creates what the cadence says
		// for that many selections — not what each block would round to on its own.
		require.Equal(t,
			builder.dataGenerator.AccountsMintedPerSelections(0, blocks*selections), createdInRun,
			"the run created %d accounts against the cadence's %d, at %d workers",
			createdInRun,
			builder.dataGenerator.AccountsMintedPerSelections(0, blocks*selections), workers)
	}
}

// createdAccountAddresses returns the address of every account a block created, in block order and
// with repeats kept, taken from the transactions that reported creating one.
func createdAccountAddresses(blk *block) [][]byte {
	created := make([][]byte, 0, blk.TransactionCount())
	for _, txn := range blk.Transactions() {
		if txn.isSrcNew {
			created = append(created, txn.srcAccount)
		}
		if txn.isDstNew {
			created = append(created, txn.dstAccount)
		}
	}
	return created
}

// reservedAccountAddresses returns the addresses of the account IDs [firstID, firstID+reserved).
//
// An ID's address is a function of the ID alone, so the run reserved for a block can be turned into the
// keys that run stands for without asking the generator what it did.
func reservedAccountAddresses(generator *DataGenerator, firstID int64, reserved int64) map[string]bool {
	addresses := make(map[string]bool, reserved)
	for id := firstID; id < firstID+reserved; id++ {
		addr := generator.Rand().Address(accountPrefix, id, keys.AddressLen)
		addresses[string(keys.BuildEVMKey(accountKeyPrefix, addr))] = true
	}
	return addresses
}

// asAddressSet indexes addresses for comparison against a reserved run.
func asAddressSet(addresses [][]byte) map[string]bool {
	set := make(map[string]bool, len(addresses))
	for _, address := range addresses {
		set[string(address)] = true
	}
	return set
}

// Where a selection is both a create slot and a hot slot, it creates. That tie is the whole defect
// this design closes: the arithmetic that reserves ID ranges counts create slots, so a tie settled the
// other way would reserve an ID that no worker ever uses.
func TestCreatingWinsOverAHotSelection(t *testing.T) {
	t.Parallel()

	builder := newMixedSelectionTestBuilder(t, 1)
	generator := builder.dataGenerator

	tie := int64(-1)
	for selection := range int64(1000) {
		if generator.selectionCreatesAccount(selection) && generator.selectionIsHot(selection) {
			tie = selection
			break
		}
	}
	require.NotEqual(t, int64(-1), tie, "the fixture must contain a selection that is both")

	generator.selectionCount = tie
	before := generator.NextAccountID()
	_, _, isNew, err := generator.RandomAccount()
	require.NoError(t, err)
	require.True(t, isNew, "selection %d is a create slot, so it must create an account", tie)
	require.Equal(t, before+1, generator.NextAccountID())
}

// Hot selections are spread evenly rather than drawn, so any run of selections carries the configured
// share of them. A block that lost them entirely, or took nothing but them, would make every other
// test here agree about a workload nobody runs.
func TestHotSelectionsFollowTheConfiguredShare(t *testing.T) {
	t.Parallel()

	builder := newTestBuilder(t, 64)
	builder.config.HotAccountProbability = 0.1
	builder.config.SelectionsPerNewAccount = 0

	const selections = 100_000
	hot := 0
	for selection := range int64(selections) {
		if builder.dataGenerator.selectionIsHot(selection) {
			hot++
		}
	}
	require.Equal(t, selections/10, hot, "a tenth of the selections must be hot, exactly")
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
