package rpc

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/eth/tracers"
	"github.com/ethereum/go-ethereum/eth/tracers/tracersutils"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/export"
	"github.com/ethereum/go-ethereum/params"
	ethrpc "github.com/ethereum/go-ethereum/rpc"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

var (
	errTraceBlockNotIndexed = errors.New("block receipts are not indexed yet")
	errTraceBlockPruned     = errors.New("block receipts are pruned")
	errTraceTxIndex         = errors.New("transaction index out of range")
)

var _ tracers.Backend = (*traceBackend)(nil)

// traceBackend serves go-ethereum's tracer API from the EVM-only node: finalized blocks holding the
// transactions that executed, and the state a transaction executed against. A transaction the block
// rejected before it ran changed nothing, so leaving it out keeps every replay faithful.
type traceBackend struct {
	backend Backend
	blocks  *blockAPI
	txs     *txAPI
	// headersOnly serves blocks without their transactions, and names the latest committed block by
	// a current-state tag, for tracing calls, which read only a block's header and state.
	headersOnly bool
}

func newTraceBackend(backend Backend, blocks *blockAPI, txs *txAPI) *traceBackend {
	return &traceBackend{backend: backend, blocks: blocks, txs: txs}
}

// forCalls returns a copy of b that serves blocks for tracing calls.
func (b *traceBackend) forCalls() *traceBackend {
	calls := *b
	calls.headersOnly = true
	return &calls
}

func (b *traceBackend) HeaderByHash(ctx context.Context, hash common.Hash) (*ethtypes.Header, error) {
	block, _, err := b.BlockByHash(ctx, hash)
	if err != nil || block == nil {
		return nil, err
	}
	return block.Header(), nil
}

func (b *traceBackend) HeaderByNumber(ctx context.Context, number ethrpc.BlockNumber) (*ethtypes.Header, error) {
	block, _, err := b.BlockByNumber(ctx, number)
	if err != nil || block == nil {
		return nil, err
	}
	return block.Header(), nil
}

// BlockByHash returns the block with hash, or nil when hash is unknown.
func (b *traceBackend) BlockByHash(ctx context.Context, hash common.Hash) (*ethtypes.Block, []tracersutils.TraceBlockMetadata, error) {
	block, err := b.blocks.resolveBlockByHash(ctx, hash)
	if err != nil || block == nil {
		return nil, nil, err
	}
	eth, err := b.ethBlock(ctx, block)
	return eth, nil, err
}

// BlockByNumber returns the block at number, or nil when there is none. Current-state tags name the
// latest block whose receipts are indexed, or for calls the latest committed block.
func (b *traceBackend) BlockByNumber(ctx context.Context, number ethrpc.BlockNumber) (*ethtypes.Block, []tracersutils.TraceBlockMetadata, error) {
	if number == 0 {
		return genesisBlock(), nil, nil
	}
	if isLatestTag(number) {
		latest := b.backend.EvmBlockNumber()
		if !b.headersOnly {
			_, latest = indexedRange(b.backend, b.blocks.store)
		}
		number = ethrpc.BlockNumber(latest) //nolint:gosec // G115: a committed height fits in int64.
	}
	block, err := b.blocks.resolveBlockByNumber(ctx, number)
	if err != nil || block == nil {
		return nil, nil, err
	}
	eth, err := b.ethBlock(ctx, block)
	return eth, nil, err
}

// genesisBlock returns block 0, which no store holds: it carries only its number and the zero hash
// block 1 names as its parent, which is all a trace reads from a parent block.
func genesisBlock() *ethtypes.Block {
	block := ethtypes.NewBlockWithHeader(&ethtypes.Header{Number: new(big.Int), Difficulty: new(big.Int), BaseFee: new(big.Int)})
	block.OverwriteHash(common.Hash{})
	return block
}

// ethBlock returns block as go-ethereum's tracer reads it: carrying the block's own hash, and holding
// the transactions that executed, in execution order, unless b serves headers only.
func (b *traceBackend) ethBlock(ctx context.Context, block *coretypes.ResultBlock) (*ethtypes.Block, error) {
	var txs []*ethtypes.Transaction
	if !b.headersOnly {
		var err error
		if txs, err = b.executedTxs(ctx, block); err != nil {
			return nil, err
		}
	}
	header, err := b.header(ctx, block)
	if err != nil {
		return nil, err
	}
	eth := ethtypes.NewBlockWithHeader(header).WithBody(ethtypes.Body{Transactions: txs})
	eth.OverwriteHash(common.BytesToHash(block.BlockID.Hash))
	return eth, nil
}

// executedTxs returns the transactions of block that executed, in execution order, read from its
// indexed receipts.
func (b *traceBackend) executedTxs(ctx context.Context, block *coretypes.ResultBlock) ([]*ethtypes.Transaction, error) {
	height := uint64(block.Block.Height) //nolint:gosec // G115: a resolved block's height is positive.
	if _, latest := indexedRange(b.backend, b.blocks.store); height > latest {
		return nil, fmt.Errorf("%w: block %d", errTraceBlockNotIndexed, height)
	}
	records, err := blockReceiptRecords(ctx, b.blocks.store, block)
	if err != nil {
		return nil, err
	}
	// Checked after the read, so a retention floor that rose during it is caught too.
	if earliest, _ := indexedRange(b.backend, b.blocks.store); height < earliest {
		return nil, fmt.Errorf("%w: block %d; earliest available block is %d", errTraceBlockPruned, height, earliest)
	}
	txs := make([]*ethtypes.Transaction, 0, len(records))
	for _, record := range records {
		if !executed(record.Receipt) {
			continue
		}
		tx, err := blockTx(block, int(record.Receipt.TransactionIndex))
		if err != nil {
			return nil, err
		}
		txs = append(txs, tx)
	}
	return txs, nil
}

// header returns block's header with the fields its execution context reads.
func (b *traceBackend) header(ctx context.Context, block *coretypes.ResultBlock) (*ethtypes.Header, error) {
	blockUnix, ok := utils.SafeCast[uint64](block.Block.Time.Unix())
	if !ok {
		return nil, fmt.Errorf("block %d time is negative: %s", block.Block.Height, block.Block.Time)
	}
	parentHash, err := b.parentHash(ctx, block.Block.Height)
	if err != nil {
		return nil, err
	}
	gasLimit, err := b.backend.EvmGasLimit()
	if err != nil {
		return nil, err
	}
	baseFee, err := b.backend.EvmBaseFee()
	if err != nil {
		return nil, err
	}
	return &ethtypes.Header{
		ParentHash: parentHash,
		Number:     big.NewInt(block.Block.Height),
		GasLimit:   gasLimit,
		Time:       blockUnix,
		Difficulty: new(big.Int),
		// Zero rather than nil, so a dynamic-fee transaction is priced at its tip as it executed.
		BaseFee: baseFee,
	}, nil
}

// parentHash returns the hash of the block before height, which a stored block does not carry. It is
// zero before the first block.
func (b *traceBackend) parentHash(ctx context.Context, height int64) (common.Hash, error) {
	parent, err := b.blocks.resolveBlockByNumber(ctx, ethrpc.BlockNumber(height-1))
	if err != nil {
		return common.Hash{}, fmt.Errorf("read the parent of block %d: %w", height, err)
	}
	if parent == nil {
		return common.Hash{}, nil
	}
	return common.BytesToHash(parent.BlockID.Hash), nil
}

// GetTransaction locates a finalized transaction by its stored receipt. Its index is its position in
// the stored block.
func (b *traceBackend) GetTransaction(ctx context.Context, hash common.Hash) (bool, *ethtypes.Transaction, common.Hash, uint64, uint64, error) {
	stored, block, err := b.txs.lookupFinalizedTx(ctx, hash)
	if err != nil || stored == nil {
		return false, nil, common.Hash{}, 0, 0, err
	}
	tx, err := blockTx(block, int(stored.TransactionIndex))
	if err != nil {
		return false, nil, common.Hash{}, 0, 0, err
	}
	return true, tx, common.BytesToHash(block.BlockID.Hash), stored.BlockNumber, uint64(stored.TransactionIndex), nil
}

// StateAtBlock returns the committed state after block, or the latest committed state when ctx is
// marked by withLatestState.
func (b *traceBackend) StateAtBlock(ctx context.Context, block *ethtypes.Block, _ uint64, _ vm.StateDB, _ bool, _ bool) (vm.StateDB, tracers.StateReleaseFunc, error) {
	height := block.Number().Int64()
	if readsLatestState(ctx) {
		// The height a latest tag resolved to can be passed by the time the state is read, and a node
		// without state history holds only the newest state.
		height = ethrpc.LatestBlockNumber.Int64()
	}
	stateDB, release, err := b.backend.EvmStateAt(ctx, height)
	return stateDB, release, err
}

// StateAtTransaction returns the transaction at txIndex of block's stored transactions, and the
// block context and state it executed against. Index 0 of an empty block returns no transaction
// and the state the block started from.
func (b *traceBackend) StateAtTransaction(ctx context.Context, block *ethtypes.Block, txIndex int, _ uint64) (*ethtypes.Transaction, vm.BlockContext, vm.StateDB, tracers.StateReleaseFunc, error) {
	stored, err := b.blocks.resolveBlockByNumber(ctx, ethrpc.BlockNumber(block.Number().Int64()))
	if err != nil {
		return nil, vm.BlockContext{}, nil, nil, err
	}
	if stored == nil {
		return nil, vm.BlockContext{}, nil, nil, fmt.Errorf("block %d not found", block.NumberU64())
	}
	var tx *ethtypes.Transaction
	if txIndex != 0 || len(stored.Block.Txs) != 0 {
		if tx, err = blockTx(stored, txIndex); err != nil {
			return nil, vm.BlockContext{}, nil, nil, err
		}
	}
	header := block.Header()
	blockCtx, err := b.backend.EvmBlockContext(header)
	if err != nil {
		return nil, vm.BlockContext{}, nil, nil, err
	}
	stateDB, release, err := b.backend.EvmStateBeforeTx(ctx, header, stored.Block.Txs[:txIndex].ToSliceOfBytes())
	if err != nil {
		return nil, vm.BlockContext{}, nil, nil, err
	}
	return tx, blockCtx, stateDB, release, nil
}

// GetBlockContext returns the block context block executed at.
func (b *traceBackend) GetBlockContext(_ context.Context, block *ethtypes.Block, _ vm.StateDB, _ export.ChainContextBackend) (vm.BlockContext, error) {
	return b.backend.EvmBlockContext(block.Header())
}

// RPCGasCap is the gas a traced call may use, the same as eth_call's.
func (b *traceBackend) RPCGasCap() uint64 {
	return defaultCallGasCap
}

// ChainConfig returns the chain configuration blocks execute against, or nil when the backend
// exposes none.
func (b *traceBackend) ChainConfig() *params.ChainConfig {
	cfg, err := b.backend.EvmChainConfig()
	if err != nil {
		return nil
	}
	return cfg
}

// ChainConfigAtHeight returns ChainConfig: the configuration never changes with height.
func (b *traceBackend) ChainConfigAtHeight(int64) *params.ChainConfig {
	return b.ChainConfig()
}

// GetCustomPrecompiles returns nil: the EVM-only chain registers no custom precompiles.
func (b *traceBackend) GetCustomPrecompiles(int64) map[common.Address]vm.PrecompiledContract {
	return nil
}

// PrepareTx does nothing: a transaction needs no preparation beyond go-ethereum's own.
func (b *traceBackend) PrepareTx(vm.StateDB, *ethtypes.Transaction) error {
	return nil
}

// Engine returns nil; only tracer methods this node does not serve read it.
func (b *traceBackend) Engine() consensus.Engine {
	return nil
}

// ChainDb returns nil; only tracer methods this node does not serve read it.
func (b *traceBackend) ChainDb() ethdb.Database {
	return nil
}

// latestStateKey marks a context whose traced call reads the latest committed state.
type latestStateKey struct{}

// withLatestState returns ctx marked so that StateAtBlock serves the latest committed state.
func withLatestState(ctx context.Context) context.Context {
	return context.WithValue(ctx, latestStateKey{}, true)
}

// readsLatestState reports whether ctx is marked by withLatestState.
func readsLatestState(ctx context.Context) bool {
	latest, _ := ctx.Value(latestStateKey{}).(bool)
	return latest
}

// executed reports whether the transaction a stored receipt belongs to ran in the EVM. A transaction
// the block rejected for a spent nonce stores a receipt that used no gas, while every transaction
// that ran paid at least its intrinsic gas.
func executed(receipt *evmtypes.Receipt) bool {
	return receipt.GasUsed > 0
}

// blockTx decodes the transaction at index of block's stored transactions.
func blockTx(block *coretypes.ResultBlock, index int) (*ethtypes.Transaction, error) {
	if index < 0 || index >= len(block.Block.Txs) {
		return nil, fmt.Errorf("%w: %d in block %d of %d transactions", errTraceTxIndex, index, block.Block.Height, len(block.Block.Txs))
	}
	return decodeBlockTx(block.Block.Txs[index], block.Block.Height, index)
}
