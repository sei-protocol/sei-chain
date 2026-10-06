package scenarios

import (
	"context"
	"crypto/ecdsa"
	"encoding/binary"
	"fmt"
	"math"
	"math/big"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/giga/evmonly"
)

func workloadRecipient(cfg Config, pool []common.Address, conflictParticipants int, uniquePrefix string, conflictPrefix string, blockNumber uint64, txIndex int, accountIndex uint64) common.Address {
	if cfg.FixedRecipient != nil {
		return *cfg.FixedRecipient
	}
	if txIndex < conflictParticipants {
		return blockScopedAddressFromSeed(conflictPrefix, blockNumber, uint64FromNonNegativeInt(txIndex/2))
	}
	// A pooled run pays a recipient out of the pool rather than to a fresh address, so state stops
	// growing with the run. The recipient is taken half a pool away so it falls outside the block's
	// own contiguous range of senders: paying a neighbour instead would make every transaction
	// depend on the one before it, and speculative execution would conflict on all of them.
	if len(pool) > 0 {
		return pool[(accountIndex+uint64(len(pool)/2))%uint64(len(pool))]
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

// SeedAccountPool seeds every sender a bounded pool will draw from. A streaming run commits genesis
// once before any block is built, so the pool has to be in state before that point.
func seedAccountPool(ctx context.Context, cfg Config, seed func(common.Address)) ([]common.Address, error) {
	pool := make([]common.Address, 0, cfg.Accounts)
	for account := uint64(0); account < cfg.Accounts; account++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		key, err := DeterministicPrivateKey(account)
		if err != nil {
			return nil, fmt.Errorf("derive pool account %d: %w", account, err)
		}
		addr := crypto.PubkeyToAddress(key.PublicKey)
		seed(addr)
		pool = append(pool, addr)
	}
	return pool, nil
}

// senderRangeBase is the first run-wide transaction index a block owns, which is a contiguous range
// so that a block's senders are distinct.
//
// A pooled run takes the range from the block's height, counted from the run's first block. A
// counter cannot serve there: builders finish out of order, so it would hand a later height a lower
// range and its senders would carry nonces the earlier height has not spent. Counting from the
// first block rather than from height zero matters for the same reason, a skipped range leaving the
// accounts in it starting above the nonce their state holds.
func senderRangeBase(cfg Config, number uint64, cursor *atomic.Uint64) uint64 {
	if cfg.Accounts == 0 {
		// No pool, so every sender is used once and indices only have to be unique. A counter gives
		// that without assuming anything about the heights a caller builds.
		return cursor.Add(uint64(cfg.TxsPerBlock)) - uint64(cfg.TxsPerBlock) + 1 //nolint:gosec // txsPerBlock is validated positive
	}
	first := cfg.FirstBlockHeight
	if first == 0 {
		first = 1
	}
	if number < first {
		return 0
	}
	return (number - first) * uint64(cfg.TxsPerBlock) //nolint:gosec // txsPerBlock is validated positive
}
