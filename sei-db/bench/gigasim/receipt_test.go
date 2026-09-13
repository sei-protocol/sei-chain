package gigasim

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	crand "github.com/sei-protocol/sei-chain/sei-db/common/rand"
)

// The receipt store refuses a block carrying one transaction hash twice, so a hash has to be unique
// for every position in the chain, not merely random-looking. Seeding a draw from the canned buffer is
// not enough on its own: two positions can hash to the same offset in it.
func TestSyntheticTxHashesAreUniqueAcrossPositions(t *testing.T) {
	t.Parallel()

	rand := crand.NewCannedRandom(1<<20, 1337)

	const (
		blocks               = 40
		transactionsPerBlock = int(2000)
	)
	seen := make(map[[hashLen]byte]struct{}, blocks*transactionsPerBlock)
	for block := range int64(blocks) {
		for txIndex := range transactionsPerBlock {
			var hash [hashLen]byte
			writeSyntheticTxHash(hash[:], rand, block, txIndex)

			_, duplicate := seen[hash]
			require.False(t, duplicate,
				"block %d transaction %d repeats a hash already used at another position", block, txIndex)
			seen[hash] = struct{}{}
		}
	}
}

// TestBuiltRecordKeysOnItsOwnReceiptHash pins a record's key to the hash inside the receipt it
// carries. The store keys on TxHash, so a record keyed on anything else would hide its receipt.
func TestBuiltRecordKeysOnItsOwnReceiptHash(t *testing.T) {
	t.Parallel()

	const count = 8
	buffer := newReceiptBuffer(count, newReceiptCache())
	rand := crand.NewCannedRandom(1<<20, 1337)
	txn := &transaction{
		erc20Contract: make([]byte, 1+keys.AddressLen+hashLen),
		srcAccount:    make([]byte, 1+keys.AddressLen+hashLen),
		dstAccount:    make([]byte, 1+keys.AddressLen+hashLen),
	}

	for index := range count {
		require.NoError(t, buffer.build(index, rand, txn, 3))

		record := buffer.records[index]
		require.Equal(t, common.HexToHash(record.Receipt.TxHashHex), record.TxHash,
			"the record's key must be the hash its own receipt reports")
		require.NotEmpty(t, record.ReceiptBytes, "a record reaches the store already marshaled")
	}
}

// A hash is recomputable from its position alone, which is what lets a run's transaction hashes be
// derived rather than stored.
func TestSyntheticTxHashDependsOnlyOnItsPosition(t *testing.T) {
	t.Parallel()

	first := crand.NewCannedRandom(1<<20, 1337)
	second := crand.NewCannedRandom(1<<20, 1337)

	// Advance one source, so that a hash that depended on the buffer cursor would differ.
	second.Bytes(1024)

	var fromFirst, fromSecond [hashLen]byte
	writeSyntheticTxHash(fromFirst[:], first, 7, 11)
	writeSyntheticTxHash(fromSecond[:], second, 7, 11)
	require.Equal(t, fromFirst, fromSecond)
}
