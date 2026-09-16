package gigasim

import (
	"testing"

	"github.com/stretchr/testify/require"

	crand "github.com/sei-protocol/sei-chain/sei-db/common/rand"
)

// newTestGenerator returns a generator over a populated account model, with everything a block needs
// to be built and nothing it needs to be stored: buildBlock touches neither the block ledger nor the
// stores.
func newTestGenerator(t *testing.T, transactionsPerBlock int) *blockGenerator {
	t.Helper()

	config := DefaultGigasimConfig()
	config.TransactionsPerBlock = transactionsPerBlock
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
		batch:    newStateBatch(writesPerTransaction*transactionsPerBlock + 1),
		next:     1,
	}
}

// keysWritten is the set of keys a block's transactions write, derived from the transactions rather
// than from the changeset so that the two can be compared.
func keysWritten(blk *simulatedBlock) map[string]bool {
	written := map[string]bool{}
	for _, txn := range blk.transactions {
		written[string(txn.srcAccount)] = true
		written[string(txn.dstAccount)] = true
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
