package evmonly

import (
	"context"
	"math/big"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"

	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
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
	PrepareBlock(BlockRequest) (PreparedBlock, error)
	ExecutePreparedBlock(context.Context, PreparedBlock) (*BlockResult, error)
}

// ResultSink persists executor-produced block outputs. The sink can retain the
// complete BlockResult without forcing the executor to copy changesets or
// receipts before handing them to an async sink. The sink is invoked once the
// block's state commit has been started, not once it has landed: that commit
// runs behind the next block, and a later AwaitCommits reports it failing. A
// sink therefore has to tolerate being handed a block whose commit fails after
// the fact, and a sink error does not roll back the state commit.
// The sink must invoke release exactly once after it no longer references
// result. If StoreBlockResult returns an error, the executor releases that sink
// reference.
type ResultSink interface {
	StoreBlockResult(ctx context.Context, height uint64, result *BlockResult, release func()) error
}

// BlockRequest contains raw transactions and their execution context.
type BlockRequest struct {
	Context BlockContext
	Txs     [][]byte
}

// DecodedBlock contains decoded transactions with verified senders for a block.
type DecodedBlock struct {
	context BlockContext
	txs     []abevm.EvmTx
}

func (b DecodedBlock) prepared() PreparedBlock {
	txs := make([]PreparedTx, len(b.txs))
	for i, tx := range b.txs {
		txs[i] = PreparedTx{Tx: tx.Transaction(), Sender: tx.Sender()}
	}
	return PreparedBlock{Context: b.context, Txs: txs}
}

// PreparedBlock contains transactions and verified senders ready for execution.
// ExecutePreparedBlock trusts it unchanged and does not recover senders again.
type PreparedBlock struct {
	Context BlockContext
	Txs     []PreparedTx
}

// PreparedTx is the stateless per-transaction work needed before EVM execution.
// Sender is trusted data recovered by PrepareBlock.
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
	// Epoch is the consensus epoch this block is in, when the app knows it.
	Epoch utils.Option[uint64]
	// ClosingEpoch is the consensus epoch this block is the last block of, if
	// it is one.
	ClosingEpoch utils.Option[uint64]
}

// BlockResult is the executor output consumed by the new runtime boundary.
type BlockResult struct {
	// The block's state changes. Never modified once the block has executed, so a holder may keep it
	// after the result is released.
	ChangeSet StateChangeSet
	Txs       []TxResult
	Receipts  ethtypes.Receipts
	GasUsed   uint64
	OCCStats  OCCStats

	releaseMu sync.Mutex
	lease     *blockResultLease
}

// Release returns a pooled BlockResult to its executor-owned pool. It is a
// no-op for results that were not allocated from a pool. Release is idempotent
// and safe for concurrent callers until the result is reacquired; callers must
// not retain the pointer after releasing it.
func (r *BlockResult) Release() {
	if r == nil {
		return
	}
	r.releaseMu.Lock()
	lease := r.lease
	if lease == nil {
		r.releaseMu.Unlock()
		return
	}
	r.lease = nil
	r.releaseMu.Unlock()
	lease.release()
}

// OCCStats reports optimistic concurrency control behavior for a block.
type OCCStats struct {
	Attempted      bool
	Fallback       bool
	FallbackReason string
	RerunCount     uint64
	// MaxIncarnation is the deepest incarnation any transaction in the block
	// reached. 0 means no transaction was rerun; RerunCount counts reruns across
	// the whole block, where this counts them for the worst single transaction.
	MaxIncarnation  uint64
	ConflictCount   uint64
	ValidationCount uint64
	ConflictSamples []OCCConflictCount
}

// OCCConflictCount aggregates conflicts observed while validating optimistic
// execution.
type OCCConflictCount struct {
	Access  string
	Kind    string
	Address common.Address
	Slot    common.Hash
	Count   uint64
}

// BlockResultPoolStats reports bounded-pool availability and the number of
// unpooled allocations used to avoid blocking when every pooled result is held.
type BlockResultPoolStats struct {
	Capacity            int
	Available           int
	OverflowAllocations uint64
}

// StateChangeSet and its entry types are shared with the state store.
type (
	StateChangeSet = gigatypes.StateChangeSet
	AccountState   = gigatypes.AccountState
	AccountChange  = gigatypes.AccountChange
	StorageChange  = gigatypes.StorageChange
)

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
	// Rejected reports that the transaction never ran: it consumed no gas and changed no state.
	Rejected bool
}
