package gigasim

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	crand "github.com/sei-protocol/sei-chain/sei-db/common/rand"
)

// newTestGenerator returns a generator over a populated account model, with everything a block needs
// to be built and nothing it needs to be stored: buildBlock touches neither the block ledger nor the
// stores.
func newTestGenerator(t *testing.T, transactionsPerBlock int) *blockGenerator {
	t.Helper()
	return newTestGeneratorOfType(t, transactionsPerBlock, transactionTypeErc20)
}

// newTestGeneratorOfType is newTestGenerator for blocks of the given TransactionType.
func newTestGeneratorOfType(t *testing.T, transactionsPerBlock int, transactionType string) *blockGenerator {
	t.Helper()

	config := DefaultGigasimConfig()
	config.TransactionsPerBlock = transactionsPerBlock
	// The transactionsPerBlock argument is the whole block these tests build.
	config.LaneBlocksPerSuperblock = 1
	config.TransactionType = transactionType
	config.EnableReceiptStore = false

	population := plannedAccountPopulation(config)
	accounts := &accountModel{
		config:     config,
		rand:       crand.NewCannedRandom(1<<20, config.Seed),
		population: population,
		feeAccount: testAccountKey(0),
		// A population past setup, so both selection paths have accounts to draw from.
		nextAccountID:       population.total,
		nextErc20ContractID: int64(config.MinimumNumberOfErc20Contracts),
	}
	accounts.highestSafeAccountID = accounts.nextAccountID - 1

	return &blockGenerator{
		config:   config,
		accounts: accounts,
		batch:    newStateBatch(maxWritesPerTransaction*config.transactionsPerSuperblock() + 1),
		next:     1,
		nextLane: 1,
	}
}

// keysWritten is the set of keys a block's transactions write, derived from the transactions rather
// than from the changeset so that the two can be compared.
func keysWritten(blk *simulatedBlock) map[string]bool {
	written := map[string]bool{}
	for _, txn := range blk.transactions {
		written[string(txn.srcAccount)] = true
		written[string(txn.srcAccountSlot)] = true
		written[string(txn.dstAccountSlot)] = true
	}
	return written
}

// A generated block carries its whole changeset, so that executing it writes nothing and committing it
// has nothing to convert: one pair per distinct key its transactions touch, the fee account, and the
// identifier counters.
func TestGeneratedBlockCarriesItsChangeset(t *testing.T) {
	t.Parallel()

	g := newTestGenerator(t, 64)
	blk, err := g.buildBlock()
	require.NoError(t, err)
	require.Len(t, blk.transactions, g.config.TransactionsPerBlock)

	staged := map[string][]byte{}
	for _, pair := range blk.writes.changeSets[0].Changeset.Pairs {
		_, duplicate := staged[string(pair.Key)]
		require.False(t, duplicate, "key %x is committed twice in one block", pair.Key)
		staged[string(pair.Key)] = pair.Value
	}

	written := keysWritten(blk)
	require.Len(t, staged, len(written)+1+len(counterKeys),
		"the changeset holds the transactions' keys, the fee account and the counters")
	for key := range written {
		require.Contains(t, staged, key)
	}
	require.Equal(t, encodeCounter(g.accounts.NextAccountID()), staged[string(counterKeys[0])])
	require.Equal(t, encodeCounter(g.accounts.NextErc20ContractID()), staged[string(counterKeys[1])])
}

// The fee account is written once per block, not once per transaction. Every transaction still draws a
// fee balance — the draw is part of the random sequence the block is defined by — but they all name one
// key, so only the last draw is written.
func TestGeneratedBlockWritesTheFeeAccountOnce(t *testing.T) {
	t.Parallel()

	g := newTestGenerator(t, 8)
	feeKey := string(g.accounts.FeeCollectionAddress())

	blk, err := g.buildBlock()
	require.NoError(t, err)

	writes := 0
	var value []byte
	for _, pair := range blk.writes.changeSets[0].Changeset.Pairs {
		if string(pair.Key) == feeKey {
			writes++
			value = pair.Value
		}
	}
	require.Equal(t, 1, writes, "the fee account must be written exactly once per block")

	last := blk.transactions[len(blk.transactions)-1]
	require.Equal(t, last.newFeeBalance, value, "the surviving value is the last draw")
}

// A block's receipts account for exactly the gas the block's totals and gigasim_gas_used_total count,
// for either kind of transaction.
func TestReceiptGasMatchesTheReportedGas(t *testing.T) {
	t.Parallel()

	for _, transactionType := range []string{transactionTypeErc20, transactionTypeTransfer} {
		t.Run(transactionType, func(t *testing.T) {
			t.Parallel()

			g := newTestGeneratorOfType(t, 32, transactionType)
			g.config.EnableReceiptStore = true
			g.receiptCache = newReceiptCache()

			blk, err := g.buildBlock()
			require.NoError(t, err)
			require.Len(t, blk.receiptRecords, len(blk.transactions))

			var receiptGas uint64
			for _, record := range blk.receiptRecords {
				receiptGas += record.Receipt.GasUsed
			}
			reported := uint64(g.config.gasUsedBy(len(blk.transactions))) //nolint:gosec // positive by construction
			require.Equal(t, reported, receiptGas, "the receipts' gas must sum to the gas the block reports")
			require.Equal(t, reported, blk.receiptRecords[len(blk.receiptRecords)-1].Receipt.CumulativeGasUsed,
				"the last receipt's cumulative gas is the block's total")
		})
	}
}

// A native transfer writes both accounts' balances and no contract state: no balance slot, and no code
// beyond the identifier counters every block carries.
func TestNativeTransferBlockWritesOnlyAccounts(t *testing.T) {
	t.Parallel()

	g := newTestGeneratorOfType(t, 64, transactionTypeTransfer)
	blk, err := g.buildBlock()
	require.NoError(t, err)

	accounts := map[string]bool{string(g.accounts.FeeCollectionAddress()): true}
	for _, txn := range blk.transactions {
		require.Equal(t, nativeTransfer, txn.kind)
		require.Nil(t, txn.erc20Contract, "a native transfer touches no contract")
		accounts[string(txn.srcAccount)] = true
		accounts[string(txn.dstAccount)] = true
	}

	counters := map[string]bool{string(counterKeys[0]): true, string(counterKeys[1]): true}
	balancePrefix := keys.BuildEVMKey(keys.EVMKeyBalance, make([]byte, keys.AddressLen))[0]
	pairs := blk.writes.changeSets[0].Changeset.Pairs
	require.Len(t, pairs, len(accounts)+len(counters),
		"the changeset holds both accounts of every transfer, the fee account and the counters")
	for _, pair := range pairs {
		key := string(pair.Key)
		require.True(t, accounts[key] || counters[key], "key %x is neither an account nor a counter", pair.Key)
		if accounts[key] {
			require.Equal(t, balancePrefix, pair.Key[0], "accounts are written through their balance")
		}
	}
}

// A block's transactions are packed into as few ledger entries as the ledger's limit requires, without
// changing how many bytes the block stores.
func TestLedgerPayloadPacksTransactionsIntoTheLedgersEntries(t *testing.T) {
	t.Parallel()

	for _, transactions := range []int{1, maxLedgerEntries - 1, maxLedgerEntries, maxLedgerEntries*5 + 3} {
		config := DefaultGigasimConfig()
		config.TransactionsPerBlock = transactions
		config.BytesPerTransaction = 7

		payload := ledgerPayload(crand.NewCannedRandom(1<<20, config.Seed), config)
		require.Len(t, payload, min(transactions, maxLedgerEntries))
		require.Equal(t, int64(transactions*config.BytesPerTransaction), payloadBytes(payload),
			"packing must not change the bytes a block stores")

		smallest, largest := len(payload[0]), len(payload[0])
		for _, entry := range payload {
			require.Zero(t, len(entry)%config.BytesPerTransaction, "an entry holds whole transactions")
			smallest, largest = min(smallest, len(entry)), max(largest, len(entry))
		}
		require.LessOrEqual(t, largest-smallest, config.BytesPerTransaction,
			"transactions are spread evenly, so no entry holds more than one extra")
	}
}

// A superblock executes every lane block's transactions as one commit, and stores one payload per
// lane block. The lane blocks of the next superblock continue at the next block store height.
func TestSuperblockBundlesLaneBlocksIntoOneCommit(t *testing.T) {
	t.Parallel()

	g := newTestGenerator(t, 4)
	g.config.LaneBlocksPerSuperblock = 2
	g.batch = newStateBatch(maxWritesPerTransaction*g.config.transactionsPerSuperblock() + 1)

	first, err := g.buildBlock()
	require.NoError(t, err)
	require.Len(t, first.transactions, 8)
	require.Len(t, first.lanePayloads, 2)
	require.Equal(t, int64(1), first.number)
	require.Equal(t, int64(1), first.firstLaneBlock)
	require.Equal(t, int64(2*g.config.blockPayloadBytes()), first.payloadBytes())

	feeWrites := 0
	for _, pair := range first.writes.changeSets[0].Changeset.Pairs {
		if string(pair.Key) == string(g.accounts.FeeCollectionAddress()) {
			feeWrites++
		}
	}
	require.Equal(t, 1, feeWrites, "the fee account is written once per superblock")

	second, err := g.buildBlock()
	require.NoError(t, err)
	require.Equal(t, int64(2), second.number)
	require.Equal(t, int64(3), second.firstLaneBlock)
}

// Blocks are built back to back off one reused batch, so a block must carry only its own writes.
func TestEachBlockCarriesOnlyItsOwnWrites(t *testing.T) {
	t.Parallel()

	g := newTestGenerator(t, 16)

	first, err := g.buildBlock()
	require.NoError(t, err)
	second, err := g.buildBlock()
	require.NoError(t, err)

	require.Len(t, second.writes.changeSets[0].Changeset.Pairs,
		len(keysWritten(second))+1+len(counterKeys))
	require.NotEqual(t, first.number, second.number)
}
