package evmonly

import (
	"context"
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

var ErrNotImplemented = errors.New("evm-only executor is not implemented")

// Executor is the Cosmos-free block execution boundary for the EVM-only path.
type Executor interface {
	ExecuteBlock(context.Context, BlockRequest) (*BlockResult, error)
}

// BlockRequest contains all consensus/runtime inputs needed to execute a block.
// Txs must be raw Ethereum transaction RLP bytes.
type BlockRequest struct {
	Context BlockContext
	Txs     [][]byte
}

// BlockContext contains block-constant EVM execution data.
type BlockContext struct {
	Number      uint64
	Time        uint64
	GasLimit    uint64
	ChainID     *big.Int
	BaseFee     *big.Int
	BlobBaseFee *big.Int
	Coinbase    common.Address
	ParentHash  common.Hash
	BlockHash   common.Hash
	PrevRandao  common.Hash
}

// BlockResult is the executor output consumed by the new runtime boundary.
type BlockResult struct {
	ChangeSet StateChangeSet
	Txs       []TxResult
	Receipts  ethtypes.Receipts
	GasUsed   uint64
}

// StateChangeSet is the deterministic EVM-native state output for a block.
// Values are post-block values, not deltas.
type StateChangeSet struct {
	Balances []BalanceChange
	Nonces   []NonceChange
	Code     []CodeChange
	Storage  []StorageChange
}

type BalanceChange struct {
	Address common.Address
	Balance *big.Int
}

type NonceChange struct {
	Address common.Address
	Nonce   uint64
}

type CodeChange struct {
	Address common.Address
	Code    []byte
	Delete  bool
}

type StorageChange struct {
	Address common.Address
	Key     common.Hash
	Value   common.Hash
	Delete  bool
}

// TxResult is the minimum per-transaction output needed for receipts, RPC, and
// runtime result reporting.
type TxResult struct {
	Hash              common.Hash
	Sender            common.Address
	To                *common.Address
	ContractAddress   common.Address
	Status            uint64
	GasUsed           uint64
	CumulativeGasUsed uint64
	EffectiveGasPrice *big.Int
	Logs              []*ethtypes.Log
	Err               error
}
