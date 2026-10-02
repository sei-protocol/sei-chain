package gigasim

import (
	"testing"

	"github.com/stretchr/testify/require"

	crand "github.com/sei-protocol/sei-chain/sei-db/common/rand"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
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

// TestBuiltRecordKeysOnItsOwnReceiptHash pins a record's key to the hash written into its body. The
// store keys on TxHash and stores ReceiptBytes as the body.
func TestBuiltRecordKeysOnItsOwnReceiptHash(t *testing.T) {
	t.Parallel()

	const count = 8
	buffer := newReceiptBuffer(count, 50_000)
	rand := crand.NewCannedRandom(1<<20, 1337)

	for index := range count {
		buffer.build(index, rand, 3)

		record := buffer.records[index]
		require.NotNil(t, record.Receipt)
		require.Equal(t, receiptLog, record.Receipt.Logs)
		require.Len(t, record.Receipt.Logs[0].Topics, 3)
		require.Len(t, record.ReceiptBytes, len(receiptBody))
		require.Equal(t, record.TxHash[:], record.ReceiptBytes[len(record.ReceiptBytes)-hashLen:])
		var decoded evmtypes.Receipt
		require.NoError(t, decoded.Unmarshal(record.ReceiptBytes))
		var expected [hashLen]byte
		writeSyntheticTxHash(expected[:], crand.NewCannedRandom(1<<20, 1337), 3, index)
		require.Equal(t, expected[:], record.TxHash[:])
	}
	require.Equal(t, int64(count*len(receiptBody)), buffer.encodedBytes)
}

// TestNativeTransferReceiptRecordsTheFixedGas pins a native transfer's receipt to the fixed 21,000 gas,
// with CumulativeGasUsed the running sum, and a body of the fixed receipt size.
func TestNativeTransferReceiptRecordsTheFixedGas(t *testing.T) {
	t.Parallel()

	config := DefaultGigasimConfig()
	config.TransactionType = transactionTypeTransfer
	buffer := newReceiptBuffer(2, uint64(config.gasUsedBy(1)))
	rand := crand.NewCannedRandom(1<<20, 1337)

	for index := range 2 {
		buffer.build(index, rand, 3)

		built := buffer.records[index].Receipt
		require.Equal(t, uint64(21_000), built.GasUsed)
		require.Equal(t, uint64(21_000*(index+1)), built.CumulativeGasUsed)
		require.Len(t, buffer.records[index].ReceiptBytes, len(receiptBody))
	}
}

// TestErc20ReceiptRecordsTheConfiguredGas pins an ERC20 transfer's receipt to Erc20GasPerTransaction,
// the gas the block's totals and gigasim_gas_used_total count, with CumulativeGasUsed their running sum.
func TestErc20ReceiptRecordsTheConfiguredGas(t *testing.T) {
	t.Parallel()

	config := DefaultGigasimConfig()
	config.Erc20GasPerTransaction = 43_210
	const count = 3
	buffer := newReceiptBuffer(count, uint64(config.gasUsedBy(1)))
	rand := crand.NewCannedRandom(1<<20, 1337)

	for index := range count {
		buffer.build(index, rand, 3)

		built := buffer.records[index].Receipt
		require.Equal(t, uint64(43_210), built.GasUsed)
		require.Equal(t, uint64(43_210*(index+1)), built.CumulativeGasUsed)
		require.Len(t, buffer.records[index].ReceiptBytes, len(receiptBody))
	}
}

// TestReceiptDrawsStayInTheBlockSequence pins the draws a receipt takes from the block's random
// sequence: two for a native transfer, three for an ERC20 transfer. Building the body takes none.
func TestReceiptDrawsStayInTheBlockSequence(t *testing.T) {
	t.Parallel()

	for _, kind := range []transactionKind{nativeTransfer, erc20Transfer} {
		got := crand.NewCannedRandom(1<<20, 1337)
		drawReceiptInputs(got, kind)
		newReceiptBuffer(1, 1).build(0, got, 1)

		want := crand.NewCannedRandom(1<<20, 1337)
		_ = want.Int64Range(0, 5)
		_ = want.Int64Range(0, receiptGasPriceSpan)
		if kind != nativeTransfer {
			_ = want.Int64Range(0, receiptTransferSpan)
		}
		require.Equal(t, want.Int64(), got.Int64())
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
