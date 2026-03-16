package cryptosim

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash"

	ethtypes "github.com/ethereum/go-ethereum/core/types"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"golang.org/x/crypto/sha3"
)

const (
	erc20TransferEventSignatureHex = "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"

	// These mirror immutable memiavl EVM key prefixes and are duplicated here to keep the hot path minimal.
	evmCodeKeyPrefixByte    = 0x07
	evmStorageKeyPrefixByte = 0x03

	hashLen            = 32
	indexedAddressBase = hashLen - AddressLen

	syntheticReceiptMinBlockNumber uint64 = 1_000_000

	syntheticReceiptGasUsedBase     uint64 = 52_000
	syntheticReceiptGasUsedSpan     uint64 = 18_000
	syntheticReceiptPreviousGasBase uint64 = 21_000
	syntheticReceiptPreviousGasSpan uint64 = 35_000
	syntheticReceiptGasPriceBase    uint64 = 1_000_000_000
	syntheticReceiptGasPriceSpan    uint64 = 9_000_000_000
	syntheticReceiptTransferBase    uint64 = 1_000_000
	syntheticReceiptTransferSpan    uint64 = 10_000_000_000
)

var erc20TransferEventSignatureBytes = [hashLen]byte{
	0xdd, 0xf2, 0x52, 0xad, 0x1b, 0xe2, 0xc8, 0x9b,
	0x69, 0xc2, 0xb0, 0x68, 0xfc, 0x37, 0x8d, 0xaa,
	0x95, 0x2b, 0xa7, 0xf1, 0x63, 0xc4, 0xa1, 0x16,
	0x28, 0xf5, 0x5a, 0x4d, 0xf5, 0x23, 0xb3, 0xef,
}

// BuildERC20TransferReceiptFromTxn produces a plausible successful ERC20 transfer receipt from a transaction.
func BuildERC20TransferReceiptFromTxn(
	crand *CannedRandom,
	feeCollectionAccount []byte,
	blockNumber uint64,
	txIndex uint32,
	txn *transaction,
) (*evmtypes.Receipt, error) {
	return BuildERC20TransferReceipt(
		crand,
		feeCollectionAccount,
		txn.srcAccount,
		txn.dstAccount,
		txn.srcAccountSlot,
		txn.dstAccountSlot,
		txn.erc20Contract,
		blockNumber,
		txIndex)
}

// BuildERC20TransferReceipt produces a plausible successful ERC20 transfer receipt.
//
// The sender and receiver are derived from the address portion of the supplied storage keys, since cryptosim tracks
// ERC20 balances as storage slots rather than separate account references. The caller supplies the block number and tx
// index so the resulting receipt can line up with the simulated block being benchmarked.
func BuildERC20TransferReceipt(
	crand *CannedRandom,
	feeCollectionAccount []byte,
	srcAccount []byte,
	dstAccount []byte,
	senderSlot []byte,
	receiverSlot []byte,
	erc20ContractCode []byte,
	blockNumber uint64,
	txIndex uint32,
) (*evmtypes.Receipt, error) {
	if crand == nil {
		return nil, errors.New("canned random is required")
	}

	if err := validateCodeKey("fee collection account", feeCollectionAccount); err != nil {
		return nil, err
	}
	srcAddressBytes, err := extractCodeKeyBytes("src account", srcAccount)
	if err != nil {
		return nil, err
	}
	if err := validateCodeKey("dst account", dstAccount); err != nil {
		return nil, err
	}
	senderAddressBytes, err := extractStorageKeyAddressBytes("sender slot", senderSlot)
	if err != nil {
		return nil, err
	}
	receiverAddressBytes, err := extractStorageKeyAddressBytes("receiver slot", receiverSlot)
	if err != nil {
		return nil, err
	}
	contractAddressBytes, err := extractCodeKeyBytes("erc20 contract code", erc20ContractCode)
	if err != nil {
		return nil, err
	}
	txType := uint32(ethtypes.DynamicFeeTxType)
	if crand.Int64Range(0, 5) == 0 {
		txType = uint32(ethtypes.LegacyTxType)
	}

	gasUsed := syntheticReceiptGasUsedBase +
		uint64(crand.Int64Range(0, int64(syntheticReceiptGasUsedSpan))) //nolint:gosec // constants fit in int64
	previousGas := syntheticReceiptPreviousGasBase +
		uint64(crand.Int64Range(0, int64(syntheticReceiptPreviousGasSpan))) //nolint:gosec // constants fit in int64
	cumulativeGasUsed := gasUsed + uint64(txIndex)*previousGas
	effectiveGasPrice := syntheticReceiptGasPriceBase +
		uint64(crand.Int64Range(0, int64(syntheticReceiptGasPriceSpan))) //nolint:gosec // constants fit in int64
	transferAmount := syntheticReceiptTransferBase +
		uint64(crand.Int64Range(0, int64(syntheticReceiptTransferSpan))) //nolint:gosec // constants fit in int64

	var senderTopic [hashLen]byte
	copy(senderTopic[indexedAddressBase:], senderAddressBytes)
	var receiverTopic [hashLen]byte
	copy(receiverTopic[indexedAddressBase:], receiverAddressBytes)

	contractAddressHex := BytesToHex(contractAddressBytes)
	amountData := encodeUint256FromUint64(transferAmount)
	var bloom ethtypes.Bloom
	hasher := sha3.NewLegacyKeccak256()
	var bloomDigest [hashLen]byte
	addToBloom(hasher, &bloomDigest, &bloom, contractAddressBytes)
	addToBloom(hasher, &bloomDigest, &bloom, erc20TransferEventSignatureBytes[:])
	addToBloom(hasher, &bloomDigest, &bloom, senderTopic[:])
	addToBloom(hasher, &bloomDigest, &bloom, receiverTopic[:])

	return &evmtypes.Receipt{
		TxType:            txType,
		CumulativeGasUsed: cumulativeGasUsed,
		ContractAddress:   contractAddressHex,
		TxHashHex:         BytesToHex(crand.Bytes(hashLen)),
		GasUsed:           gasUsed,
		EffectiveGasPrice: effectiveGasPrice,
		BlockNumber:       blockNumber,
		TransactionIndex:  txIndex,
		Status:            uint32(ethtypes.ReceiptStatusSuccessful),
		From:              BytesToHex(srcAddressBytes),
		To:                contractAddressHex,
		Logs: []*evmtypes.Log{{
			Address: contractAddressHex,
			Topics: []string{
				erc20TransferEventSignatureHex,
				BytesToHex(senderTopic[:]),
				BytesToHex(receiverTopic[:]),
			},
			Data:  amountData,
			Index: 0,
		}},
		LogsBloom: bloom[:],
	}, nil
}

func validateCodeKey(name string, key []byte) error {
	_, err := extractCodeKeyBytes(name, key)
	return err
}

func extractCodeKeyBytes(name string, key []byte) ([]byte, error) {
	if len(key) != 1+AddressLen || key[0] != evmCodeKeyPrefixByte {
		return nil, fmt.Errorf("%s must be an EVM code key with %d address bytes", name, AddressLen)
	}
	return key[1:], nil
}

func extractStorageKeyAddressBytes(name string, key []byte) ([]byte, error) {
	if len(key) != 1+StorageKeyLen || key[0] != evmStorageKeyPrefixByte {
		return nil, fmt.Errorf("%s must be an EVM storage key with %d address+slot bytes", name, StorageKeyLen)
	}
	return key[1 : 1+AddressLen], nil
}

func addToBloom(hasher hash.Hash, digest *[hashLen]byte, bloom *ethtypes.Bloom, value []byte) {
	hasher.Reset()
	_, _ = hasher.Write(value)
	hash := hasher.Sum(digest[:0])
	for i := 0; i < 6; i += 2 {
		bit := (uint(hash[i])<<8)&2047 + uint(hash[i+1])
		bloom[ethtypes.BloomByteLength-1-bit/8] |= byte(1 << (bit % 8))
	}
}

func encodeUint256FromUint64(value uint64) []byte {
	encoded := make([]byte, hashLen)
	binary.BigEndian.PutUint64(encoded[hashLen-8:], value)
	return encoded
}
