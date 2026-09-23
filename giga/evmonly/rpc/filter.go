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

// Config bounds the queries the EVM-only RPC server accepts.
type Config struct {
	// MaxBlocksForLogs is the widest inclusive block range one eth_getLogs
	// query may cover.
	MaxBlocksForLogs uint64
	// MaxLogsPerQuery is the most logs one eth_getLogs query may return.
	MaxLogsPerQuery uint64
}

// DefaultConfig returns the query bounds used when an operator sets none.
func DefaultConfig() Config {
	return Config{MaxBlocksForLogs: 2000, MaxLogsPerQuery: 10000}
}

var (
	errLogRangeTooWide    = errors.New("eth_getLogs block range exceeds the configured maximum")
	errLogRangeInverted   = errors.New("eth_getLogs fromBlock is after toBlock")
	errLogRangePruned     = errors.New("eth_getLogs block is pruned")
	errLogRangeNotIndexed = errors.New("eth_getLogs block is not yet indexed")
)

type filterAPI struct {
	backend Backend
	store   receiptpkg.ReceiptStore
	config  Config
}

// GetLogs returns the logs matching crit from finalized blocks. Open and tag
// bounds resolve to the latest indexed block; an explicit bound outside the
// receipt store's indexed heights is an error rather than a partial answer, so
// a caller never mistakes a lagging or pruned store for an empty range.
func (api *filterAPI) GetLogs(ctx context.Context, crit filters.FilterCriteria) ([]*ethtypes.Log, error) {
	fromBlock, toBlock, err := api.resolveLogRange(ctx, crit)
	if err != nil {
		return nil, err
	}
	if toBlock-fromBlock+1 > api.config.MaxBlocksForLogs {
		return nil, fmt.Errorf("%w: %d blocks requested, %d allowed", errLogRangeTooWide, toBlock-fromBlock+1, api.config.MaxBlocksForLogs)
	}

	maxLogs := min(api.config.MaxLogsPerQuery, math.MaxInt64)
	budget := receiptpkg.NewLogBudget(int64(maxLogs), receiptpkg.DefaultMaxLogBytes) //nolint:gosec // clamped above
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
// range within the heights the receipt store has indexed.
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
		earliest, latest := api.indexedRange()
		return height, height, checkIndexed(height, height, earliest, latest)
	}

	earliest, latest := api.indexedRange()
	fromBlock, err := resolveLogBound(crit.FromBlock, earliest, latest)
	if err != nil {
		return 0, 0, err
	}
	toBlock, err := resolveLogBound(crit.ToBlock, earliest, latest)
	if err != nil {
		return 0, 0, err
	}
	if fromBlock > toBlock {
		return 0, 0, errLogRangeInverted
	}
	return fromBlock, toBlock, checkIndexed(fromBlock, toBlock, earliest, latest)
}

// indexedRange returns the inclusive height range the receipt store can
// answer for: from its retention floor to the lower of the committed head and
// the last block whose receipts it has indexed.
func (api *filterAPI) indexedRange() (uint64, uint64) {
	latest := api.backend.EvmBlockNumber()
	if stored := api.store.LatestVersion(); stored >= 0 && uint64(stored) < latest { //nolint:gosec // stored is non-negative
		latest = uint64(stored) //nolint:gosec // stored is non-negative
	}
	var earliest uint64
	if stored := api.store.EarliestVersion(); stored > 0 {
		earliest = uint64(stored) //nolint:gosec // stored is positive
	}
	return earliest, latest
}

// checkIndexed reports whether [fromBlock, toBlock] lies within
// [earliest, latest], naming the offending bound otherwise.
func checkIndexed(fromBlock, toBlock, earliest, latest uint64) error {
	if fromBlock < earliest {
		return fmt.Errorf("%w: block %d; earliest available block is %d", errLogRangePruned, fromBlock, earliest)
	}
	if toBlock > latest {
		return fmt.Errorf("%w: block %d; latest indexed block is %d", errLogRangeNotIndexed, toBlock, latest)
	}
	return nil
}

// resolveLogBound maps a filter bound to a height: nil and the head tags mean
// the latest indexed block, and the earliest tag (which decodes to 0) means
// the retention floor. Other explicit numbers pass through so the caller can
// check them against the indexed range.
func resolveLogBound(bound *big.Int, earliest, latest uint64) (uint64, error) {
	if bound == nil || bound.Sign() < 0 {
		return latest, nil
	}
	if !bound.IsUint64() || bound.Uint64() > math.MaxInt64 {
		return 0, fmt.Errorf("eth_getLogs block number %s exceeds int64", bound)
	}
	if bound.Int64() == ethrpc.EarliestBlockNumber.Int64() {
		return earliest, nil
	}
	return bound.Uint64(), nil
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
