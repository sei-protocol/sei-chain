package rpc

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/big"
	"sort"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	ethrpc "github.com/ethereum/go-ethereum/rpc"

	receiptpkg "github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	tmbytes "github.com/sei-protocol/sei-chain/sei-tendermint/libs/bytes"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
)

const (
	// maxBlocksForLogs is the widest inclusive block range one eth_getLogs
	// query may cover.
	maxBlocksForLogs = 2000
	// maxLogsPerQuery is the most logs one eth_getLogs query may return.
	maxLogsPerQuery = 10000
)

var (
	errLogRangeTooWide  = fmt.Errorf("eth_getLogs block range exceeds %d blocks", maxBlocksForLogs)
	errLogRangeInverted = errors.New("eth_getLogs fromBlock is after toBlock")
)

type filterAPI struct {
	backend Backend
	store   receiptpkg.ReceiptStore
}

// GetLogs returns the logs matching crit from finalized blocks. The range form
// clamps fromBlock/toBlock to the receipt store's retained heights; the
// blockHash form errors when the block is unknown.
func (api *filterAPI) GetLogs(ctx context.Context, crit filters.FilterCriteria) ([]*ethtypes.Log, error) {
	fromBlock, toBlock, err := api.resolveLogRange(ctx, crit)
	if err != nil {
		return nil, err
	}
	if fromBlock > toBlock {
		return []*ethtypes.Log{}, nil
	}
	if toBlock-fromBlock+1 > maxBlocksForLogs {
		return nil, errLogRangeTooWide
	}

	budget := receiptpkg.NewLogBudget(maxLogsPerQuery, receiptpkg.DefaultMaxLogBytes)
	logs, err := api.store.FilterLogs(receiptContext(ctx), fromBlock, toBlock, crit, budget)
	if err != nil {
		return nil, fmt.Errorf("filter logs: %w", err)
	}
	if err := api.normalizeLogs(ctx, logs); err != nil {
		return nil, err
	}
	if logs == nil {
		logs = []*ethtypes.Log{}
	}
	return logs, nil
}

// resolveLogRange turns crit into an inclusive [fromBlock, toBlock] height
// range, clamped to the heights the receipt store retains.
func (api *filterAPI) resolveLogRange(ctx context.Context, crit filters.FilterCriteria) (uint64, uint64, error) {
	if crit.BlockHash != nil {
		block, err := api.backend.BlockByHash(ctx, &coretypes.RequestBlockByHash{Hash: tmbytes.HexBytes(crit.BlockHash.Bytes())})
		if err != nil {
			return 0, 0, err
		}
		if block == nil || block.Block == nil {
			return 0, 0, fmt.Errorf("block %s not found", crit.BlockHash)
		}
		height := uint64(block.Block.Height) //nolint:gosec // block heights are positive
		return height, height, nil
	}

	latest := api.backend.EvmBlockNumber()
	if stored := api.store.LatestVersion(); stored >= 0 && uint64(stored) < latest { //nolint:gosec // stored is non-negative
		latest = uint64(stored) //nolint:gosec // stored is non-negative
	}
	fromBlock := resolveLogBound(crit.FromBlock, latest)
	toBlock := resolveLogBound(crit.ToBlock, latest)
	if crit.FromBlock != nil && crit.ToBlock != nil && fromBlock > toBlock {
		return 0, 0, errLogRangeInverted
	}
	if toBlock > latest {
		toBlock = latest
	}
	if earliest := api.store.EarliestVersion(); earliest > 0 && fromBlock < uint64(earliest) { //nolint:gosec // earliest is positive
		fromBlock = uint64(earliest) //nolint:gosec // earliest is positive
	}
	return fromBlock, toBlock, nil
}

// resolveLogBound maps a filter bound to a height: nil and the head tags mean
// latest, earliest means genesis, and any other negative value means latest.
func resolveLogBound(bound *big.Int, latest uint64) uint64 {
	if bound == nil {
		return latest
	}
	if bound.Sign() < 0 {
		if bound.Int64() == ethrpc.EarliestBlockNumber.Int64() {
			return 0
		}
		return latest
	}
	if !bound.IsUint64() || bound.Uint64() > math.MaxInt64 {
		return math.MaxInt64
	}
	return bound.Uint64()
}

// normalizeLogs completes the fields the receipt store cannot fill: BlockHash
// from the finalized block, and Index rebased to the block-wide position that
// eth_getTransactionReceipt reports for the same log.
func (api *filterAPI) normalizeLogs(ctx context.Context, logs []*ethtypes.Log) error {
	blockHashes := make(map[uint64]common.Hash)
	firstLogIndexes := make(map[common.Hash]uint)
	for _, lg := range logs {
		if err := ctx.Err(); err != nil {
			return err
		}
		blockHash, ok := blockHashes[lg.BlockNumber]
		if !ok {
			var err error
			if blockHash, err = api.blockHash(ctx, lg.BlockNumber); err != nil {
				return err
			}
			blockHashes[lg.BlockNumber] = blockHash
		}
		lg.BlockHash = blockHash

		firstLogIndex, ok := firstLogIndexes[lg.TxHash]
		if !ok {
			var err error
			if firstLogIndex, err = api.firstLogIndex(ctx, lg.TxHash); err != nil {
				return err
			}
			firstLogIndexes[lg.TxHash] = firstLogIndex
		}
		if lg.Index < firstLogIndex {
			return fmt.Errorf("log index %d of transaction %s precedes its first log index %d", lg.Index, lg.TxHash, firstLogIndex)
		}
		lg.Index -= firstLogIndex
	}
	sort.SliceStable(logs, func(i, j int) bool {
		if logs[i].BlockNumber != logs[j].BlockNumber {
			return logs[i].BlockNumber < logs[j].BlockNumber
		}
		return logs[i].Index < logs[j].Index
	})
	return nil
}

func (api *filterAPI) blockHash(ctx context.Context, blockNumber uint64) (common.Hash, error) {
	if blockNumber > math.MaxInt64 {
		return common.Hash{}, fmt.Errorf("log block number %d exceeds int64", blockNumber)
	}
	height := coretypes.Int64(blockNumber)
	block, err := api.backend.Block(ctx, &coretypes.RequestBlockInfo{Height: &height})
	if err != nil {
		return common.Hash{}, fmt.Errorf("read log block %d: %w", blockNumber, err)
	}
	if block == nil || block.Block == nil {
		return common.Hash{}, fmt.Errorf("log block %d is not finalized", blockNumber)
	}
	return common.BytesToHash(block.BlockID.Hash), nil
}

// firstLogIndex returns the block-wide index of txHash's first log. Stored
// receipts carry block-wide log indexes, and the receipt store adds the same
// offset again when it materializes range-query logs, so this is the amount to
// subtract from each returned log's Index.
func (api *filterAPI) firstLogIndex(ctx context.Context, txHash common.Hash) (uint, error) {
	stored, err := api.store.GetReceipt(receiptContext(ctx), txHash)
	if err != nil {
		return 0, fmt.Errorf("read log receipt %s: %w", txHash, err)
	}
	if stored == nil || len(stored.Logs) == 0 || stored.Logs[0] == nil {
		return 0, fmt.Errorf("receipt %s has no logs", txHash)
	}
	return uint(stored.Logs[0].Index), nil
}
