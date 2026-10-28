package rpc

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	ethrpc "github.com/ethereum/go-ethereum/rpc"

	"github.com/sei-protocol/sei-chain/evmrpc"
	receiptpkg "github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
)

// NewFilter installs a log filter for crit and returns its ID. Its polls start at a numeric
// fromBlock, where earliest is block 0, and otherwise at the block after the latest indexed one.
func (api *filterAPI) NewFilter(_ context.Context, crit filters.FilterCriteria) (ethrpc.ID, error) {
	cursor, err := api.pollStart(crit)
	if err != nil {
		return "", err
	}
	return api.filters.add(&pollFilter{crit: &crit, cursor: cursor})
}

// NewBlockFilter installs a filter whose polls return the hashes of the blocks committed since
// the previous poll, and returns its ID.
func (api *filterAPI) NewBlockFilter(context.Context) (ethrpc.ID, error) {
	return api.filters.add(&pollFilter{cursor: api.backend.EvmBlockNumber() + 1})
}

// NewPendingTransactionFilter returns the regular RPC's not-supported error.
func (api *filterAPI) NewPendingTransactionFilter(context.Context, *bool) (ethrpc.ID, error) {
	return "", &evmrpc.ErrEVMNotSupported{Msg: "eth_newPendingTransactionFilter is not supported on Sei EVM RPC"}
}

// GetFilterChanges returns what filter id matched since its previous poll: block hashes for a
// block filter, logs for a log filter. A block filter that fell more than maxBlockFilterHashes
// blocks behind is removed instead.
func (api *filterAPI) GetFilterChanges(ctx context.Context, id ethrpc.ID) (any, error) {
	f, err := api.filters.get(id)
	if err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.crit == nil {
		hashes, next, err := api.pollBlockHashes(ctx, f.cursor)
		if errors.Is(err, errFilterNotFound) {
			api.filters.remove(id)
		}
		f.cursor = next
		return hashes, err
	}
	logs, next, err := api.pollLogs(ctx, *f.crit, f.cursor)
	f.cursor = next
	return logs, err
}

// GetFilterLogs returns every log log filter id matches, resolving its tags as eth_getLogs does,
// without moving the filter's poll cursor.
func (api *filterAPI) GetFilterLogs(ctx context.Context, id ethrpc.ID) ([]*ethtypes.Log, error) {
	f, err := api.filters.get(id)
	if err != nil {
		return nil, err
	}
	if f.crit == nil {
		return nil, errFilterNotFound
	}
	return api.GetLogs(ctx, *f.crit)
}

// UninstallFilter removes filter id, reporting whether it was installed.
func (api *filterAPI) UninstallFilter(_ context.Context, id ethrpc.ID) bool {
	return api.filters.remove(id)
}

// pollStart returns the first block a log filter for crit polls: 0 for a blockHash filter, a
// numeric fromBlock as given, and otherwise the block after the latest indexed one. It rejects a
// numeric fromBlock above 0 below the earliest indexed block, and a numeric toBlock below the
// start.
func (api *filterAPI) pollStart(crit filters.FilterCriteria) (uint64, error) {
	for _, bound := range []*big.Int{crit.FromBlock, crit.ToBlock} {
		if bound != nil && !bound.IsInt64() {
			return 0, &invalidParamsError{error: fmt.Errorf("filter block number %s exceeds int64", bound)}
		}
	}
	if crit.BlockHash != nil {
		return 0, nil
	}
	earliest, latest := indexedRange(api.backend, api.store)
	start := latest + 1
	if from, ok := numericBound(crit.FromBlock); ok {
		if from > 0 && from < earliest {
			return 0, fmt.Errorf("%w: fromBlock %d; earliest available block is %d", errLogRangePruned, from, earliest)
		}
		start = from
	}
	if to, ok := numericBound(crit.ToBlock); ok && to < start {
		return 0, &invalidParamsError{error: fmt.Errorf("toBlock %d is before the filter's start block %d", to, start)}
	}
	return start, nil
}

// pollLogs returns the logs crit matches from cursor on, and the cursor the next poll starts
// from, also when it returns an error. A poll scans from cursor to the latest indexed block, or to
// a numeric toBlock below it, and fewer blocks when that range exceeds the block or log caps. A
// cursor of 0 scans from the earliest indexed block. A cursor above 0 below that block fails the
// poll naming the pruned blocks, and the returned cursor is the earliest block. A block whose
// logs alone exceed the log caps fails the poll that reaches it, and the returned cursor is past
// it. A blockHash filter's block is returned by the poll at cursor 0.
func (api *filterAPI) pollLogs(ctx context.Context, crit filters.FilterCriteria, cursor uint64) ([]*ethtypes.Log, uint64, error) {
	if logsExhausted(crit, cursor) {
		return []*ethtypes.Log{}, cursor, nil
	}
	if crit.BlockHash != nil {
		logs, err := api.GetLogs(ctx, crit)
		if exceedsLogCaps(err) {
			return nil, cursor + 1, err
		}
		if err != nil {
			return nil, cursor, err
		}
		return logs, cursor + 1, nil
	}
	earliest, latest := indexedRange(api.backend, api.store)
	if cursor < earliest {
		// Pruned blocks cannot be served on a later poll either, so the filter moves on. A filter
		// from block 0 asked for whatever the node has, so only one that lost blocks is told.
		if cursor > 0 {
			return nil, earliest, prunedLogsError(crit, cursor, earliest)
		}
		cursor = earliest
	}
	from, to, ok := pollWindow(crit.ToBlock, cursor, latest)
	if !ok {
		return []*ethtypes.Log{}, cursor, nil
	}
	logs, end, err := api.logsWithBackoff(ctx, crit, from, to)
	var oversized *oversizedBlockError
	if errors.As(err, &oversized) {
		return nil, oversized.height + 1, err
	}
	if err != nil {
		return nil, cursor, err
	}
	return logs, end + 1, nil
}

// pollBlockHashes returns the hashes of the stored blocks from cursor to the latest committed
// one, and the cursor the next poll starts from. It returns errFilterNotFound when that is more
// than maxBlockFilterHashes blocks.
func (api *filterAPI) pollBlockHashes(ctx context.Context, cursor uint64) ([]common.Hash, uint64, error) {
	latest := api.backend.EvmBlockNumber()
	if cursor > latest {
		return []common.Hash{}, cursor, nil
	}
	if api.maxBlockFilterHashes > 0 && latest-cursor+1 > api.maxBlockFilterHashes {
		return nil, cursor, fmt.Errorf("%w: it was not polled for more than %d blocks", errFilterNotFound, api.maxBlockFilterHashes)
	}
	hashes := make([]common.Hash, 0, latest-cursor+1)
	for height := cursor; height <= latest; height++ {
		hash, err := api.blockHash(ctx, height)
		if errors.Is(err, coretypes.ErrHeightExceedsChainHead) {
			return hashes, height, nil
		}
		if err != nil {
			return nil, height, err
		}
		hashes = append(hashes, hash)
	}
	return hashes, latest + 1, nil
}

// logsWithBackoff returns the logs crit matches in the blocks from through to, and the last block
// they cover. When those logs exceed the log caps it halves the range until they fit, and fails
// with an oversizedBlockError when block from alone exceeds them.
func (api *filterAPI) logsWithBackoff(ctx context.Context, crit filters.FilterCriteria, from, to uint64) ([]*ethtypes.Log, uint64, error) {
	for {
		logs, err := api.rangeLogs(ctx, crit, from, to)
		if err == nil {
			return logs, to, nil
		}
		if !exceedsLogCaps(err) {
			return nil, 0, err
		}
		if from == to {
			return nil, 0, &oversizedBlockError{height: from, err: err}
		}
		to = from + (to-from)/2
	}
}

// pollWindow returns the blocks a log filter's poll scans: from cursor to latest, or to a numeric
// toBlock below it, and at most maxBlocksForLogs blocks. ok is false when the range is empty.
func pollWindow(toBlock *big.Int, cursor, latest uint64) (from, to uint64, ok bool) {
	to = latest
	if bound, isNumber := numericBound(toBlock); isNumber {
		to = min(to, bound)
	}
	if cursor > to {
		return 0, 0, false
	}
	if to-cursor >= maxBlocksForLogs {
		to = cursor + maxBlocksForLogs - 1
	}
	return cursor, to, true
}

// logsExhausted reports whether no poll from cursor on can match a log for crit: cursor is past
// its numeric toBlock, or its blockHash block has been polled. A tag toBlock follows the head and
// is never passed.
func logsExhausted(crit filters.FilterCriteria, cursor uint64) bool {
	if crit.BlockHash != nil {
		return cursor > 0
	}
	to, ok := numericBound(crit.ToBlock)
	return ok && cursor > to
}

// prunedLogsError reports the blocks from cursor up to earliest, or up to crit's numeric toBlock
// when it is lower, whose logs were pruned before a filter polled them.
func prunedLogsError(crit filters.FilterCriteria, cursor, earliest uint64) error {
	last := earliest - 1
	if to, ok := numericBound(crit.ToBlock); ok {
		last = min(last, to)
	}
	return fmt.Errorf("%w: logs of blocks %d to %d were pruned before the filter read them", errLogRangePruned, cursor, last)
}

// numericBound returns a filter bound that is a block number rather than a tag.
func numericBound(bound *big.Int) (uint64, bool) {
	if bound == nil || bound.Sign() < 0 {
		return 0, false
	}
	return bound.Uint64(), true
}

// exceedsLogCaps reports whether err is a query failing on the log count or byte caps.
func exceedsLogCaps(err error) bool {
	return errors.Is(err, receiptpkg.ErrTooManyLogs) || errors.Is(err, receiptpkg.ErrTooManyLogBytes)
}

// oversizedBlockError reports a block whose logs matching a filter alone exceed the log caps.
type oversizedBlockError struct {
	height uint64
	err    error
}

func (e *oversizedBlockError) Error() string {
	return fmt.Sprintf("logs of block %d skipped: %v", e.height, e.err)
}

func (e *oversizedBlockError) Unwrap() error { return e.err }
