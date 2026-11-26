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
	"github.com/sei-protocol/sei-chain/giga/evmonly/rpc/rpcerr"
	receiptdb "github.com/sei-protocol/sei-chain/sei-db/receipt_db"
	"github.com/sei-protocol/sei-chain/sei-db/receipt_db/logfilter"
	tmbytes "github.com/sei-protocol/sei-chain/sei-tendermint/libs/bytes"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
)

// These mirror the legacy evmrpc defaults for evm.max_blocks_for_log and
// evm.max_log_no_block (evmrpc/config); the EVM-only RPC has no config of its
// own yet, so keep the two in step when changing either.
const (
	// maxBlocksForLogs is the widest inclusive block range one eth_getLogs
	// query may cover.
	maxBlocksForLogs = 2000
	// maxLogsPerQuery is the most logs one eth_getLogs query may return.
	maxLogsPerQuery = 10000
)

var (
	errLogRangeTooWide    = fmt.Errorf("eth_getLogs block range exceeds %d blocks", maxBlocksForLogs)
	errLogRangeInverted   = errors.New("eth_getLogs fromBlock is after toBlock")
	errLogRangePruned     = errors.New("eth_getLogs block is pruned")
	errLogRangeNotIndexed = errors.New("eth_getLogs block is not yet indexed")
	// errUnknownBlock is go-ethereum's error for a blockHash filter naming a block it does not have.
	errUnknownBlock = errors.New("unknown block")
)

type filterAPI struct {
	backend Backend
	store   receiptdb.ReceiptStore
	filters *filterRegistry
	// maxBlockFilterHashes caps the unpolled blocks a block filter may fall behind by; zero
	// disables the cap.
	maxBlockFilterHashes uint64
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
	if toBlock-fromBlock+1 > maxBlocksForLogs {
		return nil, rpcerr.LimitExceeded(errLogRangeTooWide)
	}
	return api.rangeLogs(ctx, crit, fromBlock, toBlock)
}

// rangeLogs returns the logs crit's addresses and topics match in the indexed blocks fromBlock
// through toBlock, within the per-query log caps.
func (api *filterAPI) rangeLogs(ctx context.Context, crit filters.FilterCriteria, fromBlock, toBlock uint64) ([]*ethtypes.Log, error) {
	budget := logfilter.NewLogBudget(maxLogsPerQuery, logfilter.DefaultMaxLogBytes)
	logs, err := api.store.FilterLogs(receiptContext(logfilter.WithStrictTopicCount(ctx)), fromBlock, toBlock, crit, budget)
	if err != nil {
		return nil, fmt.Errorf("filter logs: %w", err)
	}
	// The store clamps to the retention floor it sees at read time. A floor
	// that rose since resolveLogRange means the answer covers less than asked.
	if floor := api.store.EarliestVersion(); floor > 0 && fromBlock < uint64(floor) { //nolint:gosec // floor is positive
		return nil, rpcerr.ResourceUnavailable(fmt.Errorf("%w: block %d; earliest available block is %d", errLogRangePruned, fromBlock, floor))
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
			return 0, 0, rpcerr.ResourceNotFound(errUnknownBlock)
		}
		height := uint64(block.Block.Height) //nolint:gosec // block heights are positive
		earliest, latest := indexedRange(ctx, api.backend, api.store)
		// A block the node holds but has not executed yet is above the committed head; it is not
		// yet indexed rather than out of range.
		head := max(height, committedHead(ctx, api.backend))
		return height, height, checkIndexed(height, height, earliest, latest, head)
	}

	earliest, latest := indexedRange(ctx, api.backend, api.store)
	head := committedHead(ctx, api.backend)
	fromBlock, _, err := resolveLogBound(crit.FromBlock, earliest, latest)
	if err != nil {
		return 0, 0, err
	}
	toBlock, toKind, err := resolveLogBound(crit.ToBlock, earliest, latest)
	if err != nil {
		return 0, 0, err
	}
	if fromBlock > toBlock {
		return 0, 0, invertedRangeError(fromBlock, latest, head, toKind == headBlock)
	}
	return fromBlock, toBlock, checkIndexed(fromBlock, toBlock, earliest, latest, head)
}

// invertedRangeError returns the error for a fromBlock above toBlock: a resource unavailable error
// when toBlock follows the indexed head and fromBlock is committed but not yet indexed, and an
// invalid params error otherwise.
func invertedRangeError(fromBlock, latest, head uint64, toFollowsHead bool) error {
	if toFollowsHead && fromBlock <= head {
		return rpcerr.ResourceUnavailable(notIndexedError(fromBlock, latest))
	}
	return rpcerr.InvalidParams(errLogRangeInverted)
}

// indexedRange returns the inclusive height range the receipt store can
// answer for: from its retention floor to the lower of the committed head and
// the last block whose receipts it has indexed.
func indexedRange(ctx context.Context, backend Backend, store receiptdb.ReceiptStore) (uint64, uint64) {
	latest := committedHead(ctx, backend)
	if stored := store.LatestVersion(); stored >= 0 && uint64(stored) < latest { //nolint:gosec // stored is non-negative
		latest = uint64(stored) //nolint:gosec // stored is non-negative
	}
	earliest := uint64(earliestRetainedHeight(backend, store)) //nolint:gosec // never below the first block
	return earliest, latest
}

// checkIndexed reports whether [fromBlock, toBlock] lies within [earliest, latest], naming the
// offending bound otherwise. A pruned fromBlock and a toBlock not yet indexed are a resource
// unavailable error, and a toBlock above head is an invalid params error.
func checkIndexed(fromBlock, toBlock, earliest, latest, head uint64) error {
	if fromBlock < earliest {
		return rpcerr.ResourceUnavailable(fmt.Errorf("%w: block %d; earliest available block is %d", errLogRangePruned, fromBlock, earliest))
	}
	if toBlock > latest {
		if toBlock > head {
			return rpcerr.InvalidParams(notIndexedError(toBlock, latest))
		}
		return rpcerr.ResourceUnavailable(notIndexedError(toBlock, latest))
	}
	return nil
}

// notIndexedError reports block as past latest, the last block the receipt store has indexed.
func notIndexedError(block, latest uint64) error {
	return fmt.Errorf("%w: block %d; latest indexed block is %d", errLogRangeNotIndexed, block, latest)
}

// resolveLogBound maps a filter bound to a height, and returns what the bound names: an omitted
// bound or a head tag is latest, earliest and 0x0 are the retention floor, and a height passes
// through.
func resolveLogBound(bound *big.Int, earliest, latest uint64) (uint64, blockNumberKind, error) {
	kind, err := classifyFilterBound(bound)
	if err != nil {
		return 0, 0, err
	}
	switch kind {
	case headBlock:
		return latest, kind, nil
	case earliestBlock:
		return earliest, kind, nil
	}
	return bound.Uint64(), kind, nil
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
