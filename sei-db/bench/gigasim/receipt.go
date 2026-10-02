package gigasim

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	crand "github.com/sei-protocol/sei-chain/sei-db/common/rand"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

const hashLen = 32

// txHashHexLen is the width of a transaction hash in the 0x-prefixed hex a receipt stores.
const txHashHexLen = 2 + 2*hashLen

// transferEventTopic is the ERC20 Transfer event signature, the first topic of a Transfer log.
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

// topicsPerTransferLog is the Transfer event signature plus its two indexed address topics.
const topicsPerTransferLog = 3

// addressHexLen is the width of an address in the 0x-prefixed hex a log stores.
const addressHexLen = 2 + 2*keys.AddressLen

// txHashHexWidth is a hash of txHashHexLen bytes, so a sized receipt includes a transaction hash.
var txHashHexWidth = "0x" + hex.EncodeToString(make([]byte, hashLen))

// addressHexWidth is an address of addressHexLen bytes, so a sized Transfer log includes one.
var addressHexWidth = "0x" + hex.EncodeToString(make([]byte, keys.AddressLen))

// transferLogData is the 32-byte amount a Transfer log carries.
var transferLogData = make([]byte, hashLen)

// maxReceiptSize is the most bytes a receipt in a block of count transactions encodes to. The block
// number and the transaction index are taken at their widest varint, so a later block still fits.
func maxReceiptSize(gasPerTransaction uint64, count int) int {
	cumulative := uint64(count) * gasPerTransaction //nolint:gosec // count is a non-negative transaction count
	if count > 0 && gasPerTransaction > 0 && cumulative/gasPerTransaction != uint64(count) {
		cumulative = math.MaxUint64
	}
	return (&evmtypes.Receipt{
		TxHashHex:         txHashHexWidth,
		GasUsed:           gasPerTransaction,
		CumulativeGasUsed: cumulative,
		BlockNumber:       math.MaxUint64,
		TransactionIndex:  math.MaxUint32,
		Status:            uint32(ethtypes.ReceiptStatusSuccessful),
		Logs:              []*evmtypes.Log{sizingTransferLog()},
	}).Size()
}

// sizingTransferLog is a Transfer log of the width every ERC20 receipt encodes, so the buffer fits one.
func sizingTransferLog() *evmtypes.Log {
	return &evmtypes.Log{
		Address: addressHexWidth,
		Topics:  []string{transferEventTopic, txHashHexWidth, txHashHexWidth},
		Data:    transferLogData,
	}
}

// txHashHex renders a transaction hash as the 0x-prefixed hex a receipt stores.
func txHashHex(hash []byte) string {
	var buf [txHashHexLen]byte
	buf[0], buf[1] = '0', 'x'
	hex.Encode(buf[2:], hash)
	return string(buf[:])
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

// receiptBuffer holds one block's receipts. Each record's body is the encoding of its own receipt.
type receiptBuffer struct {
	records []receipt.ReceiptRecord

	// The total size of the bodies recorded so far.
	encodedBytes int64

	storage []evmtypes.Receipt
	bodies  []byte
	stride  int
	logs    []evmtypes.Log
	logRefs []*evmtypes.Log
	topics  []string

	// The gas every receipt in the block records, the same figure the block's gas totals and
	// gigasim_gas_used_total count.
	gasPerTransaction uint64
}

// newReceiptBuffer allocates the backing storage for one block of receipts, each recording
// gasPerTransaction gas.
func newReceiptBuffer(count int, gasPerTransaction uint64) *receiptBuffer {
	stride := maxReceiptSize(gasPerTransaction, count)
	return &receiptBuffer{
		records:           make([]receipt.ReceiptRecord, count),
		storage:           make([]evmtypes.Receipt, count),
		bodies:            make([]byte, count*stride),
		stride:            stride,
		logs:              make([]evmtypes.Log, count),
		logRefs:           make([]*evmtypes.Log, count),
		topics:            make([]string, count*topicsPerTransferLog),
		gasPerTransaction: gasPerTransaction,
	}
}

// build records the receipt for txn at index and encodes that receipt as its body. An ERC20 transfer
// carries one Transfer log naming the contract, the sender and the recipient. A native transfer
// carries no log. It does not draw from rand.
func (b *receiptBuffer) build(index int, rand *crand.CannedRandom, txn *transaction, blockNumber int64) error {
	var txHash [hashLen]byte
	writeSyntheticTxHash(txHash[:], rand, blockNumber, index)

	built := &b.storage[index]
	//nolint:gosec // G115 - benchmark values are bounded well below the conversion limits
	*built = evmtypes.Receipt{
		TxHashHex:         txHashHex(txHash[:]),
		GasUsed:           b.gasPerTransaction,
		CumulativeGasUsed: uint64(index+1) * b.gasPerTransaction,
		BlockNumber:       uint64(blockNumber),
		TransactionIndex:  uint32(index),
		Status:            uint32(ethtypes.ReceiptStatusSuccessful),
	}
	if txn.kind != nativeTransfer {
		built.Logs = b.transferLog(index, txn)
	}
	window := b.bodies[index*b.stride : (index+1)*b.stride]
	n, err := built.MarshalToSizedBuffer(window)
	if err != nil {
		return fmt.Errorf("failed to encode the receipt for transaction %d of block %d: %w",
			index, blockNumber, err)
	}
	body := window[len(window)-n:]
	b.encodedBytes += int64(len(body))
	b.records[index] = receipt.ReceiptRecord{
		TxHash:       common.BytesToHash(txHash[:]),
		Receipt:      built,
		ReceiptBytes: body,
	}
	return nil
}

// transferLog is txn's ERC20 Transfer log: the contract as the emitter, and the sender and recipient
// as indexed topics.
func (b *receiptBuffer) transferLog(index int, txn *transaction) []*evmtypes.Log {
	sender := indexedAddressTopic(addressFromKey(txn.srcAccount))
	recipient := indexedAddressTopic(addressFromKey(txn.dstAccount))
	topics := b.topics[index*topicsPerTransferLog : (index+1)*topicsPerTransferLog]
	topics[0] = transferEventTopic
	topics[1] = txHashHex(sender[:])
	topics[2] = txHashHex(recipient[:])

	b.logs[index] = evmtypes.Log{
		Address: addressHex(addressFromKey(txn.erc20Contract)),
		Topics:  topics,
		Data:    transferLogData,
	}
	b.logRefs[index] = &b.logs[index]
	return b.logRefs[index : index+1]
}

// indexedAddressTopic right-aligns an address in a log topic, which is how an indexed address is encoded.
func indexedAddressTopic(address []byte) [hashLen]byte {
	var topic [hashLen]byte
	copy(topic[hashLen-len(address):], address)
	return topic
}

// addressHex renders an address as the 0x-prefixed hex a log stores.
func addressHex(address []byte) string {
	var buf [addressHexLen]byte
	buf[0], buf[1] = '0', 'x'
	hex.Encode(buf[2:], address)
	return string(buf[:])
}

// addressFromKey takes the address out of an EVM key, which carries it after a one-byte prefix. A
// storage key holds a slot after the address, which is dropped.
func addressFromKey(key []byte) []byte {
	return key[1 : 1+keys.AddressLen]
}
