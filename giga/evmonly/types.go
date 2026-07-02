package evmonly

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

// BlockExecutor is the Cosmos-free block execution boundary for the EVM-only path.
type BlockExecutor interface {
	ExecuteBlock(context.Context, BlockRequest) (*BlockResult, error)
}

// PreparedBlockExecutor exposes the split transaction preparation and block
// execution phases. Preparation is stateless, so callers may pipeline it ahead
// of ordered block execution.
type PreparedBlockExecutor interface {
	BlockExecutor
	PrepareBlock(context.Context, BlockRequest) (PreparedBlock, error)
	ExecutePreparedBlock(context.Context, PreparedBlock) (*BlockResult, error)
}

// ResultSink persists executor-produced block outputs.
type ResultSink interface {
	StoreChangeSet(ctx context.Context, height uint64, changeSet StateChangeSet) error
	StoreReceipts(ctx context.Context, height uint64, receipts ethtypes.Receipts) error
}

// BlockResultSink can retain a complete BlockResult without forcing the
// executor to copy changesets or receipts before handing them to an async sink.
// The sink must invoke release exactly once after it no longer references
// result. If StoreBlockResult returns an error, the executor releases that sink
// reference.
type BlockResultSink interface {
	StoreBlockResult(ctx context.Context, height uint64, result *BlockResult, release func()) error
}

// BlockRequest contains all consensus/runtime inputs needed to execute a block.
// Txs must be raw Ethereum transaction RLP bytes.
type BlockRequest struct {
	Context BlockContext
	Txs     [][]byte
}

// PreparedBlock contains decoded transactions with recovered senders. The
// executor treats prepared transactions as immutable.
type PreparedBlock struct {
	Context BlockContext
	Txs     []PreparedTx
}

// PreparedTx is the stateless per-transaction work needed before EVM execution.
type PreparedTx struct {
	Tx     *ethtypes.Transaction
	Sender common.Address
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
	OCCStats  OCCStats

	lease *blockResultLease
}

// Release returns a pooled BlockResult to its executor-owned pool. It is a
// no-op for results that were not allocated from a pool.
func (r *BlockResult) Release() {
	if r == nil || r.lease == nil {
		return
	}
	lease := r.lease
	r.lease = nil
	lease.release()
}

// OCCStats reports optimistic concurrency control behavior for a block.
type OCCStats struct {
	Attempted       bool
	Fallback        bool
	FallbackReason  string
	ConflictCount   uint64
	ConflictSamples []OCCConflictCount
}

// OCCConflictCount aggregates conflicts by the access key that forced OCC to
// fall back to sequential execution.
type OCCConflictCount struct {
	Access  string
	Kind    string
	Address common.Address
	Slot    common.Hash
	Count   uint64
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
