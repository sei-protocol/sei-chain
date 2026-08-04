package scenarios

import (
	"crypto/ecdsa"
	"encoding/binary"
	"fmt"
	"math"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/giga/evmonly"
)

func workloadRecipient(cfg Config, conflictParticipants int, uniquePrefix string, conflictPrefix string, blockNumber uint64, txIndex int, accountIndex uint64) common.Address {
	if cfg.FixedRecipient != nil {
		return *cfg.FixedRecipient
	}
	if txIndex < conflictParticipants {
		return blockScopedAddressFromSeed(conflictPrefix, blockNumber, uint64FromNonNegativeInt(txIndex/2))
	}
	return addressFromSeed(uniquePrefix, accountIndex)
}

func recipientConflictParticipants(txsPerBlock int, rate float64) int {
	if rate <= 0 || txsPerBlock < 2 {
		return 0
	}
	if rate >= 1 {
		if txsPerBlock%2 == 0 {
			return txsPerBlock
		}
		return txsPerBlock - 1
	}
	count := int(math.Round(rate * float64(txsPerBlock)))
	if count < 2 {
		count = 2
	}
	if count > txsPerBlock {
		count = txsPerBlock
	}
	if count%2 != 0 {
		if count == txsPerBlock {
			count--
		} else {
			count++
		}
	}
	return count
}

func uint64FromNonNegativeInt(value int) uint64 {
	if value < 0 {
		panic("negative integer cannot be converted to uint64")
	}
	return uint64(value) //nolint:gosec // negative values are rejected above.
}

func BlockContext(cfg Config, number uint64) evmonly.BlockContext {
	gasLimit := cfg.BlockGasLimit
	if gasLimit == 0 {
		gasLimit = math.MaxUint64
	}
	return evmonly.BlockContext{
		Number:      number,
		Time:        blockTimestamp(number),
		GasLimit:    gasLimit,
		ChainID:     new(big.Int).Set(cfg.ChainID),
		BaseFee:     big.NewInt(0),
		BlobBaseFee: big.NewInt(0),
		Coinbase:    cfg.Coinbase,
		ParentHash:  hashFromSeed("sei-evmonly-loadtest-parent", number-1),
		BlockHash:   hashFromSeed("sei-evmonly-loadtest-block", number),
		PrevRandao:  hashFromSeed("sei-evmonly-loadtest-randao", number),
	}
}

func blockTimestamp(number uint64) uint64 {
	if number > math.MaxUint64-DefaultGenesisTimestamp {
		return math.MaxUint64
	}
	return DefaultGenesisTimestamp + number
}

func DeterministicPrivateKey(index uint64) (*ecdsa.PrivateKey, error) {
	var buf [16]byte
	binary.BigEndian.PutUint64(buf[:8], index)
	for attempt := uint64(0); ; attempt++ {
		binary.BigEndian.PutUint64(buf[8:], attempt)
		key, err := crypto.ToECDSA(crypto.Keccak256([]byte("sei-evmonly-loadtest-sender"), buf[:]))
		if err == nil {
			return key, nil
		}
		if attempt == ^uint64(0) {
			break
		}
	}
	return nil, fmt.Errorf("could not derive private key for account %d", index)
}

func addressFromSeed(prefix string, index uint64) common.Address {
	hash := hashFromSeed(prefix, index)
	return common.BytesToAddress(hash[12:])
}

func blockScopedAddressFromSeed(prefix string, blockNumber uint64, index uint64) common.Address {
	var buf [16]byte
	binary.BigEndian.PutUint64(buf[:8], blockNumber)
	binary.BigEndian.PutUint64(buf[8:], index)
	hash := crypto.Keccak256Hash([]byte(prefix), buf[:])
	return common.BytesToAddress(hash[12:])
}

func hashFromSeed(prefix string, index uint64) common.Hash {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], index)
	return crypto.Keccak256Hash([]byte(prefix), buf[:])
}
