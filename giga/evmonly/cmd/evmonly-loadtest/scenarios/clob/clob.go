// Package clob holds the ShardedCLOB contract the load test's clob workload drives, with the
// calldata and storage-slot helpers that workload and its tests need.
//
// Regenerate the committed artifacts after editing ShardedCLOB.sol:
//
//	solc --via-ir --optimize --optimize-runs 200 --evm-version cancun \
//	  --bin-runtime --abi -o . --overwrite ShardedCLOB.sol
package clob

import (
	_ "embed"
	"encoding/binary"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// LowPrice and HighPrice are the two price levels every market supports.
const (
	LowPrice  = 90
	HighPrice = 100
)

// Storage slots of the contract's top-level mappings.
const (
	marketsSlot  = 0
	ordersSlot   = 1
	balancesSlot = 2
)

//go:embed ShardedCLOB.bin-runtime
var runtimeHex string

//go:embed ShardedCLOB.abi
var abiJSON string

var (
	placeSelector  = crypto.Keccak256([]byte("place(uint256,bool,uint256,uint256)"))[:4]
	creditSelector = crypto.Keccak256([]byte("credit(uint256,uint128,uint128)"))[:4]
	cancelSelector = crypto.Keccak256([]byte("cancel(uint256,uint64)"))[:4]
)

// RuntimeCode returns the contract's deployed bytecode.
func RuntimeCode() []byte {
	return common.FromHex(strings.TrimSpace(runtimeHex))
}

// ABI returns the contract's JSON ABI.
func ABI() string {
	return abiJSON
}

// PlaceCalldata encodes place(market, isBuy, price, size).
func PlaceCalldata(market uint64, isBuy bool, price, size uint64) []byte {
	var flag uint64
	if isBuy {
		flag = 1
	}
	return encodeCall(placeSelector, market, flag, price, size)
}

// CreditCalldata encodes credit(market, base, quote).
func CreditCalldata(market, base, quote uint64) []byte {
	return encodeCall(creditSelector, market, base, quote)
}

// CancelCalldata encodes cancel(market, orderId).
func CancelCalldata(market, orderID uint64) []byte {
	return encodeCall(cancelSelector, market, orderID)
}

func encodeCall(selector []byte, args ...uint64) []byte {
	data := make([]byte, 4+32*len(args))
	copy(data, selector)
	for i, arg := range args {
		binary.BigEndian.PutUint64(data[4+32*i+24:], arg)
	}
	return data
}

// BalanceSlots returns the storage slots of owner's base and quote balances in market. Each slot
// packs the available amount in its low 128 bits and the locked amount in its high 128 bits.
func BalanceSlots(market uint64, owner common.Address) (base, quote common.Hash) {
	inner := mappingSlot(uint64Word(market), common.BigToHash(big.NewInt(balancesSlot)))
	base = mappingSlot(common.BytesToHash(owner.Bytes()), inner)
	return base, incrementSlot(base, 1)
}

// OrderSlot returns the first storage slot of a market-local order id.
func OrderSlot(market, orderID uint64) common.Hash {
	inner := mappingSlot(uint64Word(market), common.BigToHash(big.NewInt(ordersSlot)))
	return mappingSlot(uint64Word(orderID), inner)
}

// LastOrderIDSlot returns the storage slot holding a market's last assigned order id.
func LastOrderIDSlot(market uint64) common.Hash {
	return mappingSlot(uint64Word(market), common.BigToHash(big.NewInt(marketsSlot)))
}

// PackBalance packs an available and a locked amount into one balance slot value.
func PackBalance(available, locked uint64) common.Hash {
	var value common.Hash
	binary.BigEndian.PutUint64(value[8:16], locked)
	binary.BigEndian.PutUint64(value[24:32], available)
	return value
}

// UnpackBalance splits a balance slot value into its available and locked amounts.
func UnpackBalance(value common.Hash) (available, locked *big.Int) {
	return new(big.Int).SetBytes(value[16:32]), new(big.Int).SetBytes(value[0:16])
}

func mappingSlot(key, slot common.Hash) common.Hash {
	return crypto.Keccak256Hash(key.Bytes(), slot.Bytes())
}

func incrementSlot(slot common.Hash, by int64) common.Hash {
	return common.BigToHash(new(big.Int).Add(slot.Big(), big.NewInt(by)))
}

func uint64Word(v uint64) common.Hash {
	var word common.Hash
	binary.BigEndian.PutUint64(word[24:], v)
	return word
}
