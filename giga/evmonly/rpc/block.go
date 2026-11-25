package rpc

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	receiptmetrics "github.com/sei-protocol/sei-chain/sei-db/receipt_db/metrics"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/export"
	"github.com/ethereum/go-ethereum/params"
	ethrpc "github.com/ethereum/go-ethereum/rpc"

	receiptdb "github.com/sei-protocol/sei-chain/sei-db/receipt_db"
	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

type blockAPI struct {
	backend Backend
	store   receiptdb.ReceiptStore
}

// GetBlockByNumber returns the block for number, nil for a future height, or an
// error for a pruned height.
func (api *blockAPI) GetBlockByNumber(ctx context.Context, number ethrpc.BlockNumber, fullTx bool) (map[string]any, error) {
	block, err := api.resolveBlockByNumber(ctx, number)
	if err != nil || block == nil {
		return nil, err
	}
	return api.encodeBlock(ctx, block, fullTx)
}

// GetBlockByHash returns the block with the given hash, or nil if hash is
// unknown.
func (api *blockAPI) GetBlockByHash(ctx context.Context, hash common.Hash, fullTx bool) (map[string]any, error) {
	block, err := api.resolveBlockByHash(ctx, hash)
	if err != nil || block == nil {
		return nil, err
	}
	return api.encodeBlock(ctx, block, fullTx)
}

// GetBlockTransactionCountByNumber returns the number of transactions in the
// block for number, nil for a future height, or an error for a pruned height.
func (api *blockAPI) GetBlockTransactionCountByNumber(ctx context.Context, number ethrpc.BlockNumber) (*hexutil.Uint, error) {
	block, err := api.resolveBlockByNumber(ctx, number)
	if err != nil || block == nil {
		return nil, err
	}
	return transactionCount(block), nil
}

// GetBlockTransactionCountByHash returns the number of transactions in the
// block with the given hash, or nil if hash is unknown.
func (api *blockAPI) GetBlockTransactionCountByHash(ctx context.Context, hash common.Hash) (*hexutil.Uint, error) {
	block, err := api.resolveBlockByHash(ctx, hash)
	if err != nil || block == nil {
		return nil, err
	}
	return transactionCount(block), nil
}

// GetTransactionByBlockNumberAndIndex returns the transaction at index in the
// block for number, as eth_getBlockByNumber with full transactions lists it. It
// returns nil for a future height or an index past the end of the block, and an
// error for a pruned height.
func (api *blockAPI) GetTransactionByBlockNumberAndIndex(ctx context.Context, number ethrpc.BlockNumber, index hexutil.Uint) (*export.RPCTransaction, error) {
	block, err := api.resolveBlockByNumber(ctx, number)
	if err != nil || block == nil {
		return nil, err
	}
	return api.transactionAtIndex(ctx, block, index)
}

// GetTransactionByBlockHashAndIndex returns the transaction at index in the
// block with the given hash, as eth_getBlockByHash with full transactions lists
// it. It returns nil if hash is unknown or index is past the end of the block.
func (api *blockAPI) GetTransactionByBlockHashAndIndex(ctx context.Context, hash common.Hash, index hexutil.Uint) (*export.RPCTransaction, error) {
	block, err := api.resolveBlockByHash(ctx, hash)
	if err != nil || block == nil {
		return nil, err
	}
	return api.transactionAtIndex(ctx, block, index)
}

// transactionAtIndex renders the transaction at index in block, or returns nil
// if index is past the end of the block.
func (api *blockAPI) transactionAtIndex(ctx context.Context, block *coretypes.ResultBlock, index hexutil.Uint) (*export.RPCTransaction, error) {
	i, ok := utils.SafeCast[int](uint(index))
	if !ok || i >= len(block.Block.Txs) {
		return nil, nil
	}
	blockUnix, ok := utils.SafeCast[uint64](block.Block.Time.Unix())
	if !ok {
		return nil, fmt.Errorf("block %d time is negative: %s", block.Block.Height, block.Block.Time)
	}
	baseFee := api.backend.EvmBaseFee()
	chainConfig := api.backend.EvmChainConfig()
	return api.rpcTransaction(ctx, block, i, blockUnix, baseFee, chainConfig)
}

// transactionCount returns the number of transactions in block, counting every
// transaction its body carries, as the transactions list of eth_getBlockBy*
// does.
func transactionCount(block *coretypes.ResultBlock) *hexutil.Uint {
	count := hexutil.Uint(len(block.Block.Txs)) //nolint:gosec // G115: a slice length is non-negative.
	return &count
}

// resolveBlockByHash returns the block with the given hash, or a nil block if
// hash is unknown.
func (api *blockAPI) resolveBlockByHash(ctx context.Context, hash common.Hash) (*coretypes.ResultBlock, error) {
	block, err := loadBackendBlockByHash(ctx, api.backend, hash)
	if err != nil {
		return nil, err
	}
	if block == nil || block.Block == nil {
		return nil, nil
	}
	return block, nil
}

// resolveBlockByNumber returns the block number names, with earliest and 0x0 naming the earliest
// block the receipt store retains. It returns a nil block for a future height, and coretypes.ErrHeightNotAvailable
// for a pruned height.
func (api *blockAPI) resolveBlockByNumber(ctx context.Context, number ethrpc.BlockNumber) (*coretypes.ResultBlock, error) {
	kind, err := classifyBlockNumber(number)
	if err != nil {
		return nil, err
	}
	switch kind {
	case headBlock:
		if head, ok := pinnedHead(ctx).Get(); ok {
			return api.blockAt(ctx, int64(head)) //nolint:gosec // G115: a committed height fits in int64.
		}
		return api.fetchBlock(ctx, nil)
	case earliestBlock:
		return api.blockAt(ctx, earliestRetainedHeight(api.backend, api.store))
	}
	return api.blockAt(ctx, number.Int64())
}

// blockAt returns the block at height, or a nil block for a height below 1 or above the head. It
// returns coretypes.ErrHeightNotAvailable for a pruned height.
func (api *blockAPI) blockAt(ctx context.Context, height int64) (*coretypes.ResultBlock, error) {
	h := coretypes.Int64(height)
	return api.fetchBlock(ctx, &h)
}

// fetchBlock returns the backend's block at height, or the latest committed block for a nil
// height, with a nil block for one the backend does not have.
func (api *blockAPI) fetchBlock(ctx context.Context, height *coretypes.Int64) (*coretypes.ResultBlock, error) {
	block, err := loadBackendBlock(ctx, api.backend, height)
	if errors.Is(err, coretypes.ErrHeightExceedsChainHead) ||
		errors.Is(err, coretypes.ErrZeroOrNegativeHeight) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if block == nil || block.Block == nil {
		return nil, nil
	}
	return block, nil
}

// executionHeaderAt returns the header of the block at height with the fields its execution context
// reads. Block 0, which no store holds, carries only its number.
func (api *blockAPI) executionHeaderAt(ctx context.Context, height int64) (*ethtypes.Header, error) {
	if height == 0 {
		return preChainBlock(0).Header(), nil
	}
	block, err := api.blockAt(ctx, height)
	if err != nil {
		return nil, err
	}
	if block == nil {
		return nil, fmt.Errorf("%w: %d", errBlockNotFound, height)
	}
	return api.executionHeader(ctx, block)
}

// executionHeader returns block's header with the fields its execution context reads.
func (api *blockAPI) executionHeader(ctx context.Context, block *coretypes.ResultBlock) (*ethtypes.Header, error) {
	blockUnix, ok := utils.SafeCast[uint64](block.Block.Time.Unix())
	if !ok {
		return nil, fmt.Errorf("block %d time is negative: %s", block.Block.Height, block.Block.Time)
	}
	parentHash, err := api.parentHash(ctx, block.Block.Height)
	if err != nil {
		return nil, err
	}
	gasLimit := api.backend.EvmGasLimit()
	baseFee := api.backend.EvmBaseFee()
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
// zero for the chain's first block.
func (api *blockAPI) parentHash(ctx context.Context, height int64) (common.Hash, error) {
	if height <= firstBlockHeight(api.backend) {
		return common.Hash{}, nil
	}
	parent, err := api.blockAt(ctx, height-1)
	if err != nil {
		return common.Hash{}, fmt.Errorf("read the parent of block %d: %w", height, err)
	}
	if parent == nil {
		return common.Hash{}, nil
	}
	return common.BytesToHash(parent.BlockID.Hash), nil
}

// encodeBlock renders block as an eth_getBlockBy* response. nonce, mixHash,
// sha3Uncles, difficulty, extraData, uncles, and totalDifficulty are always
// their zero value; unlike evmrpc's v2 encoder, mixHash does not report PREVRANDAO.
// logsBloom is always zero; receipt blooms are not aggregated here.
func (api *blockAPI) encodeBlock(ctx context.Context, block *coretypes.ResultBlock, fullTx bool) (map[string]any, error) {
	number := block.Block.Height
	blockUnix, ok := utils.SafeCast[uint64](block.Block.Time.Unix())
	if !ok {
		return nil, fmt.Errorf("block %d time is negative: %s", number, block.Block.Time)
	}
	gasLimit := api.backend.EvmGasLimit()
	baseFee := api.backend.EvmBaseFee()

	txs := block.Block.Txs
	transactions := make([]any, len(txs))
	if fullTx {
		chainConfig := api.backend.EvmChainConfig()
		// One receipt read per transaction here, not just for the last one:
		// deferred pending a bulk receipt-load API on the receipt store.
		for i := range txs {
			result, err := api.rpcTransaction(ctx, block, i, blockUnix, baseFee, chainConfig)
			if err != nil {
				return nil, err
			}
			transactions[i] = result
		}
	} else {
		for i, raw := range txs {
			ethtx, err := decodeBlockTx(raw, number, i)
			if err != nil {
				return nil, err
			}
			transactions[i] = ethtx.Hash()
		}
	}
	gasUsed, err := blockGasUsed(ctx, api.store, block)
	if err != nil {
		return nil, err
	}

	result := headerFields(block, blockUnix, gasLimit, baseFee, gasUsed)
	result["size"] = hexutil.Uint64(block.Block.Size()) //nolint:gosec // G115: block size is positive.
	result["uncles"] = []common.Hash{}                  // inapplicable to Sei
	result["transactions"] = transactions
	if fullTx {
		result["totalDifficulty"] = (*hexutil.Big)(big.NewInt(0)) // inapplicable to Sei
	}
	return result, nil
}

// rpcTransaction renders the transaction at index in block as a full
// eth_getBlockBy* transaction entry, taking its sender from the stored receipt
// when the signature does not resolve one.
func (api *blockAPI) rpcTransaction(ctx context.Context, block *coretypes.ResultBlock, index int, blockUnix uint64, baseFee *big.Int, chainConfig *params.ChainConfig) (*export.RPCTransaction, error) {
	number := block.Block.Height
	ethtx, err := decodeBlockTx(block.Block.Txs[index], number, index)
	if err != nil {
		return nil, err
	}
	stored, err := receiptFor(ctx, api.store, ethtx.Hash())
	if err != nil {
		return nil, fmt.Errorf("read transaction receipt at block %d index %d: %w", number, index, err)
	}
	blockHash := common.BytesToHash(block.BlockID.Hash)
	result := export.NewRPCTransaction(ethtx, blockHash, uint64(number), blockUnix, uint64(index), baseFee, chainConfig) //nolint:gosec // G115: number is a validated block height.
	if stored != nil {
		replaceFrom(result, stored)
	}
	return result, nil
}

// encodeHeader renders block's header in the shape go-ethereum's types.Header
// decodes.
func encodeHeader(backend Backend, block *coretypes.ResultBlock, gasUsed uint64) (map[string]any, error) {
	blockUnix, ok := utils.SafeCast[uint64](block.Block.Time.Unix())
	if !ok {
		return nil, fmt.Errorf("block %d time is negative: %s", block.Block.Height, block.Block.Time)
	}
	gasLimit := backend.EvmGasLimit()
	baseFee := backend.EvmBaseFee()
	return headerFields(block, blockUnix, gasLimit, baseFee, gasUsed), nil
}

// headerFields returns the Ethereum JSON header fields of block.
func headerFields(block *coretypes.ResultBlock, blockUnix, gasLimit uint64, baseFee *big.Int, gasUsed uint64) map[string]any {
	return map[string]any{
		"number":           (*hexutil.Big)(big.NewInt(block.Block.Height)),
		"hash":             common.BytesToHash(block.BlockID.Hash),
		"parentHash":       common.BytesToHash(block.Block.LastBlockID.Hash),
		"nonce":            ethtypes.BlockNonce{},   // inapplicable to Sei
		"mixHash":          common.Hash{},           // inapplicable to Sei
		"sha3Uncles":       ethtypes.EmptyUncleHash, // inapplicable to Sei
		"logsBloom":        ethtypes.Bloom{},
		"stateRoot":        common.BytesToHash(block.Block.AppHash),
		"miner":            common.BytesToAddress(block.Block.ProposerAddress),
		"difficulty":       (*hexutil.Big)(big.NewInt(0)), // inapplicable to Sei
		"extraData":        hexutil.Bytes{},               // inapplicable to Sei
		"gasLimit":         hexutil.Uint64(gasLimit),
		"gasUsed":          hexutil.Uint64(gasUsed),
		"timestamp":        hexutil.Uint64(blockUnix),
		"milliTimestamp":   hexutil.Uint64(block.Block.Time.UnixMilli()), //nolint:gosec // G115: block timestamps are positive.
		"transactionsRoot": common.BytesToHash(block.Block.DataHash),
		"receiptsRoot":     common.BytesToHash(block.Block.LastResultsHash),
		"baseFeePerGas":    (*hexutil.Big)(baseFee),
	}
}

// receiptFor returns hash's stored receipt, or nil with a nil error when no
// receipt is stored for it.
func receiptFor(ctx context.Context, store receiptdb.ReceiptStore, hash common.Hash) (*evmtypes.Receipt, error) {
	stored, err := store.GetReceipt(receiptContext(ctx), hash)
	if errors.Is(err, receiptdb.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return stored, nil
}

// blockGasUsed returns the gas used by block: its stored BlockStats when the store
// has them, otherwise the total recomputed from the receipts that belong to the
// block at their original index. It is zero when the store does not cover block.
func blockGasUsed(ctx context.Context, store receiptdb.ReceiptStore, block *coretypes.ResultBlock) (uint64, error) {
	// The total covers this lane's block; superblocks merging lanes would need a
	// combined total instead.
	height := block.Block.Height
	if store == nil || height > store.LatestVersion() || height < store.EarliestVersion() {
		return 0, nil
	}
	stats, err := store.GetBlockStats(receiptContext(ctx), uint64(height)) //nolint:gosec // G115: height is positive here.
	if err == nil {
		return stats.TotalGasUsed, nil
	}
	if errors.Is(err, receiptdb.ErrNotFound) {
		// Pruned between the version check and the read.
		return 0, nil
	}
	if !errors.Is(err, receiptdb.ErrBlockStatsNotSupported) {
		return 0, fmt.Errorf("read block stats for block %d: %w", height, err)
	}
	records, err := blockReceiptRecords(ctx, store, block)
	if err != nil {
		return 0, err
	}
	return receiptmetrics.ComputeBlockStats(records, nil).TotalGasUsed, nil
}

// loadBackendBlock returns the backend's EVM block in the RPC block envelope.
func loadBackendBlock(ctx context.Context, backend Backend, height *coretypes.Int64) (*coretypes.ResultBlock, error) {
	requested := utils.None[int64]()
	if height != nil {
		requested = utils.Some(int64(*height))
	}
	block, err := backend.BlockByNumber(ctx, requested)
	if err != nil {
		return nil, err
	}
	if block == nil {
		return &coretypes.ResultBlock{}, nil
	}
	return rpcBlock(block), nil
}

// loadBackendBlockByHash returns the backend's EVM block in the RPC block envelope.
func loadBackendBlockByHash(ctx context.Context, backend Backend, hash common.Hash) (*coretypes.ResultBlock, error) {
	block, err := backend.BlockByHash(ctx, hash)
	if err != nil {
		return nil, err
	}
	if value, ok := block.Get(); ok {
		return rpcBlock(value), nil
	}
	return &coretypes.ResultBlock{}, nil
}

// rpcBlock returns block in the RPC block envelope.
func rpcBlock(block *abevm.EvmBlock) *coretypes.ResultBlock {
	txs := make(tmtypes.Txs, len(block.Txs))
	for i, tx := range block.Txs {
		txs[i] = tx
	}
	return &coretypes.ResultBlock{
		BlockID: tmtypes.BlockID{Hash: block.Hash.Bytes()},
		Block: &tmtypes.Block{
			Header: tmtypes.Header{
				ChainID: block.ChainID,
				Height:  utils.Clamp[int64](block.Height),
				Time:    block.Time,
			},
			Data: tmtypes.Data{Txs: txs},
			// Autobahn does not feed per-validator votes into the app. Filling N
			// absent signatures would make trace replay's BeginBlock bump
			// missed-block counters and diverge from production. ToReqBeginBlock
			// skips the per-validator loop when Signatures is empty, so empty
			// Votes flow into distribution/slashing on both paths.
			LastCommit: &tmtypes.Commit{},
		},
	}
}
