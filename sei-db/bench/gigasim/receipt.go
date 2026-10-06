package gigasim

import (
	"bytes"
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

// The ranges the synthetic transaction type, gas price, and transfer amount are drawn from.
// drawReceiptInputs takes one draw from each range that applies to the transaction kind.
const (
	receiptTxTypeSpan   int64 = 5
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

// receiptBloom is the logs bloom every receipt carries.
var receiptBloom = bytes.Repeat([]byte{0xa5}, ethtypes.BloomByteLength)

// sizingLogData is a 32-byte amount, so a sized Transfer log includes one.
var sizingLogData = make([]byte, hashLen)

// receiptDraw is the transaction type, gas price, and token amount one receipt records.
type receiptDraw struct {
	txType   uint32
	gasPrice uint64
	amount   uint64
}

// maxReceiptSize is the most bytes a receipt in a block of count transactions encodes to. The block
// number and the transaction index are taken at their widest varint, so a later block still fits.
func maxReceiptSize(gasPerTransaction uint64, count int) int {
	cumulative := uint64(count) * gasPerTransaction //nolint:gosec // count is a non-negative transaction count
	if count > 0 && gasPerTransaction > 0 && cumulative/gasPerTransaction != uint64(count) {
		cumulative = math.MaxUint64
	}
	return (&evmtypes.Receipt{
		TxType:            uint32(receiptTxTypeSpan - 1),
		TxHashHex:         txHashHexWidth,
		GasUsed:           gasPerTransaction,
		EffectiveGasPrice: uint64(receiptGasPriceSpan - 1),
		CumulativeGasUsed: cumulative,
		BlockNumber:       math.MaxUint64,
		TransactionIndex:  math.MaxUint32,
		Status:            uint32(ethtypes.ReceiptStatusSuccessful),
		From:              addressHexWidth,
		To:                addressHexWidth,
		ContractAddress:   addressHexWidth,
		LogsBloom:         receiptBloom,
		Logs:              []*evmtypes.Log{sizingTransferLog()},
	}).Size()
}

// sizingTransferLog is a Transfer log of the width every ERC20 receipt encodes, so the buffer fits one.
func sizingTransferLog() *evmtypes.Log {
	return &evmtypes.Log{
		Address: addressHexWidth,
		Topics:  []string{transferEventTopic, txHashHexWidth, txHashHexWidth},
		Data:    sizingLogData,
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

// drawReceiptInputs draws the transaction type, gas price, and, for an ERC20 transfer, the token amount
// that the receipt records.
func drawReceiptInputs(rand *crand.CannedRandom, kind transactionKind) receiptDraw {
	drawn := receiptDraw{
		txType:   uint32(rand.Int64Range(0, receiptTxTypeSpan)),   //nolint:gosec // G115 - the range is [0, 5)
		gasPrice: uint64(rand.Int64Range(0, receiptGasPriceSpan)), //nolint:gosec // G115 - the range is non-negative
	}
	if kind != nativeTransfer {
		drawn.amount = uint64(rand.Int64Range(0, receiptTransferSpan)) //nolint:gosec // G115 - the range is non-negative
	}
	return drawn
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
	amounts []byte

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
		amounts:           make([]byte, count*hashLen),
		gasPerTransaction: gasPerTransaction,
	}
}

// build records the receipt for txn at index and encodes that receipt as its body. It records txn.drawn,
// the sender, and the recipient or the contract. An ERC20 transfer carries one Transfer log; a native
// transfer carries none. It does not draw from rand.
func (b *receiptBuffer) build(index int, rand *crand.CannedRandom, txn *transaction, blockNumber int64) error {
	var txHash [hashLen]byte
	writeSyntheticTxHash(txHash[:], rand, blockNumber, index)

	built := &b.storage[index]
	//nolint:gosec // G115 - benchmark values are bounded well below the conversion limits
	*built = evmtypes.Receipt{
		TxType:            txn.drawn.txType,
		TxHashHex:         txHashHex(txHash[:]),
		GasUsed:           b.gasPerTransaction,
		EffectiveGasPrice: txn.drawn.gasPrice,
		CumulativeGasUsed: uint64(index+1) * b.gasPerTransaction,
		BlockNumber:       uint64(blockNumber),
		TransactionIndex:  uint32(index),
		Status:            uint32(ethtypes.ReceiptStatusSuccessful),
		From:              addressHex(addressFromKey(txn.srcAccount)),
		LogsBloom:         receiptBloom,
	}
	if txn.kind == nativeTransfer {
		built.To = addressHex(addressFromKey(txn.dstAccount))
	} else {
		contract := addressHex(addressFromKey(txn.erc20Contract))
		built.To = contract
		built.ContractAddress = contract
		built.Logs = b.transferLog(index, txn, contract, txn.drawn.amount)
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

// transferLog is txn's ERC20 Transfer log: contract as the emitter, the sender and recipient as indexed
// topics, and amount as the 32-byte data.
func (b *receiptBuffer) transferLog(index int, txn *transaction, contract string, amount uint64) []*evmtypes.Log {
	sender := indexedAddressTopic(addressFromKey(txn.srcAccount))
	recipient := indexedAddressTopic(addressFromKey(txn.dstAccount))
	topics := b.topics[index*topicsPerTransferLog : (index+1)*topicsPerTransferLog]
	topics[0] = transferEventTopic
	topics[1] = txHashHex(sender[:])
	topics[2] = txHashHex(recipient[:])
	data := b.amounts[index*hashLen : (index+1)*hashLen]
	binary.BigEndian.PutUint64(data[hashLen-8:], amount)

	b.logs[index] = evmtypes.Log{
		Address: contract,
		Topics:  topics,
		Data:    data,
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
