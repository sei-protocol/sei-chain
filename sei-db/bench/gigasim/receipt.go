package gigasim

import (
	"encoding/binary"
	"encoding/hex"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	crand "github.com/sei-protocol/sei-chain/sei-db/common/rand"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

const hashLen = 32

// transferEventTopic is the ERC20 Transfer event signature, the first topic of receiptLog.
const transferEventTopic = "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"

// The ranges the synthetic gas price and transfer amount are drawn from. drawReceiptInputs consumes
// one draw from each range that applies to the transaction kind.
const (
	receiptGasPriceSpan int64 = 9_000_000_000
	receiptTransferSpan int64 = 10_000_000_000
)

// txIDBlockStride separates one block's transaction identifiers from the next block's, so that a
// synthetic transaction hash is unique across the run. It caps a block at a million transactions.
const txIDBlockStride int64 = 1_000_000

// txHashPositionOffset is where the position is written inside a synthetic transaction hash. It sits
// past the leading bytes so that consecutive transactions do not produce adjacent hashes.
const txHashPositionOffset = 8

// receiptLog is the Transfer log every receipt carries: the event signature and two indexed
// addresses. The receipt store writes an index key for the address and for each topic.
var receiptLog = canonicalReceiptLog()

// receiptBody is the body stored for every receipt, the encoding of one ERC20 transfer receipt.
var receiptBody = canonicalReceiptBody()

// canonicalReceiptLog returns the Transfer log shared by every receipt.
func canonicalReceiptLog() []*evmtypes.Log {
	var addr [keys.AddressLen]byte
	sender := indexedAddress(1)
	receiver := indexedAddress(2)
	amount := make([]byte, hashLen)
	binary.BigEndian.PutUint64(amount[hashLen-8:], 1_000_000)

	return []*evmtypes.Log{{
		Address: "0x" + hex.EncodeToString(addr[:]),
		Topics: []string{
			transferEventTopic,
			"0x" + hex.EncodeToString(sender[:]),
			"0x" + hex.EncodeToString(receiver[:]),
		},
		Data: amount,
	}}
}

// indexedAddress is a 32-byte topic holding mark in its last byte.
func indexedAddress(mark byte) [hashLen]byte {
	var topic [hashLen]byte
	topic[hashLen-1] = mark
	return topic
}

// canonicalReceiptBody encodes one ERC20 transfer receipt. Every stored receipt is this many bytes.
func canonicalReceiptBody() []byte {
	addrHex := receiptLog[0].Address
	built := &evmtypes.Receipt{
		TxType:            uint32(ethtypes.DynamicFeeTxType),
		TxHashHex:         "0x" + hex.EncodeToString(make([]byte, hashLen)),
		GasUsed:           21_000,
		CumulativeGasUsed: 21_000,
		EffectiveGasPrice: 1_000_000_000,
		BlockNumber:       1,
		Status:            uint32(ethtypes.ReceiptStatusSuccessful),
		From:              addrHex,
		To:                addrHex,
		ContractAddress:   addrHex,
		LogsBloom:         make([]byte, ethtypes.BloomByteLength),
		Logs:              receiptLog,
	}
	encoded, err := built.Marshal()
	if err != nil {
		panic(err)
	}
	return encoded
}

// writeSyntheticTxHash writes into dst, which must be hashLen bytes, the transaction hash for a position
// in the chain. The hash is unique across the run and depends only on the seed and the position, so an
// identically seeded buffer reproduces it from the block number and transaction index alone.
func writeSyntheticTxHash(dst []byte, rand *crand.CannedRandom, blockNumber int64, txIndex int) {
	position := blockNumber*txIDBlockStride + int64(txIndex)
	copy(dst, rand.SeededBytes(hashLen, position))
	// The position is embedded rather than left to seed the draw alone: two positions can draw from the
	// same buffer offset, and the receipt store rejects a block carrying one transaction hash twice.
	//nolint:gosec // G115 - a position is non-negative, and wrapping would still be injective
	binary.BigEndian.PutUint64(dst[txHashPositionOffset:], uint64(position))
}

// drawReceiptInputs consumes the random draws that sit between one transaction and the next in the
// block's sequence.
func drawReceiptInputs(rand *crand.CannedRandom, kind transactionKind) {
	_ = rand.Int64Range(0, 5)
	_ = rand.Int64Range(0, receiptGasPriceSpan)
	if kind != nativeTransfer {
		_ = rand.Int64Range(0, receiptTransferSpan)
	}
}

// receiptBuffer holds one block's receipts. Each record's body is a copy of receiptBody.
type receiptBuffer struct {
	records []receipt.ReceiptRecord

	// The total size of the bodies recorded so far.
	encodedBytes int64

	storage []evmtypes.Receipt
	bodies  []byte

	// The gas every receipt in the block records, the same figure the block's gas totals and
	// gigasim_gas_used_total count.
	gasPerTransaction uint64
}

// newReceiptBuffer allocates the backing storage for one block of receipts, each recording
// gasPerTransaction gas.
func newReceiptBuffer(count int, gasPerTransaction uint64) *receiptBuffer {
	return &receiptBuffer{
		records:           make([]receipt.ReceiptRecord, count),
		storage:           make([]evmtypes.Receipt, count),
		bodies:            make([]byte, count*len(receiptBody)),
		gasPerTransaction: gasPerTransaction,
	}
}

// build records the receipt for the transaction at index. The body is a copy of receiptBody with the
// transaction hash written over its last hashLen bytes, and the gas is gasPerTransaction. It does not
// draw from rand.
func (b *receiptBuffer) build(index int, rand *crand.CannedRandom, blockNumber int64) {
	var txHash [hashLen]byte
	writeSyntheticTxHash(txHash[:], rand, blockNumber, index)

	start := index * len(receiptBody)
	body := b.bodies[start : start+len(receiptBody)]
	copy(body, receiptBody)
	copy(body[len(body)-hashLen:], txHash[:])

	built := &b.storage[index]
	//nolint:gosec // G115 - benchmark values are bounded well below the conversion limits
	*built = evmtypes.Receipt{
		GasUsed:           b.gasPerTransaction,
		CumulativeGasUsed: uint64(index+1) * b.gasPerTransaction,
		BlockNumber:       uint64(blockNumber),
		TransactionIndex:  uint32(index),
		Status:            uint32(ethtypes.ReceiptStatusSuccessful),
		Logs:              receiptLog,
	}
	b.encodedBytes += int64(len(body))
	b.records[index] = receipt.ReceiptRecord{
		TxHash:       common.BytesToHash(txHash[:]),
		Receipt:      built,
		ReceiptBytes: body,
	}
}

// addressFromKey takes the address out of an EVM key, which carries it after a one-byte prefix. A
// storage key holds a slot after the address, which is dropped.
func addressFromKey(key []byte) []byte {
	return key[1 : 1+keys.AddressLen]
}
