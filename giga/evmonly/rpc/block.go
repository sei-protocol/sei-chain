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

// GetBlockByNumber returns the block identified by number, or nil if number
// does not resolve to a committed, still-retained block. latest/safe/finalized/pending
// all resolve to the current committed block; any other height, past or future,
// is looked up directly, and a height the node has since pruned returns nil rather
// than an error.
func (api *blockAPI) GetBlockByNumber(ctx context.Context, number ethrpc.BlockNumber, fullTx bool) (map[string]any, error) {
	block, err := resolveBlockByNumber(ctx, api.backend, number)
	if err != nil || block == nil {
		return nil, err
	}
	return api.encodeBlock(ctx, block, fullTx)
}

// GetBlockByHash returns the block with the given hash, or nil if hash is
// unknown.
func (api *blockAPI) GetBlockByHash(ctx context.Context, hash common.Hash, fullTx bool) (map[string]any, error) {
	block, err := api.backend.BlockByHash(ctx, &coretypes.RequestBlockByHash{Hash: tmbytes.HexBytes(hash.Bytes())})
	if err != nil {
		return nil, err
	}
	if block == nil || block.Block == nil {
		return nil, nil
	}
	return api.encodeBlock(ctx, block, fullTx)
}

// resolveBlockByNumber looks up number, returning a nil block and nil error
// for any height outside the committed range or since pruned from retention.
func resolveBlockByNumber(ctx context.Context, backend Backend, number ethrpc.BlockNumber) (*coretypes.ResultBlock, error) {
	var height *coretypes.Int64
	switch number {
	case ethrpc.LatestBlockNumber, ethrpc.SafeBlockNumber, ethrpc.FinalizedBlockNumber, ethrpc.PendingBlockNumber:
		// nil height resolves to the current committed block.
	default:
		h := coretypes.Int64(number.Int64())
		height = &h
	}
	block, err := backend.Block(ctx, &coretypes.RequestBlockInfo{Height: height})
	if errors.Is(err, coretypes.ErrHeightExceedsChainHead) ||
		errors.Is(err, coretypes.ErrZeroOrNegativeHeight) ||
		errors.Is(err, coretypes.ErrHeightNotAvailable) {
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
	blockHash := common.BytesToHash(block.BlockID.Hash)
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
		chainConfig, err = api.backend.EvmChainConfig()
		if err != nil {
			return nil, err
		}
	}
	transactions := make([]any, 0, len(block.Block.Txs))
	var gasUsed uint64
	for i := range block.Block.Txs {
		tx, stored, err := executedBlockTx(ctx, api.store, block, i)
		if err != nil {
			return nil, err
		}
		if stored == nil {
			continue
		}
		gasUsed = stored.CumulativeGasUsed
		if fullTx {
			result := export.NewRPCTransaction(tx, blockHash, uint64(number), blockUnix, uint64(len(transactions)), baseFee, chainConfig) //nolint:gosec // G115: number is a validated block height.
			replaceFrom(result, stored)
			transactions = append(transactions, result)
		} else {
			transactions = append(transactions, tx.Hash())
		}
	}

	result := map[string]any{
		"number":           (*hexutil.Big)(big.NewInt(number)),
		"hash":             blockHash,
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
		"size":             hexutil.Uint64(block.Block.Size()), //nolint:gosec // G115: block size is positive.
		"uncles":           []common.Hash{},                    // inapplicable to Sei
		"transactions":     transactions,
		"baseFeePerGas":    (*hexutil.Big)(baseFee),
	}
	if fullTx {
		result["totalDifficulty"] = (*hexutil.Big)(big.NewInt(0)) // inapplicable to Sei
	}
	return result, nil
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

// blockGasUsed returns the cumulative gas from the last receipt belonging to
// block, or zero when no transaction has a receipt at that height.
func blockGasUsed(ctx context.Context, store receiptpkg.ReceiptStore, block *coretypes.ResultBlock) (uint64, error) {
	// A trailing stale transaction may have no receipt or retain one from another
	// block. Walk backwards to the last transaction with a receipt for this block.
	// The total covers this lane's block; superblocks merging lanes would need a
	// combined total instead.
	for i := len(block.Block.Txs) - 1; i >= 0; i-- {
		_, stored, err := executedBlockTx(ctx, store, block, i)
		if err != nil {
			return 0, err
		}
		if stored != nil {
			return stored.CumulativeGasUsed, nil
		}
	}
	return 0, nil
}

// executedBlockTx returns the transaction and receipt for an executed block
// position, or a nil receipt when that occurrence was not executed.
func executedBlockTx(ctx context.Context, store receiptpkg.ReceiptStore, block *coretypes.ResultBlock, index int) (*ethtypes.Transaction, *evmtypes.Receipt, error) {
	number := block.Block.Height
	tx, err := decodeBlockTx(block.Block.Txs[index], number, index)
	if err != nil {
		return nil, nil, err
	}
	stored, err := receiptFor(ctx, store, tx.Hash())
	if err != nil {
		return nil, nil, fmt.Errorf("read transaction receipt at block %d index %d: %w", number, index, err)
	}
	// Matching the position also excludes replays of an earlier transaction in
	// this same block, which share both its hash and its receipt's block height.
	if stored == nil || stored.BlockNumber != uint64(number) || uint64(stored.TransactionIndex) != uint64(index) { //nolint:gosec // G115: block height and index are non-negative.
		return tx, nil, nil
	}
	return tx, stored, nil
}

// rpcTransactionIndex returns the transaction's position among executed block
// transactions. Stored receipt indices continue to address the raw proposal.
func rpcTransactionIndex(ctx context.Context, store receiptpkg.ReceiptStore, block *coretypes.ResultBlock, stored *evmtypes.Receipt) (uint32, error) {
	if uint64(stored.TransactionIndex) >= uint64(len(block.Block.Txs)) {
		return 0, fmt.Errorf("receipt transaction index %d exceeds block %d transaction count %d",
			stored.TransactionIndex, stored.BlockNumber, len(block.Block.Txs))
	}
	var index uint32
	for i := 0; uint64(i) < uint64(stored.TransactionIndex); i++ { //nolint:gosec // G115: i is non-negative.
		_, previous, err := executedBlockTx(ctx, store, block, i)
		if err != nil {
			return 0, err
		}
		if previous != nil {
			index++
		}
	}
	return index, nil
}
