package gigasim

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
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
	txn := testErc20Transfer(1, 2, 9)

	for index := range count {
		require.NoError(t, buffer.build(index, rand, txn, 3))

		record := buffer.records[index]
		require.NotNil(t, record.Receipt)
		require.Len(t, record.Receipt.Logs, 1)
		require.Len(t, record.Receipt.Logs[0].Topics, 3)
		encoded, err := record.Receipt.Marshal()
		require.NoError(t, err)
		require.Equal(t, encoded, record.ReceiptBytes, "the stored body must be the receipt it is stored with")

		var decoded evmtypes.Receipt
		require.NoError(t, decoded.Unmarshal(record.ReceiptBytes))
		require.Equal(t, uint64(3), decoded.BlockNumber)
		require.Equal(t, uint32(index), decoded.TransactionIndex)
		require.Equal(t, uint64(50_000), decoded.GasUsed)
		require.Equal(t, uint64(50_000*(index+1)), decoded.CumulativeGasUsed)
		require.Equal(t, record.TxHash, common.HexToHash(decoded.TxHashHex))
	}
}

// TestNativeTransferReceiptRecordsTheFixedGas pins a native transfer's receipt to the fixed 21,000 gas,
// with CumulativeGasUsed the running sum.
func TestNativeTransferReceiptRecordsTheFixedGas(t *testing.T) {
	t.Parallel()

	config := DefaultGigasimConfig()
	config.TransactionType = transactionTypeTransfer
	buffer := newReceiptBuffer(2, uint64(config.gasUsedBy(1)))
	rand := crand.NewCannedRandom(1<<20, 1337)
	txn := &transaction{kind: nativeTransfer, srcAccount: testAccountKey(1), dstAccount: testAccountKey(2)}

	for index := range 2 {
		require.NoError(t, buffer.build(index, rand, txn, 3))

		built := buffer.records[index].Receipt
		require.Equal(t, uint64(21_000), built.GasUsed)
		require.Equal(t, uint64(21_000*(index+1)), built.CumulativeGasUsed)
		require.Empty(t, built.Logs)
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
		txn := testErc20Transfer(index+1, index+100, index+1000)
		require.NoError(t, buffer.build(index, rand, txn, 3))

		built := buffer.records[index].Receipt
		require.Equal(t, uint64(43_210), built.GasUsed)
		require.Equal(t, uint64(43_210*(index+1)), built.CumulativeGasUsed)
		require.Len(t, built.Logs, 1)
		sender := indexedAddressTopic(addressFromKey(txn.srcAccount))
		recipient := indexedAddressTopic(addressFromKey(txn.dstAccount))
		require.Equal(t, addressHex(addressFromKey(txn.erc20Contract)), built.Logs[0].Address)
		require.Equal(t, transferEventTopic, built.Logs[0].Topics[0])
		require.Equal(t, txHashHex(sender[:]), built.Logs[0].Topics[1])
		require.Equal(t, txHashHex(recipient[:]), built.Logs[0].Topics[2])
	}
	require.NotEqual(t, buffer.records[0].Receipt.Logs[0].Address, buffer.records[1].Receipt.Logs[0].Address)
	require.NotEqual(t, buffer.records[0].Receipt.Logs[0].Topics[1], buffer.records[1].Receipt.Logs[0].Topics[1])
	require.NotEqual(t, buffer.records[0].Receipt.Logs[0].Topics[2], buffer.records[1].Receipt.Logs[0].Topics[2])
}

// testErc20Transfer is an ERC20 transfer between the numbered accounts of the numbered contract.
func testErc20Transfer(src, dst, contract int) *transaction {
	return &transaction{
		kind:          erc20Transfer,
		srcAccount:    testAccountKey(src),
		dstAccount:    testAccountKey(dst),
		erc20Contract: testAccountKey(contract),
	}
}

// TestReceiptDrawsStayInTheBlockSequence pins the draws a receipt takes from the block's random
// sequence: two for a native transfer, three for an ERC20 transfer. Building the body takes none.
func TestReceiptDrawsStayInTheBlockSequence(t *testing.T) {
	t.Parallel()

	for _, kind := range []transactionKind{nativeTransfer, erc20Transfer} {
		got := crand.NewCannedRandom(1<<20, 1337)
		drawReceiptInputs(got, kind)
		txn := &transaction{kind: kind}
		if kind != nativeTransfer {
			txn = testErc20Transfer(1, 2, 3)
		}
		require.NoError(t, newReceiptBuffer(1, 1).build(0, got, txn, 1))

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
