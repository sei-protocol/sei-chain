package rpc

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	gmath "github.com/ethereum/go-ethereum/common/math"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	ethrpc "github.com/ethereum/go-ethereum/rpc"

	"github.com/sei-protocol/sei-chain/evmrpc"
	receiptpkg "github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

// maxFeeHistoryBlockCount caps a single eth_feeHistory request, matching
// go-ethereum's own default block-count cap.
const maxFeeHistoryBlockCount = 1024

// earliestCommittedHeight is what "earliest" resolves to for eth_feeHistory: a fixed genesis of
// 1, not a runtime read — wrong for an Autobahn-migrated shard (see autobahn/types.Epoch.FirstBlock).
const earliestCommittedHeight = int64(1)

// gasPriceSuggestionNumerator and gasPriceSuggestionDenominator scale the admission floor up for
// eth_gasPrice's suggestion.
const (
	gasPriceSuggestionNumerator   = 110
	gasPriceSuggestionDenominator = 100
)

type infoAPI struct {
	backend Backend
	store   receiptpkg.ReceiptStore
}

// BlockNumber returns the height of the most recently committed block.
func (api *infoAPI) BlockNumber(_ context.Context) hexutil.Uint64 {
	return hexutil.Uint64(api.backend.EvmBlockNumber())
}

// ChainId returns the EVM chain ID this Autobahn shard is configured for.
//
//nolint:revive // matches the go-ethereum RPC method name eth_chainId.
func (api *infoAPI) ChainId(_ context.Context) *hexutil.Big {
	return (*hexutil.Big)(new(big.Int).SetUint64(api.backend.EvmChainID()))
}

// Syncing implements eth_syncing, matching v2: sync semantics are not exposed on this API.
func (api *infoAPI) Syncing(_ context.Context) (any, error) {
	return nil, &evmrpc.ErrEVMNotSupported{Msg: "eth_syncing is not supported on Sei EVM RPC"}
}

// gasPriceCongestionThresholdPercent is the gasUsedRatio above which GasPrice escalates to the
// congested-chain reward, matching v2's eth_gasPrice.
const gasPriceCongestionThresholdPercent = 80

// gasPriceCongestionPercentile is the reward percentile GasPrice escalates to once the chain is
// congested, matching v2's eth_gasPrice (evmrpc.InfoAPI.gasPriceHelper).
const gasPriceCongestionPercentile = 50

const defaultPriorityFeePerGas = 1_000_000_000

// GasPrice returns a suggested gas price, matching v2's eth_gasPrice: a margin over the
// admission floor, or the latest congested block's median reward when that's higher and available.
func (api *infoAPI) GasPrice(ctx context.Context) (*hexutil.Big, error) {
	floor, err := api.backend.EvmMinGasPrice()
	if err != nil {
		return nil, err
	}
	margin := suggestedGasPrice(floor)
	if reward, ok := api.congestionReward(ctx); ok && reward.Cmp(margin) >= 0 {
		return (*hexutil.Big)(reward), nil
	}
	return (*hexutil.Big)(margin), nil
}

// congestionReward answers GasPrice's escalated suggestion: the latest block's median reward, but
// only once its gasUsedRatio exceeds gasPriceCongestionThresholdPercent.
func (api *infoAPI) congestionReward(ctx context.Context) (*big.Int, bool) {
	current := api.store.LatestVersion()
	if current <= 0 {
		return nil, false
	}
	gasLimit, err := api.backend.EvmGasLimit()
	if err != nil || gasLimit == 0 {
		return nil, false
	}
	stats, err := api.store.GetBlockStats(receiptContext(ctx), uint64(current)) //nolint:gosec // G115: current is positive here.
	if err != nil {
		return nil, false
	}
	if stats.TotalGasUsed <= gasLimit*gasPriceCongestionThresholdPercent/100 {
		return nil, false
	}
	reward, ok := stats.RewardAt(gasPriceCongestionPercentile)
	if !ok {
		return nil, false
	}
	return new(big.Int).SetUint64(reward), true
}

// MaxPriorityFeePerGas returns the suggested priority fee for the latest block.
func (api *infoAPI) MaxPriorityFeePerGas(ctx context.Context) (*hexutil.Big, error) {
	current := api.store.LatestVersion()
	if current <= 0 {
		return (*hexutil.Big)(big.NewInt(defaultPriorityFeePerGas)), nil
	}
	gasLimit, err := api.backend.EvmGasLimit()
	if err != nil {
		return nil, err
	}
	if gasLimit == 0 {
		return (*hexutil.Big)(big.NewInt(defaultPriorityFeePerGas)), nil
	}
	stats, err := api.blockStatsForHeight(ctx, current, []float64{gasPriceCongestionPercentile})
	if err != nil {
		return nil, fmt.Errorf("read block stats for priority fee: %w", err)
	}
	if stats.TotalGasUsed <= gasLimit*gasPriceCongestionThresholdPercent/100 {
		return (*hexutil.Big)(big.NewInt(defaultPriorityFeePerGas)), nil
	}
	reward, _ := stats.RewardAt(gasPriceCongestionPercentile)
	return (*hexutil.Big)(new(big.Int).SetUint64(reward)), nil
}

// suggestedGasPrice scales floor up by the gas-price suggestion margin,
// returning a new *big.Int the caller owns.
func suggestedGasPrice(floor *big.Int) *big.Int {
	price := new(big.Int).Mul(floor, big.NewInt(gasPriceSuggestionNumerator))
	return price.Div(price, big.NewInt(gasPriceSuggestionDenominator))
}

// FeeHistoryResult is the eth_feeHistory response, matching go-ethereum's
// wire shape.
type FeeHistoryResult struct {
	OldestBlock  *hexutil.Big     `json:"oldestBlock"`
	Reward       [][]*hexutil.Big `json:"reward,omitempty"`
	BaseFee      []*hexutil.Big   `json:"baseFeePerGas,omitempty"`
	GasUsedRatio []float64        `json:"gasUsedRatio"`
}

// emptyFeeHistoryResult is the "no retrievable blocks" response: oldestBlock 0 and an empty
// gasUsedRatio.
func emptyFeeHistoryResult() *FeeHistoryResult {
	return &FeeHistoryResult{OldestBlock: (*hexutil.Big)(new(big.Int)), GasUsedRatio: []float64{}}
}

// FeeHistory returns gas-used ratios, base fees, and reward percentiles for the blockCount blocks
// ending at lastBlock. baseFeePerGas is always zero; see rewardRow for reward.
func (api *infoAPI) FeeHistory(ctx context.Context, blockCount gmath.HexOrDecimal64, lastBlock ethrpc.BlockNumber, rewardPercentiles []float64) (*FeeHistoryResult, error) {
	if blockCount < 1 {
		return emptyFeeHistoryResult(), nil
	}
	if blockCount > maxFeeHistoryBlockCount {
		blockCount = maxFeeHistoryBlockCount
	}
	if err := validateRewardPercentiles(rewardPercentiles); err != nil {
		return nil, err
	}

	end, err := api.resolveEndHeight(lastBlock)
	if err != nil {
		return nil, err
	}

	// gasLimit is applied to every block in the range, not looked up per height.
	gasLimit, err := api.backend.EvmGasLimit()
	if err != nil {
		return nil, err
	}

	return api.walkFeeHistoryRange(ctx, end, int64(blockCount), gasLimit, rewardPercentiles)
}

// resolveEndHeight turns lastBlock's tag or explicit height into a concrete, committed height.
func (api *infoAPI) resolveEndHeight(lastBlock ethrpc.BlockNumber) (int64, error) {
	current := api.store.LatestVersion()
	switch lastBlock {
	case ethrpc.SafeBlockNumber, ethrpc.FinalizedBlockNumber, ethrpc.LatestBlockNumber, ethrpc.PendingBlockNumber:
		if current <= 0 {
			return 0, errors.New("no committed block available for fee history")
		}
		return current, nil
	case ethrpc.EarliestBlockNumber:
		// EarliestVersion is 0 until something has pruned the store, in which case
		// earliestCommittedHeight (this deployment's genesis) is the true earliest.
		return max(earliestCommittedHeight, api.store.EarliestVersion()), nil
	default:
		if lastBlock < 0 {
			return 0, fmt.Errorf("requested last block %d is not available", lastBlock)
		}
		if int64(lastBlock) > current {
			return 0, fmt.Errorf("requested last block %d is not yet available; latest is %d", lastBlock, current)
		}
		return int64(lastBlock), nil
	}
}

// walkFeeHistoryRange collects gasUsedRatio, baseFee, and reward entries for the blockCount
// blocks ending at end, oldest first, skipping heights below 1 or pruned below the retention floor.
func (api *infoAPI) walkFeeHistoryRange(ctx context.Context, end, blockCount int64, gasLimit uint64, rewardPercentiles []float64) (*FeeHistoryResult, error) {
	result := &FeeHistoryResult{GasUsedRatio: []float64{}}
	// lastGoodResult is the most recent contiguous run completed before a hole, returned if
	// nothing follows the hole.
	var lastGoodResult *FeeHistoryResult
	start := end - blockCount + 1
	for height := start; height <= end; height++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if height < 1 {
			continue
		}
		stats, err := api.blockStatsForHeight(ctx, height, rewardPercentiles)
		if err != nil {
			if errors.Is(err, receiptpkg.ErrNotFound) {
				if result.OldestBlock != nil {
					// A hole after rows are emitted restarts the accumulation, keeping the
					// result a contiguous run.
					lastGoodResult = result
					result = &FeeHistoryResult{GasUsedRatio: []float64{}}
				}
				continue
			}
			return nil, fmt.Errorf("read block stats for block %d: %w", height, err)
		}
		if result.OldestBlock == nil {
			result.OldestBlock = (*hexutil.Big)(big.NewInt(height))
		}
		result.GasUsedRatio = append(result.GasUsedRatio, gasUsedRatio(stats.TotalGasUsed, gasLimit))
		result.BaseFee = append(result.BaseFee, (*hexutil.Big)(new(big.Int)))
		if len(rewardPercentiles) > 0 {
			result.Reward = append(result.Reward, rewardRow(stats, rewardPercentiles))
		}
	}
	if result.OldestBlock == nil {
		// Nothing followed the last hole either: fall back to the contiguous run before it.
		if lastGoodResult != nil {
			result = lastGoodResult
		} else {
			// end resolved successfully just before this call; only a store eviction racing that
			// resolution reaches here.
			return nil, fmt.Errorf("block %d is no longer available", end)
		}
	}
	// baseFeePerGas carries one more entry than gasUsedRatio: the projected fee for the block
	// after end. Always zero here.
	result.BaseFee = append(result.BaseFee, (*hexutil.Big)(new(big.Int)))
	return result, nil
}

// gasUsedRatio divides totalGasUsed by gasLimit to 4 decimal places: multiply by 10000, integer
// divide, then divide back down, which preserves more precision than a bare float division.
func gasUsedRatio(totalGasUsed, gasLimit uint64) float64 {
	if gasLimit == 0 {
		return 0
	}
	ratioInt := (totalGasUsed * 10000) / gasLimit
	return float64(ratioInt) / 10000.0
}

// blockStatsForHeight answers height's BlockStats: cached when it covers every requested
// percentile, otherwise recomputed from receipts (including on ErrBlockStatsNotSupported).
func (api *infoAPI) blockStatsForHeight(ctx context.Context, height int64, rewardPercentiles []float64) (receiptpkg.BlockStats, error) {
	stats, err := api.store.GetBlockStats(receiptContext(ctx), uint64(height)) //nolint:gosec // G115: height is positive here.
	switch {
	case err == nil && coversRewardPercentiles(stats, rewardPercentiles):
		return stats, nil
	case err != nil && !errors.Is(err, receiptpkg.ErrBlockStatsNotSupported):
		return receiptpkg.BlockStats{}, err
	}
	return api.recomputeBlockStats(ctx, height, rewardPercentiles)
}

// coversRewardPercentiles reports whether stats alone answers every one of rewardPercentiles: a
// block with no reward-eligible receipts always does, at zero.
func coversRewardPercentiles(stats receiptpkg.BlockStats, rewardPercentiles []float64) bool {
	if len(rewardPercentiles) == 0 || len(stats.RewardPercentiles) == 0 {
		return true
	}
	for _, p := range rewardPercentiles {
		if _, ok := stats.RewardAt(p); !ok {
			return false
		}
	}
	return true
}

// recomputeBlockStats answers height's BlockStats by reading its receipts directly and running
// receiptpkg.ComputeBlockStats over exactly rewardPercentiles.
func (api *infoAPI) recomputeBlockStats(ctx context.Context, height int64, rewardPercentiles []float64) (receiptpkg.BlockStats, error) {
	records, err := api.receiptRecordsForHeight(ctx, height)
	if err != nil {
		return receiptpkg.BlockStats{}, err
	}
	return receiptpkg.ComputeBlockStats(records, rewardPercentiles), nil
}

// receiptRecordsForHeight answers height's receipts via IterateReceipts when the store supports
// it — each one already scoped to height by the iterator's own BlockNumber, not trusted from a
// tx-hash lookup — falling back to decoding the block and fetching receipts by hash otherwise.
func (api *infoAPI) receiptRecordsForHeight(ctx context.Context, height int64) ([]receiptpkg.ReceiptRecord, error) {
	records, err := api.receiptRecordsFromIterator(ctx, height)
	if !errors.Is(err, receiptpkg.ErrRangeQueryNotSupported) {
		return records, err
	}
	return api.receiptRecordsFromBlock(ctx, height)
}

// receiptRecordsFromIterator answers height's receipts by walking IterateReceipts from height,
// stopping at the first receipt belonging to a later block (or immediately, for an empty block).
func (api *infoAPI) receiptRecordsFromIterator(ctx context.Context, height int64) ([]receiptpkg.ReceiptRecord, error) {
	it, err := api.store.IterateReceipts(uint64(height)) //nolint:gosec // G115: height is positive here.
	if err != nil {
		return nil, err
	}
	defer func() { _ = it.Close() }()
	var records []receiptpkg.ReceiptRecord
	for {
		ok, err := it.Next()
		if err != nil {
			return nil, fmt.Errorf("iterate receipts for block %d: %w", height, err)
		}
		if !ok || it.BlockNumber() != uint64(height) { //nolint:gosec // G115: height is positive here.
			return records, nil
		}
		stored, err := it.Receipt()
		if err != nil {
			return nil, fmt.Errorf("decode receipt for block %d: %w", height, err)
		}
		records = append(records, receiptRecordFor(it.TxHash(), stored))
	}
}

// receiptRecordsFromBlock answers height's receipts by decoding its block body and fetching each
// transaction's receipt by hash, for a store whose IterateReceipts is unsupported.
func (api *infoAPI) receiptRecordsFromBlock(ctx context.Context, height int64) ([]receiptpkg.ReceiptRecord, error) {
	blockHeight := coretypes.Int64(height)
	block, err := api.backend.Block(ctx, &coretypes.RequestBlockInfo{Height: &blockHeight})
	if err != nil {
		return nil, fmt.Errorf("read block %d to recompute stats: %w", height, err)
	}
	if block == nil || block.Block == nil {
		return nil, fmt.Errorf("block %d body is not available to recompute stats", height)
	}
	records := make([]receiptpkg.ReceiptRecord, 0, len(block.Block.Txs))
	for _, txbz := range block.Block.Txs {
		tx := new(ethtypes.Transaction)
		if err := tx.UnmarshalBinary(txbz); err != nil {
			return nil, fmt.Errorf("decode transaction in block %d: %w", height, err)
		}
		hash := tx.Hash()
		stored, err := api.store.GetReceipt(receiptContext(ctx), hash)
		if err != nil {
			return nil, fmt.Errorf("read receipt %s for block %d: %w", hash, height, err)
		}
		records = append(records, receiptRecordFor(hash, stored))
	}
	return records, nil
}

// receiptRecordFor builds the ReceiptRecord ComputeBlockStats expects from a stored receipt.
func receiptRecordFor(hash common.Hash, stored *evmtypes.Receipt) receiptpkg.ReceiptRecord {
	var reward *big.Int
	if stored.EffectiveGasPrice != 0 {
		// giga's base fee is always zero, so the priority fee is the raw effective gas price.
		reward = new(big.Int).SetUint64(stored.EffectiveGasPrice)
	}
	return receiptpkg.ReceiptRecord{TxHash: hash, Receipt: stored, Reward: reward}
}

// rewardRow formats stats into the reward row eth_feeHistory returns for rewardPercentiles. stats
// must already cover every requested percentile (see blockStatsForHeight).
func rewardRow(stats receiptpkg.BlockStats, rewardPercentiles []float64) []*hexutil.Big {
	row := make([]*hexutil.Big, len(rewardPercentiles))
	for i, p := range rewardPercentiles {
		reward, _ := stats.RewardAt(p)
		row[i] = (*hexutil.Big)(new(big.Int).SetUint64(reward))
	}
	return row
}

// validateRewardPercentiles rejects a percentiles list that is not strictly ascending or leaves
// the [0, 100] range.
func validateRewardPercentiles(percentiles []float64) error {
	if len(percentiles) > 100 {
		return errors.New("rewardPercentiles length must be less than or equal to 100")
	}
	previous := -1.0
	for _, p := range percentiles {
		if p < 0 || p > 100 || p <= previous {
			return errors.New("invalid reward percentiles: must be ascending and between 0 and 100")
		}
		previous = p
	}
	return nil
}
