package gigasim

import (
	"encoding/binary"
	"encoding/hex"
	"hash"

	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	crand "github.com/sei-protocol/sei-chain/sei-db/common/rand"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

// The ERC20 Transfer event signature, and the digest width the log topics are padded to.
const (
	erc20TransferEventSignatureHex = "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"
	hashLen                        = 32

	// Where a 20-byte address starts inside a 32-byte indexed log topic.
	indexedAddressBase = hashLen - keys.AddressLen
)

var erc20TransferEventSignatureBytes = [hashLen]byte{
	0xdd, 0xf2, 0x52, 0xad, 0x1b, 0xe2, 0xc8, 0x9b,
	0x69, 0xc2, 0xb0, 0x68, 0xfc, 0x37, 0x8d, 0xaa,
	0x95, 0x2b, 0xa7, 0xf1, 0x63, 0xc4, 0xa1, 0x16,
	0x28, 0xf5, 0x5a, 0x4d, 0xf5, 0x23, 0xb3, 0xef,
}

// The ranges the synthetic gas and transfer values are drawn from, chosen so a receipt's numeric
// fields occupy as many bytes as a real transfer's would.
const (
	receiptGasUsedBase     int64 = 52_000
	receiptGasUsedSpan     int64 = 18_000
	receiptPreviousGasBase int64 = 21_000
	receiptPreviousGasSpan int64 = 35_000
	receiptGasPriceBase    int64 = 1_000_000_000
	receiptGasPriceSpan    int64 = 9_000_000_000
	receiptTransferBase    int64 = 1_000_000
	receiptTransferSpan    int64 = 10_000_000_000
)

// txIDBlockStride separates one block's transaction identifiers from the next block's, so that a
// synthetic transaction hash is unique across the run. It caps a block at a million transactions.
const txIDBlockStride int64 = 1_000_000

// Where the position is written inside a synthetic transaction hash. It sits past the leading bytes so
// that consecutive transactions do not produce lexicographically adjacent hashes.
const txHashPositionOffset = 8

// writeSyntheticTxHash writes the deterministic transaction hash for a position in the chain into dst,
// which must be hashLen bytes.
//
// The position is written into the hash rather than only seeding it. Seeding alone draws from the
// canned random buffer at a hashed offset, which two positions can share: at a block's worth of draws
// that is a birthday collision every few dozen blocks, and the receipt store rejects a block that
// carries one transaction hash twice.
//
// The hash still depends only on the seed and the position, so any holder of an identically seeded
// buffer can recompute it from the block number and transaction index alone.
func writeSyntheticTxHash(dst []byte, rand *crand.CannedRandom, blockNumber int64, txIndex int) {
	position := blockNumber*txIDBlockStride + int64(txIndex)
	copy(dst, rand.SeededBytes(hashLen, position))
	//nolint:gosec // G115 - a position is non-negative, and wrapping would still be injective
	binary.BigEndian.PutUint64(dst[txHashPositionOffset:], uint64(position))
}

// topicsPerTransferLog is the Transfer event signature plus its two indexed address topics.
const topicsPerTransferLog = 3

// receiptBuffer holds a block's receipts in a fixed number of allocations rather than a dozen per
// transaction. Every array a receipt points into is carved out of one slice, so the generator's cost
// per block is the data it writes rather than the objects it leaves for the collector.
type receiptBuffer struct {
	receipts []*evmtypes.Receipt

	storage []evmtypes.Receipt
	logs    []evmtypes.Log
	logRefs []*evmtypes.Log
	topics  []string
	blooms  []ethtypes.Bloom
	data    []byte

	// The bloom hasher, which belongs to the generator rather than to any one block: it is reset
	// before each use, and building one per receipt costs more than the hashing does.
	hasher hash.Hash
}

// newReceiptBuffer allocates the backing storage for one block of receipts.
func newReceiptBuffer(count int, hasher hash.Hash) *receiptBuffer {
	return &receiptBuffer{
		receipts: make([]*evmtypes.Receipt, count),
		storage:  make([]evmtypes.Receipt, count),
		logs:     make([]evmtypes.Log, count),
		logRefs:  make([]*evmtypes.Log, count),
		topics:   make([]string, count*topicsPerTransferLog),
		blooms:   make([]ethtypes.Bloom, count),
		data:     make([]byte, count*hashLen),
		hasher:   hasher,
	}
}

// build fills in the receipt an ERC20 transfer would leave behind, sized and shaped like a real one:
// one Transfer log with two indexed address topics, and a bloom covering them.
//
// The values are synthetic rather than derived from the transfer, because what the receipt store is
// measured on is the volume and shape of what it stores.
func (b *receiptBuffer) build(index int, rand *crand.CannedRandom, txn *transaction, blockNumber int64) {
	contractAddress := addressFromKey(txn.erc20Contract)
	senderTopic := indexedAddressTopic(addressFromKey(txn.srcAccountSlot))
	receiverTopic := indexedAddressTopic(addressFromKey(txn.dstAccountSlot))

	txType := uint32(ethtypes.DynamicFeeTxType)
	if rand.Int64Range(0, 5) == 0 {
		txType = uint32(ethtypes.LegacyTxType)
	}
	gasUsed := receiptGasUsedBase + rand.Int64Range(0, receiptGasUsedSpan)
	previousGas := receiptPreviousGasBase + rand.Int64Range(0, receiptPreviousGasSpan)
	effectiveGasPrice := receiptGasPriceBase + rand.Int64Range(0, receiptGasPriceSpan)
	transferAmount := receiptTransferBase + rand.Int64Range(0, receiptTransferSpan)

	contractAddressHex := bytesToHex(contractAddress)

	bloom := &b.blooms[index]
	*bloom = ethtypes.Bloom{}
	b.addTransferLogToBloom(bloom, contractAddress, senderTopic[:], receiverTopic[:])

	topics := b.topics[index*topicsPerTransferLog : (index+1)*topicsPerTransferLog]
	topics[0] = erc20TransferEventSignatureHex
	topics[1] = bytesToHex(senderTopic[:])
	topics[2] = bytesToHex(receiverTopic[:])

	amount := b.data[index*hashLen : (index+1)*hashLen]
	//nolint:gosec // G115 - benchmark values are bounded well below the conversion limits
	binary.BigEndian.PutUint64(amount[hashLen-8:], uint64(transferAmount))

	log := &b.logs[index]
	b.logRefs[index] = log
	*log = evmtypes.Log{
		Address: contractAddressHex,
		Topics:  topics,
		Data:    amount,
		Index:   0,
	}

	var txHash [hashLen]byte
	writeSyntheticTxHash(txHash[:], rand, blockNumber, index)

	receipt := &b.storage[index]
	b.receipts[index] = receipt
	//nolint:gosec // G115 - benchmark values are bounded well below the conversion limits
	*receipt = evmtypes.Receipt{
		TxType:            txType,
		CumulativeGasUsed: uint64(gasUsed + int64(index)*previousGas),
		ContractAddress:   contractAddressHex,
		TxHashHex:         bytesToHex(txHash[:]),
		GasUsed:           uint64(gasUsed),
		EffectiveGasPrice: uint64(effectiveGasPrice),
		BlockNumber:       uint64(blockNumber),
		TransactionIndex:  uint32(index),
		Status:            uint32(ethtypes.ReceiptStatusSuccessful),
		From:              bytesToHex(addressFromKey(txn.srcAccount)),
		To:                contractAddressHex,
		Logs:              b.logRefs[index : index+1],
		LogsBloom:         bloom[:],
	}
}

// addTransferLogToBloom sets the bits a Transfer log contributes: the emitting contract, the event
// signature and both indexed topics.
func (b *receiptBuffer) addTransferLogToBloom(
	bloom *ethtypes.Bloom,
	contractAddress, senderTopic, receiverTopic []byte,
) {
	var digest [hashLen]byte
	for _, value := range [4][]byte{
		contractAddress,
		erc20TransferEventSignatureBytes[:],
		senderTopic,
		receiverTopic,
	} {
		addToBloom(b.hasher, &digest, bloom, value)
	}
}

// addToBloom sets the three bits a value contributes to a bloom filter.
func addToBloom(hasher hash.Hash, digest *[hashLen]byte, bloom *ethtypes.Bloom, value []byte) {
	hasher.Reset()
	_, _ = hasher.Write(value)
	sum := hasher.Sum(digest[:0])
	for i := 0; i < 6; i += 2 {
		bit := (uint(sum[i])<<8)&2047 + uint(sum[i+1])
		bloom[ethtypes.BloomByteLength-1-bit/8] |= byte(1 << (bit % 8))
	}
}

// addressFromKey takes the address out of an EVM key, which carries it after a one-byte prefix. A
// storage key holds a slot after the address, which is dropped.
func addressFromKey(key []byte) []byte {
	return key[1 : 1+keys.AddressLen]
}

// indexedAddressTopic right-aligns an address in a log topic, which is how an indexed address is
// encoded.
func indexedAddressTopic(address []byte) [hashLen]byte {
	var topic [hashLen]byte
	copy(topic[indexedAddressBase:], address)
	return topic
}

// bytesToHex renders bytes as the 0x-prefixed lowercase hex the receipt fields hold. It accepts at
// most hashLen bytes, which is the widest field a receipt carries.
//
// Encoding through a stack buffer costs one allocation, the returned string. Encoding to a string and
// prefixing it costs three, on five fields of every receipt.
func bytesToHex(b []byte) string {
	var buf [len("0x") + 2*hashLen]byte
	buf[0], buf[1] = '0', 'x'
	encoded := hex.Encode(buf[2:], b)
	return string(buf[:2+encoded])
}
