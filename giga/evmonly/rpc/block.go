package rpc

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/export"
	"github.com/ethereum/go-ethereum/params"
	ethrpc "github.com/ethereum/go-ethereum/rpc"

	receiptpkg "github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	tmbytes "github.com/sei-protocol/sei-chain/sei-tendermint/libs/bytes"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

type blockAPI struct {
	backend Backend
	store   receiptpkg.ReceiptStore
}

// GetBlockByNumber returns the block for number, nil for a zero/negative or
// future height, or an error for a pruned height.
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
// block for number, nil for a zero/negative or future height, or an error for a
// pruned height.
func (api *blockAPI) GetBlockTransactionCountByNumber(ctx context.Context, number ethrpc.BlockNumber) (*hexutil.Uint, error) {
	block, err := api.resolveBlockByNumber(ctx, number)
	if err != nil || block == nil {
		return nil, err
	}
	return transactionCount(ctx, api.store, block)
}

// GetBlockTransactionCountByHash returns the number of transactions in the
// block with the given hash, or nil if hash is unknown.
func (api *blockAPI) GetBlockTransactionCountByHash(ctx context.Context, hash common.Hash) (*hexutil.Uint, error) {
	block, err := api.resolveBlockByHash(ctx, hash)
	if err != nil || block == nil {
		return nil, err
	}
	return transactionCount(ctx, api.store, block)
}

// GetTransactionByBlockNumberAndIndex returns the transaction at index in the
// block for number, as eth_getBlockByNumber with full transactions lists it. It
// returns nil for a zero/negative or future height or an index past the end of
// the block, and an error for a pruned height.
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

// transactionAtIndex renders the transaction at index in the transactions list
// of eth_getBlockBy* for block, or returns nil if index is past its end.
func (api *blockAPI) transactionAtIndex(ctx context.Context, block *coretypes.ResultBlock, index hexutil.Uint) (*export.RPCTransaction, error) {
	// The list never holds more transactions than the body.
	i, ok := utils.SafeCast[int](uint(index))
	if !ok || i >= len(block.Block.Txs) {
		return nil, nil
	}
	blockUnix, ok := utils.SafeCast[uint64](block.Block.Time.Unix())
	if !ok {
		return nil, fmt.Errorf("block %d time is negative: %s", block.Block.Height, block.Block.Time)
	}
	baseFee, err := api.backend.EvmBaseFee()
	if err != nil {
		return nil, err
	}
	chainConfig, err := api.backend.EvmChainConfig()
	if err != nil {
		return nil, err
	}
	txs, err := blockTransactions(ctx, api.store, block)
	if err != nil {
		return nil, err
	}
	if i >= len(txs) {
		return nil, nil
	}
	return rpcTransaction(block, txs[i], blockUnix, baseFee, chainConfig), nil
}

// transactionCount returns the length of the transactions list of
// eth_getBlockBy* for block, building the list only when neither the body nor
// the block's stored stats answer it.
func transactionCount(ctx context.Context, store receiptpkg.ReceiptStore, block *coretypes.ResultBlock) (*hexutil.Uint, error) {
	height := block.Block.Height
	if !receiptsCover(store, height) {
		count := hexutil.Uint(len(block.Block.Txs)) //nolint:gosec // G115: a slice length is non-negative.
		return &count, nil
	}
	stats, err := store.GetBlockStats(receiptContext(ctx), uint64(height)) //nolint:gosec // G115: height is positive here.
	if err == nil {
		count := hexutil.Uint(stats.TxCount)
		return &count, nil
	}
	if !errors.Is(err, receiptpkg.ErrBlockStatsNotSupported) && !errors.Is(err, receiptpkg.ErrNotFound) {
		return nil, fmt.Errorf("read block stats for block %d: %w", height, err)
	}
	txs, err := blockTransactions(ctx, store, block)
	if err != nil {
		return nil, err
	}
	count := hexutil.Uint(len(txs)) //nolint:gosec // G115: a slice length is non-negative.
	return &count, nil
}

// blockTransaction is one entry of the transactions list of eth_getBlockBy*.
type blockTransaction struct {
	tx *ethtypes.Transaction
	// index is the transactionIndex the block lists tx at.
	index uint64
	// receipt is nil when the receipt store does not cover the block.
	receipt *evmtypes.Receipt
}

// blockTransactions returns the transactions block lists, in order. When the
// receipt store covers block, those are the transactions with a receipt stored
// there, each at its receipt's index, leaving out every transaction the
// executor rejected. Otherwise every transaction in the body is listed at its
// position.
func blockTransactions(ctx context.Context, store receiptpkg.ReceiptStore, block *coretypes.ResultBlock) ([]blockTransaction, error) {
	if !receiptsCover(store, block.Block.Height) {
		return bodyTransactions(block)
	}
	records, err := blockReceiptRecords(ctx, store, block)
	if err != nil {
		return nil, err
	}
	txs := make([]blockTransaction, len(records))
	for i, record := range records {
		ethtx, err := receiptTransaction(block, record.TxHash, record.Receipt)
		if err != nil {
			return nil, err
		}
		txs[i] = blockTransaction{tx: ethtx, index: uint64(record.Receipt.TransactionIndex), receipt: record.Receipt}
	}
	return txs, nil
}

// bodyTransactions returns every transaction in block's body, each listed at
// its position.
func bodyTransactions(block *coretypes.ResultBlock) ([]blockTransaction, error) {
	txs := make([]blockTransaction, len(block.Block.Txs))
	for i, raw := range block.Block.Txs {
		ethtx, err := decodeBlockTx(raw, block.Block.Height, i)
		if err != nil {
			return nil, err
		}
		txs[i] = blockTransaction{tx: ethtx, index: uint64(i)} //nolint:gosec // G115: a slice index is non-negative.
	}
	return txs, nil
}

// resolveBlockByHash returns the block with the given hash, or a nil block if
// hash is unknown.
func (api *blockAPI) resolveBlockByHash(ctx context.Context, hash common.Hash) (*coretypes.ResultBlock, error) {
	block, err := api.backend.BlockByHash(ctx, &coretypes.RequestBlockByHash{Hash: tmbytes.HexBytes(hash.Bytes())})
	if err != nil {
		return nil, err
	}
	if block == nil || block.Block == nil {
		return nil, nil
	}
	return block, nil
}

// resolveBlockByNumber returns a nil block for a zero/negative or future
// height, and coretypes.ErrHeightNotAvailable for a pruned height.
func (api *blockAPI) resolveBlockByNumber(ctx context.Context, number ethrpc.BlockNumber) (*coretypes.ResultBlock, error) {
	var height *coretypes.Int64
	switch number {
	case ethrpc.LatestBlockNumber, ethrpc.SafeBlockNumber, ethrpc.FinalizedBlockNumber, ethrpc.PendingBlockNumber:
		// nil height resolves to the current committed block.
	default:
		h := coretypes.Int64(number.Int64())
		height = &h
	}
	block, err := api.backend.Block(ctx, &coretypes.RequestBlockInfo{Height: height})
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

// encodeBlock renders block as an eth_getBlockBy* response. nonce, mixHash,
// sha3Uncles, difficulty, extraData, uncles, and totalDifficulty are always
// their Ethereum-inapplicable zero value, matching evmrpc's v2 encoder.
// logsBloom is always zero; receipt blooms are not aggregated here.
func (api *blockAPI) encodeBlock(ctx context.Context, block *coretypes.ResultBlock, fullTx bool) (map[string]any, error) {
	number := block.Block.Height
	blockUnix, ok := utils.SafeCast[uint64](block.Block.Time.Unix())
	if !ok {
		return nil, fmt.Errorf("block %d time is negative: %s", number, block.Block.Time)
	}
	gasLimit, err := api.backend.EvmGasLimit()
	if err != nil {
		return nil, err
	}
	baseFee, err := api.backend.EvmBaseFee()
	if err != nil {
		return nil, err
	}

	var chainConfig *params.ChainConfig
	if fullTx {
		if chainConfig, err = api.backend.EvmChainConfig(); err != nil {
			return nil, err
		}
	}
	txs, err := blockTransactions(ctx, api.store, block)
	if err != nil {
		return nil, err
	}
	transactions := make([]any, len(txs))
	for i, tx := range txs {
		if fullTx {
			transactions[i] = rpcTransaction(block, tx, blockUnix, baseFee, chainConfig)
		} else {
			transactions[i] = tx.tx.Hash()
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

// rpcTransaction renders tx as a full eth_getBlockBy* transaction entry of
// block, taking its sender from its receipt when the signature does not
// resolve one.
func rpcTransaction(block *coretypes.ResultBlock, tx blockTransaction, blockUnix uint64, baseFee *big.Int, chainConfig *params.ChainConfig) *export.RPCTransaction {
	blockHash := common.BytesToHash(block.BlockID.Hash)
	result := export.NewRPCTransaction(tx.tx, blockHash, uint64(block.Block.Height), blockUnix, tx.index, baseFee, chainConfig) //nolint:gosec // G115: the height is a validated block height.
	if tx.receipt != nil {
		replaceFrom(result, tx.receipt)
	}
	return result
}

// encodeHeader renders block's header in the shape go-ethereum's types.Header
// decodes.
func encodeHeader(backend Backend, block *coretypes.ResultBlock, gasUsed uint64) (map[string]any, error) {
	blockUnix, ok := utils.SafeCast[uint64](block.Block.Time.Unix())
	if !ok {
		return nil, fmt.Errorf("block %d time is negative: %s", block.Block.Height, block.Block.Time)
	}
	gasLimit, err := backend.EvmGasLimit()
	if err != nil {
		return nil, err
	}
	baseFee, err := backend.EvmBaseFee()
	if err != nil {
		return nil, err
	}
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
func receiptFor(ctx context.Context, store receiptpkg.ReceiptStore, hash common.Hash) (*evmtypes.Receipt, error) {
	stored, err := store.GetReceipt(receiptContext(ctx), hash)
	if errors.Is(err, receiptpkg.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return stored, nil
}

// receiptsCover reports whether store holds the receipts of the block at height:
// they are written and not yet pruned.
func receiptsCover(store receiptpkg.ReceiptStore, height int64) bool {
	return store != nil && height <= store.LatestVersion() && height >= store.EarliestVersion()
}

// blockReceiptRecords returns the receipts stored for block, in transaction
// order: by walking the store when it can, otherwise by looking up each
// transaction in the body.
func blockReceiptRecords(ctx context.Context, store receiptpkg.ReceiptStore, block *coretypes.ResultBlock) ([]receiptpkg.ReceiptRecord, error) {
	records, err := receiptRecordsFromIterator(ctx, store, block.Block.Height)
	if errors.Is(err, receiptpkg.ErrRangeQueryNotSupported) {
		return receiptRecordsFromBlock(ctx, store, block)
	}
	return records, err
}

// blockGasUsed returns the gas used by block: its stored BlockStats when the store
// has them, otherwise the total recomputed from the receipts that belong to the
// block. It is zero when the store does not cover block.
func blockGasUsed(ctx context.Context, store receiptpkg.ReceiptStore, block *coretypes.ResultBlock) (uint64, error) {
	// The total covers this lane's block; superblocks merging lanes would need a
	// combined total instead.
	height := block.Block.Height
	if !receiptsCover(store, height) {
		return 0, nil
	}
	stats, err := store.GetBlockStats(receiptContext(ctx), uint64(height)) //nolint:gosec // G115: height is positive here.
	if err == nil {
		return stats.TotalGasUsed, nil
	}
	if errors.Is(err, receiptpkg.ErrNotFound) {
		// Pruned between the version check and the read.
		return 0, nil
	}
	if !errors.Is(err, receiptpkg.ErrBlockStatsNotSupported) {
		return 0, fmt.Errorf("read block stats for block %d: %w", height, err)
	}
	records, err := blockReceiptRecords(ctx, store, block)
	if err != nil {
		return 0, err
	}
	return receiptpkg.ComputeBlockStats(records, nil).TotalGasUsed, nil
}
