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
	block, err := api.backend.BlockByHash(ctx, &coretypes.RequestBlockByHash{Hash: tmbytes.HexBytes(hash.Bytes())})
	if err != nil {
		return nil, err
	}
	if block == nil || block.Block == nil {
		return nil, nil
	}
	return api.encodeBlock(ctx, block, fullTx)
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

	txs := block.Block.Txs
	transactions := make([]any, len(txs))
	if fullTx {
		chainConfig, err := api.backend.EvmChainConfig()
		if err != nil {
			return nil, err
		}
		// One receipt read per transaction here, not just for the last one:
		// deferred pending a bulk receipt-load API on the receipt store.
		for i, raw := range txs {
			ethtx, err := decodeBlockTx(raw, number, i)
			if err != nil {
				return nil, err
			}
			stored, err := receiptFor(ctx, api.store, ethtx.Hash())
			if err != nil {
				return nil, fmt.Errorf("read transaction receipt at block %d index %d: %w", number, i, err)
			}
			result := export.NewRPCTransaction(ethtx, blockHash, uint64(number), blockUnix, uint64(i), baseFee, chainConfig) //nolint:gosec // G115: number is a validated block height.
			if stored != nil {
				replaceFrom(result, stored)
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

// blockGasUsed returns the gas used by block: its stored BlockStats when the store
// has them, otherwise the total recomputed from the receipts that belong to the
// block at their original index. It is zero when the store does not cover block.
func blockGasUsed(ctx context.Context, store receiptpkg.ReceiptStore, block *coretypes.ResultBlock) (uint64, error) {
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
	if errors.Is(err, receiptpkg.ErrNotFound) {
		// Pruned between the version check and the read.
		return 0, nil
	}
	if !errors.Is(err, receiptpkg.ErrBlockStatsNotSupported) {
		return 0, fmt.Errorf("read block stats for block %d: %w", height, err)
	}
	records, err := receiptRecordsFromIterator(ctx, store, height)
	if errors.Is(err, receiptpkg.ErrRangeQueryNotSupported) {
		records, err = receiptRecordsFromBlock(ctx, store, block)
	}
	if err != nil {
		return 0, err
	}
	return receiptpkg.ComputeBlockStats(records, nil).TotalGasUsed, nil
}
