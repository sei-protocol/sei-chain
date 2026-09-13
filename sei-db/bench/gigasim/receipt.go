package gigasim

import (
	"encoding/binary"
	"encoding/hex"

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

// erc20TransferEventSignatureBytes is the Transfer event signature in the form the bloom filter hashes.
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

// txHashPositionOffset is where the position is written inside a synthetic transaction hash. It sits
// past the leading bytes so that consecutive transactions do not produce adjacent hashes.
const txHashPositionOffset = 8

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

// topicsPerTransferLog is the Transfer event signature plus its two indexed address topics.
const topicsPerTransferLog = 3

// bloomBits are the three bit positions a value contributes to a log bloom.
type bloomBits [3]uint

// bloomBitsFor derives the three bits a value sets in a log bloom.
//
// A real bloom takes them from the value's keccak digest. This one mixes the bytes instead, which
// is not a bloom any filter could match against. Nothing in the benchmark reads a log back, and the
// store is measured on what a receipt occupies rather than on what its bloom would answer, so the
// properties kept are the ones that reach the store: the same three bits per value, spread over the
// same 2048 positions, and the same bits for the same value on a rerun of the same seed.
//
// Keccak over four values per transaction was most of the cost of building a block's receipts.
func bloomBitsFor(value []byte) bloomBits {
	// FNV-1a, for a spread across the bloom's positions that costs a multiply per byte.
	const (
		fnvOffset uint64 = 14695981039346656037
		fnvPrime  uint64 = 1099511628211
	)
	mixed := fnvOffset
	for _, b := range value {
		mixed ^= uint64(b)
		mixed *= fnvPrime
	}
	var bits bloomBits
	for i := range bits {
		bits[i] = uint(mixed & 2047)
		mixed >>= 11
	}
	return bits
}

// receiptCache holds what a receipt repeats rather than derives anew: the constant event
// signature's bloom bits, and the values that follow from a contract address. The contract pool is
// fixed, so it is worth keeping across the blocks a run produces.
//
// It is not safe for concurrent use; only the generator builds receipts.
type receiptCache struct {
	signature bloomBits
	contracts map[[keys.AddressLen]byte]contractFields
}

// contractFields are the per-contract values a receipt repeats and none of its transactions change.
type contractFields struct {
	bits bloomBits
	hex  string
}

// newReceiptCache returns a cache with the constant inputs already resolved.
func newReceiptCache() *receiptCache {
	return &receiptCache{
		signature: bloomBitsFor(erc20TransferEventSignatureBytes[:]),
		contracts: make(map[[keys.AddressLen]byte]contractFields),
	}
}

// contract returns an ERC20 contract's bloom bits and hex address, resolving one it has not seen.
func (c *receiptCache) contract(address []byte) contractFields {
	var key [keys.AddressLen]byte
	copy(key[:], address)
	if fields, ok := c.contracts[key]; ok {
		return fields
	}
	fields := contractFields{bits: bloomBitsFor(address), hex: bytesToHex(address)}
	c.contracts[key] = fields
	return fields
}

// setBits marks bits in a bloom.
func setBits(bloom *ethtypes.Bloom, bits bloomBits) {
	for _, bit := range bits {
		bloom[ethtypes.BloomByteLength-1-bit/8] |= byte(1 << (bit % 8))
	}
}

// receiptBuffer holds one block's receipts in a fixed number of allocations: every array a receipt
// points into is carved out of a slice the buffer owns.
type receiptBuffer struct {
	receipts []*evmtypes.Receipt

	storage []evmtypes.Receipt
	logs    []evmtypes.Log
	logRefs []*evmtypes.Log
	topics  []string
	blooms  []ethtypes.Bloom
	data    []byte

	// What the generator has already resolved about the contract pool, which is worth keeping
	// across blocks rather than rebuilding per block.
	cache *receiptCache
}

// newReceiptBuffer allocates the backing storage for one block of receipts.
func newReceiptBuffer(count int, cache *receiptCache) *receiptBuffer {
	return &receiptBuffer{
		receipts: make([]*evmtypes.Receipt, count),
		storage:  make([]evmtypes.Receipt, count),
		logs:     make([]evmtypes.Log, count),
		logRefs:  make([]*evmtypes.Log, count),
		topics:   make([]string, count*topicsPerTransferLog),
		blooms:   make([]ethtypes.Bloom, count),
		data:     make([]byte, count*hashLen),
		cache:    cache,
	}
}

// build fills in the receipt an ERC20 transfer would leave behind: one Transfer log with two indexed
// address topics, and a bloom covering them. The values are synthetic, since the receipt store is
// measured on the volume and shape of what it stores rather than on the arithmetic behind it.
func (b *receiptBuffer) build(index int, rand *crand.CannedRandom, txn *transaction, blockNumber int64) {
	contract := b.cache.contract(addressFromKey(txn.erc20Contract))
	senderTopic := indexedAddressTopic(addressFromKey(txn.srcAccount))
	receiverTopic := indexedAddressTopic(addressFromKey(txn.dstAccount))

	txType := uint32(ethtypes.DynamicFeeTxType)
	if rand.Int64Range(0, 5) == 0 {
		txType = uint32(ethtypes.LegacyTxType)
	}
	gasUsed := receiptGasUsedBase + rand.Int64Range(0, receiptGasUsedSpan)
	previousGas := receiptPreviousGasBase + rand.Int64Range(0, receiptPreviousGasSpan)
	effectiveGasPrice := receiptGasPriceBase + rand.Int64Range(0, receiptGasPriceSpan)
	transferAmount := receiptTransferBase + rand.Int64Range(0, receiptTransferSpan)

	bloom := &b.blooms[index]
	*bloom = ethtypes.Bloom{}
	b.addTransferLogToBloom(bloom, contract.bits, senderTopic[:], receiverTopic[:])

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
		Address: contract.hex,
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
		ContractAddress:   contract.hex,
		TxHashHex:         bytesToHex(txHash[:]),
		GasUsed:           uint64(gasUsed),
		EffectiveGasPrice: uint64(effectiveGasPrice),
		BlockNumber:       uint64(blockNumber),
		TransactionIndex:  uint32(index),
		Status:            uint32(ethtypes.ReceiptStatusSuccessful),
		From:              bytesToHex(addressFromKey(txn.srcAccount)),
		To:                contract.hex,
		Logs:              b.logRefs[index : index+1],
		LogsBloom:         bloom[:],
	}
}

// addTransferLogToBloom sets the bits a Transfer log contributes: the emitting contract, the event
// signature and both indexed topics.
func (b *receiptBuffer) addTransferLogToBloom(
	bloom *ethtypes.Bloom,
	contractBits bloomBits, senderTopic, receiverTopic []byte,
) {
	setBits(bloom, contractBits)
	setBits(bloom, b.cache.signature)
	setBits(bloom, bloomBitsFor(senderTopic))
	setBits(bloom, bloomBitsFor(receiverTopic))
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

// bytesToHex renders bytes as the 0x-prefixed lowercase hex the receipt fields hold. It accepts at most
// hashLen bytes, which is the widest field a receipt carries.
func bytesToHex(b []byte) string {
	var buf [len("0x") + 2*hashLen]byte
	buf[0], buf[1] = '0', 'x'
	encoded := hex.Encode(buf[2:], b)
	return string(buf[:2+encoded])
}
